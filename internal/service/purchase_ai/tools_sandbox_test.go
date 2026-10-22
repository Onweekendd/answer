/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package purchaseai

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWorkspace 是测试用的工作台：文件在 map 里，Run 回 run 给的结果
type fakeWorkspace struct {
	files map[string][]byte
	cmds  []string
	run   func(cmd string) (sandbox.RunResult, error)
}

func newFakeWorkspace() *fakeWorkspace {
	return &fakeWorkspace{files: map[string][]byte{}}
}

func (w *fakeWorkspace) Upload(_ context.Context, p string, data []byte) error {
	w.files[p] = append([]byte(nil), data...)
	return nil
}

func (w *fakeWorkspace) Download(_ context.Context, p string) ([]byte, error) {
	data, ok := w.files[p]
	if !ok {
		return nil, fmt.Errorf("no such file: %s", p)
	}
	return data, nil
}

func (w *fakeWorkspace) Run(_ context.Context, cmd string, _ time.Duration) (sandbox.RunResult, error) {
	w.cmds = append(w.cmds, cmd)
	if w.run != nil {
		return w.run(cmd)
	}
	return sandbox.RunResult{}, nil
}

// fakeWorkspaces 每次 Acquire 都交出同一个工作台，记下被要了几次
type fakeWorkspaces struct {
	ws       *fakeWorkspace
	reset    bool
	err      error
	acquired int
}

func (f *fakeWorkspaces) Acquire(context.Context, int64, string) (Workspace, bool, error) {
	f.acquired++
	if f.err != nil {
		return nil, false, f.err
	}
	return f.ws, f.reset, nil
}

// sandboxRun 是一个有工作台、没有插件的任务
func sandboxRun(ws *fakeWorkspace) (*agentRun, *fakeWorkspaces) {
	r := newAgentRun(nil, noBrowser{}, testLimits())
	f := &fakeWorkspaces{ws: ws}
	r.workspaces = f
	return r, f
}

// call 按名字调一个工具，和循环里一样
func callTool(t *testing.T, r *agentRun, name, arguments string) (toolResult, error) {
	t.Helper()
	tools, _, err := r.tools(false)
	require.NoError(t, err)
	tool, ok := tools[name]
	require.True(t, ok, "没有工具 %s", name)
	return tool.run(context.Background(), arguments)
}

// ---------- 什么时候有哪些工具 ----------

func TestTools_SandboxToolsOnlyWithAWorkspace(t *testing.T) {
	names := func(r *agentRun, browserOn bool) []string {
		_, defs, err := r.tools(browserOn)
		require.NoError(t, err)
		out := make([]string, len(defs))
		for i, d := range defs {
			out[i] = d.Name
		}
		return out
	}

	// Arrange
	without := newAgentRun(nil, noBrowser{}, testLimits())
	with, _ := sandboxRun(newFakeWorkspace())

	// Act + Assert：插件没连着，有沙箱也能读写文件，但收割要从页面上收
	assert.NotContains(t, names(without, true), toolBash)
	assert.Equal(t, []string{toolBash, toolReadFile, toolWriteFile, toolEditFile, toolDeliver}, names(with, false))
	assert.Contains(t, names(with, true), toolHarvest)
}

// ---------- 懒创建 ----------

func TestWorkspace_AcquiredOnceOnFirstUse(t *testing.T) {
	// Arrange
	r, f := sandboxRun(newFakeWorkspace())
	assert.Equal(t, 0, f.acquired, "还没用沙箱就不要")

	// Act
	_, err1 := callTool(t, r, toolWriteFile, `{"path": "a.py", "content": "print(1)"}`)
	_, err2 := callTool(t, r, toolWriteFile, `{"path": "b.py", "content": "print(2)"}`)

	// Assert
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, 1, f.acquired)
}

func TestWorkspace_ResetIsToldOnce(t *testing.T) {
	// Arrange：原来的沙箱没了，这是新建的
	r, f := sandboxRun(newFakeWorkspace())
	f.reset = true

	// Act
	first, _ := callTool(t, r, toolWriteFile, `{"path": "a.py", "content": "x"}`)
	second, _ := callTool(t, r, toolWriteFile, `{"path": "a.py", "content": "y"}`)

	// Assert
	assert.True(t, strings.HasPrefix(first.Text, workspaceResetText))
	assert.NotContains(t, second.Text, workspaceResetText)
}

