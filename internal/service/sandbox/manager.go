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
	"fmt"
	"sync"
	"time"

	"github.com/segmentfault/pacman/log"
)

// Manager 交出每个对话的沙箱：第一次用时才建，同一个对话复用，没了就重建（概念 2）
type Manager struct {
	cube  Cube
	store Store

	// 每个对话一把锁：只让同一个对话的请求排队，不同对话互不等待（一把全局锁会让所有人排队等
	// 约 250ms 的 Create）。mu 只保护 locks 这个 map，取完锁就放。
	// 不用 singleflight：这里还有"连不上就重建""抢输了重查"这些分支，锁写起来更直接。
	// 代价：locks 只增不减，一个对话十几个字节，现阶段不清理
	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

// NewManager 给依赖注入用
func NewManager(cube Cube, store Store) *Manager {
	return &Manager{cube: cube, store: store, locks: map[int64]*sync.Mutex{}}
}

// acquireAttempts 是第 3～6 步最多走几遍：抢输一次就够让人起疑了，不能无限转下去
const acquireAttempts = 3

// Acquire 交出 userID 的对话 chatID 的沙箱。reset 为 true 表示原来有沙箱、但已经没了，这是新建的：
// 之前生成的文件都不在了，调用方要告诉模型（概念 2 第 5 点）。
// chatID 为 0 是没有对话 id 的请求：建一个临时沙箱，不记下来，闲置 tempIdle 后 Cube 自己销毁。
//
// 上下文：
//   - 谁调用：模型第一次用沙箱工具时，purchase_ai 的 r.workspace → managerWorkspaces.Acquire → 这里；
//     拿到的 Box 存进 r.ws，这次运行剩下的工具都用它。同一个对话的两次运行（连发两条、开两个标签页）会并发调用
//   - 读写的那一行：purchase_chat 表里 id = chatID 的那行。它是前端在新对话发第一条消息时
//     POST /purchase/chat（PurchaseChatService.Create）建的，sandbox_id 初始为 ""；
//     这里第一次建好沙箱后用 SwapSandbox 写进去，沙箱没了重建时再换成新的
//   - SandboxOf 的三种结果：found=false（对话不存在或不是 userID 的）、found=true 且 id=""（还没有沙箱）、
//     found=true 且 id 不为空（记着一台）
//
// 步骤：
//  1. chatID 为 0：建一个临时沙箱直接返回，不加锁：每次都是新的，不会和谁抢
//  2. 加上这个对话的锁，3～6 步整个在锁里。先加锁再查表，否则两个请求都查到"没有"，各建一个（概念 2 第 4 点）
//  3. 查记着的沙箱
//  4. 记着一台就连上去；它没了（ErrGone）记下 reset，往下重建；别的错误（Cube 本身出问题）原样返回，
//     不能当成"没了"去重建，否则 Cube 抖一下用户昨天的文件就丢了
//  5. 建一台新的
//  6. CAS 记下来。抢输了（别的后端实例先记了一台，进程里的锁管不到别的进程）：
//     销毁自己建的，回到第 3 步去连人家那台
//
// 记不下来（数据库出错）时也要销毁刚建的：不然没人知道它属于谁，闲置以后被暂停，
// 暂停的快照一直占着 Cube 的磁盘，成了孤儿。沙箱 metadata 里有 chat_id，以后的对账任务还能找到漏网的
func (m *Manager) Acquire(ctx context.Context, chatID int64, userID string) (box Box, reset bool, err error) {
	if chatID == 0 {
		box, err := m.cube.Create(ctx, CreateOpts{ChatID: 0, Idle: tempIdle, Pause: false})
		if err != nil {
			return nil, false, err
		}
		return box, false, nil
	}

	l := m.lock(chatID)
	l.Lock()
	defer l.Unlock()

	for range acquireAttempts {
		sid, found, err := m.store.SandboxOf(ctx, chatID, userID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, ErrChatNotFound
		}

		if sid != "" {
			box, err := m.cube.Connect(ctx, sid)
			if err == nil {
				return box, reset, nil
			}
			if !errors.Is(err, ErrGone) {
				return nil, false, err
			}
			reset = true // 记着的那台没了，往下重建
		}

		box, err := m.cube.Create(ctx, CreateOpts{ChatID: chatID, Idle: chatIdle, Pause: true})
		if err != nil {
			return nil, false, err
		}

		swapped, err := m.store.SwapSandbox(ctx, chatID, sid, box.ID())
		if err != nil {
			m.discard(box)
			return nil, false, err
		}
		if swapped {
			return box, reset, nil
		}
		m.discard(box) // 抢输了，回去重查
	}
	return nil, false, fmt.Errorf("sandbox: chat %d: lost the race %d times", chatID, acquireAttempts)
}

// discard 销毁一台没记下来的沙箱。用新的 ctx：Acquire 的 ctx 可能正是因为到期才失败的
func (m *Manager) discard(box Box) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := box.Kill(ctx); err != nil {
		log.Warnf("sandbox: cannot kill unrecorded sandbox %s: %v", box.ID(), err)
	}
}

func (m *Manager) lock(chatID int64) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()

	l, ok := m.locks[chatID]
	if !ok {
		l = &sync.Mutex{}
		m.locks[chatID] = l // 放回去：下一个来要对话 42 的请求，拿到的是同一把
	}
	return l
}
