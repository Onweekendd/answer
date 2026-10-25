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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 真的 HTTP API 跑在 httptest 上，HubClient 连过去：错误发出去、收回来以后要和 Hub.Call 返回的一样
func newTestClient(t *testing.T, backend APIBackend, token string) *HubClient {
	t.Helper()
	server := httptest.NewServer(NewAPIHandler(backend, testToken))
	t.Cleanup(server.Close)
	return NewHubClient(server.URL+"/", token)
}

func TestHubClient_Call(t *testing.T) {
	t.Run("tabId、cmd、args 原样到达 Hub，返回它的 data", func(t *testing.T) {
		// Arrange
		var gotTab int
		var gotCmd string
		var gotArgs string
		backend := &fakeBackend{call: func(_ context.Context, tabID int, cmd string, args any) (json.RawMessage, error) {
			gotTab, gotCmd = tabID, cmd
			raw, _ := json.Marshal(args)
			gotArgs = string(raw)
			return json.RawMessage(`{"tabId":7}`), nil
		}}
		client := newTestClient(t, backend, testToken)

		// Act
		data, err := client.Call(context.Background(), 3, "open", map[string]string{"url": "https://a.example"})

		// Assert
		require.NoError(t, err)
		assert.JSONEq(t, `{"tabId":7}`, string(data))
		assert.Equal(t, 3, gotTab)
		assert.Equal(t, "open", gotCmd)
		assert.JSONEq(t, `{"url":"https://a.example"}`, gotArgs)
	})

	t.Run("ctx 剩下的时间作为 Hub 那边的期限", func(t *testing.T) {
		// Arrange
		var remaining time.Duration
		backend := &fakeBackend{call: func(ctx context.Context, _ int, _ string, _ any) (json.RawMessage, error) {
			deadline, _ := ctx.Deadline()
			remaining = time.Until(deadline)
			return json.RawMessage(`{}`), nil
		}}
		client := newTestClient(t, backend, testToken)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Act
		_, err := client.Call(ctx, 1, "snapshot", struct{}{})

		// Assert：默认是 30 秒，传过去了就在 5 秒以内
		require.NoError(t, err)
		assert.Greater(t, remaining, time.Duration(0))
		assert.LessOrEqual(t, remaining, 5*time.Second)
	})

	t.Run("插件的错误还原成 CommandError", func(t *testing.T) {
		// Arrange
		client := newTestClient(t, returning("", &CommandError{Code: "ELEMENT_NOT_FOUND", Message: "没有 8 号"}), testToken)

		// Act
		_, err := client.Call(context.Background(), 1, "click", map[string]int{"index": 8})

		// Assert
		var cmdErr *CommandError
		require.ErrorAs(t, err, &cmdErr)
		assert.Equal(t, "ELEMENT_NOT_FOUND", cmdErr.Code)
		assert.Equal(t, "没有 8 号", cmdErr.Message)
	})

	for _, tt := range []struct {
		name string
		err  error
	}{
		{"插件没连上还原成 ErrNotConnected", ErrNotConnected},
		{"插件断开还原成 ErrDisconnected", ErrDisconnected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			client := newTestClient(t, returning("", tt.err), testToken)

			// Act
			_, err := client.Call(context.Background(), 1, "snapshot", struct{}{})

			// Assert
			require.ErrorIs(t, err, tt.err)
		})
	}

	t.Run("超时还原成 context.DeadlineExceeded", func(t *testing.T) {
		// Arrange
		backend := &fakeBackend{call: func(ctx context.Context, _ int, _ string, _ any) (json.RawMessage, error) {
			return nil, waitForCtx(ctx)
		}}
		client := newTestClient(t, backend, testToken)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Act
		_, err := client.Call(ctx, 1, "navigate", map[string]string{"url": "https://a.example"})

		// Assert
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("token 不对返回错误，不是插件的错误", func(t *testing.T) {
		// Arrange
		client := newTestClient(t, returning(`{}`, nil), "wrong")

		// Act
		_, err := client.Call(context.Background(), 1, "snapshot", struct{}{})

		// Assert
		require.Error(t, err)
		var cmdErr *CommandError
		assert.NotErrorAs(t, err, &cmdErr)
		assert.Contains(t, err.Error(), "UNAUTHORIZED")
	})
}

func TestHubClient_Connected(t *testing.T) {
	t.Run("插件连着时返回 true", func(t *testing.T) {
		client := newTestClient(t, &fakeBackend{connected: true}, testToken)

		assert.True(t, client.Connected())
	})

	t.Run("插件没连着时返回 false", func(t *testing.T) {
		client := newTestClient(t, &fakeBackend{}, testToken)

		assert.False(t, client.Connected())
	})

	t.Run("Hub 本身连不上时返回 false", func(t *testing.T) {
		// Arrange：服务器开了又关，地址上没有人在听
		server := httptest.NewServer(NewAPIHandler(&fakeBackend{connected: true}, testToken))
		server.Close()
		client := NewHubClient(server.URL, testToken)

		// Act & Assert
		assert.False(t, client.Connected())
	})
}