func TestWorkspace_UnavailableIsAFailureNotAnError(t *testing.T) {
	// Arrange
	r, f := sandboxRun(nil)
	f.err = errors.New("connection refused")

	// Act
	res, err := callTool(t, r, toolBash, `{"command": "ls"}`)

	// Assert：任务不停，模型直接回答
	require.NoError(t, err)
	assert.True(t, res.Failed)
	assert.Equal(t, workspaceDownText, res.Text)
}

// ---------- write_file / edit_file ----------

func TestWriteFile_RelativePathIsUnderHome(t *testing.T) {
	// Arrange
	ws := newFakeWorkspace()
	r, _ := sandboxRun(ws)

	// Act
	res, err := callTool(t, r, toolWriteFile, `{"path": "out/collage.py", "content": "print('hi')"}`)

	// Assert
	require.NoError(t, err)
	assert.False(t, res.Failed)
	assert.Equal(t, "print('hi')", string(ws.files["/home/user/out/collage.py"]))
}

func TestEditFile(t *testing.T) {
	const script = "size = 100\nprint(size)\nsize = 100\n"

	tests := []struct {
		name      string
		args      string
		wantFail  string // 失败时 Text 里要有的话；空表示成功
		wantAfter string
	}{
		{"只出现一次就替换", `{"path": "a.py", "old_text": "print(size)", "new_text": "print(size * 2)"}`, "",
			"size = 100\nprint(size * 2)\nsize = 100\n"},
		{"找不到就让它先读", `{"path": "a.py", "old_text": "size=100", "new_text": "size=200"}`, "read_file", script},
		{"出现两次就让它多带上下文", `{"path": "a.py", "old_text": "size = 100", "new_text": "size = 200"}`, "出现了 2 次", script},
		{"replace_all 全部替换", `{"path": "a.py", "old_text": "size = 100", "new_text": "size = 200", "replace_all": true}`, "",
			"size = 200\nprint(size)\nsize = 200\n"},
		{"文件不存在", `{"path": "nope.py", "old_text": "a", "new_text": "b"}`, "write_file", script},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			ws := newFakeWorkspace()
			ws.files["/home/user/a.py"] = []byte(script)
			r, _ := sandboxRun(ws)

			// Act
			res, err := callTool(t, r, toolEditFile, tt.args)

			// Assert
			require.NoError(t, err)
			if tt.wantFail != "" {
				assert.True(t, res.Failed)
				assert.Contains(t, res.Text, tt.wantFail)
			} else {
				assert.False(t, res.Failed, res.Text)
			}
			assert.Equal(t, tt.wantAfter, string(ws.files["/home/user/a.py"]))
		})
	}
}

// ---------- 小工具 ----------

func TestNumberedLines(t *testing.T) {
	// Arrange
	text := "a\nb\nc\nd\n"

	// Act
	lines, from, to, total := numberedLines(text, 2, 2)

	// Assert
	assert.Equal(t, "     2\tb\n     3\tc\n", lines)
	assert.Equal(t, []int{2, 3, 4}, []int{from, to, total})
}

func TestNumberedLines_OffsetPastTheEnd(t *testing.T) {
	lines, from, to, total := numberedLines("a\nb", 10, 0)
	assert.Empty(t, lines)
	assert.Greater(t, from, to)
	assert.Equal(t, 2, total)
}

