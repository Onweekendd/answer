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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSandboxStore(t *testing.T) {
	ctx := context.Background()

	newChat := func(t *testing.T) (*sandboxStore, *entity.PurchaseChat) {
		t.Helper()
		_, x := newRepo(t)
		chat := &entity.PurchaseChat{UserID: "1", Title: "参考图"}
		_, err := x.Insert(chat)
		require.NoError(t, err)
		return &sandboxStore{data: &data.Data{DB: x}}, chat
	}

	t.Run("新对话还没有沙箱", func(t *testing.T) {
		// Arrange
		store, chat := newChat(t)

		// Act
		id, found, err := store.SandboxOf(ctx, chat.ID, "1")

		// Assert
		require.NoError(t, err)
		assert.True(t, found)
		assert.Empty(t, id)
	})

	t.Run("别人的对话当作不存在", func(t *testing.T) {
		// Arrange
		store, chat := newChat(t)

		// Act
		_, found, err := store.SandboxOf(ctx, chat.ID, "2")

		// Assert
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("旧值对得上才换，换了以后查得到", func(t *testing.T) {
		// Arrange
		store, chat := newChat(t)

		// Act
		swapped, err := store.SwapSandbox(ctx, chat.ID, "", "sb-1")

		// Assert
		require.NoError(t, err)
		assert.True(t, swapped)
		id, _, err := store.SandboxOf(ctx, chat.ID, "1")
		require.NoError(t, err)
		assert.Equal(t, "sb-1", id)
	})

	t.Run("旧值已经被别人换掉了就不换", func(t *testing.T) {
		// Arrange：别的实例先记了 sb-other
		store, chat := newChat(t)
		_, err := store.SwapSandbox(ctx, chat.ID, "", "sb-other")
		require.NoError(t, err)

		// Act：我们还以为是空的
		swapped, err := store.SwapSandbox(ctx, chat.ID, "", "sb-mine")

		// Assert
		require.NoError(t, err)
		assert.False(t, swapped)
		id, _, _ := store.SandboxOf(ctx, chat.ID, "1")
		assert.Equal(t, "sb-other", id)
	})

	t.Run("记沙箱不改对话的更新时间", func(t *testing.T) {
		// Arrange
		store, chat := newChat(t)
		before := &entity.PurchaseChat{}
		_, err := store.data.DB.ID(chat.ID).Get(before)
		require.NoError(t, err)
		time.Sleep(1100 * time.Millisecond) // TIMESTAMP 精确到秒

		// Act
		_, err = store.SwapSandbox(ctx, chat.ID, "", "sb-1")

		// Assert
		require.NoError(t, err)
		after := &entity.PurchaseChat{}
		_, err = store.data.DB.ID(chat.ID).Get(after)
		require.NoError(t, err)
		assert.Equal(t, before.UpdatedAt, after.UpdatedAt, "对话列表按用户最后说话的时间排，不能被 Agent 顶上去")
	})
}
