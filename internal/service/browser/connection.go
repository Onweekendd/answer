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

package browser

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// connection 是一条插件连接，以及在这条连接上等结果的调用。
// pending 跟着连接走：旧连接断开时，不会误伤新连接上的调用。
type connection struct {
	conn *websocket.Conn

	mu      sync.Mutex
	pending map[string]chan Message // 命令 id → 等结果的 channel
	closed  bool                    // 为 true 后不再接受新的调用
}

func newConnection(conn *websocket.Conn) *connection {
	return &connection{conn: conn, pending: make(map[string]chan Message)}
}

// call 发出命令，等结果、超时或断线三者之一。
func (s *connection) call(ctx context.Context, cmd Command) (json.RawMessage, error) {
	s.mu.Lock()
	// 检查 closed 和登记必须在同一把锁里，否则 failAll 可能夹在中间，这次调用就没人通知了
	if s.closed {
		s.mu.Unlock()
		return nil, ErrDisconnected
	}
	ch := make(chan Message, 1)
	s.pending[cmd.ID] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, cmd.ID)
		s.mu.Unlock()
	}()

	if err := wsjson.Write(ctx, s.conn, cmd); err != nil {
		return nil, err
	}

	select {
	case msg := <-ch:
		if msg.OK {
			return msg.Data, nil
		}
		if msg.Error == nil {
			return nil, &CommandError{Code: "UNKNOWN", Message: "extension returned ok=false without an error"}
		}
		if msg.Error.Code == "DISCONNECTED" {
			return nil, ErrDisconnected
		}
		return nil, &CommandError{Code: msg.Error.Code, Message: msg.Error.Message}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readLoop 是这条连接上唯一读消息的 goroutine，一直跑到连接断开。
func (s *connection) readLoop(ctx context.Context, h *Hub) {
	for {
		var msg Message // 每次新建：Unmarshal 不清零旧字段，RawMessage 还会复用底层数组
		if err := wsjson.Read(ctx, s.conn, &msg); err != nil {
			s.failAll()
			return
		}

		switch msg.Type {
		case "result":
			// 谁从表里拿走，谁负责发：每个 channel 只会被发一次，容量 1 所以不会阻塞
			s.mu.Lock()
			ch, ok := s.pending[msg.ID]
			delete(s.pending, msg.ID)
			s.mu.Unlock()

			if ok {
				ch <- msg
			}
		case "event":
			h.handleEvent(msg)
		}
	}
}

// failAll 让还在等的调用立刻失败，并且不再接受新的调用。
func (s *connection) failAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	for key := range s.pending {
		s.pending[key] <- Message{Type: "result", OK: false, Error: &ErrorBody{Code: "DISCONNECTED", Message: ErrDisconnected.Error()}}
	}
}
