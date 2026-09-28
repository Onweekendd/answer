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

package purchaseai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/websearch"
	"github.com/stretchr/testify/require"
)

// fakeLLM answers from a script, one reply per call, and keeps every request
// so a test can check what the loop sent.
type fakeLLM struct {
	replies []*llm.ChatResp
	err     error
	reqs    []llm.ChatReq
}

func (f *fakeLLM) Chat(_ context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	// copy the messages: the loop keeps appending to its slice after this call
	snapshot := *req
	snapshot.Messages = append([]llm.Message(nil), req.Messages...)
	f.reqs = append(f.reqs, snapshot)

	if f.err != nil {
		return nil, f.err
	}
	if len(f.replies) == 0 {
		return nil, errors.New("fakeLLM: no scripted reply left")
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	return reply, nil
}

func searchCall(id, query string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: "web_search", Arguments: `{"query":"` + query + `"}`}
}

func startMessages() []llm.Message {
	return []llm.Message{llm.Text(llm.RoleSystem, "sys"), llm.Text(llm.RoleUser, "Switch OLED 值得买吗")}
}

func Test_runAgent(t *testing.T) {
	t.Run("先搜索，再根据结果回答", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			{Content: "我先查一下", Reasoning: "需要价格", ToolCalls: []llm.ToolCall{searchCall("call_1", "Switch OLED 价格")}},
			{Content: "**买**"},
		}}
		search := &fakeSearch{results: []websearch.Result{{Title: "京东 1849 元", URL: "https://a.example"}}}
		pas := &PurchaseAIService{llmClient: model, searchProvider: search}

		// Act
		resp, err := pas.runAgent(context.Background(), startMessages())

		// Assert
		require.NoError(t, err)
		require.Equal(t, "**买**", resp.Content)
		require.Equal(t, []string{"Switch OLED 价格"}, search.Queries())

		require.Len(t, model.reqs, 2)
		require.Len(t, model.reqs[0].Tools, 1)
		require.Equal(t, "web_search", model.reqs[0].Tools[0].Name)

		// the second request carries the model's own turn back, then the result
		second := model.reqs[1].Messages
		require.Len(t, second, 4)
		assistant := second[2]
		require.Equal(t, llm.RoleAssistant, assistant.Role)
		require.Equal(t, "需要价格", assistant.Reasoning)
		require.Equal(t, "call_1", assistant.ToolCalls[0].ID)
		require.Equal(t, "我先查一下", assistant.Parts[0].Text)

		result := second[3]
		require.Equal(t, llm.RoleTool, result.Role)
		require.Equal(t, "call_1", result.ToolCallID)
		require.Contains(t, result.Parts[0].Text, "[1] 京东 1849 元")
	})

	t.Run("一轮里的多个调用都执行，结果按 id 对应", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			{ToolCalls: []llm.ToolCall{searchCall("call_1", "a"), searchCall("call_2", "b")}},
			{Content: "done"},
		}}
		pas := &PurchaseAIService{llmClient: model, searchProvider: &fakeSearch{}}

		// Act
		_, err := pas.runAgent(context.Background(), startMessages())

		// Assert
		require.NoError(t, err)
		second := model.reqs[1].Messages
		require.Len(t, second, 5)
		require.Equal(t, "call_1", second[3].ToolCallID)
		require.Equal(t, "call_2", second[4].ToolCallID)
	})

	t.Run("模型调用了不存在的工具时告诉它，而不是失败", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{
			{ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "buy_now", Arguments: `{}`}}},
			{Content: "done"},
		}}
		pas := &PurchaseAIService{llmClient: model, searchProvider: &fakeSearch{}}

		// Act
		resp, err := pas.runAgent(context.Background(), startMessages())

		// Assert
		require.NoError(t, err)
		require.Equal(t, "done", resp.Content)
		result := model.reqs[1].Messages[3]
		require.Equal(t, "call_1", result.ToolCallID)
		require.Contains(t, result.Parts[0].Text, "buy_now")
	})

	t.Run("轮数用完后不带工具再问一次，逼它作答", func(t *testing.T) {
		// Arrange: the model asks for a search every time it is allowed to
		replies := make([]*llm.ChatResp, 0, maxToolRounds+1)
		for i := 0; i < maxToolRounds; i++ {
			replies = append(replies, &llm.ChatResp{ToolCalls: []llm.ToolCall{searchCall("call", "again")}})
		}
		replies = append(replies, &llm.ChatResp{Content: "根据已有资料：再等等"})
		model := &fakeLLM{replies: replies}
		pas := &PurchaseAIService{llmClient: model, searchProvider: &fakeSearch{}}

		// Act
		resp, err := pas.runAgent(context.Background(), startMessages())

		// Assert
		require.NoError(t, err)
		require.Equal(t, "根据已有资料：再等等", resp.Content)
		require.Len(t, model.reqs, maxToolRounds+1)
		last := model.reqs[maxToolRounds]
		require.Empty(t, last.Tools)
		require.Equal(t, llm.RoleUser, last.Messages[len(last.Messages)-1].Role)
	})

	t.Run("模型调用失败时直接返回错误", func(t *testing.T) {
		// Arrange
		pas := &PurchaseAIService{llmClient: &fakeLLM{err: errors.New("provider down")}, searchProvider: &fakeSearch{}}

		// Act
		resp, err := pas.runAgent(context.Background(), startMessages())

		// Assert
		require.Error(t, err)
		require.Nil(t, resp)
	})

	t.Run("ctx 结束后不再继续请求模型", func(t *testing.T) {
		// Arrange
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		model := &fakeLLM{replies: []*llm.ChatResp{
			{ToolCalls: []llm.ToolCall{searchCall("call_1", "a")}},
			{Content: "should not be reached"},
		}}
		pas := &PurchaseAIService{llmClient: model, searchProvider: &fakeSearch{err: context.Canceled}}

		// Act
		_, err := pas.runAgent(ctx, startMessages())

		// Assert
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, model.reqs, 1)
	})
}

