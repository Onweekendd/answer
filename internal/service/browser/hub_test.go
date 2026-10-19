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
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "test-token"

// fakeReply 决定假插件怎么回一条命令；返回 nil 表示不回（模拟插件卡住）
type fakeReply func(cmd Command) *Message

// echo 原样把参数作为结果返回
func echo(cmd Command) *Message {
	data, _ := json.Marshal(cmd.Args)
	return &Message{OK: true, Data: data}
}

func neverReply(Command) *Message { return nil }

// newHub 起一个真的 HTTP 服务挂上 Hub，测试结束自动关掉
func newHub(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	hub := NewHub(testToken)
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)
	return hub, srv
}

// dialFake 模拟插件：用 origin 连上 Hub，发 hello，然后在后台按 reply 回复每一条命令
func dialFake(t *testing.T, srv *httptest.Server, origin, token string, reply fakeReply) (*websocket.Conn, error) {
	t.Helper()
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {origin}},
	})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	hello := map[string]any{
		"type":  "event",
		"event": "hello",
		"data":  map[string]any{"version": "test", "token": token, "tabs": []TabInfo{{ID: 7, Title: "首页"}}},
	}
	require.NoError(t, wsjson.Write(ctx, conn, hello))

	// reply 为 nil：不读命令，测试自己读。两边一起读的话会抢同一条消息，
	// 被拒绝时的关闭帧被后台这个读走，测试读到的就不是关闭（2026-10-19 CI 的 Linux 上这样失败过）
	if reply == nil {
		return conn, nil
	}
	go func() {
		for {
			var cmd Command
			if err := wsjson.Read(ctx, conn, &cmd); err != nil {
				return
			}
			// 每条命令单独一个 goroutine 回复：真实的插件也是并发处理的，结果的顺序和命令的顺序不一定一样
			go func() {
				msg := reply(cmd)
				if msg == nil {
					return
				}
				msg.Type, msg.ID = "result", cmd.ID
				_ = wsjson.Write(ctx, conn, msg)
			}()
		}
	}()
	return conn, nil
}

// connectFake 连上并等到 Hub 那边认可了这条连接
func connectFake(t *testing.T, hub *Hub, srv *httptest.Server, reply fakeReply) *websocket.Conn {
	t.Helper()
	conn, err := dialFake(t, srv, "chrome-extension://test", testToken, reply)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return hub.current() != nil }, time.Second, 5*time.Millisecond)
	return conn
}

func pendingLen(s *session) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

