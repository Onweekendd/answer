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
//	Prompt       系统提示词里加一段                              （第二步再做）
//	AfterTool    一个工具执行完                                 链接核对、记下快照
//	BeforeModel  每次请求模型前，改这一次要发的消息（不改历史）      旧快照、旧截图换成占位语
//	OnAnswer     最终回答出来时，可以改文字、附带产出              存回答引用的图、切掉 <followups>
//	EndOfTurn    一轮结束，返回「追加提醒」或「走收尾」            步数、连续失败、重复动作、交付提醒
//	OnFinish     任务结束，往结果里放东西                        商品卡片、图、产物
//
// 多个插件挂在同一个钩子上：按注册顺序依次调用。
// EndOfTurn 里第一个要求收尾的赢，但后面插件的提醒照样追加。
// 某个插件的处理函数 panic 了，只记日志，不拖垮这一轮（pi 的 emitError 是这样做的）。
//
// # 核心自己管的
//
// 循环本身、调模型（超时、重试一次）、工具分发和跳过规则（页面变了 / 失败 / done 之后不再执行同一轮后面的）、
// done 工具、被迫收尾（finish）、把每条消息交给记录器、把事件交给观察者。
package agent
