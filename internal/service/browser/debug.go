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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// 调试接口：让命令行（curl、脚本、测试）不经过交互式终端就能调用 Hub。
//
//	GET  /debug/tabs   插件上报的标签页
//	POST /debug/call   {"tabId": 1, "cmd": "snapshot", "args": {"maxChars": 8000}, "timeoutMs": 30000}
//
// 两个接口都要带 Authorization: Bearer <token>，token 和插件 popup 里填的是同一个。

const (
	debugDefaultTimeout = 30 * time.Second
	debugMaxTimeout     = 2 * time.Minute
	debugMaxBody        = 1 << 20

	// 客户端在结果出来之前断开了。net/http 没有这个常量，499 是 nginx 定的惯例
	statusClientClosedRequest = 499
)

// DebugBackend 是调试接口用到的方法。*Hub 实现了它，测试里换成假的
type DebugBackend interface {
	Call(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error)
	Tabs() []TabInfo
	Connected() bool
}

type debugCall struct {
	TabID     int             `json:"tabId"`
	Cmd       string          `json:"cmd"`
	Args      json.RawMessage `json:"args"`
	TimeoutMs int             `json:"timeoutMs"`
}

// 成功和失败都是这个格式，和插件回给 Go 的 result 一样
type debugResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *ErrorBody      `json:"error,omitempty"`
}

type debugHandler struct {
	backend DebugBackend
	token   string
}

// NewDebugHandler 返回挂在 /debug/ 下的调试接口。
func NewDebugHandler(backend DebugBackend, token string) http.Handler {
	d := &debugHandler{backend: backend, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/status", d.status)
	mux.HandleFunc("GET /debug/tabs", d.tabs)
	mux.HandleFunc("POST /debug/call", d.call)
	return d.guard(mux)
}

// guard 在所有接口前面先过一遍 authorize
func (d *debugHandler) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, body := d.authorize(r); status != 0 {
			writeDebugJSON(w, status, debugResponse{Error: body})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorize 判断这个请求能不能用调试接口。能用返回 (0, nil)，不能用返回状态码和错误。
func (d *debugHandler) authorize(r *http.Request) (int, *ErrorBody) {
	// 浏览器替网页发出的跨域请求一定带 Origin，curl 和 Go 程序不带
	if r.Header.Get("Origin") != "" {
		return http.StatusForbidden, &ErrorBody{Code: "FORBIDDEN", Message: "requests from web pages are not allowed"}
	}
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found || subtle.ConstantTimeCompare([]byte(token), []byte(d.token)) != 1 {
		return http.StatusUnauthorized, &ErrorBody{Code: "UNAUTHORIZED", Message: "missing or wrong token"}
	}
	return 0, nil
}

// callContext 给一次调用定期限：默认 30 秒，最多 2 分钟。
// 从 r.Context() 派生，客户端断开时 Hub 那边也不用再等。
func callContext(r *http.Request, timeoutMs int) (context.Context, context.CancelFunc) {
	timeout := debugDefaultTimeout
	if timeoutMs > 0 {
		timeout = min(time.Duration(timeoutMs)*time.Millisecond, debugMaxTimeout)
	}
	return context.WithTimeout(r.Context(), timeout)
}

// callError 把 Call 返回的错误翻译成 HTTP 状态码和错误体。
func callError(err error) (int, *ErrorBody) {
	var cmdErr *CommandError
	if errors.As(err, &cmdErr) {
		return http.StatusUnprocessableEntity, &ErrorBody{Code: cmdErr.Code, Message: cmdErr.Message}
	}
	switch {
	case errors.Is(err, ErrNotConnected):
		return http.StatusServiceUnavailable, &ErrorBody{Code: "NOT_CONNECTED", Message: err.Error()}
	case errors.Is(err, ErrDisconnected):
		return http.StatusBadGateway, &ErrorBody{Code: "DISCONNECTED", Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, &ErrorBody{Code: "TIMEOUT", Message: err.Error()}
	case errors.Is(err, context.Canceled):
		return statusClientClosedRequest, &ErrorBody{Code: "CANCELED", Message: err.Error()}
	default:
		return http.StatusInternalServerError, &ErrorBody{Code: "INTERNAL", Message: err.Error()}
	}
}

// debugStatus 是 GET /debug/status 的 data
type debugStatus struct {
	Connected bool `json:"connected"`
}

func (d *debugHandler) status(w http.ResponseWriter, _ *http.Request) {
	data, _ := json.Marshal(debugStatus{Connected: d.backend.Connected()})
	writeDebugJSON(w, http.StatusOK, debugResponse{OK: true, Data: data})
}

func (d *debugHandler) tabs(w http.ResponseWriter, _ *http.Request) {
	tabs := d.backend.Tabs()
	if tabs == nil {
		tabs = []TabInfo{} // 输出 []，不输出 null
	}
	data, _ := json.Marshal(tabs)
	writeDebugJSON(w, http.StatusOK, debugResponse{OK: true, Data: data})
}

func (d *debugHandler) call(w http.ResponseWriter, r *http.Request) {
	var req debugCall
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, debugMaxBody)).Decode(&req); err != nil {
		writeDebugJSON(w, http.StatusBadRequest, debugResponse{Error: &ErrorBody{Code: "BAD_REQUEST", Message: "body must be JSON: " + err.Error()}})
		return
	}
	if req.Cmd == "" {
		writeDebugJSON(w, http.StatusBadRequest, debugResponse{Error: &ErrorBody{Code: "BAD_REQUEST", Message: "cmd is required"}})
		return
	}
	// 插件那边按对象读 args（args.maxChars），没传的话给一个空对象，不给 null
	var args any = req.Args
	if len(req.Args) == 0 || string(req.Args) == "null" {
		args = struct{}{}
	}

	ctx, cancel := callContext(r, req.TimeoutMs)
	defer cancel()

	data, err := d.backend.Call(ctx, req.TabID, req.Cmd, args)
	if err != nil {
		status, body := callError(err)
		writeDebugJSON(w, status, debugResponse{Error: body})
		return
	}
	writeDebugJSON(w, http.StatusOK, debugResponse{OK: true, Data: data})
}

func writeDebugJSON(w http.ResponseWriter, status int, resp debugResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}
