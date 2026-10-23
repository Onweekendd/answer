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
	"encoding/base64"
	"fmt"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/links"
	"github.com/apache/answer/internal/service/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeArtifactStore 记下存过的文件，按顺序给假网址
type fakeArtifactStore struct {
	saved []savedArtifact
}

type savedArtifact struct {
	ext  string
	data []byte
}

func (f *fakeArtifactStore) SaveAgentFile(_ context.Context, ext string, data []byte) (string, error) {
	f.saved = append(f.saved, savedArtifact{ext, data})
	return fmt.Sprintf("https://buy.example.com/uploads/agent-files/%d%s", len(f.saved), ext), nil
}

// pngBytes 开头是 PNG 的魔数，http.DetectContentType 认得它是 image/png
var pngBytes = []byte("\x89PNG\r\n\x1a\n0000IHDR")

// deliverRun 是一个有工作台、能存产物的任务。shot 命令照着样子在 /tmp/deliver/ 下写一张图
func deliverRun() (*toolRun, *fakeWorkspace, *fakeArtifactStore, chan Event) {
	ws := newFakeWorkspace()
	ws.run = func(cmd string) (sandbox.RunResult, error) {
		if strings.Contains(cmd, "shot ") {
			out := cmd[strings.LastIndex(cmd, " ")+1:]
			ws.files[out] = pngBytes
		}
		return sandbox.RunResult{}, nil
	}
	r, _ := sandboxRun(ws)
	store := &fakeArtifactStore{}
	r.arts.store = store
	events := make(chan Event, 4)
	r.pres.events = events
	return r, ws, store, events
}

