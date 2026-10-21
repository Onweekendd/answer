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
	"testing"

	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/uploader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageMessages(t *testing.T) {
	// Arrange：第一轮带截图、截过一张图（中间有一条截图提醒）；第二轮问完又重新生成了一次（a3 和 a4 是兄弟）；
	// 第三个问题没等到回答（进程重启了），用户接着又问了一句
	entries := []Entry{
		msgEntry("u1", "", userMessage(&schema.PurchaseAIChatMessage{Text: "XM5 值不值", Images: []string{"https://x/uploads/post/a.jpg"}}), messageMeta{}),
		msgEntry("a1", "u1", callMsg("", llm.ToolCall{ID: "c1", Name: toolShot}), messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "已截图"), messageMeta{ToolName: toolShot, Details: &Details{Summary: "截图看页面"}}),
		msgEntry("s1", "t1", llm.Text(llm.RoleUser, "这是第 1 步截的图"), messageMeta{Synthetic: true}),
		msgEntry("a2", "s1", llm.Text(llm.RoleAssistant, "值"), messageMeta{Details: &Details{Final: true}}),
		msgEntry("u2", "a2", llm.Text(llm.RoleUser, "那 bose 呢"), messageMeta{}),
		msgEntry("a3", "u2", llm.Text(llm.RoleAssistant, "也行"), messageMeta{Details: &Details{Final: true}}),
		msgEntry("a4", "u2", llm.Text(llm.RoleAssistant, "不如索尼"), messageMeta{Details: &Details{Final: true}}),
		msgEntry("u3", "a4", llm.Text(llm.RoleUser, "淘宝上呢"), messageMeta{}),
		msgEntry("u4", "u3", llm.Text(llm.RoleUser, "怎么不说话"), messageMeta{}),
	}

	// Act
	got := pageMessages(entries, chatOptions{})

	// Assert：截图那条提醒、工具调用都不是页面上的消息；回答的 id 是那一轮的最后一条
	type node struct{ ID, ParentID, Role string }
	var nodes []node
	for _, m := range got {
		nodes = append(nodes, node{m.ID, m.ParentID, m.Role})
	}
	assert.Equal(t, []node{
		{"u1", "", "user"},
		{"a2", "u1", "assistant"},
		{"u2", "a2", "user"},
		{"a3", "u2", "assistant"},
		{"a4", "u2", "assistant"}, // 重新生成的回答和原来那个是兄弟
		{"u3", "a4", "user"},
		{"u4", "u3", "user"}, // 前一个问题没有回答，就接在它后面
	}, nodes)
	assert.Equal(t, "XM5 值不值", got[0].Text)
	assert.Equal(t, []string{"https://x/uploads/post/a.jpg"}, got[0].Images)
}

// replayRun 是重放用的 agentRun：只有打码和要不要显示思考
func replayRun(showReasoning bool) *agentRun {
	return &agentRun{showReasoning: showReasoning}
}

// toolCardTurn 是一轮：打开京东（同一步的 snapshot 因为页面变了被跳过），然后直接回答
func toolCardTurn() []Entry {
	open := callMsg("先看京东，客服电话 13812345678", openJD("c1"), toolCall("c2", toolSnapshot, `{}`))
	return []Entry{
		msgEntry("a1", "u1", open, messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "已打开京东"), messageMeta{ToolName: toolOpen, Details: &Details{Summary: "打开 www.jd.com", Display: "打开了「京东」"}}),
		msgEntry("t2", "t1", toolMessage("c2", skippedText), messageMeta{ToolName: toolSnapshot}),
		msgEntry("a2", "t2", llm.Text(llm.RoleAssistant, "买"), messageMeta{Details: &Details{Final: true}}),
	}
}

func TestReplayTurn_ToolCards(t *testing.T) {
	// Arrange
	r := replayRun(true)
	ok := true

	// Act
	got := r.replayTurn(toolCardTurn())

	// Assert：思考打了码；跳过的 snapshot 没有卡片；id 是「第几步-第几个调用」
	want := []Event{
		{Type: EventReasoning, Delta: "先看京东，客服电话 [手机号]"},
		{Type: EventToolStart, ID: "0-0", Name: toolOpen, Args: &ToolView{Summary: "打开 www.jd.com"}},
		{Type: EventToolEnd, ID: "0-0", Result: &ToolView{Summary: "打开了「京东」", OK: &ok}},
	}
	want = append(want, r.answerEvents(&agentResult{Text: "买"})...)
	assert.Equal(t, want, got)
}

