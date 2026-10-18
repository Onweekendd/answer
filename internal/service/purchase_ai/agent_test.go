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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/browser"
	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/require"
)

// fakeLLM answers from a script, one reply per call, and keeps every request
// so a test can check what the loop sent. The first hang calls block until
// their context ends, like a provider that stopped responding.
type fakeLLM struct {
	replies []*llm.ChatResp
	err     error
	hang    int
	reqs    []llm.ChatReq
}

func (f *fakeLLM) Chat(ctx context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	// copy the messages: the loop keeps appending to its slice after this call
	snapshot := *req
	snapshot.Messages = append([]llm.Message(nil), req.Messages...)
	f.reqs = append(f.reqs, snapshot)

	if f.hang > 0 {
		f.hang--
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	if len(f.replies) == 0 {
		return nil, errors.New("fakeLLM: no scripted reply left")
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	return reply, nil
}

// lastMessage 是第 i 次请求的最后一条消息的文字
func (f *fakeLLM) lastMessage(i int) string {
	msgs := f.reqs[i].Messages
	return msgs[len(msgs)-1].Parts[0].Text
}

// ---------- 假浏览器：一个很小的"假网站" ----------

// fakePage 是假网站里的一页
type fakePage struct {
	title    string
	elements string         // 快照里的文字，一行一个元素
	links    map[int]string // 点几号元素会跳到哪个网址
	submitTo string         // 在这一页输入并回车会跳到哪个网址
	screens  float64        // 页面有几屏高，滚到底就不再动；0 表示 3 屏
	newTab   bool           // 点链接、回车搜索的结果在新标签页里打开（淘宝的搜索就是这样）
}

func (p fakePage) height() float64 {
	if p.screens == 0 {
		return 3
	}
	return p.screens
}

// fakeBrowser 代替插件：网址对应写死的页面，点击按 links 跳转，收到的每一次调用都记下来
type fakeBrowser struct {
	pages     map[string]fakePage
	connected bool
	fail      map[string]error // 某个命令总是返回这个错误
	nextTab   int
	tabs      map[int]string  // 标签页 id → 现在的网址
	scrolled  map[int]float64 // 标签页 id → 往下滚了几屏，跳转以后回到顶部
	calls     []string        // 收到的调用，比如 "open https://www.jd.com"、"click 3"
}

func newFakeBrowser() *fakeBrowser {
	return &fakeBrowser{pages: shopPages(), connected: true, fail: map[string]error{}, tabs: map[int]string{}, scrolled: map[int]float64{}}
}

func (f *fakeBrowser) Connected() bool { return f.connected }

func (f *fakeBrowser) Call(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error) {
	raw, _ := json.Marshal(args)
	var a struct {
		URL       string `json:"url"`
		Index     int    `json:"index"`
		Text      string `json:"text"`
		Submit    bool   `json:"submit"`
		Direction string `json:"direction"`
		Pages     int    `json:"pages"`
	}
	_ = json.Unmarshal(raw, &a)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.fail[cmd]; err != nil {
		f.calls = append(f.calls, cmd+" failed")
		return nil, err
	}

	switch cmd {
	case "open":
		f.calls = append(f.calls, "open "+a.URL)
		f.nextTab++
		f.go2(f.nextTab, a.URL)
		return f.pageInfo(f.nextTab), nil
	case "navigate":
		f.calls = append(f.calls, "navigate "+a.URL)
		if _, ok := f.tabs[tabID]; !ok {
			return nil, &browser.CommandError{Code: "TAB_NOT_FOUND", Message: "no tab"}
		}
		f.go2(tabID, a.URL)
		return f.pageInfo(tabID), nil
	case "snapshot":
		f.calls = append(f.calls, "snapshot")
		url := f.tabs[tabID]
		page := f.pages[url]
		above := f.scrolled[tabID]
		return mustJSON(map[string]any{
			"url": url, "title": page.title, "elements": page.elements, "truncated": false,
			"stats": map[string]any{"pagesAbove": above, "pagesBelow": page.height() - above, "stable": true},
		}), nil
	case "click":
		f.calls = append(f.calls, fmt.Sprintf("click %d", a.Index))
		page := f.pages[f.tabs[tabID]]
		if a.Index < 0 || a.Index >= len(strings.Split(page.elements, "\n")) {
			return nil, &browser.CommandError{Code: "ELEMENT_NOT_FOUND", Message: "no such element"}
		}
		if to, ok := page.links[a.Index]; ok {
			return f.follow(tabID, to, page.newTab), nil
		}
		return f.acted(tabID, nil), nil
	case "type":
		f.calls = append(f.calls, fmt.Sprintf("type %d %s submit=%v", a.Index, a.Text, a.Submit))
		if page := f.pages[f.tabs[tabID]]; a.Submit && page.submitTo != "" {
			return f.follow(tabID, page.submitTo, page.newTab), nil
		}
		return f.acted(tabID, nil), nil
	case "scroll":
		f.calls = append(f.calls, fmt.Sprintf("scroll %s %d", a.Direction, a.Pages))
		// 往下滚到底就停在底部，往上滚到顶就停在顶部，和真的页面一样
		height := f.pages[f.tabs[tabID]].height()
		delta := float64(a.Pages)
		if a.Direction == "up" {
			delta = -delta
		}
		above := min(max(f.scrolled[tabID]+delta, 0), height)
		f.scrolled[tabID] = above
		return mustJSON(map[string]float64{"pagesAbove": above, "pagesBelow": height - above}), nil
	case "close":
		f.calls = append(f.calls, fmt.Sprintf("close %d", tabID))
		delete(f.tabs, tabID)
		return mustJSON(map[string]bool{"closed": true}), nil
	}
	return nil, &browser.CommandError{Code: "UNKNOWN_COMMAND", Message: cmd}
}

// follow 是点击或回车以后跳到 url：在原来的标签页里跳，或者像淘宝那样开一个新标签页
func (f *fakeBrowser) follow(tabID int, url string, newTab bool) json.RawMessage {
	if !newTab {
		f.go2(tabID, url)
		return f.acted(tabID, nil)
	}
	f.nextTab++
	f.go2(f.nextTab, url)
	return f.acted(tabID, f.pageInfo(f.nextTab))
}

// acted 是插件 click / type 的回复：动作之后的网址，以及新开的标签页（有的话）
func (f *fakeBrowser) acted(tabID int, newTab json.RawMessage) json.RawMessage {
	out := map[string]any{"url": f.tabs[tabID]}
	if newTab != nil {
		out["newTab"] = newTab
	}
	return mustJSON(out)
}

// go2 让标签页跳到 url，回到页面顶部
func (f *fakeBrowser) go2(tabID int, url string) {
	f.tabs[tabID] = url
	f.scrolled[tabID] = 0
}

func (f *fakeBrowser) pageInfo(tabID int) json.RawMessage {
	url := f.tabs[tabID]
	return mustJSON(map[string]any{"tabId": tabID, "url": url, "title": f.pages[url].title, "loaded": true})
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

const (
	jdHome   = "https://www.jd.com"
	jdSearch = "https://search.jd.com/Search?keyword=iphone"
	jdItem   = "https://item.jd.com/1.html"

	tbHome   = "https://www.taobao.com"
	tbSearch = "https://s.taobao.com/search?q=iphone"
)

func shopPages() map[string]fakePage {
	return map[string]fakePage{
		jdHome: {
			title:    "京东",
			elements: "[0]<input placeholder=搜索>\n[1]<button>搜索",
			links:    map[int]string{1: jdSearch},
			submitTo: jdSearch,
		},
		jdSearch: {
			title:    "iphone - 商品搜索",
			elements: "[0]<a>Apple iPhone 17 256GB ¥5999 自营\n[1]<a>销量\n[2]<a>下一页",
			links:    map[int]string{0: jdItem},
		},
		jdItem: {title: "Apple iPhone 17", elements: "[0]<a>Apple 京东自营旗舰店\n[1]<button>加入购物车"},
		// 淘宝首页：搜索结果在新标签页里打开
		tbHome: {
			title:    "淘宝",
			elements: "[0]<input aria-label=搜索>\n[1]<button>搜索",
			links:    map[int]string{1: tbSearch},
			submitTo: tbSearch,
			newTab:   true,
		},
		tbSearch: {title: "iphone_淘宝搜索", elements: "[0]<a>Apple iPhone 17 256GB ¥5299 300+人付款"},
	}
}

// ---------- 模型的回复 ----------

func toolCall(id, name, args string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: name, Arguments: args}
}

func calls(cs ...llm.ToolCall) *llm.ChatResp { return &llm.ChatResp{ToolCalls: cs} }

func openJD(id string) llm.ToolCall { return toolCall(id, toolOpen, `{"url":"`+jdHome+`"}`) }

func doneCall(id, text string) llm.ToolCall {
	return toolCall(id, toolDone, `{"text":"`+text+`","success":true}`)
}

func startMessages() []llm.Message {
	return []llm.Message{llm.Text(llm.RoleSystem, "sys"), llm.Text(llm.RoleUser, "iPhone 17 值得买吗")}
}

// testLimits 是默认的上限，只把超时调短，测试不用真等一分钟
func testLimits() agentLimits {
	limits := defaultLimits
	limits.llmTimeout = 200 * time.Millisecond
	limits.toolTimeout = 200 * time.Millisecond
	return limits
}

func runAgentWith(model *fakeLLM, b Browser, limits agentLimits) (*agentResult, error) {
	return newAgentRun(model, b, limits).run(context.Background(), startMessages(), true)
}

// toolReplies 找出第 i 次请求里所有的工具消息，按顺序
func toolReplies(model *fakeLLM, i int) []llm.Message {
	var out []llm.Message
	for _, msg := range model.reqs[i].Messages {
		if msg.Role == llm.RoleTool {
			out = append(out, msg)
		}
	}
	return out
}

// ---------- 整个循环（要 ①～⑨ 都写完才会全绿） ----------

func Test_run(t *testing.T) {
	t.Run("打开网页、看页面、搜索、再看，最后用 done 回答", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolSnapshot, `{}`)),
			calls(toolCall("c3", toolType, `{"index":0,"text":"iphone 17","submit":true}`)),
			calls(toolCall("c4", toolSnapshot, `{}`)),
			calls(doneCall("c5", "**买** 京东自营 5999 元")),
		}}
		b := newFakeBrowser()

		// Act
		res, err := runAgentWith(model, b, testLimits())

		// Assert
		require.NoError(t, err)
		require.Equal(t, "**买** 京东自营 5999 元", res.Text)
		require.True(t, res.Success)
		require.Empty(t, res.Finish)
		require.Equal(t, []string{"open " + jdHome, "snapshot", "type 0 iphone 17 submit=true", "snapshot"}, b.calls)

		require.Len(t, res.Steps, 5)
		require.Equal(t, jdHome, res.Steps[0].URL)
		require.Equal(t, jdSearch, res.Steps[3].URL)

		var names []string
		for _, tool := range model.reqs[0].Tools {
			names = append(names, tool.Name)
		}
		require.Equal(t, []string{toolOpen, toolNavigate, toolSnapshot, toolClick, toolType, toolScroll, toolDone}, names)
	})

	t.Run("同一轮里的调用按顺序执行：先填字，再点搜索", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolType, `{"index":0,"text":"iphone"}`), toolCall("c3", toolClick, `{"index":1}`)),
			calls(doneCall("c4", "好")),
		}}
		b := newFakeBrowser()

		// Act
		_, err := runAgentWith(model, b, testLimits())

		// Assert：填字不改变页面，所以点击也执行了，而且在填字之后
		require.NoError(t, err)
		require.Equal(t, []string{"open " + jdHome, "type 0 iphone submit=false", "click 1"}, b.calls)
		replies := toolReplies(model, 2)
		require.Equal(t, "c2", replies[1].ToolCallID)
		require.Equal(t, "c3", replies[2].ToolCallID)
		require.NotEqual(t, skippedText, replies[2].Parts[0].Text)
	})

	t.Run("点击改变了页面，同一轮后面的调用不执行，但每个都有回复", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolClick, `{"index":1}`), toolCall("c3", toolClick, `{"index":0}`)),
			calls(doneCall("c4", "好")),
		}}
		b := newFakeBrowser()

		// Act
		_, err := runAgentWith(model, b, testLimits())

		// Assert
		require.NoError(t, err)
		require.Equal(t, []string{"open " + jdHome, "click 1"}, b.calls)
		replies := toolReplies(model, 2)
		require.Len(t, replies, 3)
		require.Equal(t, "c3", replies[2].ToolCallID)
		require.Equal(t, skippedText, replies[2].Parts[0].Text)
	})

	t.Run("搜索结果在新标签页里打开：切换过去，接下来在新标签页里看页面", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(toolCall("c1", toolOpen, `{"url":"`+tbHome+`"}`)),
			calls(toolCall("c2", toolType, `{"index":0,"text":"iphone","submit":true}`)),
			calls(toolCall("c3", toolSnapshot, `{}`)),
			calls(doneCall("c4", "好")),
		}}

		// Act
		res, err := runAgentWith(model, newFakeBrowser(), testLimits())

		// Assert：回车以后告诉模型切到了新标签页，快照拍的是搜索结果页
		require.NoError(t, err)
		require.Contains(t, toolReplies(model, 2)[1].Parts[0].Text, "新标签页")
		require.Contains(t, toolReplies(model, 3)[2].Parts[0].Text, "Apple iPhone 17 256GB ¥5299")
		require.Equal(t, tbSearch, res.Steps[2].URL)
	})

	t.Run("一个调用失败了，同一轮后面的不执行：它们是按前一个成功来安排的", func(t *testing.T) {
		// Arrange：type 的文字是空的，直接失败；后面的 click 本来要点搜索
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolType, `{"index":0,"text":"  "}`), toolCall("c3", toolClick, `{"index":1}`)),
			calls(doneCall("c4", "好")),
		}}
		b := newFakeBrowser()

		// Act
		_, err := runAgentWith(model, b, testLimits())

		// Assert：搜索按钮没有被点
		require.NoError(t, err)
		require.Equal(t, []string{"open " + jdHome}, b.calls)
		replies := toolReplies(model, 2)
		require.Contains(t, replies[1].Parts[0].Text, "text 不能为空")
		require.Equal(t, "c3", replies[2].ToolCallID)
		require.Equal(t, skippedAfterFailText, replies[2].Parts[0].Text)
	})

	t.Run("done 后面的调用不执行", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(doneCall("c1", "直接回答"), openJD("c2")),
		}}
		b := newFakeBrowser()

		// Act
		res, err := runAgentWith(model, b, testLimits())

		// Assert
		require.NoError(t, err)
		require.Equal(t, "直接回答", res.Text)
		require.Empty(t, b.calls)
	})

	t.Run("插件没连上时不给工具，模型直接回答", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: "**再等等**"}}}

		// Act
		res, err := newAgentRun(model, noBrowser{}, testLimits()).run(context.Background(), startMessages(), false)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "**再等等**", res.Text)
		require.True(t, res.Success)
		require.Empty(t, model.reqs[0].Tools)
	})

	t.Run("操作过浏览器以后只回文字：先提醒调用 done，第二次才收下", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			{Content: "我先看看页面"},
			{Content: "**买**"},
		}}

		// Act
		res, err := runAgentWith(model, newFakeBrowser(), testLimits())

		// Assert
		require.NoError(t, err)
		require.Equal(t, "**买**", res.Text)
		require.Len(t, model.reqs, 3)
		require.Equal(t, doneReminder, model.lastMessage(2))
	})

	t.Run("给了工具，第一步就只回文字：也要先提醒调用 done", func(t *testing.T) {
		// Arrange：还没操作过浏览器，模型只说了一句要去做什么
		model := &fakeLLM{replies: []*llm.ChatResp{
			{Content: "我先打开京东看看"},
			calls(openJD("c1")),
			calls(doneCall("c2", "**买**")),
		}}
		b := newFakeBrowser()

		// Act
		res, err := runAgentWith(model, b, testLimits())

		// Assert：第一句没有被当成答案，提醒以后模型真的去打开了京东
		require.NoError(t, err)
		require.Equal(t, "**买**", res.Text)
		require.Equal(t, doneReminder, model.lastMessage(1))
		require.Equal(t, []string{"open " + jdHome}, b.calls)
	})

	t.Run("做到一半插件断开：走收尾，不报错", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolSnapshot, `{}`), toolCall("c3", toolClick, `{"index":0}`)),
			{Content: "根据已有信息：再等等"},
		}}
		b := newFakeBrowser()
		b.fail["snapshot"] = browser.ErrDisconnected

		// Act
		res, err := runAgentWith(model, b, testLimits())

		// Assert
		require.NoError(t, err)
		require.Equal(t, finishBrowserGone, res.Finish)
		require.False(t, res.Success)
		require.Equal(t, "根据已有信息：再等等", res.Text)

		last := model.reqs[2]
		require.Empty(t, last.Tools)
		require.Equal(t, finishPrompts[finishBrowserGone], model.lastMessage(2))
		replies := toolReplies(model, 2)
		require.Len(t, replies, 3)
		require.Equal(t, browserGoneText, replies[1].Parts[0].Text)
		require.Equal(t, skippedText, replies[2].Parts[0].Text)
	})

	t.Run("连续失败 3 次走收尾", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolClick, `{"index":99}`)),
			calls(toolCall("c3", toolClick, `{"index":98}`)),
			calls(toolCall("c4", toolClick, `{"index":97}`)),
			{Content: "没能查到"},
		}}

		// Act
		res, err := runAgentWith(model, newFakeBrowser(), testLimits())

		// Assert
		require.NoError(t, err)
		require.Equal(t, finishFailures, res.Finish)
		require.Len(t, model.reqs, 5)
		require.Empty(t, model.reqs[4].Tools)
		require.Equal(t, finishPrompts[finishFailures], model.lastMessage(4))
		// 每次失败模型都能看到原因
		require.Contains(t, toolReplies(model, 2)[1].Parts[0].Text, "没有这个编号的元素")
	})

	t.Run("滚到底了还在滚：同一个状态第 3 次提醒，第 5 次收尾", func(t *testing.T) {
		// Arrange：页面 3 屏高，滚 3 次就到底了，之后每次滚动的位置都不再变
		replies := []*llm.ChatResp{calls(openJD("c0"))}
		for i := 1; i <= 8; i++ {
			replies = append(replies, calls(toolCall(fmt.Sprintf("c%d", i), toolScroll, `{"direction":"down"}`)))
		}
		replies = append(replies, &llm.ChatResp{Content: "没能查到"})
		model := &fakeLLM{replies: replies}

		// Act
		res, err := runAgentWith(model, newFakeBrowser(), testLimits())

		// Assert：指纹用的是滚动之前的位置。前 3 次是从 0、1、2 屏往下滚，各不相同；
		// 从第 4 次起，每次都是"已经在第 3 屏（底部）还往下滚"：第 6 次是这个状态的第 3 次，提醒；第 8 次是第 5 次，收尾
		require.NoError(t, err)
		require.Equal(t, finishLoop, res.Finish)
		require.Len(t, model.reqs, 10)
		require.Contains(t, model.lastMessage(7), "第 3 次执行 scroll")
		require.Equal(t, finishPrompts[finishLoop], model.lastMessage(9))
	})

	t.Run("正常往下滚、页面一直在加载更多：不会被当成打转", func(t *testing.T) {
		// Arrange：搜索结果页很长（20 屏），连着往下滚 8 次都没到底，中间不看页面
		b := newFakeBrowser()
		page := b.pages[jdSearch]
		page.screens = 20
		b.pages[jdSearch] = page
		replies := []*llm.ChatResp{calls(toolCall("c0", toolOpen, `{"url":"`+jdSearch+`"}`))}
		for i := 1; i <= 8; i++ {
			replies = append(replies, calls(toolCall(fmt.Sprintf("c%d", i), toolScroll, `{"direction":"down"}`)))
		}
		replies = append(replies, calls(doneCall("c9", "好")))
		model := &fakeLLM{replies: replies}

		// Act
		res, err := runAgentWith(model, b, testLimits())

		// Assert：没有提醒，也没有收尾
		require.NoError(t, err)
		require.Empty(t, res.Finish)
		for i := range model.reqs {
			require.NotContains(t, model.lastMessage(i), "第 3 次执行")
		}
	})

	t.Run("步数用完走收尾，剩 warnAt 步时提醒一次", func(t *testing.T) {
		// Arrange
		limits := testLimits()
		limits.maxSteps = 4
		limits.warnAt = 2
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolSnapshot, `{}`)),
			calls(toolCall("c3", toolScroll, `{"direction":"down"}`)),
			calls(toolCall("c4", toolScroll, `{"direction":"up"}`)),
			{Content: "根据已有信息：买"},
		}}

		// Act
		res, err := runAgentWith(model, newFakeBrowser(), limits)

		// Assert
		require.NoError(t, err)
		require.Equal(t, finishSteps, res.Finish)
		require.Equal(t, "根据已有信息：买", res.Text)
		require.Len(t, model.reqs, 5)
		require.Equal(t, stepWarningText(2), model.lastMessage(2))
		require.Empty(t, model.reqs[4].Tools)
		require.Equal(t, finishPrompts[finishSteps], model.lastMessage(4))

		warnings := 0
		for _, msg := range model.reqs[4].Messages {
			if msg.Role == llm.RoleUser && strings.HasPrefix(msg.Parts[0].Text, "只剩") {
				warnings++
			}
		}
		require.Equal(t, 1, warnings)
	})

	t.Run("旧快照在下一次请求里换成占位语，最新的一份保留", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1")),
			calls(toolCall("c2", toolSnapshot, `{}`)),
			calls(toolCall("c3", toolType, `{"index":0,"text":"iphone","submit":true}`)),
			calls(toolCall("c4", toolSnapshot, `{}`)),
			calls(doneCall("c5", "好")),
		}}

		// Act
		_, err := runAgentWith(model, newFakeBrowser(), testLimits())

		// Assert
		require.NoError(t, err)
		latest := toolReplies(model, 4)
		require.Equal(t, snapshotOmitted, latest[1].Parts[0].Text)
		require.Equal(t, "c2", latest[1].ToolCallID)
		require.Contains(t, latest[3].Parts[0].Text, "iPhone 17 256GB")
		// 之前发出去的那次请求没有被改掉
		require.Contains(t, toolReplies(model, 3)[1].Parts[0].Text, "<input placeholder=搜索>")
	})

	t.Run("不改调用方传进来的切片，有空余容量时也不往它的底层数组里写", func(t *testing.T) {
		// Arrange：长度 2、容量 10，append 时不用搬家，会直接写进这个数组后面的空位
		input := make([]llm.Message, 2, 10)
		copy(input, startMessages())
		model := &fakeLLM{replies: []*llm.ChatResp{calls(openJD("c1")), calls(doneCall("c2", "好"))}}

		// Act
		_, err := newAgentRun(model, newFakeBrowser(), testLimits()).run(context.Background(), input, true)

		// Assert：长度以外、容量以内的空位还是空的
		require.NoError(t, err)
		require.Equal(t, startMessages(), input)
		for i, msg := range input[len(input):cap(input)] {
			require.Empty(t, msg.Role, "input 的底层数组第 %d 个空位被写过", len(input)+i)
		}
	})

	t.Run("模型调用失败时直接返回错误", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{err: errors.New("provider down")}

		// Act
		res, err := runAgentWith(model, newFakeBrowser(), testLimits())

		// Assert
		require.Error(t, err)
		require.Nil(t, res)
	})

	t.Run("ctx 结束以后不再继续请求模型", func(t *testing.T) {
		// Arrange
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		model := &fakeLLM{replies: []*llm.ChatResp{calls(openJD("c1")), calls(doneCall("c2", "不该走到这里"))}}

		// Act
		_, err := newAgentRun(model, newFakeBrowser(), testLimits()).run(ctx, startMessages(), true)

		// Assert
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, model.reqs, 1)
	})
}
