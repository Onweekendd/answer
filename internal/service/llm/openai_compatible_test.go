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

package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// wire marshals what buildRequest produces and decodes it back into plain maps,
// so the assertions are about the JSON the provider receives, omitempty and all.
func wire(t *testing.T, req *ChatReq) map[string]any {
	t.Helper()
	raw, err := json.Marshal((&openAICompatibleClient{}).buildRequest(req, "test-model"))
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func Test_buildRequest_tools(t *testing.T) {
	t.Run("工具的参数 schema 以 JSON 对象发出，而不是字符串", func(t *testing.T) {
		// Arrange
		req := &ChatReq{
			Messages: []Message{Text(RoleUser, "hi")},
			Tools: []Tool{{
				Name:        "web_search",
				Description: "search the web",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
			}},
		}

		// Act
		body := wire(t, req)

		// Assert
		tools := body["tools"].([]any)
		require.Len(t, tools, 1)
		tool := tools[0].(map[string]any)
		require.Equal(t, "function", tool["type"])
		fn := tool["function"].(map[string]any)
		require.Equal(t, "web_search", fn["name"])
		require.IsType(t, map[string]any{}, fn["parameters"])
	})

	t.Run("没有工具时请求和以前一样", func(t *testing.T) {
		// Arrange
		req := &ChatReq{Messages: []Message{Text(RoleUser, "hi")}}

		// Act
		body := wire(t, req)

		// Assert
		require.NotContains(t, body, "tools")
		msg := body["messages"].([]any)[0].(map[string]any)
		require.Equal(t, map[string]any{"role": "user", "content": "hi"}, msg)
	})
}

func Test_toSDKMessages_toolRound(t *testing.T) {
	// Arrange: the second request of a tool loop
	assistant := Text(RoleAssistant, "let me look that up")
	assistant.Reasoning = "need a price first"
	assistant.ToolCalls = []ToolCall{
		{ID: "call_1", Name: "web_search", Arguments: `{"query":"a"}`},
		{ID: "call_2", Name: "web_search", Arguments: `{"query":"b"}`},
	}
	result1 := Text(RoleTool, "result a")
	result1.ToolCallID = "call_1"
	result2 := Text(RoleTool, "result b")
	result2.ToolCallID = "call_2"

	// Act
	out := toSDKMessages([]Message{Text(RoleUser, "q"), assistant, result1, result2})

	// Assert
	require.Len(t, out, 4)

	require.Equal(t, "need a price first", out[1].ReasoningContent)
	require.Equal(t, "let me look that up", out[1].Content)
	require.Len(t, out[1].ToolCalls, 2)
	require.Equal(t, "call_2", out[1].ToolCalls[1].ID)
	require.Equal(t, "web_search", out[1].ToolCalls[1].Function.Name)
	require.Equal(t, `{"query":"b"}`, out[1].ToolCalls[1].Function.Arguments)

	require.Equal(t, RoleTool, out[2].Role)
	require.Equal(t, "call_1", out[2].ToolCallID)
	require.Equal(t, "call_2", out[3].ToolCallID)
}
