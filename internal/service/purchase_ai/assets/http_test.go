// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information regarding
// copyright ownership. The ASF licenses this file to You under the
// Apache License, Version 2.0 (the "License"); you may not use this
// file except in compliance with the License. You may obtain a copy
// at http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

package assets

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const testToken = "test-token-at-least-16-characters"

func testHandler(t *testing.T) (*Service, http.Handler) {
	t.Helper()
	s, err := NewService(t.TempDir(), func(ctx context.Context, _ Job, _ func(Asset) error, _ func(string)) (Outcome, error) {
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}, &memoryStore{})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	h, err := NewHandler(s, testToken)
	require.NoError(t, err)
	return s, h
}

func serve(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHTTPAccessAndInput(t *testing.T) {
	_, h := testHandler(t)
	require.Equal(t, 401, serve(h, "GET", "/health", "", "wrong").Code)
	r := httptest.NewRequest("GET", "/health", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "https://untrusted.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code)
	for _, body := range []string{`{"project_id":"../escape","kind":"video","query":"x"}`, `{"project_id":"p","kind":"video","query":"x","count":11}`, `{"project_id":"p","kind":"audio","query":"x"}`, `{"project_id":"p","kind":"video","query":"x","s3_secret":"leak"}`, `{} {}`} {
		require.Equal(t, 400, serve(h, "POST", "/api/assets/tasks", body, testToken).Code, body)
	}
	w = serve(h, "POST", "/api/assets/tasks", `{"project_id":"p","kind":"video","query":"x","request_id":"same"}`, testToken)
	require.Equal(t, 202, w.Code)
	var job Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &job))
	require.Equal(t, 200, serve(h, "GET", "/api/assets/tasks/"+job.ID, "", testToken).Code)
	require.Equal(t, 409, serve(h, "POST", "/api/assets/tasks", `{"project_id":"p","kind":"video","query":"changed","request_id":"same"}`, testToken).Code)
	require.Equal(t, 404, serve(h, "GET", "/api/assets/tasks/missing", "", testToken).Code)
}

func TestMCPStructuredResultsAndCancellation(t *testing.T) {
	s, h := testHandler(t)
	w := serve(h, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, testToken)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "create_asset_task")
	require.Contains(t, w.Body.String(), "get_asset_task")
	w = serve(h, "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_asset_task","arguments":{"project_id":"p","kind":"video","query":"x"}}}`, testToken)
	require.Equal(t, 200, w.Code)
	var reply struct {
		Result struct {
			Structured Job `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply))
	require.NotEmpty(t, reply.Result.Structured.ID)
	id := reply.Result.Structured.ID
	for _, name := range []string{"get_asset_task", "cancel_asset_task"} {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]string{"task_id": id}}})
		w = serve(h, "POST", "/mcp", string(body), testToken)
		require.Equal(t, 200, w.Code)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply))
		require.Equal(t, id, reply.Result.Structured.ID)
	}
	j, err := s.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, Canceled, j.Status)
}
