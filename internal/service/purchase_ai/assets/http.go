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
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/apache/answer/internal/service/llm"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewHandler serves both machine-facing HTTP and Streamable HTTP MCP. The
// bearer token identifies one trusted application, not individual end users.
func NewHandler(s *Service, token string) (http.Handler, error) {
	if len(token) < 16 {
		return nil, errors.New("ASSET_API_TOKEN must contain at least 16 characters")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/assets/tasks", func(w http.ResponseWriter, r *http.Request) {
		var req Request
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid task request"})
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			writeJSON(w, 400, map[string]string{"error": "expected one JSON object"})
			return
		}
		if err := req.normalize(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		j, err := s.Create(req)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Location", "/api/assets/tasks/"+j.ID)
		writeJSON(w, 202, j)
	})
	mux.HandleFunc("GET /api/assets/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		j, err := s.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("POST /api/assets/tasks/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		j, err := s.Cancel(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, j)
	})
	mcpServer, err := newMCPServer(s)
	if err != nil {
		return nil, err
	}
	mux.Handle("/mcp", server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true), server.WithDisableStreaming(true)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" {
			writeJSON(w, 403, map[string]string{"error": "web page origins are not allowed"})
			return
		}
		value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(value), []byte(token)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func writeError(w http.ResponseWriter, err error) {
	status, message := 500, "asset service operation failed"
	switch {
	case errors.Is(err, ErrNotFound):
		status, message = 404, err.Error()
	case errors.Is(err, ErrConflict):
		status, message = 409, err.Error()
	case errors.Is(err, ErrCapacity), errors.Is(err, ErrClosed):
		status, message = 503, err.Error()
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func mcpResult(value any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := mcp.NewToolResultText(string(raw))
	result.StructuredContent = value
	return result, nil
}

func newMCPServer(s *Service) (*server.MCPServer, error) {
	m := server.NewMCPServer("Answer Media Agent", "0.1.0", server.WithToolCapabilities(false))
	schema, err := llm.SchemaFor(Request{})
	if err != nil {
		return nil, err
	}
	m.AddTool(mcp.NewToolWithRawSchema("create_asset_task", "Search, select, download, validate and upload third-party video or image assets using the internal agent. Returns a queued task immediately. Poll get_asset_task for actual uploaded assets. Video search currently supports Douyin; images can be collected from supplied source pages. Defaults: count=1, min_short_side=1080, max_duration_seconds=120. project_id scopes storage keys; request_id provides idempotency within that project. Storage credentials are configured server-side.", schema), func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var req Request
		if err := request.BindArguments(&req); err != nil {
			return mcp.NewToolResultError("invalid task request"), nil
		}
		j, err := s.Create(req)
		return mcpResult(j, err)
	})
	for _, name := range []string{"get_asset_task", "cancel_asset_task"} {
		read := name == "get_asset_task"
		description := "Cancel queued or running work. Already uploaded objects are retained and reported."
		if read {
			description = "Get progress or final assets, with refreshed private S3 access URLs. completed means the requested count was actually uploaded. input_required needs human login/verification then a new task. partial retains successful assets. Poll every 3-5 seconds."
		}
		m.AddTool(mcp.NewTool(name, mcp.WithDescription(description), mcp.WithString("task_id", mcp.Required()), mcp.WithReadOnlyHintAnnotation(read)), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, _ := request.GetArguments()["task_id"].(string)
			var j Job
			var err error
			if read {
				j, err = s.Get(ctx, id)
			} else {
				j, err = s.Cancel(id)
			}
			return mcpResult(j, err)
		})
	}
	return m, nil
}
