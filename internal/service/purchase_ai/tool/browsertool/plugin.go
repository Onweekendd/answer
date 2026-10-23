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

package browsertool

import (
	"context"
	"slices"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

const (
	// SnapshotOmitted 替换旧快照的内容（历史里的快照也用它，见 purchaseai 的 project）
	SnapshotOmitted = "（这是之前的页面，内容已省略，以最新一份快照为准。）"

	// ScreenshotOmitted 替换 Agent 自己截的旧截图（用户上传的截图不换）
	ScreenshotOmitted = "（这是之前的截图，已省略，以最新一张截图为准。）"
)

// RunFunc 是一个工具的执行函数
type RunFunc = func(ctx context.Context, arguments string) (tool.Result, error)

// Plugin 把浏览器工具装进 Agent，并且管着它们留在对话里的东西：
//   - 工具：open navigate snapshot read screenshot click hover type press scroll，按这个顺序
//   - 每次请求模型前，旧快照换成占位语、只留最新一份；Agent 自己截的旧截图也一样，只留最新一张
//
// 换的只是这一次发给模型的消息，对话历史和日志里的原样不动
func Plugin(s *TaskState) agent.Plugin {
	return func(r *agent.Registry) {
		for _, spec := range s.Specs() {
			spec.Run = s.Track(spec.Run)
			r.Tool(spec)
		}

		var snapshots []string // 带快照的工具调用的 id，按先后
		r.AfterTool(func(_ context.Context, call llm.ToolCall, res *tool.Result) {
			if res.Snapshot {
				snapshots = append(snapshots, call.ID)
			}
		})
		r.BeforeModel(func(msgs []llm.Message) []llm.Message {
			return s.compact(msgs, snapshots)
		})
	}
}

// compact 把旧快照和旧截图换成占位语，只留最新的。msgs 是浅拷贝：整个换掉一条消息的 Parts 是安全的，
// 往 Parts 里写不是（它的底层数组和历史共用）
func (s *TaskState) compact(msgs []llm.Message, snapshots []string) []llm.Message {
	var old []string
	if len(snapshots) > 1 {
		old = snapshots[:len(snapshots)-1]
	}
	var shots []int // 截图消息的下标
	for i, m := range msgs {
		switch {
		case m.Role == llm.RoleTool && slices.Contains(old, m.ToolCallID):
			msgs[i].Parts = []llm.Part{{Text: SnapshotOmitted}}
		case m.Tag == agent.TagScreenshot:
			shots = append(shots, i)
		}
	}
	if len(shots) > 1 {
		for _, i := range shots[:len(shots)-1] {
			msgs[i].Parts = []llm.Part{{Text: ScreenshotOmitted}}
		}
	}
	return msgs
}

// Track 包一个要看页面状态的工具：执行之前记下当时的页面状态（State，循环检测用），
// 执行之后如果换了页面（open、navigate、点开了新页面），页面状态跟着网址换。
// 不一定拍快照，所以不能只看快照：在几个网页上各 read 一次，指纹都是「read@上一份快照」，会被当成原地打转
// （2026-10-21 成都案例：熊猫基地官网、秘塔、必应、官网票务页各读一次，第 5 次 read 就被收尾）
func (s *TaskState) Track(run RunFunc) RunFunc {
	return func(ctx context.Context, arguments string) (tool.Result, error) {
		before := s.PageHash
		res, err := run(ctx, arguments)
		res.State = before
		if res.PageChanged && res.URL != "" {
			s.PageHash = PageState("url:"+res.URL, 0)
		}
		return res, err
	}
}
