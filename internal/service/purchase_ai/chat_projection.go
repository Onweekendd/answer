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
	"slices"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
)

// unfinishedCallText 是补给半截工具调用的结果：上一轮在这一步之前断开了（用户关了页面、进程重启），
// 日志里只有调用、没有结果。服务商要求每个调用后面都跟着它的结果，不补的话整段对话会被拒绝
const unfinishedCallText = "这一步没有做完：上一轮在这里中断了。"

// project 把一条分支上的日志变成这一轮发给模型的历史（上下文管理概念 3）。纯函数：
// 输入一串条目，输出一串消息，不碰数据库、不调模型，所有规则都能用表驱动测试覆盖。
// 不含系统提示词（每轮现做，里面有今天的日期），也不含这一轮新的用户消息（Chat 自己接上）。
// 一轮只调一次：path 里全是已经结束的轮次，这一轮新产生的消息由 Agent 循环在内存里往后追加，不经过这里。
//
// 上下文：
//   - 谁调用：Chat 开始一轮时，先 ChatLog.Entries 取出这个对话的全部条目，pathTo 走出请求指定的那条分支，
//     再交给这里。返回的消息接在系统提示词后面、这一轮的用户消息前面，交给 Agent 循环
//   - path 里是什么：从根到叶，按时间先后。现在每条都是 EntryMessage（Message 不为 nil，一条模型消息）；
//     以后会有别的种类（第二批的压缩摘要），所以还是按 Kind 认。一轮典型的样子：
//     user「那 bose 的呢」→ assistant(tool_calls: open) → tool(ToolName open) → assistant(tool_calls: snapshot)
//     → tool(ToolName snapshot，8000 字) → user(Synthetic，「这是第 3 步截的图」+ 图片占位语) → … → assistant(调用 done) → tool(done 的结果)
//   - 截图在写日志时（storeParts）就已经换成了 imageOmitted，这里不用管；用户上传的图是网址，原样留着，
//     Chat 发给模型前会把网址换成 data URI
//   - 消息怎么还原：e.Message.Message() 得到 llm.Message
//
// 规则，按这个顺序：
//  1. 只要 Kind 为 EntryMessage 的条目，别的种类跳过。Details 不用管：Message() 不带它，模型永远看不到页面的东西
//  2. 快照全部换成占位：ToolName 为 toolSnapshot 的 tool 消息，Parts 整个换成 []llm.Part{{Text: snapshotOmitted}}。
//     每一轮结束时 Chat 关掉这一轮开的标签页，历史里的快照描述的页面都已经不在了，模型要看页面得重新打开。
//     不留「最后一份」还为了前缀缓存：一份快照在它那一轮是全文、下一轮变成占位，变的地方之后都不命中；
//     全部换掉，变的只是刚结束那一轮的快照，前面的历史每一轮都一样
//  3. 旧回答里的图引用改成纯文字：每一轮的图都从「图1」重新编号，上一轮回答里的 [图3：…] 留着，
//     模型这一轮就可能引用一个不存在的图 3。assistant 消息的文字（Parts 里的 Text）和
//     done 调用的参数（ToolCalls 里 Name 为 toolDone 的 Arguments，回答在里面）都过一遍 plainShotRefs
//  4. 补上半截的工具调用：一条带 ToolCalls 的 assistant 消息后面，紧跟着的 tool 消息要把每个调用的 ID 都答到
//     （ToolCallID 对得上）。缺哪个，就在这些 tool 消息之后、下一条非 tool 消息之前，补一条
//     toolMessage(缺的 ID, unfinishedCallText)。日志的最后一条正好是带调用的 assistant 时也要补
//  5. 其余原样：reasoning 要带着（deepseek 在有工具的对话里要原样回传），Synthetic 的提醒也留着
//
// 第 4 步补「结果」，而不是删掉那条半截的 assistant：它的 Reasoning 要原样带回去，同一条里已经做完的调用
// 也有结果，删了就得连它们一起删。pi 的 transformMessages 也是这么做的（插一条 "No result provided"）
func project(path []Entry) []llm.Message {
	res := make([]llm.Message, 0, len(path))

	// waiting 是欠条：上一条 assistant 发起了、还没见到结果的调用 ID，按调用的顺序（第 4 步）
	var waiting []string
	// settle 还欠条：给每个还欠着结果的调用补一条结果，追加到 res，然后清空 waiting
	settle := func() {
		for _, id := range waiting {
			res = append(res, toolMessage(id, unfinishedCallText))
		}
		waiting = nil
	}

	for _, e := range path {
		if e.Kind != EntryMessage {
			continue
		}

		m := e.Message.Message()
		// 快照换成占位语：页面在那一轮结束时已经关了。整个换掉 Parts
		if e.Message.ToolName == toolSnapshot {
			m.Parts = []llm.Part{{Text: browsertool.SnapshotOmitted}}
		}

		if m.Role == llm.RoleAssistant {
			for i := range m.Parts {
				m.Parts[i].Text = plainShotRefs(m.Parts[i].Text)
			}
			for i := range m.ToolCalls {
				if m.ToolCalls[i].Name == toolDone {
					m.ToolCalls[i].Arguments = plainShotRefs(m.ToolCalls[i].Arguments) // done 的参数
				}
			}
		}

		// 第 4 步：记欠条、还欠条。要在 append(m) 之前：补上的结果得排在 m 前面
		if m.Role == llm.RoleTool {
			// 见到结果：这个调用不欠了
			waiting = slices.DeleteFunc(waiting, func(toolCallID string) bool {
				return toolCallID == m.ToolCallID
			})
		} else {
			settle() // 下一条不是 tool 了：还欠着的先补上
			if m.Role == llm.RoleAssistant {
				// 发起调用：先全部记为欠着，见到结果再划掉
				for _, toolCall := range m.ToolCalls {
					waiting = append(waiting, toolCall.ID)
				}
			}
		}
		res = append(res, m)
	}
	settle() // 最后一条是带调用的 assistant、结果一个都没有时，也要补

	return res
}

// toolMessage 是一条工具回复
func toolMessage(callID, text string) llm.Message {
	msg := llm.Text(llm.RoleTool, text)
	msg.ToolCallID = callID
	return msg
}
