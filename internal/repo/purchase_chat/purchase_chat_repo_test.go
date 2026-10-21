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
	"time"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	purchasechat "github.com/apache/answer/internal/service/purchase_chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newRepo(t *testing.T) (purchasechat.PurchaseChatRepo, *xorm.Engine) {
	t.Helper()
	x, err := xorm.NewEngine("sqlite", ":memory:")
	require.NoError(t, err)
	// 每个连接是一个独立的内存库，只留一个连接，事务和普通查询才看得到同一份数据
	x.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = x.Close() })
	require.NoError(t, x.Sync(new(entity.PurchaseChat), new(entity.PurchaseChatEntry)))
	return NewPurchaseChatRepo(&data.Data{DB: x}), x
}

func TestPurchaseChatRepo(t *testing.T) {
	ctx := context.Background()

	t.Run("列表只有自己的会话，刚说过话的排前面", func(t *testing.T) {
		// Arrange
		repo, x := newRepo(t)
		old := &entity.PurchaseChat{UserID: "1", Title: "旧的"}
		recent := &entity.PurchaseChat{UserID: "1", Title: "新的"}
		other := &entity.PurchaseChat{UserID: "2", Title: "别人的"}
		for _, c := range []*entity.PurchaseChat{old, recent, other} {
			require.NoError(t, repo.AddChat(ctx, c))
		}
		// 把两条会话的时间拉开，再往旧的那条里说一句话
		past := time.Now().Add(-time.Hour)
		_, err := x.Exec("UPDATE purchase_chat SET updated_at = ?", past)
		require.NoError(t, err)
		said := purchaseai.Entry{ID: "e_1", Kind: purchaseai.EntryMessage, Message: &purchaseai.StoredMessage{V: 1, Role: "user"}}
		require.NoError(t, NewChatLog(&data.Data{DB: x}).Append(ctx, old.ID, []purchaseai.Entry{said}))

		// Act
		chats, err := repo.ListChats(ctx, "1", 10)

		// Assert
		require.NoError(t, err)
		require.Len(t, chats, 2)
		assert.Equal(t, "旧的", chats[0].Title)
		assert.Equal(t, "新的", chats[1].Title)
	})
}
