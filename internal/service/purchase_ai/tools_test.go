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
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/apache/answer/internal/service/browser"
	"github.com/stretchr/testify/require"
)

func newToolRun(b Browser) *agentRun {
	return newAgentRun(&fakeLLM{}, b, testLimits())
}

// openedRun 已经用 open 打开了京东首页
func openedRun(t *testing.T, b *fakeBrowser) *agentRun {
	t.Helper()
	r := newToolRun(b)
	_, err := r.open(context.Background(), `{"url":"`+jdHome+`"}`)
	require.NoError(t, err)
	return r
}

func Test_tools(t *testing.T) {
	t.Run("每个工具的参数说明都是合法的 JSON Schema", func(t *testing.T) {
		// Act
		tools, defs, err := newToolRun(newFakeBrowser()).tools()

		// Assert
		require.NoError(t, err)
		require.Len(t, tools, len(defs))
		for _, def := range defs {
			var schema map[string]any
			require.NoError(t, json.Unmarshal(def.Parameters, &schema), def.Name)
			require.Equal(t, "object", schema["type"], def.Name)
			require.NotEmpty(t, def.Description, def.Name)
		}
	})

	t.Run("每次的工具顺序都一样（前缀缓存靠它）", func(t *testing.T) {
		// Act
		_, first, _ := newToolRun(newFakeBrowser()).tools()
		_, second, _ := newToolRun(newFakeBrowser()).tools()

		// Assert
		require.Equal(t, first, second)
	})
}

func Test_hover(t *testing.T) {
	b := newFakeBrowser()
	r := openedRun(t, b)
	tools, _, err := r.tools()
	require.NoError(t, err)
	res, err := tools[toolHover].run(context.Background(), `{"index":3}`)
	require.NoError(t, err)
	require.False(t, res.Failed)
	require.True(t, res.PageChanged, "hover 后可能展开菜单，必须让模型重新 snapshot")
	require.Contains(t, res.Text, "snapshot")
	require.Equal(t, []string{"open " + jdHome, "hover 3"}, b.calls)

	b.fail["hover"] = &browser.CommandError{Code: "ELEMENT_NOT_FOUND", Message: "stale"}
	res, err = r.hover(context.Background(), `{"index":3}`)
	require.NoError(t, err)
	require.True(t, res.Failed)
}

