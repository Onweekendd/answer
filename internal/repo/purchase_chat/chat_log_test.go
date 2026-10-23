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

package purchasechat

import (
	"context"
	"testing"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatLog(t *testing.T) {
	ctx := context.Background()

	newLog := func(t *testing.T) (*chatLog, *entity.PurchaseChat) {
		t.Helper()
		_, x := newRepo(t)
		require.NoError(t, x.Sync(new(entity.PurchaseChatEntry)))
		chat := &entity.PurchaseChat{UserID: "1", Title: "耳机"}
		_, err := x.Insert(chat)
		require.NoError(t, err)
		return &chatLog{data: &data.Data{DB: x}}, chat
	}
	user := purchaseai.Entry{ID: "e_1", Kind: purchaseai.EntryMessage,
		Message: &purchaseai.StoredMessage{V: 1, Role: "user", Parts: []purchaseai.StoredPart{{Text: "索尼 XM5 值不值"}, {Image: "https://example.com/a.jpg"}}}}
	call := purchaseai.Entry{ID: "e_2", ParentID: "e_1", Kind: purchaseai.EntryMessage,
		Message: &purchaseai.StoredMessage{V: 1, Role: "assistant", Reasoning: "先看京东",
			ToolCalls: []purchaseai.StoredCall{{ID: "c1", Name: "open", Arguments: `{"url":"https://www.jd.com"}`}}}}
	result := purchaseai.Entry{ID: "e_3", ParentID: "e_2", Kind: purchaseai.EntryMessage,
		Message: &purchaseai.StoredMessage{V: 1, Role: "tool", ToolCallID: "c1", ToolName: "open", Parts: []purchaseai.StoredPart{{Text: "已打开"}}}}
	done := purchaseai.Entry{ID: "e_4", ParentID: "e_3", Kind: purchaseai.EntryMessage,
		Message: &purchaseai.StoredMessage{V: 1, Role: "tool", ToolCallID: "c2", ToolName: "done", Parts: []purchaseai.StoredPart{{Text: "已结束。"}},
			Details: &purchaseai.Details{Shots: []purchaseai.Shot{{ID: 1, Image: "https://example.com/uploads/agent/1.jpg"}}}}}

	t.Run("追加以后按顺序原样读回来", func(t *testing.T) {
		// Arrange
		log, chat := newLog(t)

		// Act
		require.NoError(t, log.Append(ctx, chat.ID, []purchaseai.Entry{user, call}))
		require.NoError(t, log.Append(ctx, chat.ID, []purchaseai.Entry{result, done}))
		got, found, err := log.Entries(ctx, chat.ID, "1")

		// Assert
		require.NoError(t, err)
		assert.True(t, found)
		require.Len(t, got, 4)
		assert.Equal(t, []string{"e_1", "e_2", "e_3", "e_4"}, []string{got[0].ID, got[1].ID, got[2].ID, got[3].ID})
		assert.Equal(t, call.Message, got[1].Message)
		assert.Equal(t, "open", got[2].Message.ToolName)
		assert.Equal(t, done.Message, got[3].Message, "只给页面看的 Details 也原样存取")
	})

	t.Run("别人的对话当作不存在", func(t *testing.T) {
		// Arrange
		log, chat := newLog(t)
		require.NoError(t, log.Append(ctx, chat.ID, []purchaseai.Entry{user}))

		// Act
		got, found, err := log.Entries(ctx, chat.ID, "2")

		// Assert
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, got)
	})

	t.Run("同一个对话里条目 id 不能重复", func(t *testing.T) {
		// Arrange
		log, chat := newLog(t)
		require.NoError(t, log.Append(ctx, chat.ID, []purchaseai.Entry{user}))

		// Act
		err := log.Append(ctx, chat.ID, []purchaseai.Entry{user})

		// Assert
		assert.Error(t, err)
	})
}

func TestChatLog_SetDraftOutcome(t *testing.T) {
	ctx := context.Background()
	_, x := newRepo(t)
	require.NoError(t, x.Sync(new(entity.PurchaseChatEntry)))
	mine := &entity.PurchaseChat{UserID: "1", Title: "耳机"}
	_, err := x.Insert(mine)
	require.NoError(t, err)
	log := &chatLog{data: &data.Data{DB: x}}
	draft := purchaseai.Entry{ID: "e_2", ParentID: "e_1", Kind: purchaseai.EntryMessage,
		Message: &purchaseai.StoredMessage{V: 1, Role: "tool", ToolCallID: "c1", ToolName: "draft_post", Parts: []purchaseai.StoredPart{{Text: "草稿"}},
			Details: &purchaseai.Details{Summary: "整理草稿", Draft: &purchaseai.DraftInput{DraftID: "abc123def456", Title: "XM5 值不值"}}}}
	require.NoError(t, log.Append(ctx, mine.ID, []purchaseai.Entry{draft}))
	outcome := purchaseai.DraftOutcome{Status: purchaseai.DraftPublished, QuestionID: "10", Title: "XM5 值不值？"}

	t.Run("别人的草稿找不到，也不会被改", func(t *testing.T) {
		// Act
		found, err := log.SetDraftOutcome(ctx, "2", "abc123def456", outcome)

		// Assert
		require.NoError(t, err)
		assert.False(t, found)
		got, _, _ := log.Entries(ctx, mine.ID, "1")
		assert.Nil(t, got[0].Message.Details.Outcome)
	})

	t.Run("id 里带通配符也只认完全相同的草稿", func(t *testing.T) {
		// Act
		found, err := log.SetDraftOutcome(ctx, "1", "abc%", outcome)

		// Assert
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("自己的草稿：只多一个 outcome，别的原样", func(t *testing.T) {
		// Act
		found, err := log.SetDraftOutcome(ctx, "1", "abc123def456", outcome)
		got, _, readErr := log.Entries(ctx, mine.ID, "1")

		// Assert
		require.NoError(t, err)
		require.NoError(t, readErr)
		assert.True(t, found)
		want := *draft.Message
		details := *draft.Message.Details
		details.Outcome = &outcome
		want.Details = &details
		assert.Equal(t, &want, got[0].Message)
	})
}
