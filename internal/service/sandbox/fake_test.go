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
	"fmt"
	"sync"
	"time"
)

// fakeBox 是测试用的沙箱：文件放在 map 里，命令按 runs 的脚本回答
type fakeBox struct {
	id string

	mu      sync.Mutex
	files   map[string][]byte
	cmds    []string
	results []RunResult // 第 i 次 Run 回 results[i]，用完了回退出码 0
	killed  bool
}

func newFakeBox(id string) *fakeBox {
	return &fakeBox{id: id, files: map[string][]byte{}}
}

func (b *fakeBox) ID() string { return b.id }

func (b *fakeBox) Upload(_ context.Context, path string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.files[path] = append([]byte(nil), data...)
	return nil
}

func (b *fakeBox) Download(_ context.Context, path string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.files[path]
	if !ok {
		return nil, fmt.Errorf("no such file: %s", path)
	}
	return data, nil
}

func (b *fakeBox) Run(_ context.Context, cmd string, _ time.Duration) (RunResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cmds = append(b.cmds, cmd)
	if i := len(b.cmds) - 1; i < len(b.results) {
		return b.results[i], nil
	}
	return RunResult{}, nil
}

func (b *fakeBox) Kill(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.killed = true
	return nil
}

// fakeCube 记下建了几个、建的时候用的选项；gone 里的 id 连不上
type fakeCube struct {
	mu          sync.Mutex
	boxes       map[string]*fakeBox
	created     []CreateOpts
	gone        map[string]bool
	connectErr  error         // 不为 nil 时所有 Connect 都回它
	createDelay time.Duration // 建一个要多久：并发测试靠它把"查表"和"记下来"之间的窗口拉开
}

func newFakeCube() *fakeCube {
	return &fakeCube{boxes: map[string]*fakeBox{}, gone: map[string]bool{}}
}

func (c *fakeCube) Create(_ context.Context, opts CreateOpts) (Box, error) {
	time.Sleep(c.createDelay)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created = append(c.created, opts)
	box := newFakeBox(fmt.Sprintf("sb-%d", len(c.created)))
	c.boxes[box.id] = box
	return box, nil
}

func (c *fakeCube) Connect(_ context.Context, id string) (Box, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connectErr != nil {
		return nil, c.connectErr
	}
	box, ok := c.boxes[id]
	if !ok || c.gone[id] {
		return nil, fmt.Errorf("%w: %s", ErrGone, id)
	}
	return box, nil
}

func (c *fakeCube) createCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.created)
}

// fakeStore 是 purchase_chat 表里的两列：对话属于谁、记着哪个沙箱
type fakeStore struct {
	mu    sync.Mutex
	owner map[int64]string
	box   map[int64]string
	// beforeSwap 在比较之前调用一次：模拟另一个后端实例在这时抢先记了一个
	beforeSwap func(chatID int64)
	swapErr    error // 不为 nil 时 SwapSandbox 回它：数据库出错
}

func newFakeStore() *fakeStore {
	return &fakeStore{owner: map[int64]string{}, box: map[int64]string{}}
}

func (s *fakeStore) addChat(chatID int64, sandboxID string) {
	s.owner[chatID] = "u1"
	s.box[chatID] = sandboxID
}

func (s *fakeStore) SandboxOf(_ context.Context, chatID int64, userID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner[chatID] != userID || userID == "" {
		return "", false, nil
	}
	return s.box[chatID], true, nil
}

func (s *fakeStore) SwapSandbox(_ context.Context, chatID int64, old, newID string) (bool, error) {
	if s.swapErr != nil {
		return false, s.swapErr
	}
	if hook := s.beforeSwap; hook != nil {
		s.beforeSwap = nil
		hook(chatID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.box[chatID] != old {
		return false, nil
	}
	s.box[chatID] = newID
	return true, nil
}

func (s *fakeStore) sandboxOf() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.box[7]
}
