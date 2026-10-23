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

package browsertool

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/apache/answer/internal/service/browser"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_shotForModel(t *testing.T) {
	jpegShot := func(t *testing.T, w, h int) tool.Image {
		t.Helper()
		var buf bytes.Buffer
		require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), &jpeg.Options{Quality: shotKeepQuality}))
		return tool.Image{Data: base64.StdEncoding.EncodeToString(buf.Bytes()), Mime: "image/jpeg", Width: w, Height: h}
	}

	t.Run("整屏截图按屏幕原样截，发给模型的缩到 1600 宽", func(t *testing.T) {
		// Arrange
		shot := jpegShot(t, 2560, 888)

		// Act
		uri, w, h := shotForModel(shot)

		// Assert：模型那份变小了，原图（存给用户看的）不动
		assert.Equal(t, screenshotMaxWidth, w)
		assert.Equal(t, 555, h)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:image/jpeg;base64,"))
		require.NoError(t, err)
		img, err := jpeg.Decode(bytes.NewReader(raw))
		require.NoError(t, err)
		assert.Equal(t, screenshotMaxWidth, img.Bounds().Dx())
		assert.Equal(t, 2560, shot.Width)
	})

	t.Run("只截一个元素的小图原样发", func(t *testing.T) {
		// Arrange
		shot := jpegShot(t, 496, 504)

		// Act
		uri, w, h := shotForModel(shot)

		// Assert
		assert.Equal(t, "data:image/jpeg;base64,"+shot.Data, uri)
		assert.Equal(t, 496, w)
		assert.Equal(t, 504, h)
	})

	t.Run("解不开的图原样发", func(t *testing.T) {
		// Arrange
		shot := tool.Image{Data: base64.StdEncoding.EncodeToString([]byte("shot1")), Mime: "image/jpeg", Width: 1024, Height: 576}

		// Act
		uri, w, _ := shotForModel(shot)

		// Assert
		assert.Equal(t, "data:image/jpeg;base64,"+shot.Data, uri)
		assert.Equal(t, 1024, w)
	})
}

func Test_formatRead(t *testing.T) {
	t.Run("截断了就说一共多少字、读了多少", func(t *testing.T) {
		// Arrange
		data := readData{Title: "笔记", URL: "xiaohongshu.com/explore/1", Scope: "overlay", Text: "正文", Truncated: true, TotalChars: 5000}

		// Act
		got := formatRead(data, nil)

		// Assert
		require.Contains(t, got, "读的是页面上的浮层")
		require.Contains(t, got, "一共 5000 字，只读了前 3000 字")
	})

	t.Run("有大图时提醒截图：图里的字读不到", func(t *testing.T) {
		// Act
		got := formatRead(readData{Scope: "overlay", Text: "#富冈义勇", Images: 1}, nil)

		// Assert
		require.Contains(t, got, "这里有大图")
		require.Contains(t, got, "screenshot")
	})

	t.Run("没读到文字时建议截图", func(t *testing.T) {
		// Act
		got := formatRead(readData{Scope: "page", Text: "  "}, nil)

		// Assert
		require.Contains(t, got, "screenshot")
	})
}

func Test_formatSnapshot(t *testing.T) {
	unstable := false
	snap := snapshotData{URL: "https://www.jd.com", Title: "京东", Truncated: true}
	snap.Stats.Stable = &unstable

	// Act
	text := formatSnapshot(snap)

	// Assert
	require.Contains(t, text, "没有可以操作的元素")
	require.Contains(t, text, "只列出了前面一部分")
	require.Contains(t, text, "一直在变化")
	require.NotContains(t, text, "浮层")
}

