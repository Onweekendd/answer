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
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolMessage(id, text string) llm.Message {
	m := llm.Text(llm.RoleTool, text)
	m.ToolCallID = id
	return m
}

func screenshot(text string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Tag: agent.TagScreenshot, Parts: []llm.Part{{Text: text}, {ImageURL: "data:image/png;base64,AA"}}}
}

func TestTaskState_compact(t *testing.T) {
	s := &TaskState{}
	newMessages := func() []llm.Message {
		return []llm.Message{
			llm.Text(llm.RoleSystem, "sys"),
			toolMessage("c1", "快照一"),
			toolMessage("c2", "已点击 [1]"),
			toolMessage("c3", "快照二"),
			toolMessage("c4", "快照三"),
		}
	}

	t.Run("只留最新的一份快照，其余换成占位语，别的消息不动", func(t *testing.T) {
		// Arrange
		messages := newMessages()

		// Act
		s.compact(messages, []string{"c1", "c3", "c4"})

		// Assert
		require.Equal(t, SnapshotOmitted, messages[1].Parts[0].Text)
		require.Equal(t, SnapshotOmitted, messages[3].Parts[0].Text)
		require.Equal(t, "快照三", messages[4].Parts[0].Text)
		require.Equal(t, "已点击 [1]", messages[2].Parts[0].Text)
		require.Equal(t, "c1", messages[1].ToolCallID)
		require.Len(t, messages[1].Parts, 1)
	})

	t.Run("调用两次结果一样，占位语换上以后不再变", func(t *testing.T) {
		// Arrange
		messages := newMessages()
		s.compact(messages, []string{"c1", "c3", "c4"})

		// Act
		s.compact(messages, []string{"c1", "c3", "c4"})

		// Assert
		require.Equal(t, SnapshotOmitted, messages[1].Parts[0].Text)
		require.Equal(t, "快照三", messages[4].Parts[0].Text)
	})

	t.Run("只有一份或者没有快照时什么都不改", func(t *testing.T) {
		// Arrange
		messages := newMessages()

		// Act
		s.compact(messages, []string{"c1"})
		s.compact(messages, nil)

		// Assert
		require.Equal(t, newMessages(), messages)
	})

	t.Run("不改动之前发出去的那份：Parts 的底层数组是共享的", func(t *testing.T) {
		// Arrange：sent 像模型的假实现那样复制了一份消息，Parts 和 messages 共用底层数组
		messages := newMessages()
		sent := append([]llm.Message(nil), messages...)

		// Act
		s.compact(messages, []string{"c1", "c4"})

		// Assert
		require.Equal(t, "快照一", sent[1].Parts[0].Text)
	})

	t.Run("Agent 自己截的图只留最新一张，用户上传的不动", func(t *testing.T) {
		// Arrange
		upload := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{{Text: "我的截图"}, {ImageURL: "data:image/png;base64,BB"}}}
		messages := []llm.Message{upload, screenshot("第 1 步"), toolMessage("c1", "x"), screenshot("第 2 步"), screenshot("第 3 步")}

		// Act
		s.compact(messages, nil)

		// Assert
		assert.Equal(t, "我的截图", messages[0].Parts[0].Text)
		assert.Equal(t, ScreenshotOmitted, messages[1].Parts[0].Text)
		assert.Len(t, messages[1].Parts, 1, "图也去掉了")
		assert.Equal(t, ScreenshotOmitted, messages[3].Parts[0].Text)
		assert.Equal(t, "第 3 步", messages[4].Parts[0].Text)
	})
}

func TestTaskState_Track(t *testing.T) {
	t.Run("记下执行之前的页面状态；换了页面以后状态跟着网址换", func(t *testing.T) {
		// Arrange
		s := &TaskState{PageHash: "before"}
		run := s.Track(func(context.Context, string) (tool.Result, error) {
			return tool.Result{PageChanged: true, URL: "https://a.com/x"}, nil
		})

		// Act
		res, err := run(context.Background(), "{}")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "before", res.State)
		assert.Equal(t, PageState("url:https://a.com/x", 0), s.PageHash)
	})

	t.Run("没换页面就不动", func(t *testing.T) {
		// Arrange
		s := &TaskState{PageHash: "before"}
		run := s.Track(func(context.Context, string) (tool.Result, error) { return tool.Result{}, nil })

		// Act
		res, _ := run(context.Background(), "{}")

		// Assert
		assert.Equal(t, "before", res.State)
		assert.Equal(t, "before", s.PageHash)
	})
}

// pageBrowser 对 snapshot 命令回一份每次内容都不同的快照
type pageBrowser struct{ n int }

func (b *pageBrowser) Connected() bool { return true }

func (b *pageBrowser) Call(_ context.Context, _ int, cmd string, _ any) (json.RawMessage, error) {
	b.n++
	return json.RawMessage(fmt.Sprintf(`{"url":"https://a.com","title":"A","elements":"[1]<a>第 %d 份</a>","stats":{}}`, b.n)), nil
}

type scripted struct {
	reqs    []llm.ChatReq
	replies []*llm.ChatResp
}

func (m *scripted) Chat(_ context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	snapshot := *req
	snapshot.Messages = append([]llm.Message(nil), req.Messages...)
	m.reqs = append(m.reqs, snapshot)
	return m.replies[len(m.reqs)-1], nil
}

func TestPlugin_OnlyTheLatestSnapshotIsSent(t *testing.T) {
	// Arrange
	s := &TaskState{Browser: &pageBrowser{}, Timeout: time.Second, TabID: 1}
	model := &scripted{replies: []*llm.ChatResp{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: NameSnapshot, Arguments: `{}`}}},
		{ToolCalls: []llm.ToolCall{{ID: "c2", Name: NameSnapshot, Arguments: `{}`}}},
		{Content: "好"},
	}}
	a, err := agent.New(agent.Config{Model: model, MaxSteps: 5, Timeouts: agent.Timeouts{Run: time.Second, Model: time.Second},
		Plugins: []agent.Plugin{Plugin(s)}})
	require.NoError(t, err)

	// Act
	_, err = a.Run(context.Background(), []llm.Message{llm.Text(llm.RoleUser, "q")})

	// Assert：第 3 次请求里，第一份快照是占位语，第二份是原文
	require.NoError(t, err)
	var replies []llm.Message
	for _, m := range model.reqs[2].Messages {
		if m.Role == llm.RoleTool {
			replies = append(replies, m)
		}
	}
	require.Len(t, replies, 2)
	assert.Equal(t, SnapshotOmitted, replies[0].Parts[0].Text)
	assert.Contains(t, replies[1].Parts[0].Text, "第 2 份")
}