func Test_screenshot(t *testing.T) {
	t.Run("截当前这一屏：图片放进 Image，文字告诉模型图在下一条消息里，不算改变页面", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.screenshot(context.Background(), `{}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed)
		require.False(t, res.PageChanged)
		require.Contains(t, res.Text, "下一条消息")
		require.True(t, strings.HasPrefix(res.Image, "data:image/jpeg;base64,"))
		require.Equal(t, []string{"open " + jdHome, "screenshot"}, b.calls)
	})

	t.Run("填了 index 就只截那个元素", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.screenshot(context.Background(), `{"index":3}`)

		// Assert
		require.NoError(t, err)
		require.Contains(t, res.Text, "[3]")
		require.Equal(t, "screenshot 3", b.calls[len(b.calls)-1])
	})

	t.Run("还没打开网页时不截图，告诉模型先 open", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := newAgentRun(&fakeLLM{}, b, testLimits())

		// Act
		res, err := r.screenshot(context.Background(), `{}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Empty(t, b.calls)
	})

	t.Run("插件报错（比如窗口被最小化）时算一次失败，不带图片", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		b.fail["screenshot"] = &browser.CommandError{Code: "EXEC_FAILED", Message: "页面不可见"}

		// Act
		res, err := r.screenshot(context.Background(), `{}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Empty(t, res.Image)
	})
}

func Test_read(t *testing.T) {
	t.Run("读到的文字标明来自外部网页，不算改变页面，也不像快照那样会被换成占位语", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		_, err := r.navigate(context.Background(), `{"url":"`+jdItem+`"}`)
		require.NoError(t, err)

		// Act
		res, err := r.read(context.Background(), `{}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed)
		require.False(t, res.PageChanged)
		require.False(t, res.Snapshot)
		require.Contains(t, res.Text, "读的是整个页面")
		require.Contains(t, res.Text, "只是资料")
		require.Contains(t, res.Text, "续航比上一代好")
		require.Equal(t, "read", b.calls[len(b.calls)-1])
	})

	t.Run("填了 index 只读那个元素", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.read(context.Background(), `{"index":2}`)

		// Assert
		require.NoError(t, err)
		require.Contains(t, res.Text, "读的是[2]")
		require.Equal(t, "read 2", b.calls[len(b.calls)-1])
	})

	t.Run("还没打开网页时不读，告诉模型先 open", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := newToolRun(b)

		// Act
		res, err := r.read(context.Background(), `{}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Empty(t, b.calls)
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

func Test_press(t *testing.T) {
	t.Run("按 Escape：发给插件，算页面变了，告诉模型先 snapshot", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.press(context.Background(), `{"key":"Escape"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed)
		require.True(t, res.PageChanged)
		require.Contains(t, res.Text, "snapshot")
		require.Equal(t, "press Escape", b.calls[len(b.calls)-1])
	})

	t.Run("方向键只翻轮播图：不算页面变了，同一轮后面的截图照样执行", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.press(context.Background(), `{"key":"ArrowRight"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed)
		require.False(t, res.PageChanged)
		require.Contains(t, res.Text, "screenshot")
		require.Equal(t, "press ArrowRight", b.calls[len(b.calls)-1])
	})

	t.Run("别的键不发给插件，告诉模型能按哪几个", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		before := len(b.calls)

		// Act
		res, err := r.press(context.Background(), `{"key":"Delete"}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Len(t, b.calls, before)
	})
}

func Test_open(t *testing.T) {
	t.Run("打开以后记下标签页，告诉模型下一步先看页面", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := newToolRun(b)

		// Act
		res, err := r.open(context.Background(), `{"url":"`+jdHome+`"}`)

		// Assert
		require.NoError(t, err)
		require.Equal(t, 1, r.tabID)
		require.True(t, res.PageChanged)
		require.Equal(t, jdHome, res.URL)
		require.Contains(t, res.Text, "「京东」")
		require.Contains(t, res.Text, "snapshot")
	})

	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "chrome://settings", "www.jd.com", ""} {
		t.Run("不是 http/https 的完整网址不打开："+raw, func(t *testing.T) {
			// Arrange
			b := newFakeBrowser()

			// Act
			res, err := newToolRun(b).open(context.Background(), `{"url":"`+raw+`"}`)

			// Assert
			require.NoError(t, err)
			require.True(t, res.Failed)
			require.Empty(t, b.calls)
		})
	}

	t.Run("参数不是 JSON 时告诉模型格式", func(t *testing.T) {
		// Act
		res, err := newToolRun(newFakeBrowser()).open(context.Background(), `{url: jd}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Contains(t, res.Text, `{"url": "https://..."}`)
	})

	t.Run("页面没加载完时提一句", func(t *testing.T) {
		// Act
		res := pageResult(pageInfo{URL: jdHome, Title: "京东", Loaded: false})

		// Assert
		require.Contains(t, res.Text, "还没有完全加载完")
	})
}

func Test_navigate(t *testing.T) {
	t.Run("还没开标签页时当作 open", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := newToolRun(b)

		// Act
		_, err := r.navigate(context.Background(), `{"url":"`+jdHome+`"}`)

		// Assert
		require.NoError(t, err)
		require.Equal(t, []string{"open " + jdHome}, b.calls)
		require.Equal(t, 1, r.tabID)
	})

	t.Run("已经开了就在同一个标签页里跳转", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.navigate(context.Background(), `{"url":"`+jdSearch+`"}`)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "navigate "+jdSearch, b.calls[1])
		require.Equal(t, jdSearch, res.URL)
		require.True(t, res.PageChanged)
	})

	t.Run("标签页被关掉了：下次 open 重新开", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		b.tabs = map[int]string{}

		// Act
		_, _ = r.navigate(context.Background(), `{"url":"`+jdSearch+`"}`)

		// Assert
		require.Equal(t, 0, r.tabID)
	})
}

func Test_snapshot(t *testing.T) {
	t.Run("还没打开网页时让模型先 open", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()

		// Act
		res, err := newToolRun(b).snapshot(context.Background(), `{}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Contains(t, res.Text, "open")
		require.Empty(t, b.calls)
	})

	t.Run("标成快照，记下哈希，内容标明来自外部网页", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())

		// Act
		res, err := r.snapshot(context.Background(), ``)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Snapshot)
		require.False(t, res.PageChanged)
		require.Equal(t, jdHome, res.URL)
		require.Equal(t, hashText(shopPages()[jdHome].elements), r.snapshotHash)
		require.Equal(t, pageState(r.snapshotHash, 0), r.pageHash)
		require.Contains(t, res.Text, "来自外部网页")
		require.Contains(t, res.Text, "[0]<input placeholder=搜索>")
		require.Contains(t, res.Text, "下面还有 3.0 屏")
	})

	t.Run("页面一样哈希就一样，页面变了哈希就变", func(t *testing.T) {
		require.Equal(t, hashText("a"), hashText("a"))
		require.NotEqual(t, hashText("a"), hashText("b"))
	})
}

