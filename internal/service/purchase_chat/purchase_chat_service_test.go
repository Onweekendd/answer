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

// fakeRepo keeps chats in a map.
type fakeRepo struct {
	chats map[int64]*entity.PurchaseChat
}

func newFakeRepo() *fakeRepo { return &fakeRepo{chats: map[int64]*entity.PurchaseChat{}} }

func (f *fakeRepo) AddChat(_ context.Context, chat *entity.PurchaseChat) error {
	chat.ID = int64(len(f.chats) + 1)
	f.chats[chat.ID] = chat
	return nil
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

func TestPurchaseChatService(t *testing.T) {
	ctx := context.Background()

	t.Run("新建的会话在自己的列表里，标题是第一个问题，别人看不到", func(t *testing.T) {
		// Arrange
		s := NewPurchaseChatService(newFakeRepo())

		// Act
		created, err := s.Create(ctx, &schema.PurchaseChatCreateReq{Title: "这个显示器值不值", UserID: "1"})
		mine, mineErr := s.List(ctx, "1")
		others, othersErr := s.List(ctx, "2")

		// Assert
		require.NoError(t, err)
		require.NoError(t, mineErr)
		require.NoError(t, othersErr)
		require.Len(t, mine, 1)
		assert.Equal(t, created.ID, mine[0].ID)
		assert.Equal(t, "这个显示器值不值", mine[0].Title)
		assert.Empty(t, others)
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
