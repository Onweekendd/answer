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

// Package browser 把用户浏览器里的插件包装成一个普通的函数调用：
//
//	data, err := hub.Call(ctx, tabID, "snapshot", args)
//
// 连接由插件发起，命令由 Go 发出；每条命令带 id，结果按 id 配对。
package browser

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/segmentfault/pacman/log"
)

const (
	helloTimeout = 5 * time.Second
	readLimit    = 4 << 20 // 库默认 32KB，一份快照就可能超过
)

// Hub 管理插件连接。同一时间只保留一条，插件重连时新的替换旧的。
type Hub struct {
	token  string
	nextID atomic.Uint64

	mu   sync.Mutex
	cur  *session // nil 表示没有插件连着
	tabs []TabInfo
}

// NewHub 创建 Hub。token 要和插件 popup 里填的一致。
func NewHub(token string) *Hub {
	return &Hub{token: token}
}

// Call 发一条命令给插件，等它的结果。ctx 决定最多等多久。
func (h *Hub) Call(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error) {
	s := h.current()
	if s == nil {
		return nil, ErrNotConnected
	}
	id := "c-" + strconv.FormatUint(h.nextID.Add(1), 10)
	return s.call(ctx, Command{ID: id, Type: "command", Cmd: cmd, TabID: tabID, Args: args})
}

// Tabs 返回插件上报的网页标签页。
func (h *Hub) Tabs() []TabInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.tabs)
}

// ServeHTTP 接受插件的 WebSocket 连接，阻塞到连接断开。只挂在 127.0.0.1 上。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 任何网页都能连 ws://127.0.0.1，靠 Origin 挡掉网页，再靠 hello 里的 token
	if !strings.HasPrefix(r.Header.Get("Origin"), "chrome-extension://") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Origin 上面已经检查过；库默认只允许同源，会把插件也挡掉
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(readLimit)

	hello, err := h.readHello(r.Context(), conn)
	if err != nil {
		log.Warnf("browser hub: rejected a connection: %v", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "bad hello")
		return
	}

	s := newSession(conn)
	h.attach(s, hello.Tabs)
	log.Infof("browser hub: extension %s connected, %d tabs", hello.Version, len(hello.Tabs))

	s.readLoop(r.Context(), h)

	h.detach(s)
	log.Info("browser hub: extension disconnected")
}

// readHello 读第一条消息：必须是 hello，5 秒内到，token 正确。
func (h *Hub) readHello(ctx context.Context, conn *websocket.Conn) (*helloData, error) {
	ctx, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()

	var msg Message
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		return nil, err
	}
	if msg.Type != "event" || msg.Event != "hello" {
		return nil, errBadHello("first message is not hello")
	}
	var hello helloData
	if err := json.Unmarshal(msg.Data, &hello); err != nil {
		return nil, errBadHello("hello data is not valid")
	}
	// 固定时间比较，不让对方从耗时猜出 token
	if subtle.ConstantTimeCompare([]byte(hello.Token), []byte(h.token)) != 1 {
		return nil, errBadHello("wrong token")
	}
	return &hello, nil
}

type errBadHello string

func (e errBadHello) Error() string { return string(e) }

// attach 把新连接设为当前连接，并关掉旧连接；旧连接上的调用由它自己的 readLoop 让它们失败。
func (h *Hub) attach(s *session, tabs []TabInfo) {
	h.mu.Lock()
	old := h.cur
	h.cur = s
	h.tabs = tabs
	h.mu.Unlock()
	if old != nil {
		_ = old.conn.Close(websocket.StatusGoingAway, "replaced by a new connection")
	}
}

// detach 只在 s 还是当前连接时清空，它可能已经被新连接替换了。
func (h *Hub) detach(s *session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur == s {
		h.cur = nil
		h.tabs = nil
	}
}

func (h *Hub) current() *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur
}

// handleEvent 处理插件主动上报的事件。在 readLoop 里调用，不能阻塞。
func (h *Hub) handleEvent(msg Message) {
	switch msg.Event {
	case "ping":
	case "tabs":
		// 插件每次都报完整的列表，直接替换
		var tabs []TabInfo
		if err := json.Unmarshal(msg.Data, &tabs); err != nil {
			log.Debugf("browser hub: bad tabs event: %v", err)
			return
		}
		h.mu.Lock()
		h.tabs = tabs
		h.mu.Unlock()
	case "captcha_detected":
		// 只记标签页 id，网址里可能有搜索词
		log.Warnf("browser hub: captcha detected in tab %d", msg.TabID)
	default:
		log.Debugf("browser hub: ignored event %q", msg.Event)
	}
}
