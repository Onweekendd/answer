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

package agent

// 这个文件是循环，由你来写（先写完 plugin.go 的钩子签名）。
//
// 目标：循环读下来就是「调模型 → 执行工具 → 反馈 → 继续或结束」，大约 60～80 行，
// 里面只出现钩子的调用，不出现商品、图、产物、草稿、followups、浏览器。
//
// 现在的循环是 ../agent.go 的 loop()（约 140 行，步骤已经标了 ①～⑨）。下面是每一步在新结构里的去处。
//
//	现在                                       新结构
//	────────────────────────────────────────  ──────────────────────────────────────────────
//	开始：tools(browserOn) 收集工具             遍历插件，调 Plugin(registry)，收集 Tool
//	messages = slices.Clone(messages)         保留：循环只改自己的数组（原因看现在 agent.go 的注释）
//	① compactSnapshots / compactScreenshots   BeforeModel 钩子（返回值只用于这次请求）
//	② r.chat（超时、超时重试一次、发思考事件）   核心自己的 chat 方法：照搬 ../agent.go 的 chat()，Test_chat 一起搬
//	③ 分类回答/空回复/工具调用                   核心。是最终回答 → OnAnswer 钩子，再记进日志
//	④ runCalls：顺序执行、跳过规则、发事件       核心。每个工具执行完调 AfterTool 钩子
//	⑤ 回复一一对应，截图放整批回复之后            核心（这是消息顺序的不变量，只在一处维护）
//	⑥ 插件断开 → finish                        核心。见下面的问题 1
//	done → 返回                               核心。见下面的问题 2
//	⑦⑧⑨ 失败/重复/步数提醒                     EndOfTurn 钩子
//	循环耗尽 → finish(finishSteps)             核心
//	run() 里带上 products、shots                OnFinish 钩子
//
// 核心还要依赖两个接口（也由你来定它们的形状，名字随意）：
//   - 记录器：现在是 agentRun.keep(ctx, messages, msg, meta)，调 chatRecorder.record。
//     核心不认识对话日志，只认「每加一条消息就告诉它一下」的接口。chat_recorder.go 里的 chatRecorder 去实现它。
//   - 观察者：现在是 agentRun.emit* 发的 tool_start、tool_end、reasoning。核心发出去，Chat 入口接着推给页面。
//     注意 describeCall（agent_events.go，20 个 case，认识每一个工具）要变成每个工具自己的
//     tool.Spec.Describe func(arguments string) string，这件事我来做，你只需要在观察者里留一个收「这次调用的一句话」的地方。
//
// 需要你拍板的问题：
//
//  1. 插件断开（tool.ErrBrowserGone）怎么表达？
//     现在循环认识这个具体的错误：这一轮每个调用都先有回复，然后走 finishBrowserGone 收尾。
//     核心不该认识「浏览器」。建议：tool 包里的这个错误换成通用的 tool.ErrAbort{Reason}，
//     核心遇到它就「本轮收完、走收尾，原因是 Reason」。浏览器工具返回它，核心不用知道是浏览器。
//
//  2. done 怎么办？
//     建议：done 是核心自己注册的工具（它的参数只有 text 和 success，followups 参数不要了）。
//     它的结果带 Terminate 标记，这一批里全部带标记 → 返回（pi 的 terminate 就是这个）。
//     根包现在三处特判 done：循环、runCalls（skippedAfterDoneText）、chat_projection.go。
//
//  3. 第 ③ 步要提前知道「这是不是最终回答」。
//     原因：日志只追加，回答引用的图要在记进日志「之前」存好、一起记下。
//     现在的写法是 answer := len(resp.ToolCalls)==0 && !empty && (!browserOn || reminded || 最后一步)。
//     新结构里 browserOn（插件有没有装 browser）核心不知道，建议改成「没有任何工具时，直接回答就是最终回答」，
//     「已经提醒过 done」仍然是核心自己的状态。
//
// 写完告诉我。我会：把 agent_test.go 里 Test_run、Test_chat 搬到这个包（或让根包的入口通过新核心跑），
// 它们就是安全网——不变的行为都在那里面。
