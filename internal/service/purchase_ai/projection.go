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
	"github.com/apache/answer/internal/service/llm"
)

// unfinishedCallText 是补给半截工具调用的结果：上一轮在这一步之前断开了（用户关了页面、进程重启），
// 日志里只有调用、没有结果。服务商要求每个调用后面都跟着它的结果，不补的话整段对话会被拒绝
const unfinishedCallText = "这一步没有做完：上一轮在这里中断了。"

// project 把一条分支上的日志变成这一轮发给模型的历史（上下文管理概念 3）。纯函数：
// 输入一串条目，输出一串消息，不碰数据库、不调模型，所有规则都能用表驱动测试覆盖。
// 不含系统提示词（每轮现做，里面有今天的日期），也不含这一轮新的用户消息（Chat 自己接上）。
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
// TODO(精华，你来写)，按这个顺序：
//  1. 只要 Kind 为 EntryMessage 的条目，别的种类跳过。Details 不用管：Message() 不带它，模型永远看不到页面的东西
//  2. 快照只留整条分支上最新的一份：ToolName 为 toolSnapshot 的 tool 消息，除了最后一条，
//     其余的 Parts 整个换成 []llm.Part{{Text: snapshotOmitted}}（和循环里 compactSnapshots 一样，扩展到跨轮次）。
//     提示：先扫一遍找到最后一份快照的下标，再还原消息
//  3. 旧回答里的图引用改成纯文字：每一轮的图都从「图1」重新编号，上一轮回答里的 [图3：…] 留着，
//     模型这一轮就可能引用一个不存在的图 3。assistant 消息的文字（Parts 里的 Text）和
//     done 调用的参数（ToolCalls 里 Name 为 toolDone 的 Arguments，回答在里面）都过一遍 plainShotRefs
//  4. 补上半截的工具调用：一条带 ToolCalls 的 assistant 消息后面，紧跟着的 tool 消息要把每个调用的 ID 都答到
//     （ToolCallID 对得上）。缺哪个，就在这些 tool 消息之后、下一条非 tool 消息之前，补一条
//     toolMessage(缺的 ID, unfinishedCallText)。日志的最后一条正好是带调用的 assistant 时也要补
//  5. 其余原样：reasoning 要带着（deepseek 在有工具的对话里要原样回传），Synthetic 的提醒也留着
//
// 思考题：第 4 步为什么要补「结果」，而不是干脆把那条半截的 assistant 消息删掉？想想它的 Reasoning 和
// 它之前已经做完的那几个调用
func project(path []Entry) []llm.Message {
	return nil
}
