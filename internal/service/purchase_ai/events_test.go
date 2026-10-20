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
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_elementLabels(t *testing.T) {
	t.Run("有文字用文字，没文字用 placeholder，太长的截断", func(t *testing.T) {
		// Arrange
		elements := "[0]<input placeholder=搜索小红书>\n[1]<button>搜索\n[2]<a id=x>" + strings.Repeat("长", 30) + "\n不是元素的一行"

		// Act
		got := elementLabels(elements)

		// Assert
		require.Equal(t, "搜索小红书", got[0])
		require.Equal(t, "搜索", got[1])
		require.Equal(t, strings.Repeat("长", labelMaxRunes)+"…", got[2])
		require.Len(t, got, 3)
	})
}

func Test_describeCall(t *testing.T) {
	r := newToolRun(newFakeBrowser())
	r.labels = map[int]string{44: "Phone17续航差"}

	cases := []struct {
		name, tool, args, want string
	}{
		{"点击写元素的名字，不写编号", toolClick, `{"index":44}`, "点击「Phone17续航差」"},
		{"快照里没有的编号就说编号", toolHover, `{"index":7}`, "把鼠标移到「7 号元素」上"},
		{"网址只留域名，查询参数里有 token", toolOpen, `{"url":"https://www.xiaohongshu.com/explore/1?xsec_token=abc"}`, "打开 www.xiaohongshu.com"},
		{"搜索词照写", toolType, `{"index":0,"text":"iPhone 17 续航","submit":true}`, "输入「iPhone 17 续航」并回车"},
		{"skill 只写文件名", toolSkill, `{"path":"site-guide/references/xiaohongshu.md"}`, "查阅网站使用指南：xiaohongshu"},
		{"滚动默认 1 屏", toolScroll, `{"direction":"down"}`, "向下滚动 1 屏"},
		{"方向键说成翻图", toolPress, `{"key":"ArrowRight"}`, "翻到下一张图"},
		{"参数坏了也不出错", toolClick, `not json`, "点击「元素」"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, r.describeCall(c.tool, c.args))
		})
	}
}

func Test_view(t *testing.T) {
	t.Run("打码以后再换屏蔽词", func(t *testing.T) {
		// Arrange
		r := newToolRun(newFakeBrowser())
		r.redact = []string{"小明的号", ""}

		// Act
		got := r.view("右上角是小明的号，电话 13812345678，邮箱 a@b.com，订单 138123456789012")

		// Assert
		require.Equal(t, "右上角是[已屏蔽]，电话 [手机号]，邮箱 [邮箱]，订单 138123456789012", got)
	})

	t.Run("身份证号", func(t *testing.T) {
		require.Equal(t, "号码 [身份证号]", maskPrivate("号码 11010519491231002X"))
	})
}

// collectEvents 跑一次 Agent，收下它发出的全部事件
func collectEvents(t *testing.T, model *fakeLLM, showReasoning bool) []Event {
	t.Helper()
	events := make(chan Event, 100)
	r := newAgentRun(model, newFakeBrowser(), testLimits())
	r.events = events
	r.showReasoning = showReasoning
	_, err := r.run(context.Background(), startMessages(), true)
	require.NoError(t, err)
	close(events)
	var out []Event
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func Test_events(t *testing.T) {
	t.Run("每一步先发思考，再发每个调用的开始和结束；done 不是工具卡片", func(t *testing.T) {
		// Arrange
		open := calls(openJD("c1"))
		open.Reasoning = "先打开京东，客服电话 13812345678"
		model := &fakeLLM{replies: []*llm.ChatResp{
			open,
			calls(toolCall("c2", toolSnapshot, `{}`)),
			calls(toolCall("c3", toolClick, `{"index":1}`)),
			calls(doneCall("c4", "买")),
		}}

		// Act
		got := collectEvents(t, model, true)

		// Assert
		var types []string
		for _, ev := range got {
			types = append(types, ev.Type)
		}
		require.Equal(t, []string{EventReasoning, EventToolStart, EventToolEnd, EventToolStart, EventToolEnd, EventToolStart, EventToolEnd}, types)
		require.Equal(t, "先打开京东，客服电话 [手机号]", got[0].Delta)
		require.Equal(t, got[1].ID, got[2].ID)
		require.Equal(t, "打开 www.jd.com", got[1].Args.Summary)
		require.Equal(t, "打开了「京东」", got[2].Result.Summary)
		require.True(t, *got[2].Result.OK)
		require.Equal(t, "页面上有 2 个可以操作的元素", got[4].Result.Summary)
		require.Equal(t, "点击「搜索」", got[5].Args.Summary, "用快照里的名字")
	})

	t.Run("页面变了被跳过的调用不发事件", func(t *testing.T) {
		// Arrange：点击以后页面变了，同一轮的 snapshot 被跳过
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(openJD("c1"), toolCall("c2", toolSnapshot, `{}`)),
			calls(doneCall("c3", "买")),
		}}

		// Act
		got := collectEvents(t, model, false)

		// Assert
		require.Len(t, got, 2)
		require.Equal(t, toolOpen, got[0].Name)
	})

	t.Run("失败的不说具体原因", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			calls(toolCall("c1", toolClick, `{"index":1}`)),
			calls(doneCall("c2", "买")),
		}}

		// Act：还没打开网页就点击，工具报失败
		got := collectEvents(t, model, false)

		// Assert
		require.False(t, *got[1].Result.OK)
		require.Equal(t, "没做成，换个办法", got[1].Result.Summary)
	})

	t.Run("思考的开关关了就不发思考", func(t *testing.T) {
		// Arrange
		resp := calls(doneCall("c1", "买"))
		resp.Reasoning = "想一想"
		model := &fakeLLM{replies: []*llm.ChatResp{resp}}

		// Act
		got := collectEvents(t, model, false)

		// Assert
		require.Empty(t, got)
	})
}

