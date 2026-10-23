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

package sandboxtool

import (
	"bytes"
	"context"
	"encoding/base64"
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

func TestBashResult_LongOutputKeepsTheEnd(t *testing.T) {
	// Arrange：一大段输出，最后是 traceback
	noise := strings.Repeat("downloading 一张图……\n", 2000)
	run := sandbox.RunResult{ExitCode: 1, Stderr: noise + "ValueError: images do not match"}

	// Act
	res, err := (&Bench{}).bashResult(context.Background(), run, nil, time.Minute)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, res.Text, "ValueError: images do not match", "报错在最后，不能被截掉")
	assert.Less(t, len([]rune(res.Text)), 2*bashOutputMax+500)
}

func TestBashResult_CanceledTaskStopsTheLoop(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	_, err := (&Bench{}).bashResult(ctx, sandbox.RunResult{}, context.Canceled, time.Minute)

	// Assert
	assert.ErrorIs(t, err, context.Canceled)
}
