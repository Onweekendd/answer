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

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeChatLog 是测试用的对话日志：对话 1 属于用户 "7"，条目按追加的先后放在切片里
type fakeChatLog struct {
	entries   []Entry
	appends   int   // Append 被调用了几次
	appendErr error // 不为 nil 时 Append 回它：数据库出错
	ctxErr    error // Append 被调用时 ctx 的状态：close 是不是摘掉了取消
}

const fakeChatID, fakeUserID = int64(1), "7"

func (f *fakeChatLog) Entries(_ context.Context, chatID int64, userID string) ([]Entry, bool, error) {
	if chatID != fakeChatID || userID != fakeUserID {
		return nil, false, nil
	}
	return append([]Entry(nil), f.entries...), true, nil
}

func (f *fakeChatLog) Append(ctx context.Context, _ int64, entries []Entry) error {
	f.appends++
	f.ctxErr = ctx.Err()
	if f.appendErr != nil {
		return f.appendErr
	}
	// 和真的数据库一样：ctx 已经取消了就写不进去
	if err := ctx.Err(); err != nil {
		return err
	}
	f.entries = append(f.entries, entries...)
	return nil
}

// SetDraftOutcome 和真的一样：只在 fakeUserID 的对话里找带着这份草稿的那条
func (f *fakeChatLog) SetDraftOutcome(_ context.Context, userID, draftID string, outcome DraftOutcome) (bool, error) {
	if userID != fakeUserID {
		return false, nil
	}
	for _, e := range f.entries {
		if e.Message != nil && e.Message.Details != nil && e.Message.Details.Draft != nil && e.Message.Details.Draft.DraftID == draftID {
			e.Message.Details.Outcome = &outcome
			return true, nil
		}
	}
	return false, nil
}

// msgEntry 拼一条测试用的日志条目
func msgEntry(id, parent string, m llm.Message, meta messageMeta) Entry {
	return Entry{ID: id, ParentID: parent, Kind: EntryMessage, Message: storeMessage(m, meta)}
}

func TestStoreMessage_DataImagesAreNotKept(t *testing.T) {
	// Arrange：截图那条消息，一张 data: 图；用户上传的截图是网址
	shot := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{{Text: "这是第 2 步截的图"}, {ImageURL: "data:image/jpeg;base64,/9j/AAAA"}}}
	upload := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{{Text: "值不值"}, {ImageURL: "https://example.com/uploads/post/a.jpg"}}}

	// Act
	s1 := storeMessage(shot, messageMeta{Synthetic: true})
	s2 := storeMessage(upload, messageMeta{})

	// Assert
	assert.Equal(t, []StoredPart{{Text: "这是第 2 步截的图"}, {Text: imageOmitted}}, s1.Parts)
	assert.True(t, s1.Synthetic)
	assert.Equal(t, []StoredPart{{Text: "值不值"}, {Image: "https://example.com/uploads/post/a.jpg"}}, s2.Parts)
}

func TestStoredMessage_RoundTrip(t *testing.T) {
	// Arrange：一条带思考和工具调用的 assistant 消息，一条工具结果
	call := llm.Message{Role: llm.RoleAssistant, Reasoning: "先去京东",
		ToolCalls: []llm.ToolCall{{ID: "c1", Name: toolOpen, Arguments: `{"url":"https://www.jd.com"}`}}}
	result := toolMessage("c1", "已打开")

	// Act
	gotCall := storeMessage(call, messageMeta{PromptTokens: 1200}).Message()
	gotResult := storeMessage(result, messageMeta{ToolName: toolOpen}).Message()

	// Assert：变回来和原来一样，ToolName、PromptTokens 只在存储格式里
	assert.Equal(t, call.Reasoning, gotCall.Reasoning)
	assert.Equal(t, call.ToolCalls, gotCall.ToolCalls)
	assert.Equal(t, result, gotResult)
}

func TestPathTo(t *testing.T) {
	// Arrange：e1 → e2 → e3，e2 下面还有一个分支 e4（重新生成的）
	entries := []Entry{
		{ID: "e1"}, {ID: "e2", ParentID: "e1"}, {ID: "e3", ParentID: "e2"}, {ID: "e4", ParentID: "e1"},
	}
	ids := func(path []Entry) []string {
		out := make([]string, len(path))
		for i, e := range path {
			out[i] = e.ID
		}
		return out
	}

	t.Run("从根到叶，不含别的分支", func(t *testing.T) {
		path, err := pathTo(entries, "e3")
		require.NoError(t, err)
		assert.Equal(t, []string{"e1", "e2", "e3"}, ids(path))

		path, err = pathTo(entries, "e4")
		require.NoError(t, err)
		assert.Equal(t, []string{"e1", "e4"}, ids(path))
	})

	t.Run("新对话的第一条消息没有 parent", func(t *testing.T) {
		path, err := pathTo(entries, "")
		require.NoError(t, err)
		assert.Empty(t, path)
	})

	t.Run("找不到", func(t *testing.T) {
		_, err := pathTo(entries, "e9")
		assert.ErrorIs(t, err, errNoSuchEntry)
	})

	t.Run("绕成环不会死循环", func(t *testing.T) {
		_, err := pathTo([]Entry{{ID: "a", ParentID: "b"}, {ID: "b", ParentID: "a"}}, "a")
		assert.Error(t, err)
	})
}

func TestNewEntryID(t *testing.T) {
	id := newEntryID()
	assert.Regexp(t, `^e_[0-9a-z]{12}$`, id)
	assert.NotEqual(t, id, newEntryID())
}

func TestPlainShotRefs(t *testing.T) {
	tests := []struct{ in, want string }{
		{"价格见下\n[图3：券后 2299]\n", "价格见下\n（配图：券后 2299）\n"},
		{"【图 1: 参数表】", "（配图：参数表）"},
		{"没说明的去掉[图2]。", "没说明的去掉。"},
		{`{"text":"看 [图1：价格]","success":true}`, `{"text":"看 （配图：价格）","success":true}`},
		{"没有图", "没有图"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, plainShotRefs(tt.in))
	}
}
