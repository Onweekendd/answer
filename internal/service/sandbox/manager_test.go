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

package sandbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipAcquire 写完 Acquire 以后删掉
func skipAcquire(t *testing.T) {
	t.Helper()
	t.Skip("TODO(精华)：写完 Manager.Acquire 删掉这一行")
}

func TestAcquire_TemporaryChat(t *testing.T) {
	skipAcquire(t)
	// Arrange
	cube, store := newFakeCube(), newFakeStore()
	m := NewManager(cube, store)

	// Act
	box, reset, err := m.Acquire(context.Background(), 0, "u1")

	// Assert：建了一个会被销毁的临时沙箱，没有记下来
	require.NoError(t, err)
	assert.NotNil(t, box)
	assert.False(t, reset)
	require.Len(t, cube.created, 1)
	assert.Equal(t, CreateOpts{ChatID: 0, Idle: tempIdle, Pause: false}, cube.created[0])
}

func TestAcquire_FirstUseCreatesAndRecords(t *testing.T) {
	skipAcquire(t)
	// Arrange
	cube, store := newFakeCube(), newFakeStore()
	store.addChat(7, "u1", "")
	m := NewManager(cube, store)

	// Act
	box, reset, err := m.Acquire(context.Background(), 7, "u1")

	// Assert
	require.NoError(t, err)
	assert.False(t, reset, "原来没有沙箱，谈不上重置")
	assert.Equal(t, CreateOpts{ChatID: 7, Idle: chatIdle, Pause: true}, cube.created[0])
	assert.Equal(t, box.ID(), store.sandboxOf(7))
}

func TestAcquire_ReusesRecordedSandbox(t *testing.T) {
	skipAcquire(t)
	// Arrange
	cube, store := newFakeCube(), newFakeStore()
	store.addChat(7, "u1", "")
	m := NewManager(cube, store)
	first, _, err := m.Acquire(context.Background(), 7, "u1")
	require.NoError(t, err)

	// Act
	second, reset, err := m.Acquire(context.Background(), 7, "u1")

	// Assert
	require.NoError(t, err)
	assert.False(t, reset)
	assert.Equal(t, first.ID(), second.ID())
	assert.Equal(t, 1, cube.createCount())
}

func TestAcquire_GoneSandboxIsRecreatedWithReset(t *testing.T) {
	skipAcquire(t)
	// Arrange：记着 sb-1，但它已经没了
	cube, store := newFakeCube(), newFakeStore()
	store.addChat(7, "u1", "")
	m := NewManager(cube, store)
	old, _, err := m.Acquire(context.Background(), 7, "u1")
	require.NoError(t, err)
	cube.gone[old.ID()] = true

	// Act
	box, reset, err := m.Acquire(context.Background(), 7, "u1")

	// Assert
	require.NoError(t, err)
	assert.True(t, reset, "要告诉模型之前的文件没了")
	assert.NotEqual(t, old.ID(), box.ID())
	assert.Equal(t, box.ID(), store.sandboxOf(7))
}

func TestAcquire_OtherConnectErrorIsReturned(t *testing.T) {
	skipAcquire(t)
	// Arrange：Cube 本身出了问题（不是沙箱没了），不能当成"没了"去重建一个
	cube, store := newFakeCube(), newFakeStore()
	store.addChat(7, "u1", "sb-x")
	cube.connectErr = errors.New("connection refused")
	m := NewManager(cube, store)

	// Act
	_, _, err := m.Acquire(context.Background(), 7, "u1")

	// Assert
	require.Error(t, err)
	assert.Equal(t, 0, cube.createCount())
	assert.Equal(t, "sb-x", store.sandboxOf(7))
}

func TestAcquire_SomeoneElsesChat(t *testing.T) {
	skipAcquire(t)
	// Arrange
	cube, store := newFakeCube(), newFakeStore()
	store.addChat(7, "u1", "")
	m := NewManager(cube, store)

	// Act
	_, _, err := m.Acquire(context.Background(), 7, "u2")

	// Assert
	assert.ErrorIs(t, err, ErrChatNotFound)
	assert.Equal(t, 0, cube.createCount())
}

func TestAcquire_ConcurrentCallsCreateOneSandbox(t *testing.T) {
	skipAcquire(t)
	// Arrange：建一个要 50ms，足够让 10 个请求都挤进"查表"和"记下来"之间
	cube, store := newFakeCube(), newFakeStore()
	cube.createDelay = 50 * time.Millisecond
	store.addChat(7, "u1", "")
	m := NewManager(cube, store)

	// Act
	const n = 10
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			box, _, err := m.Acquire(context.Background(), 7, "u1")
			errs[i] = err
			if err == nil {
				ids[i] = box.ID()
			}
		})
	}
	wg.Wait()

	// Assert
	for i := range n {
		require.NoError(t, errs[i])
		assert.Equal(t, ids[0], ids[i], "第 %d 个拿到了另一个沙箱", i)
	}
	assert.Equal(t, 1, cube.createCount())
}

func TestAcquire_DifferentChatsDoNotWaitForEachOther(t *testing.T) {
	skipAcquire(t)
	// Arrange：锁要按对话分开，一把全局锁会让 10 个对话排着队建
	cube, store := newFakeCube(), newFakeStore()
	cube.createDelay = 50 * time.Millisecond
	for id := int64(1); id <= 10; id++ {
		store.addChat(id, "u1", "")
	}
	m := NewManager(cube, store)

	// Act
	start := time.Now()
	var wg sync.WaitGroup
	for id := int64(1); id <= 10; id++ {
		wg.Go(func() {
			_, _, _ = m.Acquire(context.Background(), id, "u1")
		})
	}
	wg.Wait()

	// Assert：串行要 500ms
	assert.Equal(t, 10, cube.createCount())
	assert.Less(t, time.Since(start), 300*time.Millisecond)
}

func TestAcquire_LosingTheSwapUsesTheWinnersSandbox(t *testing.T) {
	skipAcquire(t)
	// Arrange：另一个后端实例在我们记下之前，抢先建好并记下了 sb-other
	cube, store := newFakeCube(), newFakeStore()
	store.addChat(7, "u1", "")
	other := newFakeBox("sb-other")
	cube.boxes[other.id] = other
	store.beforeSwap = func(chatID int64) { store.box[chatID] = "sb-other" }
	m := NewManager(cube, store)

	// Act
	box, _, err := m.Acquire(context.Background(), 7, "u1")

	// Assert：用人家那个，自己建的销毁掉
	require.NoError(t, err)
	assert.Equal(t, "sb-other", box.ID())
	assert.True(t, cube.boxes["sb-1"].killed, "自己建的那个不销毁就成了孤儿")
	assert.Equal(t, "sb-other", store.sandboxOf(7))
}
