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

package guard

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 插件放进真的核心里跑一遍：每一轮模型都再要一次同一个工具，看它们什么时候出手

type model struct {
	reqs []llm.ChatReq
	call llm.ToolCall
}

func (m *model) Chat(_ context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	snapshot := *req
	snapshot.Messages = append([]llm.Message(nil), req.Messages...)
	m.reqs = append(m.reqs, snapshot)
	if len(req.Tools) == 0 {
		return &llm.ChatResp{Content: "收尾的回答"}, nil
	}
	return &llm.ChatResp{ToolCalls: []llm.ToolCall{m.call}}, nil
}

type noArgs struct{}

func run(t *testing.T, res tool.Result, maxSteps int, plugins ...agent.Plugin) (*agent.Result, *model) {
	t.Helper()
	m := &model{call: tc("c", "look", `{}`)}
	look := func(r *agent.Registry) {
		r.Tool(tool.Spec{Name: "look", Description: "x", Args: noArgs{}, Run: func(context.Context, string) (tool.Result, error) {
			return res, nil
		}})
	}
	a, err := agent.New(agent.Config{
		Model: m, MaxSteps: maxSteps, Timeouts: agent.Timeouts{Run: time.Second, Model: time.Second},
		Plugins: append([]agent.Plugin{look}, plugins...),
	})
	require.NoError(t, err)
	out, err := a.Run(context.Background(), []llm.Message{llm.Text(llm.RoleUser, "q")})
	require.NoError(t, err)
	return out, m
}

func texts(m *model, req int) []string {
	var out []string
	for _, msg := range m.reqs[req].Messages {
		if msg.Role == llm.RoleUser && len(msg.Parts) > 0 {
			out = append(out, msg.Parts[0].Text)
		}
	}
	return out
}

func TestFailures_FinishesAfterTheLimit(t *testing.T) {
	// Act
	res, m := run(t, tool.Result{Failed: true}, 10, Failures(3))

	// Assert
	assert.Equal(t, ReasonFailures, res.Finish)
	assert.Len(t, res.Steps, 3)
	assert.Contains(t, texts(m, len(m.reqs)-1)[len(texts(m, len(m.reqs)-1))-1], "连续失败")
}

func TestLoops_NudgesThenFinishes(t *testing.T) {
	// Act
	res, m := run(t, tool.Result{State: "same page"}, 10, Loops(10, 3, 5))

	// Assert：第 3 次提醒，第 5 次收尾
	assert.Equal(t, ReasonLoop, res.Finish)
	assert.Len(t, res.Steps, 5)
	nudges := 0
	for _, text := range texts(m, len(m.reqs)-1) {
		if strings.HasPrefix(text, "你已经") {
			nudges++
		}
	}
	assert.Equal(t, 1, nudges, "只提醒一次")
}

func TestLoops_DifferentStateIsNotALoop(t *testing.T) {
	// Arrange：每一步页面状态都不一样
	n := 0
	m := &model{}
	look := func(r *agent.Registry) {
		r.Tool(tool.Spec{Name: "look", Description: "x", Args: noArgs{}, Run: func(context.Context, string) (tool.Result, error) {
			n++
			return tool.Result{State: fmt.Sprint(n)}, nil
		}})
	}
	m.call = tc("c", "look", `{}`)
	a, err := agent.New(agent.Config{Model: m, MaxSteps: 8, Timeouts: agent.Timeouts{Run: time.Second, Model: time.Second},
		Plugins: []agent.Plugin{look, Loops(10, 3, 5)}})
	require.NoError(t, err)

	// Act
	res, err := a.Run(context.Background(), []llm.Message{llm.Text(llm.RoleUser, "q")})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, agent.ReasonSteps, res.Finish, "只会因为步数用完而收尾")
}

func TestWarn_SaysItOnceWhenTheStepsAreAlmostGone(t *testing.T) {
	// Act
	res, m := run(t, tool.Result{}, 6, Warn(3))

	// Assert：6 步，第 2 步结束后还剩 3 步
	assert.Equal(t, agent.ReasonSteps, res.Finish)
	warned := 0
	for _, text := range texts(m, len(m.reqs)-1) {
		if strings.HasPrefix(text, "只剩 ") {
			warned++
		}
	}
	assert.Equal(t, 1, warned)
	assert.Len(t, texts(m, 3), len(texts(m, 2))+1, "提醒是第 3 次请求时多出来的那一条")
}

func TestOrder_FailuresWinsOverLoops(t *testing.T) {
	// Arrange：失败的同一个动作重复：连续失败 3 次和重复 3 次同时出现
	res, m := run(t, tool.Result{Failed: true, State: "s"}, 10, Failures(3), Loops(10, 3, 5))

	// Assert：Failures 先被问，收尾了，Loops 的提醒不会出现
	assert.Equal(t, ReasonFailures, res.Finish)
	for _, text := range texts(m, len(m.reqs)-1) {
		assert.NotContains(t, text, "你已经在同一个页面状态下")
	}
}