func TestReplayTurn_ReasoningOff(t *testing.T) {
	// Act
	got := replayRun(false).replayTurn(toolCardTurn())

	// Assert
	require.NotEmpty(t, got)
	assert.Equal(t, EventToolStart, got[0].Type)
}

func TestReplayTurn_FailedCall(t *testing.T) {
	// Arrange
	r := replayRun(false)
	turn := []Entry{
		msgEntry("a1", "u1", callMsg("", toolCall("c1", toolClick, `{"index":1}`)), messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "还没有打开网页"), messageMeta{ToolName: toolClick, Details: &Details{Summary: "点击「搜索」", Failed: true}}),
		msgEntry("a2", "t1", llm.Text(llm.RoleAssistant, "没查到"), messageMeta{Details: &Details{Final: true}}),
	}

	// Act
	got := r.replayTurn(turn)

	// Assert：失败的不说具体原因
	require.GreaterOrEqual(t, len(got), 2)
	assert.Equal(t, "没做成，换个办法", got[1].Result.Summary)
	assert.False(t, *got[1].Result.OK)
}

func TestReplayTurn_ProductAndDraft(t *testing.T) {
	// Arrange：第一步记下一个商品，第二步整理草稿挂起
	r := replayRun(false)
	card := &ProductCard{ID: 1, Title: "索尼 WH-1000XM5", Price: "¥2299", URL: "https://item.jd.com/100.html", Image: "https://x/uploads/agent/1.jpg"}
	draft := &DraftInput{DraftID: "d1", Title: "索尼 XM5 值不值得买", Vote: draftVotes["wait"], Candidates: []string{}}
	turn := []Entry{
		msgEntry("a1", "u1", callMsg("", toolCall("c1", toolProduct, `{"index":3,"title":"索尼 WH-1000XM5","price":"¥2299"}`)), messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "已记下"), messageMeta{ToolName: toolProduct, Details: &Details{Summary: "把「索尼 WH-1000XM5」记下来", Display: "记下了「索尼 WH-1000XM5」", Product: card}}),
		msgEntry("a2", "t1", callMsg("", toolCall("c2", toolDraft, `{"title":"索尼 XM5 值不值得买","vote":"wait"}`)), messageMeta{}),
		msgEntry("t2", "a2", toolMessage("c2", "草稿已经交给用户确认。"), messageMeta{ToolName: toolDraft, Details: &Details{Summary: "整理帖子草稿", Display: "草稿交给你确认", Draft: draft, Final: true}}),
	}
	ok := true

	// Act
	got := r.replayTurn(turn)

	// Assert：卡片夹在开始和结束中间；草稿的回答是固定那句
	want := []Event{
		{Type: EventToolStart, ID: "0-0", Name: toolProduct, Args: &ToolView{Summary: "把「索尼 WH-1000XM5」记下来"}},
		{Type: EventProduct, Product: card},
		{Type: EventToolEnd, ID: "0-0", Result: &ToolView{Summary: "记下了「索尼 WH-1000XM5」", OK: &ok}},
		{Type: EventToolStart, ID: "1-0", Name: toolDraft, Args: &ToolView{Summary: "整理帖子草稿"}},
		{Type: EventToolSuspend, ID: "1-0", Name: toolDraft, Input: draft},
		{Type: EventToolEnd, ID: "1-0", Result: &ToolView{Summary: "草稿交给你确认", OK: &ok}},
	}
	want = append(want, r.answerEvents(&agentResult{Text: draftAnswer})...)
	assert.Equal(t, want, got)
}

func TestReplayTurn_DoneAnswer(t *testing.T) {
	// Arrange：第一次 done 没写回答被退回，第二次才做成；回答引用了一张图
	r := replayRun(false)
	shots := []Shot{{ID: 1, Image: "https://x/uploads/agent/2.jpg"}}
	turn := []Entry{
		msgEntry("a1", "u1", callMsg("", toolCall("c1", toolDone, `{"text":"","success":true}`)), messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "参数无效：text 不能为空"), messageMeta{ToolName: toolDone}),
		msgEntry("a2", "t1", callMsg("", toolCall("c2", toolDone, `{"text":"买，券后 2299\n[图1：券后价]","success":true,"followups":["那 bose 呢"]}`)), messageMeta{}),
		msgEntry("t2", "a2", toolMessage("c2", "已结束。"), messageMeta{ToolName: toolDone, Details: &Details{Final: true, Shots: shots}}),
	}

	// Act
	got := r.replayTurn(turn)

	// Assert：done 不是工具卡片，只有回答
	want := r.answerEvents(&agentResult{Text: "买，券后 2299\n[图1：券后价]", Followups: []string{"那 bose 呢"}, Shots: shots})
	assert.Equal(t, want, got)
}

