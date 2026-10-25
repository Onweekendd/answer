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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HubClient 通过 HTTP API 调用另一个进程里正在跑的 Hub，方法和 *Hub 一样。
// 业务后端和 case 运行器用它：复用同一个 Hub，插件也不用重连。
//
// 返回的错误和 *Hub 是同一套：插件的错误还原成 *CommandError，502 还原成 ErrDisconnected，
// 503 还原成 ErrNotConnected，调用方不用关心自己用的是哪一个。
type HubClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewHubClient 创建客户端。baseURL 形如 http://127.0.0.1:8765，token 和 Hub 的一致。
func NewHubClient(baseURL, token string) *HubClient {
	return &HubClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{}}
}

// Call 和 Hub.Call 一样。ctx 剩下的时间会作为 timeoutMs 交给 Hub，让两边在同一时刻放弃。
func (c *HubClient) Call(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error) {
	body := map[string]any{"tabId": tabID, "cmd": cmd, "args": args}
	if deadline, ok := ctx.Deadline(); ok {
		body["timeoutMs"] = max(time.Until(deadline).Milliseconds(), 1)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(ctx, http.MethodPost, "/api/browser/call", payload)
	if err != nil {
		return nil, err
	}
	if resp.OK {
		return resp.Data, nil
	}
	return nil, resp.err()
}

// Connected 问 Hub 现在有没有插件连着。Hub 本身连不上也算没连着。
func (c *HubClient) Connected() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.do(ctx, http.MethodGet, "/api/browser/status", nil)
	if err != nil || !resp.OK {
		return false
	}
	var status apiStatus
	return json.Unmarshal(resp.Data, &status) == nil && status.Connected
}

// apiResult 是 HTTP API 的响应，带上状态码，好还原错误
type apiResult struct {
	apiResponse
	status int
}

func (c *HubClient) do(ctx context.Context, method, path string, payload []byte) (*apiResult, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		// ctx 结束导致的失败，返回 ctx 自己的错误，调用方才能用 errors.Is 判断
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("browser hub unreachable: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	var out apiResult
	out.status = res.StatusCode
	if err := json.NewDecoder(res.Body).Decode(&out.apiResponse); err != nil {
		return nil, fmt.Errorf("browser hub: bad response (status %d): %w", res.StatusCode, err)
	}
	return &out, nil
}

// err 是 callError 反过来：从状态码和错误体还原出 Hub.Call 会返回的错误
func (r *apiResult) err() error {
	body := r.Error
	if body == nil {
		body = &ErrorBody{Code: "INTERNAL", Message: fmt.Sprintf("status %d without an error body", r.status)}
	}
	switch r.status {
	case http.StatusUnprocessableEntity:
		return &CommandError{Code: body.Code, Message: body.Message}
	case http.StatusServiceUnavailable:
		return ErrNotConnected
	case http.StatusBadGateway:
		return ErrDisconnected
	case http.StatusGatewayTimeout:
		return fmt.Errorf("browser hub: %s: %w", body.Message, context.DeadlineExceeded)
	default:
		return errors.New("browser hub: " + body.Code + ": " + body.Message)
	}
}
