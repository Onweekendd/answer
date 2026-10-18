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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBackend 代替 Hub：不需要 WebSocket，也不需要插件
type fakeBackend struct {
	tabs []TabInfo
	call func(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error)
}

func (f *fakeBackend) Call(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error) {
	return f.call(ctx, tabID, cmd, args)
}

func (f *fakeBackend) Tabs() []TabInfo { return f.tabs }

// waitForCtx 一直等到 ctx 结束；2 秒还没结束说明 ctx 没设好，直接让测试失败，不要卡住
func waitForCtx(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return errors.New("test: ctx did not end within 2s")
	}
}

type debugRequest struct {
	method, path, body string
	header             map[string]string
	ctx                context.Context
}

func serveDebug(t *testing.T, backend DebugBackend, req debugRequest) (int, debugResponse) {
	t.Helper()
	r := httptest.NewRequest(req.method, req.path, strings.NewReader(req.body))
	if req.ctx != nil {
		r = r.WithContext(req.ctx)
	}
	for k, v := range req.header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()

	NewDebugHandler(backend, testToken).ServeHTTP(w, r)

	var resp debugResponse
	if w.Code != http.StatusMethodNotAllowed { // 405 是 ServeMux 自己写的纯文本
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())
	}
	return w.Code, resp
}

var authed = map[string]string{"Authorization": "Bearer " + testToken}

func postCall(t *testing.T, backend DebugBackend, body string) (int, debugResponse) {
	t.Helper()
	return serveDebug(t, backend, debugRequest{method: http.MethodPost, path: "/debug/call", body: body, header: authed})
}

func returning(data string, err error) *fakeBackend {
	return &fakeBackend{call: func(context.Context, int, string, any) (json.RawMessage, error) {
		if err != nil {
			return nil, err
		}
		return json.RawMessage(data), nil
	}}
}