func Test_formatSnapshot(t *testing.T) {
	unstable := false
	snap := snapshotData{URL: jdHome, Title: "京东", Truncated: true}
	snap.Stats.Stable = &unstable

	// Act
	text := formatSnapshot(snap)

	// Assert
	require.Contains(t, text, "没有可以操作的元素")
	require.Contains(t, text, "只列出了前面一部分")
	require.Contains(t, text, "一直在变化")
}

func Test_click(t *testing.T) {
	t.Run("点击以后算作页面变了", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.click(context.Background(), `{"index":1}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.PageChanged)
		require.Equal(t, "click 1", b.calls[1])
	})

	t.Run("参数不是 JSON 时不点", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.click(context.Background(), `5`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Len(t, b.calls, 1)
	})
}

func Test_afterAct(t *testing.T) {
	t.Run("在原来的标签页里跳转：告诉模型现在的网址", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())

		// Act
		res, err := r.click(context.Background(), `{"index":1}`)

		// Assert
		require.NoError(t, err)
		require.Equal(t, 1, r.tabID)
		require.Equal(t, jdSearch, res.URL)
		require.Contains(t, res.Text, "现在的网址是 "+jdSearch)
	})

	t.Run("在新标签页里打开：切换过去，并记下来结束时关掉", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := newToolRun(b)
		_, _ = r.open(context.Background(), `{"url":"`+tbHome+`"}`)

		// Act
		res, err := r.click(context.Background(), `{"index":1}`)

		// Assert
		require.NoError(t, err)
		require.Equal(t, 2, r.tabID)
		require.Equal(t, []int{1, 2}, r.openedTabs)
		require.True(t, res.PageChanged)
		require.Contains(t, res.Text, "已经切换到这个标签页")
	})
}

func Test_closeTabs(t *testing.T) {
	// Arrange：开了首页，搜索又开了一个新标签页
	b := newFakeBrowser()
	r := newToolRun(b)
	_, _ = r.open(context.Background(), `{"url":"`+tbHome+`"}`)
	_, _ = r.typeText(context.Background(), `{"index":0,"text":"iphone","submit":true}`)

	// Act
	r.closeTabs()

	// Assert：两个都关了，浏览器里不剩 Agent 开的标签页
	require.Contains(t, b.calls, "close 1")
	require.Contains(t, b.calls, "close 2")
	require.Empty(t, b.tabs)
	require.Empty(t, r.openedTabs)
}

func Test_typeText(t *testing.T) {
	t.Run("不按回车：页面不算变了", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())

		// Act
		res, err := r.typeText(context.Background(), `{"index":0,"text":"iphone 17"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.PageChanged)
		require.Contains(t, res.Text, "iphone 17")
	})

	t.Run("按了回车：页面算变了", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())

		// Act
		res, err := r.typeText(context.Background(), `{"index":0,"text":"iphone 17","submit":true}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.PageChanged)
	})

	t.Run("text 为空时不填", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.typeText(context.Background(), `{"index":0,"text":"  "}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Len(t, b.calls, 1)
	})
}