// barrierSearch holds every search until n of them are in flight, so it only
// finishes when the calls really run at the same time. Run one after another,
// the first call waits for the rest forever, until the test's context expires.
type barrierSearch struct {
	n int

	mu      sync.Mutex
	started int
	all     chan struct{}
}

func newBarrierSearch(n int) *barrierSearch {
	return &barrierSearch{n: n, all: make(chan struct{})}
}

func (b *barrierSearch) Search(ctx context.Context, query string, _ websearch.Options) ([]websearch.Result, error) {
	b.mu.Lock()
	b.started++
	if b.started == b.n {
		close(b.all)
	}
	b.mu.Unlock()

	select {
	case <-b.all:
		return []websearch.Result{{Title: "结果-" + query, URL: "https://example.com/" + query}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func Test_runAgent_parallelTools(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	model := &fakeLLM{replies: []*llm.ChatResp{
		{ToolCalls: []llm.ToolCall{searchCall("call_1", "a"), searchCall("call_2", "b"), searchCall("call_3", "c")}},
		{Content: "done"},
	}}
	pas := &PurchaseAIService{llmClient: model, searchProvider: newBarrierSearch(3)}

	// Act
	resp, err := pas.runAgent(ctx, startMessages())

	// Assert: all three ran at once, and each result sits after its own call
	// in the order the model asked, whatever order they finished in
	require.NoError(t, err)
	require.Equal(t, "done", resp.Content)
	second := model.reqs[1].Messages
	require.Len(t, second, 6)
	for i, query := range []string{"a", "b", "c"} {
		msg := second[3+i]
		require.Equal(t, fmt.Sprintf("call_%d", i+1), msg.ToolCallID)
		require.Contains(t, msg.Parts[0].Text, "结果-"+query)
	}
}
