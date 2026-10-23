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
	"time"

	"github.com/apache/answer/internal/service/llm"
)

// TaskResult 是 RunTask 的结果，字段和 agentResult 一样，只是导出给包外的脚本用
type TaskResult struct {
	Text    string
	Success bool
	Finish  string // 空表示模型自己结束的；否则是收尾的原因：steps、failures、loop、browser_gone
	Steps   []TaskStep
	// Products 是做好的商品卡片。case 运行器没有存图的地方，卡片都没有图
	Products []ProductCard
	// Artifacts 是交付的网页，网址是 WithArtifactStore 给的那个存储返回的
	Artifacts []Artifact
}

// TaskStep 是执行过的一次工具调用
type TaskStep struct {
	Step      int
	Tool      string
	Arguments string
	Failed    bool
	URL       string
	Duration  time.Duration
}

// TaskOption 调整 RunTask 的一次运行
type TaskOption func(*agentRun)

// WithWorkspaces 让 Agent 有工作台（沙箱），ws 为 nil 时不给。RunTask 没有对话，
// 用的是临时沙箱（chatID 0）：不记进数据库，闲置以后 Cube 自己销毁
func WithWorkspaces(ws Workspaces) TaskOption {
	return func(r *agentRun) { r.Workspaces = ws }
}

// WithArtifactStore 让 deliver 能把网页存下来。不给的话 deliver 会告诉模型存不了
func WithArtifactStore(store ArtifactStore) TaskOption {
	return func(r *agentRun) { r.artifactStore = store }
}

// RunTask 不经过帖子，直接让 Agent 做一个任务：系统提示词和 Analyze 用的一样，用户消息就是 task。
// 给 case 运行器和手动试跑用，它们在 answer run 外面，自己带模型和浏览器。
func RunTask(ctx context.Context, client llm.Client, b Browser, task string, opts ...TaskOption) (*TaskResult, error) {
	browserOn := b.Connected()
	messages := []llm.Message{
		llm.Text(llm.RoleSystem, buildSystemPrompt(time.Now(), browserOn)),
		llm.Text(llm.RoleUser, task),
	}

	r := newAgentRun(client, b, defaultLimits)
	for _, opt := range opts {
		opt(r)
	}
	// 和对话一样：有工作台才告诉模型工作台的规矩（chatPrompt）
	if r.Workspaces != nil {
		messages[0] = llm.Text(llm.RoleSystem, buildSystemPrompt(time.Now(), browserOn)+"\n\n"+workspaceRules)
	}
	defer r.CloseTabs()
	res, err := r.run(ctx, messages, browserOn)
	if err != nil {
		return nil, err
	}

	// 判卷看的是回答本身，后续建议是给页面的按钮
	answer, _ := splitFollowups(res.Text)
	out := &TaskResult{Text: answer, Success: res.Success, Finish: string(res.Finish), Products: res.Products, Artifacts: r.artifacts}
	for _, s := range res.Steps {
		out.Steps = append(out.Steps, TaskStep(s))
	}
	return out, nil
}