func TestHub_Call(t *testing.T) {
	t.Run("插件回复结果时，Call 拿到结果里的 data", func(t *testing.T) {
		// Arrange
		hub, srv := newHub(t)
		connectFake(t, hub, srv, echo)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		// Act
		data, err := hub.Call(ctx, 7, "snapshot", map[string]int{"maxChars": 8000})

		// Assert
		require.NoError(t, err)
		assert.JSONEq(t, `{"maxChars":8000}`, string(data))
	})

	t.Run("插件报告失败时，Call 返回带错误码的 CommandError", func(t *testing.T) {
		// Arrange
		hub, srv := newHub(t)
		connectFake(t, hub, srv, func(Command) *Message {
			return &Message{OK: false, Error: &ErrorBody{Code: "ELEMENT_NOT_FOUND", Message: "编号 12 不存在"}}
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		// Act
		_, err := hub.Call(ctx, 7, "click", map[string]int{"index": 12})

		// Assert
		var cmdErr *CommandError
		require.ErrorAs(t, err, &cmdErr)
		assert.Equal(t, "ELEMENT_NOT_FOUND", cmdErr.Code)
	})

	t.Run("等不到结果时，按 ctx 超时返回，pending 表里不留下这条", func(t *testing.T) {
		// Arrange
		hub, srv := newHub(t)
		connectFake(t, hub, srv, neverReply)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Act
		_, err := hub.Call(ctx, 7, "navigate", map[string]string{"url": "https://example.com"})

		// Assert
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 0, pendingLen(hub.current()))
	})

	t.Run("插件断开时，正在等的 Call 立刻返回 ErrDisconnected，不用等到超时", func(t *testing.T) {
		// Arrange：一个要等 5 秒才超时的调用
		hub, srv := newHub(t)
		conn := connectFake(t, hub, srv, neverReply)
		s := hub.current()
		done := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := hub.Call(ctx, 7, "snapshot", nil)
			done <- err
		}()
		require.Eventually(t, func() bool { return pendingLen(s) == 1 }, time.Second, 5*time.Millisecond)

		// Act：插件断开
		_ = conn.Close(websocket.StatusNormalClosure, "bye")

		// Assert
		select {
		case err := <-done:
			require.ErrorIs(t, err, ErrDisconnected)
		case <-time.After(time.Second):
			t.Fatal("插件断开 1 秒后，Call 还没有返回")
		}
		require.Eventually(t, func() bool { return hub.current() == nil }, time.Second, 5*time.Millisecond)
		_, err := hub.Call(context.Background(), 7, "snapshot", nil)
		assert.ErrorIs(t, err, ErrNotConnected)
	})

	t.Run("50 个并发调用，结果乱序回来也不会串", func(t *testing.T) {
		// Arrange：假插件随机等一会儿再回，让结果的顺序和命令的顺序不一样
		hub, srv := newHub(t)
		connectFake(t, hub, srv, func(cmd Command) *Message {
			time.Sleep(time.Duration(rand.IntN(20)) * time.Millisecond)
			return echo(cmd)
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Act
		var wg sync.WaitGroup
		errs := make(chan error, 50)
		for i := range 50 {
			wg.Go(func() {
				data, err := hub.Call(ctx, 7, "click", map[string]int{"index": i})
				if err != nil {
					errs <- err
					return
				}
				var got struct{ Index int }
				if err := json.Unmarshal(data, &got); err != nil || got.Index != i {
					errs <- errors.New("调用 " + string(data) + " 拿到了别人的结果")
				}
			})
		}
		wg.Wait()
		close(errs)

		// Assert
		for err := range errs {
			t.Error(err)
		}
		assert.Equal(t, 0, pendingLen(hub.current()))
	})
}

func TestHub_Connect(t *testing.T) {
	t.Run("没有插件连着时，Call 返回 ErrNotConnected", func(t *testing.T) {
		hub, _ := newHub(t)
		_, err := hub.Call(context.Background(), 7, "snapshot", nil)
		assert.ErrorIs(t, err, ErrNotConnected)
	})

	t.Run("token 不对时，连接被拒绝", func(t *testing.T) {
		// Arrange
		hub, srv := newHub(t)

		// Act：不起读命令的 goroutine，只有测试自己读
		conn, err := dialFake(t, srv, "chrome-extension://test", "wrong-token", nil)
		require.NoError(t, err)

		// Assert：Hub 会关掉这条连接，假插件读到关闭
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _, err = conn.Read(ctx)
		assert.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
		assert.Nil(t, hub.current())
	})

	t.Run("网页发起的连接被拒绝", func(t *testing.T) {
		// Arrange：任何网页都能连 ws://127.0.0.1，但它的 Origin 不是 chrome-extension://
		_, srv := newHub(t)

		// Act
		_, err := dialFake(t, srv, "https://evil.example.com", testToken, neverReply)

		// Assert
		assert.Error(t, err)
	})

	t.Run("hello 带上的标签页会被记下来", func(t *testing.T) {
		hub, srv := newHub(t)
		connectFake(t, hub, srv, echo)
		assert.Equal(t, []TabInfo{{ID: 7, Title: "首页"}}, hub.Tabs())
	})

	t.Run("tabs 事件用新的列表替换旧的", func(t *testing.T) {
		// Arrange
		hub, srv := newHub(t)
		conn := connectFake(t, hub, srv, echo)
		event := map[string]any{
			"type":  "event",
			"event": "tabs",
			"data":  []TabInfo{{ID: 8, Title: "搜索结果"}, {ID: 9, Title: "详情"}},
		}

		// Act
		require.NoError(t, wsjson.Write(context.Background(), conn, event))

		// Assert
		want := []TabInfo{{ID: 8, Title: "搜索结果"}, {ID: 9, Title: "详情"}}
		assert.Eventually(t, func() bool { return slices.Equal(hub.Tabs(), want) }, time.Second, 5*time.Millisecond)
	})
}
