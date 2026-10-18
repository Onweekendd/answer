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

// RunTask 不经过帖子，直接让 Agent 做一个任务：系统提示词和 Analyze 用的一样，用户消息就是 task。
// 给 case 运行器和手动试跑用，它们在 answer run 外面，自己带模型和浏览器。
func RunTask(ctx context.Context, client llm.Client, b Browser, task string) (*TaskResult, error) {
	browserOn := b.Connected()
	messages := []llm.Message{
		llm.Text(llm.RoleSystem, buildSystemPrompt(time.Now(), browserOn)),
		llm.Text(llm.RoleUser, task),
	}

	r := newAgentRun(client, b, defaultLimits)
	defer r.closeTabs()
	res, err := r.run(ctx, messages, browserOn)
	if err != nil {
		return nil, err
	}

	out := &TaskResult{Text: res.Text, Success: res.Success, Finish: string(res.Finish)}
	for _, s := range res.Steps {
		out.Steps = append(out.Steps, TaskStep(s))
	}
	return out, nil
}