// pngOf 画一张 w×h 的 png，左右两半颜色不同（太单调的图会被当成空白）
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			c := color.RGBA{R: 200, A: 255}
			if x > w/2 {
				c = color.RGBA{B: 200, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestImageShot_WideImageIsScaledDown(t *testing.T) {
	// Act
	shot, err := imageShot(pngOf(t, 3200, 400))

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", shot.Mime)
	assert.Equal(t, imageMaxWidth, shot.Width)
	assert.Equal(t, 200, shot.Height)
	raw, err := base64.StdEncoding.DecodeString(shot.Data)
	require.NoError(t, err)
	_, err = jpeg.Decode(bytes.NewReader(raw))
	assert.NoError(t, err)
}

func TestContactSheet_TenColumnsAndAGreyCellForMissingThumbs(t *testing.T) {
	// Arrange：12 张，第 3 张没有缩略图
	entries := make([]sandbox.Entry, 12)
	for i := range entries {
		entries[i] = sandbox.Entry{N: i + 1, Thumb: pngOf(t, 200, 300)}
	}
	entries[2].Thumb = nil

	// Act
	sheet, err := contactSheet(entries)

	// Assert：10 列 2 行，宽不超过模型看图的上限
	require.NoError(t, err)
	assert.Equal(t, 10*sheetCell+11*sheetGap, sheet.Width)
	assert.Equal(t, 2*sheetCell+3*sheetGap, sheet.Height)
	assert.LessOrEqual(t, sheet.Width, imageMaxWidth)
}

// ---------- bash ----------

func Test_clipMiddle(t *testing.T) {
	t.Run("不超过的原样返回", func(t *testing.T) {
		assert.Equal(t, "短", clipMiddle("短", 8))
	})

	t.Run("按字截，开头留四分之一，结尾留其余，中间写省略了多少", func(t *testing.T) {
		// Arrange：20 个汉字，最多留 8 个
		s := "一二三四五六七八九十甲乙丙丁戊己庚辛壬癸"

		// Act
		got := clipMiddle(s, 8)

		// Assert
		assert.Equal(t, "一二\n…（中间省略了 12 个字）…\n戊己庚辛壬癸", got)
	})
}

func TestBash_NonZeroExitIsNotAFailure(t *testing.T) {
	// Arrange
	ws := newFakeWorkspace()
	ws.run = func(string) (sandbox.RunResult, error) {
		return sandbox.RunResult{ExitCode: 127, Stderr: "bash: line 1: magick: command not found"}, nil
	}
	r, _ := sandboxRun(ws)

	// Act
	res, err := callTool(t, r, toolBash, `{"command": "magick a.png b.jpg"}`)

	// Assert：退出码是信息，模型看了自己换个办法
	require.NoError(t, err)
	assert.False(t, res.Failed)
	assert.Contains(t, res.Text, "127")
	assert.Contains(t, res.Text, "command not found")
}

func TestBash_LongOutputKeepsTheEnd(t *testing.T) {
	// Arrange：一大段输出，最后是 traceback
	ws := newFakeWorkspace()
	noise := strings.Repeat("downloading 一张图……\n", 2000)
	ws.run = func(string) (sandbox.RunResult, error) {
		return sandbox.RunResult{ExitCode: 1, Stderr: noise + "ValueError: images do not match"}, nil
	}
	r, _ := sandboxRun(ws)

	// Act
	res, _ := callTool(t, r, toolBash, `{"command": "python3 collage.py"}`)

	// Assert
	assert.Contains(t, res.Text, "ValueError: images do not match", "报错在最后，不能被截掉")
	assert.Less(t, len([]rune(res.Text)), 2*bashOutputMax+500)
}

func TestBash_TimedOutTellsHowLong(t *testing.T) {
	// Arrange
	ws := newFakeWorkspace()
	ws.run = func(string) (sandbox.RunResult, error) { return sandbox.RunResult{TimedOut: true}, nil }
	r, _ := sandboxRun(ws)

	// Act
	res, err := callTool(t, r, toolBash, `{"command": "python3 big.py", "timeout": 30}`)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, res.Text, "30")
}

func TestBash_TransportErrorDropsTheWorkspace(t *testing.T) {
	// Arrange：沙箱连不上了
	ws := newFakeWorkspace()
	ws.run = func(string) (sandbox.RunResult, error) { return sandbox.RunResult{}, errors.New("502 Bad Gateway") }
	r, f := sandboxRun(ws)

	// Act
	res, err := callTool(t, r, toolBash, `{"command": "ls"}`)
	_, _ = callTool(t, r, toolBash, `{"command": "ls"}`)

	// Assert：这次失败，下一次重新要沙箱
	require.NoError(t, err)
	assert.True(t, res.Failed)
	assert.Equal(t, 2, f.acquired)
}

func TestBash_CanceledTaskStopsTheLoop(t *testing.T) {
	// Arrange
	r, _ := sandboxRun(newFakeWorkspace())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	_, err := r.bashResult(ctx, sandbox.RunResult{}, context.Canceled, time.Minute)

	// Assert
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------- read_file ----------

func TestReadFile_ImageIsShownAndNumbered(t *testing.T) {
	// Arrange：扩展名是 .jpg，内容其实是 png，按内容认
	ws := newFakeWorkspace()
	ws.files["/home/user/out/sheet.jpg"] = pngOf(t, 400, 300)
	r, _ := sandboxRun(ws)

	// Act
	res, err := callTool(t, r, toolReadFile, `{"path": "out/sheet.jpg"}`)

	// Assert
	require.NoError(t, err)
	assert.False(t, res.Failed)
	assert.True(t, strings.HasPrefix(res.Image, "data:image/jpeg;base64,"), "模型要看得见")
	assert.Contains(t, res.Text, "图1")
	assert.Len(t, r.pictures, 1)
}

func TestReadFile_TextHasLineNumbers(t *testing.T) {
	// Arrange
	ws := newFakeWorkspace()
	ws.files["/home/user/harvest/items.json"] = []byte("[\n  {\"n\": 1}\n]\n")
	r, _ := sandboxRun(ws)

	// Act
	res, err := callTool(t, r, toolReadFile, `{"path": "harvest/items.json"}`)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, res.Text, "     2\t  {\"n\": 1}")
	assert.Empty(t, res.Image)
}

func Test_textFileResult(t *testing.T) {
	tenLines := strings.Repeat("一行\n", 10)

	t.Run("翻页：说清楚读到第几行、从哪接着读", func(t *testing.T) {
		// Act
		res := textFileResult("/home/user/a.txt", tenLines, 3, 4)

		// Assert
		assert.False(t, res.Failed)
		assert.Contains(t, res.Text, "一共 10 行，这是第 3 到 6 行，还有 4 行没读，用 offset=7 接着读")
		assert.Contains(t, res.Text, "     6\t一行")
		assert.NotContains(t, res.Text, "     7\t")
	})

	t.Run("读到最后不提还有没读的", func(t *testing.T) {
		res := textFileResult("/home/user/a.txt", tenLines, 0, 0)
		assert.Contains(t, res.Text, "这是第 1 到 10 行。")
	})

	t.Run("offset 超出了算失败，告诉模型一共几行", func(t *testing.T) {
		res := textFileResult("/home/user/a.txt", tenLines, 11, 0)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "一共只有 10 行")
	})

	t.Run("空文件", func(t *testing.T) {
		res := textFileResult("/home/user/a.txt", "", 0, 0)
		assert.False(t, res.Failed)
		assert.Contains(t, res.Text, "空文件")
	})

	t.Run("一行很长的文件只给开头", func(t *testing.T) {
		// Arrange：压缩过的 JSON，只有一行
		long := strings.Repeat("字", 3*readFileMaxRunes)

		// Act
		res := textFileResult("/home/user/min.json", long, 0, 0)

		// Assert
		assert.Contains(t, res.Text, "太长")
		assert.Less(t, len([]rune(res.Text)), readFileMaxRunes+300)
	})
}

func TestReadFile_BinaryOnlyTellsTheSize(t *testing.T) {
	// Arrange：zip 的魔数 PK\x03\x04
	ws := newFakeWorkspace()
	ws.files["/home/user/a.zip"] = append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0}, 996)...)
	r, _ := sandboxRun(ws)

	// Act
	res, _ := callTool(t, r, toolReadFile, `{"path": "a.zip"}`)

	// Assert
	assert.Contains(t, res.Text, "1000")
	assert.NotContains(t, res.Text, "PK\x03\x04")
	assert.Empty(t, res.Image)
}