func Test_deliver(t *testing.T) {
	t.Run("交付网页：工作台里的图内嵌进去，截一张图，两样都存下，卡片推给页面", func(t *testing.T) {
		// Arrange
		r, ws, store, events := deliverRun()
		ws.files["/home/user/harvest/001.jpg"] = pngBytes
		ws.files["/home/user/trip.html"] = []byte(`<img src="harvest/001.jpg"><img src="https://cdn.test/a.png">`)

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"trip.html","title":"成都 4 天行程"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		require.Len(t, store.saved, 2)
		page := string(store.saved[0].data)
		assert.Equal(t, ".html", store.saved[0].ext)
		assert.Contains(t, page, `<img src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(pngBytes)+`">`)
		assert.Contains(t, page, `<img src="https://cdn.test/a.png">`, "外网的不动")
		assert.Equal(t, ".png", store.saved[1].ext)
		assert.Contains(t, ws.cmds[0], "shot '/home/user/trip.html' /tmp/deliver/1.png")

		want := Artifact{ID: 1, Title: "成都 4 天行程", URL: "https://buy.example.com/uploads/agent-files/1.html",
			Image: "https://buy.example.com/uploads/agent-files/2.png", Size: len(page)}
		assert.Equal(t, &want, res.Extras[extraArtifact])
		assert.Equal(t, "交付了「成都 4 天行程」", res.Display)
		assert.Contains(t, res.Text, "文件1")
		assert.Contains(t, res.Text, "外网的资源", "告诉模型外网的图加载不出来")
		ev := <-events
		assert.Equal(t, EventArtifact, ev.Type)
		assert.Equal(t, want, *ev.Artifact)
	})

	t.Run("再交付同一个网页就是更新：编号不变，告诉模型是更新", func(t *testing.T) {
		// Arrange
		r, ws, _, _ := deliverRun()
		ws.files["/home/user/trip.html"] = []byte("<p>v1</p>")
		_, _ = callTool(t, r, toolDeliver, `{"path":"trip.html","title":"行程"}`)
		ws.files["/home/user/trip.html"] = []byte("<p>v2，改了第三天</p>")

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"/home/user/trip.html","title":"行程 v2"}`)

		// Assert
		require.NoError(t, err)
		require.Len(t, r.arts.list, 1)
		assert.Equal(t, 1, res.Extras[extraArtifact].(*Artifact).ID)
		assert.Equal(t, "行程 v2", r.arts.list[0].Title)
		assert.Equal(t, "更新了「行程 v2」", res.Display)
	})

	t.Run("不是网页不收，也不碰工作台", func(t *testing.T) {
		// Arrange
		r, ws, store, _ := deliverRun()

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"out/card.png","title":"行程卡"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "只能交付网页")
		assert.Empty(t, store.saved)
		assert.Empty(t, ws.cmds)
	})

	t.Run("文件不存在：失败，让模型去看看", func(t *testing.T) {
		// Arrange
		r, _, store, _ := deliverRun()

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"nope.html","title":"行程"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "读不到 /home/user/nope.html")
		assert.Empty(t, store.saved)
	})

	t.Run("截图没截成照样交付：只是卡片上没有缩略图", func(t *testing.T) {
		// Arrange
		r, ws, store, _ := deliverRun()
		ws.files["/home/user/trip.html"] = []byte("<p>行程</p>")
		ws.run = func(string) (sandbox.RunResult, error) {
			return sandbox.RunResult{ExitCode: 127, Stderr: "shot: command not found"}, nil
		}

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"trip.html","title":"行程"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Empty(t, res.Extras[extraArtifact].(*Artifact).Image)
		assert.Len(t, store.saved, 1, "只存了网页")
		assert.Contains(t, res.Text, "截图没截成")
	})

	t.Run("没有地方存：失败，让模型在回答里说文件在哪", func(t *testing.T) {
		// Arrange
		r, ws, _, _ := deliverRun()
		r.arts.store = nil
		ws.files["/home/user/trip.html"] = []byte("<p>行程</p>")

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"trip.html","title":"行程"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "存不了")
	})

	t.Run("用户关了页面：返回 error，循环停下", func(t *testing.T) {
		// Arrange
		r, ws, _, _ := deliverRun()
		ws.files["/home/user/trip.html"] = []byte("<p>行程</p>")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Act
		_, err := r.arts.deliver(ctx, ws, `{"path":"nope.html","title":"行程"}`)

		// Assert
		require.ErrorIs(t, err, context.Canceled)
	})
}

func Test_deliver_links(t *testing.T) {
	t.Run("链接洗掉追踪参数、在新标签页打开；没打开过的、没链上的商品都告诉模型", func(t *testing.T) {
		// Arrange：打开过一篇笔记（xsec_token 和网页里的不一样也算同一篇），记过两个商品，一个链上了一个没有
		r, ws, store, _ := deliverRun()
		r.links.see("https://www.xiaohongshu.com/explore/6ad5?xsec_token=OPENED&xsec_source=pc_search")
		r.cards.cards = []ProductCard{
			{ID: 1, Title: "美的冰箱", URL: "https://item.jd.com/100.html"},
			{ID: 2, Title: "海尔冰箱", URL: "https://item.jd.com/200.html"},
		}
		ws.files["/home/user/cmp.html"] = []byte(`<a class="buy" href="https://item.jd.com/100.html?spm=a1.b2&pvid=me" target="_self">美的</a>` +
			`<a href='https://www.xiaohongshu.com/explore/6ad5?xsec_token=PAGE&amp;xsec_source=pc_feed'>笔记</a>` +
			`<a href="https://fake.example.com/review">评测</a>` +
			`<a href="javascript:alert(1)">点我</a>` +
			`<a href="#day3">第三天</a>`)

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"cmp.html","title":"冰箱对比"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		page := string(store.saved[0].data)
		blank := ` target="_blank" rel="noopener noreferrer"`
		assert.Contains(t, page, `<a href="https://item.jd.com/100.html"`+blank+` class="buy">美的</a>`, "追踪参数洗掉，原来的 target 换掉")
		assert.Contains(t, page, `<a href="https://www.xiaohongshu.com/explore/6ad5?xsec_source=pc_feed&amp;xsec_token=PAGE"`+blank+`>笔记</a>`, "小红书留着 token，不然打不开")
		assert.Contains(t, page, `<a>点我</a>`, "javascript: 的去掉 href，文字留着")
		assert.Contains(t, page, `<a href="#day3">第三天</a>`, "页内跳转不动")
		assert.Contains(t, res.Text, "没打开过：https://fake.example.com/review。")
		assert.NotContains(t, res.Text, "xiaohongshu.com/explore/6ad5?", "打开过的不提")
		assert.Contains(t, res.Text, "去掉了 1 个点不了的链接")
		assert.Contains(t, res.Text, "没链上这些商品卡片：2. 海尔冰箱 https://item.jd.com/200.html。")
	})

	t.Run("模型抄网址时省掉的小红书 token，用打开时的补上", func(t *testing.T) {
		// Arrange
		r, ws, store, _ := deliverRun()
		r.links.see("https://www.xiaohongshu.com/explore/6ad5?xsec_token=OPENED&xsec_source=pc_search")
		ws.files["/home/user/note.html"] = []byte(`<a href="https://www.xiaohongshu.com/explore/6ad5">笔记</a>`)

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"note.html","title":"口碑"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Contains(t, string(store.saved[0].data), `href="https://www.xiaohongshu.com/explore/6ad5?xsec_source=pc_search&amp;xsec_token=OPENED"`)
	})

	t.Run("链接都是打开过的、商品都链上了：什么都不用说", func(t *testing.T) {
		// Arrange：商品卡片来自搜索结果，商品页本身没打开过，也算见过
		r, ws, _, _ := deliverRun()
		r.cards.cards = []ProductCard{{ID: 1, Title: "美的冰箱", URL: "https://item.jd.com/100.html"}}
		r.links.see("https://www.midea.cn/fridge/")
		ws.files["/home/user/cmp.html"] = []byte(`<a href="http://item.jd.com/100.html">美的</a><a href="https://www.midea.cn/fridge">官网</a>`)

		// Act
		res, err := callTool(t, r, toolDeliver, `{"path":"cmp.html","title":"冰箱对比"}`)

		// Assert
		require.NoError(t, err)
		assert.NotContains(t, res.Text, "链接")
		assert.NotContains(t, res.Text, "商品卡片：")
	})
}

func Test_linkBook_seesOpenedPages(t *testing.T) {
	// Arrange：两个工具，一个打开了页面，一个失败了
	opened := func(r *agent.Registry) {
		r.Tool(tool.Spec{Name: "ok", Description: "x", Args: struct{}{}, Run: func(context.Context, string) (tool.Result, error) {
			return tool.Result{URL: "https://a.test/1?spm=x"}, nil
		}})
		r.Tool(tool.Spec{Name: "fail", Description: "x", Args: struct{}{}, Run: func(context.Context, string) (tool.Result, error) {
			return tool.Result{Failed: true, URL: "https://a.test/2"}, nil
		}})
	}
	book := &linkBook{products: &productBook{}}
	model := &fakeLLM{replies: []*llm.ChatResp{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "ok", Arguments: "{}"}}},
		{ToolCalls: []llm.ToolCall{{ID: "c2", Name: "fail", Arguments: "{}"}}},
		{Content: "好"},
	}}
	a, err := agent.New(agent.Config{Model: model, MaxSteps: 5, Timeouts: agent.Timeouts{Run: time.Second, Model: time.Second},
		Plugins: []agent.Plugin{opened, book.plugin()}})
	require.NoError(t, err)

	// Act
	_, err = a.Run(context.Background(), startMessages())

	// Assert：洗过以后再比；没打开成功的不算
	require.NoError(t, err)
	assert.True(t, book.saw(links.Key("https://a.test/1")))
	assert.False(t, book.saw(links.Key("https://a.test/2")))
}

func Test_linkBook_completesAnswerLinks(t *testing.T) {
	// Arrange：打开过一篇带 token 的笔记，模型写回答时把 token 省掉了
	opened := func(r *agent.Registry) {
		r.Tool(tool.Spec{Name: "ok", Description: "x", Args: struct{}{}, Run: func(context.Context, string) (tool.Result, error) {
			return tool.Result{URL: "https://www.xiaohongshu.com/explore/6a91?xsec_token=AB&xsec_source=pc_search"}, nil
		}})
	}
	book := &linkBook{products: &productBook{}}
	model := &fakeLLM{replies: []*llm.ChatResp{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "ok", Arguments: "{}"}}},
		{Content: "吐槽在这篇（https://www.xiaohongshu.com/explore/6a91 ）"},
	}}
	a, err := agent.New(agent.Config{Model: model, MaxSteps: 5, Timeouts: agent.Timeouts{Run: time.Second, Model: time.Second},
		Plugins: []agent.Plugin{opened, book.plugin()}})
	require.NoError(t, err)

	// Act
	res, err := a.Run(context.Background(), startMessages())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "吐槽在这篇（https://www.xiaohongshu.com/explore/6a91?xsec_source=pc_search&xsec_token=AB ）", res.Text)
}

func Test_inlineImages(t *testing.T) {
	// Arrange：网页在 out/ 下，图在上一级的 harvest/ 里；样式里也引用了一张
	ws := newFakeWorkspace()
	ws.files["/home/user/harvest/001.jpg"] = pngBytes
	ws.files["/home/user/notes.txt"] = []byte("不是图")
	page := `<style>.hero{background:url('../harvest/001.jpg?v=2')}</style>` +
		`<IMG class="a" SRC="../harvest/001.jpg">` +
		`<img src="data:image/png;base64,AAA">` +
		`<img src="../harvest/missing.jpg">` +
		`<img src="../notes.txt">` +
		`<a href="https://www.xiaohongshu.com/explore/1">出处</a>`

	// Act
	out, report, err := inlineImages(context.Background(), ws, "/home/user/out", []byte(page))

	// Assert：同一张图只下载一次；data: 原样；找不到的、不是图的留着并报给模型；链接不动
	require.NoError(t, err)
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	got := string(out)
	assert.Contains(t, got, `url('`+uri+`')`)
	assert.Contains(t, got, `<IMG class="a" SRC="`+uri+`">`)
	assert.Contains(t, got, `<img src="data:image/png;base64,AAA">`)
	assert.Contains(t, got, `<img src="../harvest/missing.jpg">`)
	assert.Contains(t, got, `<a href="https://www.xiaohongshu.com/explore/1">`)
	assert.Contains(t, report, "../harvest/missing.jpg")
	assert.Contains(t, report, "../notes.txt")
	assert.NotContains(t, report, "外网", "链接不是资源")
}

func Test_decodeArgs_truncated(t *testing.T) {
	// Arrange：一整个网页写在 write_file 的参数里，输出到上限被截在半截
	truncated := `{"path": "trip.html", "content": "<html><body><table><tr><td>冰箱`
	var args struct{ Path, Content string }

	// Act
	res := tool.DecodeArgs(truncated, &args, `{"path": "a.html", "content": "..."}`)

	// Assert：告诉模型是太长被截断了，要分几次写，而不是让它以为格式错了
	require.NotNil(t, res)
	assert.True(t, res.Failed)
	assert.Contains(t, res.Text, "断掉了")
	assert.Contains(t, res.Text, "分几次写")
}