func Test_formatSnapshot_浮层(t *testing.T) {
	// Arrange：插件说页面中间开着一个浮层
	snap := snapshotData{URL: "https://www.jd.com", Title: "京东", Elements: "[0]<div>关闭弹窗"}
	snap.Stats.Overlay = true

	// Act
	text := formatSnapshot(snap)

	// Assert：告诉模型先关掉它，怎么关，以及验证码不要关
	require.Contains(t, text, "开着一个浮层")
	require.Contains(t, text, "「关闭弹窗」")
	require.Contains(t, text, "验证码")
}

func Test_commandFailure(t *testing.T) {
	for code, want := range map[string]string{
		"ELEMENT_NOT_FOUND":   "先 snapshot",
		"NOT_EDITABLE":        "不是输入框",
		"TAB_NOT_FOUND":       "open",
		"PAGE_NOT_SCRIPTABLE": "换一个网址",
		"EXEC_FAILED":         "插件执行失败：出错了",
	} {
		t.Run(code, func(t *testing.T) {
			// Act
			res := commandFailure(&browser.CommandError{Code: code, Message: "出错了"})

			// Assert
			require.True(t, res.Failed)
			require.Contains(t, res.Text, want)
		})
	}
}

func Test_afterAct_newTabClosed(t *testing.T) {
	// Arrange：新标签页还没加载完就自己关掉了（文件下载）
	s := &TaskState{TabID: 1}

	// Act
	res := s.afterAct("已点击 [19]。", actResult{URL: "https://metaso.cn/chat/1", NewTabClosed: true})

	// Assert：留在原来的标签页，让模型换一个来源
	require.Equal(t, 1, s.TabID)
	require.False(t, res.Failed)
	require.False(t, res.PageChanged, "页面没变，编号还能用")
	require.Contains(t, res.Text, "文件下载")
	require.Contains(t, res.Text, "不要再点它")
}

func Test_browserFailure(t *testing.T) {
	alive := context.Background()

	t.Run("整次任务的 ctx 结束了：原样返回 ctx 的错误，插件报什么都不管", func(t *testing.T) {
		// Arrange
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Act
		_, err := browserFailure(ctx, browser.ErrDisconnected)

		// Assert
		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, tool.ErrBrowserGone)
	})

	for _, tt := range []struct {
		name string
		err  error
	}{
		{"插件没连上：走收尾", browser.ErrNotConnected},
		{"插件断开了：走收尾", browser.ErrDisconnected},
		{"包了一层的断开也认得出来", fmt.Errorf("call snapshot: %w", browser.ErrDisconnected)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, err := browserFailure(alive, tt.err)

			// Assert
			require.ErrorIs(t, err, tool.ErrBrowserGone)
		})
	}

	t.Run("插件的错误码：模型能处理，交给 commandFailure", func(t *testing.T) {
		// Act
		res, err := browserFailure(alive, &browser.CommandError{Code: "NOT_EDITABLE", Message: "x"})

		// Assert
		require.NoError(t, err)
		require.Equal(t, commandFailure(&browser.CommandError{Code: "NOT_EDITABLE"}), res)
	})

	t.Run("单次调用超时，整次任务还活着：模型能处理", func(t *testing.T) {
		// Act
		res, err := browserFailure(alive, context.DeadlineExceeded)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Equal(t, timeoutText, res.Text)
	})

	t.Run("别的错误：模型能处理，并告诉它原因", func(t *testing.T) {
		// Act
		res, err := browserFailure(alive, errors.New("write: broken pipe"))

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Contains(t, res.Text, "插件执行失败")
		require.Contains(t, res.Text, "broken pipe")
	})
}

// ⑦ 概念 3

func Test_pageResult(t *testing.T) {
	// Act：页面没加载完
	res := pageResult(pageInfo{URL: "https://www.jd.com", Title: "京东", Loaded: false})

	// Assert：提一句
	require.Contains(t, res.Text, "还没有完全加载完")
}

func Test_hashText(t *testing.T) {
	// 页面一样哈希就一样，页面变了哈希就变
	require.Equal(t, hashText("a"), hashText("a"))
	require.NotEqual(t, hashText("a"), hashText("b"))
}
