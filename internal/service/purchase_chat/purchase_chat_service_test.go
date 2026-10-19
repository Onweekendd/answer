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
	"strings"
	"testing"

	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepo keeps chats and messages in maps.
type fakeRepo struct {
	chats map[int64]*entity.PurchaseChat
	msgs  []*entity.PurchaseChatMessage
}

func newFakeRepo() *fakeRepo { return &fakeRepo{chats: map[int64]*entity.PurchaseChat{}} }

func (f *fakeRepo) AddChat(_ context.Context, chat *entity.PurchaseChat) error {
	chat.ID = int64(len(f.chats) + 1)
	f.chats[chat.ID] = chat
	return nil
}

func (f *fakeRepo) GetChat(_ context.Context, id int64) (*entity.PurchaseChat, bool, error) {
	c, ok := f.chats[id]
	return c, ok, nil
}

func (f *fakeRepo) ListChats(_ context.Context, userID string, _ int) ([]*entity.PurchaseChat, error) {
	var out []*entity.PurchaseChat
	for _, c := range f.chats {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeRepo) SaveMessage(_ context.Context, msg *entity.PurchaseChatMessage) error {
	f.msgs = append(f.msgs, msg)
	return nil
}

func (f *fakeRepo) ListMessages(_ context.Context, chatID int64) ([]*entity.PurchaseChatMessage, error) {
	var out []*entity.PurchaseChatMessage
	for _, m := range f.msgs {
		if m.ChatID == chatID {
			out = append(out, m)
		}
	}
	return out, nil
}

func TestPurchaseChatService(t *testing.T) {
	ctx := context.Background()

	t.Run("存进去的消息原样取回来", func(t *testing.T) {
		// Arrange
		s := NewPurchaseChatService(newFakeRepo())
		created, err := s.Create(ctx, &schema.PurchaseChatCreateReq{Title: "这个显示器值不值", UserID: "1"})
		require.NoError(t, err)
		msg := `{"id":"u1","role":"user","content":[{"type":"text","text":"这个显示器值不值"}]}`

		// Act
		err = s.SaveMessage(ctx, &schema.PurchaseChatMessageReq{
			ChatID: created.ID, MessageID: "u1", Message: []byte(msg), UserID: "1",
		})
		got, getErr := s.Get(ctx, &schema.PurchaseChatGetReq{ID: created.ID, UserID: "1"})

		// Assert
		require.NoError(t, err)
		require.NoError(t, getErr)
		assert.Equal(t, "这个显示器值不值", got.Title)
		require.Len(t, got.Messages, 1)
		assert.JSONEq(t, msg, string(got.Messages[0].Message))
	})

	t.Run("别人的会话读不到也写不进", func(t *testing.T) {
		// Arrange
		repo := newFakeRepo()
		s := NewPurchaseChatService(repo)
		created, err := s.Create(ctx, &schema.PurchaseChatCreateReq{Title: "我的", UserID: "1"})
		require.NoError(t, err)

		// Act
		_, getErr := s.Get(ctx, &schema.PurchaseChatGetReq{ID: created.ID, UserID: "2"})
		saveErr := s.SaveMessage(ctx, &schema.PurchaseChatMessageReq{
			ChatID: created.ID, MessageID: "x", Message: []byte("{}"), UserID: "2",
		})

		// Assert
		assert.Error(t, getErr)
		assert.Error(t, saveErr)
		assert.Empty(t, repo.msgs)
	})

	t.Run("id 不是数字时当作不存在", func(t *testing.T) {
		// Arrange
		s := NewPurchaseChatService(newFakeRepo())

		// Act
		_, err := s.Get(ctx, &schema.PurchaseChatGetReq{ID: "abc", UserID: "1"})

		// Assert
		assert.Error(t, err)
	})

	t.Run("太大的消息不存", func(t *testing.T) {
		// Arrange
		repo := newFakeRepo()
		s := NewPurchaseChatService(repo)
		created, err := s.Create(ctx, &schema.PurchaseChatCreateReq{UserID: "1"})
		require.NoError(t, err)
		big := `"` + strings.Repeat("a", maxMessageBytes) + `"`

		// Act
		err = s.SaveMessage(ctx, &schema.PurchaseChatMessageReq{
			ChatID: created.ID, MessageID: "u1", Message: []byte(big), UserID: "1",
		})

		// Assert
		assert.Error(t, err)
		assert.Empty(t, repo.msgs)
	})
}

func TestChatTitle(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"取第一行", "  显示器\n预算两千  ", "显示器"},
		{"空的叫新对话", "  \n ", defaultTitle},
		{"太长截断加省略号", strings.Repeat("长", maxTitleRunes+5), strings.Repeat("长", maxTitleRunes) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, chatTitle(tt.in))
		})
	}
}