// chatService 是只够 Chat 用的服务：模型、浏览器、对话日志是假的，没有图片所以用不到上传
func chatService(model *fakeLLM) *PurchaseAIService {
	return chatServiceWith(model, &fakeChatLog{})
}

func chatServiceWith(model *fakeLLM, log *fakeChatLog) *PurchaseAIService {
	return &PurchaseAIService{llmClient: model, browser: newFakeBrowser(), chatOptions: chatOptions{showReasoning: true}, chatLog: log}
}

// chatReq 是新对话的第一条消息
func chatReq(text string) *schema.PurchaseAIChatReq {
	return &schema.PurchaseAIChatReq{ChatID: "1", UserID: fakeUserID, Message: &schema.PurchaseAIChatMessage{Text: text}}
}

// collect 把一轮的事件全部收下来
func collect(events <-chan Event) []Event {
	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	return got
}

// savedOf 找出 saved 事件
func savedOf(t *testing.T, events []Event) *SavedIDs {
	t.Helper()
	for _, ev := range events {
		if ev.Type == EventSaved {
			return ev.Saved
		}
	}
	t.Fatal("没有 saved 事件")
	return nil
}

func Test_Chat(t *testing.T) {
	t.Run("最后是回答和 done，然后 channel 关闭", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{calls(openJD("c1")), calls(doneCall("c2", "**买**"))}}

		// Act
		var got []Event
		for ev := range chatService(model).Chat(context.Background(), chatReq("iPhone 17 值得买吗")) {
			got = append(got, ev)
		}

		// Assert
		n := len(got)
		require.Equal(t, Event{Type: EventText, Delta: "**买**"}, got[n-2])
		require.Equal(t, Event{Type: EventDone}, got[n-1])
	})

	t.Run("出错时发一句给用户看的话，不发内部错误", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{err: context.DeadlineExceeded}

		// Act
		var got []Event
		for ev := range chatService(model).Chat(context.Background(), chatReq("你好")) {
			got = append(got, ev)
		}

		// Assert：最后一条是给用户看的那句话；前面那条 saved 让页面还能接着这一轮往下聊
		require.Equal(t, Event{Type: EventError, Message: chatFailedText}, got[len(got)-1])
		assert.Equal(t, EventSaved, got[len(got)-2].Type)
	})

	t.Run("没人收了也不会卡住：ctx 一取消，goroutine 结束、关掉 channel", func(t *testing.T) {
		// Arrange：模型一直不回，用户关掉了页面
		model := &fakeLLM{hang: 10}
		ctx, cancel := context.WithCancel(context.Background())
		events := chatService(model).Chat(ctx, chatReq("你好"))

		// Act
		cancel()

		// Assert：channel 在一秒内关闭（只读走，不需要谁来收）
		deadline := time.After(time.Second)
		for {
			select {
			case _, ok := <-events:
				if !ok {
					return
				}
			case <-deadline:
				t.Fatal("取消以后 channel 没有关闭，goroutine 泄漏了")
			}
		}
	})

	t.Run("历史从日志里来：接着 parent_id 那条分支，最后一条是新消息", func(t *testing.T) {
		skipProject(t)
		// Arrange：日志里已经有一轮
		model := &fakeLLM{replies: []*llm.ChatResp{calls(doneCall("c1", "好"))}}
		log := &fakeChatLog{entries: []Entry{
			msgEntry("u1", "", llm.Text(llm.RoleUser, "iPhone 17 值得买吗"), messageMeta{}),
			msgEntry("a1", "u1", llm.Text(llm.RoleAssistant, "**再等等**"), messageMeta{}),
			viewEntry("v1", "a1", Event{Type: EventText, Delta: "**再等等**"}),
		}}
		req := chatReq("那 Pro 呢")
		req.ParentID = "v1"

		// Act
		collect(chatServiceWith(model, log).Chat(context.Background(), req))

		// Assert
		msgs := model.reqs[0].Messages
		require.Len(t, msgs, 4)
		assert.Equal(t, llm.RoleSystem, msgs[0].Role)
		assert.Equal(t, "iPhone 17 值得买吗", msgs[1].Parts[0].Text)
		assert.Equal(t, llm.RoleAssistant, msgs[2].Role)
		assert.Equal(t, "那 Pro 呢", msgs[3].Parts[0].Text)
	})

	t.Run("别人的对话不跑", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{}
		req := chatReq("你好")
		req.UserID = "8"

		// Act
		got := collect(chatService(model).Chat(context.Background(), req))

		// Assert
		assert.Equal(t, []Event{{Type: EventError, Message: chatNotFoundText}}, got)
		assert.Empty(t, model.reqs)
	})

	t.Run("parent_id 不在这个对话里不跑", func(t *testing.T) {
		// Arrange
		req := chatReq("你好")
		req.ParentID = "e_nope"

		// Act
		got := collect(chatService(&fakeLLM{}).Chat(context.Background(), req))

		// Assert
		assert.Equal(t, []Event{{Type: EventError, Message: chatNotFoundText}}, got)
	})

	t.Run("重新生成：parent_id 要是一条用户消息", func(t *testing.T) {
		// Arrange：parent 是上一轮的 view，又没带新消息
		log := &fakeChatLog{entries: []Entry{
			msgEntry("u1", "", llm.Text(llm.RoleUser, "XM5 值不值"), messageMeta{}),
			viewEntry("v1", "u1"),
		}}
		req := &schema.PurchaseAIChatReq{ChatID: "1", UserID: fakeUserID, ParentID: "v1"}

		// Act
		got := collect(chatServiceWith(&fakeLLM{}, log).Chat(context.Background(), req))

		// Assert
		assert.Equal(t, []Event{{Type: EventError, Message: chatNotFoundText}}, got)
	})

	t.Run("saved 在回答之前：页面拿到 id 才能接着聊", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{calls(doneCall("c1", "好"))}}

		// Act
		got := collect(chatService(model).Chat(context.Background(), chatReq("XM5 值不值")))

		// Assert
		n := len(got)
		require.Equal(t, EventSaved, got[n-3].Type)
		assert.Equal(t, EventText, got[n-2].Type)
		assert.NotEmpty(t, got[n-3].Saved.User)
		assert.NotEmpty(t, got[n-3].Saved.Leaf)
	})

	t.Run("这一轮记进日志：用户消息、调用和结果、最后一条 view", func(t *testing.T) {
		skipRecorder(t)
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{calls(openJD("c1")), calls(doneCall("c2", "**买**"))}}
		log := &fakeChatLog{}

		// Act
		saved := savedOf(t, collect(chatServiceWith(model, log).Chat(context.Background(), chatReq("XM5 值不值"))))

		// Assert
		entries := log.entries
		require.NotEmpty(t, entries)
		first, last := entries[0], entries[len(entries)-1]
		assert.Equal(t, saved.User, first.ID)
		assert.Equal(t, "XM5 值不值", first.Message.Parts[0].Text)
		assert.Equal(t, saved.Leaf, last.ID)
		require.NotNil(t, last.View, "最后一条是 view")
		assert.Contains(t, last.View.Events, Event{Type: EventText, Delta: "**买**"})

		var tools []string
		for _, e := range entries {
			if e.Message != nil && e.Message.Role == llm.RoleTool {
				tools = append(tools, e.Message.ToolName)
			}
		}
		assert.Equal(t, []string{toolOpen, toolDone}, tools)
		path, err := pathTo(entries, saved.Leaf)
		require.NoError(t, err)
		assert.Len(t, path, len(entries), "从 leaf 走得回根：链是连着的")
	})
}