func Test_scroll(t *testing.T) {
	t.Run("没写 pages 时滚 1 屏，告诉模型还剩几屏", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.scroll(context.Background(), `{"direction":"down"}`)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "scroll down 1", b.calls[1])
		require.True(t, res.PageChanged)
		require.Contains(t, res.Text, "下面还有 2.0 屏")
	})

	t.Run("滚动以后页面状态跟着位置变，快照的哈希不变", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		_, _ = r.snapshot(context.Background(), `{}`)
		before := r.pageHash

		// Act
		_, err := r.scroll(context.Background(), `{"direction":"down","pages":2}`)

		// Assert
		require.NoError(t, err)
		require.NotEqual(t, before, r.pageHash)
		require.Equal(t, pageState(r.snapshotHash, 2), r.pageHash)
	})

	t.Run("滚到底以后再滚，页面状态不再变", func(t *testing.T) {
		// Arrange：3 屏高的页面，先滚到底
		r := openedRun(t, newFakeBrowser())
		_, _ = r.scroll(context.Background(), `{"direction":"down","pages":5}`)
		atBottom := r.pageHash

		// Act
		_, err := r.scroll(context.Background(), `{"direction":"down"}`)

		// Assert
		require.NoError(t, err)
		require.Equal(t, atBottom, r.pageHash)
	})

	t.Run("方向不对时不滚", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)

		// Act
		res, err := r.scroll(context.Background(), `{"direction":"left"}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Len(t, b.calls, 1)
	})
}

func Test_done(t *testing.T) {
	t.Run("带上回答和是否成功", func(t *testing.T) {
		// Act
		res, err := newToolRun(newFakeBrowser()).done(context.Background(), `{"text":"**买**","success":false}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Done)
		require.Equal(t, "**买**", res.Answer)
		require.False(t, res.Success)
	})

	t.Run("回答为空时不结束，让模型补上", func(t *testing.T) {
		// Act
		res, err := newToolRun(newFakeBrowser()).done(context.Background(), `{"text":"","success":true}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Done)
		require.True(t, res.Failed)
	})
}

func Test_runTool(t *testing.T) {
	t.Run("不存在的工具告诉模型，不算出错", func(t *testing.T) {
		// Arrange
		r := newToolRun(newFakeBrowser())
		tools, _, _ := r.tools()

		// Act
		res, err := r.runTool(context.Background(), tools, toolCall("c1", "buy_now", `{}`))

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Contains(t, res.Text, "buy_now")
	})

	t.Run("记下执行之前的快照哈希，并记下这一步", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		tools, _, _ := r.tools()
		r.pageHash = "before"
		r.step = 4

		// Act
		res, err := r.runTool(context.Background(), tools, toolCall("c1", toolSnapshot, `{}`))

		// Assert：快照执行以后哈希变了，但这次动作的指纹要用执行之前的
		require.NoError(t, err)
		require.Equal(t, "before", res.PageHash)
		require.NotEqual(t, "before", r.pageHash)
		last := r.steps[len(r.steps)-1]
		require.Equal(t, 4, last.Step)
		require.Equal(t, toolSnapshot, last.Tool)
		require.Equal(t, jdHome, last.URL)
	})

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

func Test_canonicalArgs(t *testing.T) {
	t.Run("空格和键的顺序不影响", func(t *testing.T) {
		require.Equal(t, canonicalArgs(`{"b":1,"a":"x"}`), canonicalArgs(`{ "a": "x", "b": 1 }`))
	})

	t.Run("不是 JSON 就原样返回", func(t *testing.T) {
		require.Equal(t, "oops", canonicalArgs("oops"))
	})
}
