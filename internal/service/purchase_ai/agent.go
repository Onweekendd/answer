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
	"sync"

	"github.com/apache/answer/internal/service/llm"
	"github.com/segmentfault/pacman/log"
)

// maxToolRounds caps how many times the model may ask for tools in one
// analysis. Each round resends the whole conversation and waits on the
// provider again, so it bounds both the cost and the time of an analysis.
// When the rounds run out the model gets one more call, without tools, to
// answer from what it has.
//
// The ceiling is the queue's three minute timeout per analysis: a round is
// roughly a model call (up to ~7s) plus a page read (up to ~12s), so six rounds
// and the closing call fit, and eight could time out and lose them all.
const maxToolRounds = 6

// outOfRoundsPrompt closes the loop when the model is still asking for tools.
const outOfRoundsPrompt = "搜索次数已用完。请根据已有资料作答，查不到的信息直接说明未能核实。"

// runAgent lets the model call tools until it answers, and returns that answer.
//
// Every tool call gets a tool message back, even one for a tool that does not
// exist, because the provider rejects a conversation with an unanswered call.
// Only a tool's error stops the loop, and tools return one only when the
// context is done; everything the model can act on comes back as text.
func (pas *PurchaseAIService) runAgent(ctx context.Context, messages []llm.Message) (*llm.ChatResp, error) {
	tools, err := pas.newAgentRun().tools()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]agentTool, len(tools))
	defs := make([]llm.Tool, 0, len(tools))
	for _, tool := range tools {
		byName[tool.def.Name] = tool
		defs = append(defs, tool.def)
	}

	for round := 0; round < maxToolRounds; round++ {
		resp, err := pas.llmClient.Chat(ctx, &llm.ChatReq{Messages: messages, Tools: defs})
		if err != nil {
			return nil, err
		}
		if len(resp.ToolCalls) == 0 {
			return resp, nil
		}

		// the model's own turn goes back first, then one result per call
		assistantMsg := llm.Text(llm.RoleAssistant, resp.Content)
		assistantMsg.ToolCalls = resp.ToolCalls
		assistantMsg.Reasoning = resp.Reasoning
		messages = append(messages, assistantMsg)

		// the calls of one round were chosen together, before any result was
		// known, so none depends on another and they can all run at once. Each
		// goroutine writes only its own index, so the slices need no lock, and
		// the results go back in the order the model asked for them.
		calls := resp.ToolCalls
		results := make([]string, len(calls))
		errs := make([]error, len(calls))
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Go(func() {
				results[i], errs[i] = runTool(ctx, byName, call)
			})
		}
		wg.Wait()

		for i, call := range calls {
			if errs[i] != nil {
				return nil, errs[i]
			}
			log.Debugf("purchase ai: round %d, ran %s", round, call.Name)

			toolMsg := llm.Text(llm.RoleTool, results[i])
			toolMsg.ToolCallID = call.ID
			messages = append(messages, toolMsg)
		}
	}

	messages = append(messages, llm.Text(llm.RoleUser, outOfRoundsPrompt))
	return pas.llmClient.Chat(ctx, &llm.ChatReq{Messages: messages})
}

// runTool runs one call. A name the model made up is answered, not failed, so
// the model can correct itself.
func runTool(ctx context.Context, byName map[string]agentTool, call llm.ToolCall) (string, error) {
	tool, ok := byName[call.Name]
	if !ok {
		log.Warnf("purchase ai: model called unknown tool %q", call.Name)
		return "没有这个工具：" + call.Name, nil
	}
	return tool.run(ctx, call.Arguments)
}