func TestReplayTurn_Unfinished(t *testing.T) {
	// Arrange：打开了京东，下一次调用模型时出错了，没有回答
	r := replayRun(false)
	turn := toolCardTurn()[:3]

	// Act
	got := r.replayTurn(turn)

	// Assert
	require.NotEmpty(t, got)
	assert.Equal(t, Event{Type: EventError, Message: chatFailedText}, got[len(got)-1])
}

// agentImages 只实现 SaveAgentImage：和 fakeUploader 一样嵌一个 nil 的接口值补齐别的方法
type agentImages struct {
	uploader.UploaderService
	store *fakeImageStore
}

func (f agentImages) SaveAgentImage(ctx context.Context, data []byte) (string, error) {
	return f.store.SaveAgentImage(ctx, data)
}

// liveAndReplayed 真的跑一轮 Chat：返回直播时推给页面的事件（saved、done 不算，它们不是回答的内容），
// 和打开历史对话时从日志里重放出来的那条回答的事件
func liveAndReplayed(t *testing.T, model *fakeLLM, browserOn bool) (live, replayed []Event) {
	t.Helper()
	log := &fakeChatLog{}
	pas := chatServiceWith(model, log)
	pas.uploaderService = agentImages{store: &fakeImageStore{}}
	pas.drafts = &fakeDrafts{saved: map[string]*Draft{}}
	pas.browser.(*fakeBrowser).connected = browserOn

	for _, ev := range collect(pas.Chat(context.Background(), chatReq("XM5 值不值"))) {
		if ev.Type != EventSaved && ev.Type != EventDone {
			live = append(live, ev)
		}
	}
	page := pageMessages(log.entries, pas.chatOptions)
	require.Len(t, page, 2, "一条问题、一条回答")
	return live, page[1].Events
}

func TestReplayTurn_SameAsLive(t *testing.T) {

	t.Run("打开、被跳过的调用、记商品、截图，done 引用了截图", func(t *testing.T) {
		// Arrange
		first := calls(openJD("c1"), toolCall("c2", toolSnapshot, `{}`))
		first.Reasoning = "先打开京东，客服电话 13812345678"
		model := &fakeLLM{replies: []*llm.ChatResp{
			first,
			calls(toolCall("c3", toolProduct, `{"index":3,"title":"Apple iPhone 17 256G","price":"¥5999"}`), toolCall("c4", toolShot, `{}`)),
			calls(toolCall("c5", toolDone, `{"text":"买，券后 5999\n[图1：券后价]","success":true,"followups":["那 Pro 呢"]}`)),
		}}

		// Act
		live, replayed := liveAndReplayed(t, model, true)

		// Assert：场景真的走到了（有商品卡片、回答里有存好的图），重放和直播一模一样
		types := map[string]bool{}
		answer := ""
		for _, ev := range live {
			types[ev.Type] = true
			if ev.Type == EventText {
				answer = ev.Delta
			}
		}
		require.True(t, types[EventProduct] && types[EventFollowups] && types[EventReasoning], "%v", types)
		require.Contains(t, answer, "/uploads/agent/", "回答里的 [图1] 换成了存好的图")
		assert.Equal(t, live, replayed)
	})

	t.Run("整理草稿挂起", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(toolCall("c1", toolDraft, `{"title":"索尼 XM5 值不值得买","content":"降噪好，券后 2299","vote":"wait"}`)),
		}}

		// Act
		live, replayed := liveAndReplayed(t, model, false)

		// Assert
		require.Equal(t, EventToolSuspend, live[1].Type)
		assert.Equal(t, live, replayed)
	})

	t.Run("插件没连着，直接回答", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: "建议再等等，双十一还会降"}}}

		// Act
		live, replayed := liveAndReplayed(t, model, false)

		// Assert
		require.Equal(t, EventText, live[0].Type)
		assert.Equal(t, live, replayed)
	})

	t.Run("模型出错，这一轮没跑完", func(t *testing.T) {
		// Arrange：打开京东以后模型就没有回复了
		model := &fakeLLM{replies: []*llm.ChatResp{calls(openJD("c1"))}}

		// Act
		live, replayed := liveAndReplayed(t, model, true)

		// Assert
		require.Equal(t, EventError, live[len(live)-1].Type)
		assert.Equal(t, live, replayed)
	})
}