func TestDebugHandler_Authorize(t *testing.T) {
	backend := &fakeBackend{tabs: []TabInfo{{ID: 1, Title: "首页"}}}

	t.Run("没带 token 返回 401", func(t *testing.T) {
		status, resp := serveDebug(t, backend, debugRequest{method: http.MethodGet, path: "/debug/tabs"})

		assert.Equal(t, http.StatusUnauthorized, status)
		require.NotNil(t, resp.Error)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
	})

	t.Run("token 不对返回 401", func(t *testing.T) {
		status, _ := serveDebug(t, backend, debugRequest{
			method: http.MethodGet, path: "/debug/tabs",
			header: map[string]string{"Authorization": "Bearer wrong"},
		})

		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("没有 Bearer 前缀返回 401", func(t *testing.T) {
		status, _ := serveDebug(t, backend, debugRequest{
			method: http.MethodGet, path: "/debug/tabs",
			header: map[string]string{"Authorization": testToken},
		})

		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("网页发来的请求带 Origin，token 对也返回 403", func(t *testing.T) {
		status, resp := serveDebug(t, backend, debugRequest{
			method: http.MethodGet, path: "/debug/tabs",
			header: map[string]string{"Authorization": "Bearer " + testToken, "Origin": "https://evil.example.com"},
		})

		assert.Equal(t, http.StatusForbidden, status)
		require.NotNil(t, resp.Error)
		assert.Equal(t, "FORBIDDEN", resp.Error.Code)
	})
}

func TestDebugHandler_Tabs(t *testing.T) {
	t.Run("返回插件上报的标签页", func(t *testing.T) {
		backend := &fakeBackend{tabs: []TabInfo{{ID: 1, Title: "首页"}, {ID: 2, Title: "详情"}}}

		status, resp := serveDebug(t, backend, debugRequest{method: http.MethodGet, path: "/debug/tabs", header: authed})

		assert.Equal(t, http.StatusOK, status)
		assert.JSONEq(t, `[{"id":1,"title":"首页"},{"id":2,"title":"详情"}]`, string(resp.Data))
	})

	t.Run("没有标签页时返回空数组，不返回 null", func(t *testing.T) {
		status, resp := serveDebug(t, &fakeBackend{}, debugRequest{method: http.MethodGet, path: "/debug/tabs", header: authed})

		assert.Equal(t, http.StatusOK, status)
		assert.JSONEq(t, `[]`, string(resp.Data))
	})
}

func TestDebugHandler_Call(t *testing.T) {
	t.Run("把 tabId、cmd、args 原样交给 Hub，返回它的 data", func(t *testing.T) {
		// Arrange
		var gotTab int
		var gotCmd, gotArgs string
		backend := &fakeBackend{call: func(_ context.Context, tabID int, cmd string, args any) (json.RawMessage, error) {
			gotTab, gotCmd = tabID, cmd
			b, _ := json.Marshal(args)
			gotArgs = string(b)
			return json.RawMessage(`{"title":"首页"}`), nil
		}}

		// Act
		status, resp := postCall(t, backend, `{"tabId":7,"cmd":"snapshot","args":{"maxChars":8000}}`)

		// Assert
		assert.Equal(t, http.StatusOK, status)
		assert.True(t, resp.OK)
		assert.JSONEq(t, `{"title":"首页"}`, string(resp.Data))
		assert.Equal(t, 7, gotTab)
		assert.Equal(t, "snapshot", gotCmd)
		assert.JSONEq(t, `{"maxChars":8000}`, gotArgs)
	})

	t.Run("没传 args 时给插件一个空对象", func(t *testing.T) {
		var gotArgs string
		backend := &fakeBackend{call: func(_ context.Context, _ int, _ string, args any) (json.RawMessage, error) {
			b, _ := json.Marshal(args)
			gotArgs = string(b)
			return json.RawMessage(`{}`), nil
		}}

		postCall(t, backend, `{"tabId":7,"cmd":"snapshot"}`)

		assert.JSONEq(t, `{}`, gotArgs)
	})

	t.Run("请求体不是 JSON 返回 400", func(t *testing.T) {
		status, resp := postCall(t, returning(`{}`, nil), `snapshot 7`)

		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, "BAD_REQUEST", resp.Error.Code)
	})

	t.Run("缺 cmd 返回 400", func(t *testing.T) {
		status, _ := postCall(t, returning(`{}`, nil), `{"tabId":7}`)

		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("GET 调用 /debug/call 返回 405", func(t *testing.T) {
		status, _ := serveDebug(t, returning(`{}`, nil), debugRequest{method: http.MethodGet, path: "/debug/call", header: authed})

		assert.Equal(t, http.StatusMethodNotAllowed, status)
	})
}

func TestDebugHandler_CallErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"插件执行失败返回 422 和插件的错误码", &CommandError{Code: "ELEMENT_NOT_FOUND", Message: "编号 99 不存在"}, 422, "ELEMENT_NOT_FOUND"},
		{"包了一层的 CommandError 也能认出来", fmt.Errorf("click: %w", &CommandError{Code: "TAB_NOT_FOUND", Message: "x"}), 422, "TAB_NOT_FOUND"},
		{"插件没连上返回 503", ErrNotConnected, http.StatusServiceUnavailable, "NOT_CONNECTED"},
		{"等结果时断线返回 502", ErrDisconnected, http.StatusBadGateway, "DISCONNECTED"},
		{"包了一层的断线错误也能认出来", fmt.Errorf("snapshot: %w", ErrDisconnected), http.StatusBadGateway, "DISCONNECTED"},
		{"超时返回 504", context.DeadlineExceeded, http.StatusGatewayTimeout, "TIMEOUT"},
		{"客户端断开返回 499", context.Canceled, statusClientClosedRequest, "CANCELED"},
		{"其他错误返回 500", errors.New("boom"), http.StatusInternalServerError, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := postCall(t, returning("", tt.err), `{"tabId":7,"cmd":"click","args":{"index":99}}`)

			assert.Equal(t, tt.wantStatus, status)
			require.NotNil(t, resp.Error)
			assert.Equal(t, tt.wantCode, resp.Error.Code)
			assert.False(t, resp.OK)
		})
	}
}

func TestDebugHandler_CallContext(t *testing.T) {
	t.Run("timeoutMs 到了，Hub 收到的 ctx 结束，返回 504", func(t *testing.T) {
		// Arrange
		backend := &fakeBackend{call: func(ctx context.Context, _ int, _ string, _ any) (json.RawMessage, error) {
			return nil, waitForCtx(ctx)
		}}
		start := time.Now()

		// Act
		status, _ := postCall(t, backend, `{"tabId":7,"cmd":"snapshot","timeoutMs":50}`)

		// Assert
		assert.Equal(t, http.StatusGatewayTimeout, status)
		assert.Less(t, time.Since(start), time.Second)
	})

	t.Run("不传 timeoutMs 用默认的 30 秒", func(t *testing.T) {
		var left time.Duration
		backend := &fakeBackend{call: func(ctx context.Context, _ int, _ string, _ any) (json.RawMessage, error) {
			deadline, _ := ctx.Deadline()
			left = time.Until(deadline)
			return json.RawMessage(`{}`), nil
		}}

		postCall(t, backend, `{"tabId":7,"cmd":"snapshot"}`)

		assert.InDelta(t, debugDefaultTimeout.Seconds(), left.Seconds(), 1)
	})

	t.Run("timeoutMs 超过上限时按 2 分钟算", func(t *testing.T) {
		var left time.Duration
		backend := &fakeBackend{call: func(ctx context.Context, _ int, _ string, _ any) (json.RawMessage, error) {
			deadline, _ := ctx.Deadline()
			left = time.Until(deadline)
			return json.RawMessage(`{}`), nil
		}}

		postCall(t, backend, `{"tabId":7,"cmd":"snapshot","timeoutMs":3600000}`)

		assert.InDelta(t, debugMaxTimeout.Seconds(), left.Seconds(), 1)
	})

	t.Run("客户端断开时，Hub 收到的 ctx 也被取消", func(t *testing.T) {
		// Arrange：模拟 curl 等到一半被 Ctrl+C
		reqCtx, cancel := context.WithCancel(context.Background())
		backend := &fakeBackend{call: func(ctx context.Context, _ int, _ string, _ any) (json.RawMessage, error) {
			cancel()
			return nil, waitForCtx(ctx)
		}}

		// Act
		status, resp := serveDebug(t, backend, debugRequest{
			method: http.MethodPost, path: "/debug/call", header: authed, ctx: reqCtx,
			body: `{"tabId":7,"cmd":"snapshot"}`,
		})

		// Assert
		assert.Equal(t, statusClientClosedRequest, status)
		require.NotNil(t, resp.Error)
		assert.Equal(t, "CANCELED", resp.Error.Code)
	})
}
