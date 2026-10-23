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

// Package agent 是 Agent 的核心：调模型、执行工具、把结果交回模型、决定继续还是停。
// 它不认识浏览器、沙箱、商品卡片、产物、草稿，也不认识 followups：这些都是插件（Plugin）。
//
// 这个设计照着根目录 pi/ 里的 pi agent 来：
//
//	pi/packages/agent/src/agent-loop.ts              循环，只有「消息、工具、模型」，定制全靠回调
//	pi/packages/coding-agent/src/core/extensions/    插件系统（runner.ts 是多个插件怎么叠加）
//	pi/packages/coding-agent/src/extensions/         内置功能（mcp、tool-search……）也是插件，不写进核心
//
// # 一个插件是什么
//
// 一个函数。核心把注册器（Registry）交给它，它想要什么就自己注册：
//
//	func Browser(s *browsertool.TaskState) agent.Plugin {
//	    return func(r *agent.Registry) {
//	        r.Tool(...)                 // 我有这些工具
//	        r.BeforeModel(...)          // 每次请求模型前，我要把旧快照换成占位语
//	    }
//	}
//
// 插件自己的状态放在这个函数的闭包里，不放在核心的任何对象上。
// 所以核心里不会出现 products、artifacts、seenLinks 这样的字段。
//
// # 钩子：核心在固定的时机调用插件
//
//	Tool         收集工具。注册顺序就是发给模型的工具顺序（前缀缓存靠它稳定）
//	AfterTool    一个工具执行完                                 链接核对、记下快照
//	BeforeModel  每次请求模型前，改这一次要发的消息（不改历史）      旧快照、旧截图换成占位语
//	OnAnswer     最终回答出来时，可以改文字、附带产出              存回答引用的图、切掉 <followups>
//	EndOfTurn    一轮结束，返回「追加提醒」或「走收尾」            步数、连续失败、重复动作、交付提醒
//
// 多个插件挂在同一个钩子上：按注册顺序依次调用，所以装配列表的顺序是规则。
// EndOfTurn 里每个插件的提醒都追加，第一个要求收尾的赢，排在它后面的不再被问。
// 钩子里 panic 不接：插件的 bug 应该让这次任务出错，而不是被悄悄吞掉（入口的 goroutine 会接住）。
//
// # 新增一个能力要改哪里
//
//   - 一个新工具：写一个返回 Plugin 的函数，里面 r.Tool(...)；在 purchaseai.newTask 的插件列表里加一行，位置就是它在工具定义里的位置
//   - 一个新的上限或提醒：在 guard 包里写一个 EndOfTurn 插件，在 newTask 里按优先级放进去
//   - 回答出来时要处理一下（切块、存图）：OnAnswer
//   - 每次请求前要整理消息（压缩、丢旧的）：BeforeModel，只改这次要发的，不改历史
//   - 要知道每个工具的结果：AfterTool
//     核心（本包）一行都不用动
//
// # 核心自己管的
//
// 循环本身（run.go）、调模型（超时、重试一次）、工具分发和跳过规则（页面变了 / 失败 / 结束之后不再执行同一轮后面的）、
// 被迫收尾（finish）、步数上限、把每条消息交给记录器（Recorder）、把过程交给观察者（Observer）。
// done 也是一个插件（Done），放在最后。
package agent
