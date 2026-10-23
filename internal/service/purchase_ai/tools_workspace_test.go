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
	"errors"
	"fmt"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"image"
	"image/color"
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
func sandboxRun(ws *fakeWorkspace) (*toolRun, *fakeWorkspaces) {
	f := &fakeWorkspaces{ws: ws}
	return newToolRun(noBrowser{}, func(o *taskOptions) {
		o.browserOn = false
		o.workspaces = f
	}), f
}

// callTool 按名字调一个工具，和循环里一样
func callTool(t *testing.T, r *toolRun, name, arguments string) (tool.Result, error) {
	t.Helper()
	for _, spec := range r.agent.Tools() {
		if spec.Name == name {
			return spec.Run(context.Background(), arguments)
		}
	}
	require.FailNow(t, "没有工具 "+name)
	return tool.Result{}, nil
}

// ---------- 什么时候有哪些工具 ----------

func TestTools_SandboxToolsOnlyWithAWorkspace(t *testing.T) {
	names := func(r *toolRun) []string {
		var out []string
		for _, d := range r.agent.Defs() {
			out = append(out, d.Name)
		}
		return out
	}

	// Arrange
	without := newToolRun(newFakeBrowser())
	with, _ := sandboxRun(newFakeWorkspace())
	withBoth := newToolRun(newFakeBrowser(), func(o *taskOptions) { o.workspaces = &fakeWorkspaces{ws: newFakeWorkspace()} })

	// Act + Assert：插件没连着，有沙箱也能读写文件，但收割要从页面上收
	assert.NotContains(t, names(without), toolBash)
	assert.Equal(t, []string{toolBash, toolReadFile, toolWriteFile, toolEditFile, toolImport, toolDeliver}, names(with))
	assert.Contains(t, names(withBoth), toolHarvest)
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
	assert.Len(t, r.pics.list, 1)
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
