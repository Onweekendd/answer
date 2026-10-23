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

// Package purchaseai 是「买不买」的 AI：对话页里替用户做功课的 Agent，和帖子发出来以后的 AI 分析。
//
// # 怎么读这个目录
//
// 核心是 agent/ 子包：一个只懂「模型、工具、消息」的循环，什么业务都不认识。
// 浏览器、沙箱、商品卡片、交付网页、发帖草稿、后续建议、各种上限……都是插件（agent.Plugin），
// 每个插件注册自己的工具，和在固定时机被核心调用的钩子（设计照根目录 pi/ 的 pi agent，见 agent/doc.go）。
// 这个包做三件事：把插件装配成一次任务（task.go）、定义业务类型和提示词、提供三个入口。
//
// 根包的文件按前缀分组，同一组的文件排在一起：
//
//	service.go、service_task.go   入口：PurchaseAIService（controller 调）、RunTask（case 运行器调）
//	post_*.go                     帖子分析：从截图识别商品（identify）、给出买不买的结论（verdict）
//	chat*.go                      对话页：一轮对话（chat）、打开历史对话（page）、对话日志（log、projection、recorder）
//	task.go                       装配：newTask 按固定的顺序把插件装起来，顺序就是工具定义的顺序和上限的优先级
//	task_*.go                     一次任务要用的：事件和推给用户的出口（events）、提示词（prompt）、
//	                              结果和上限的数值（result）、回答的排版（markdown）
//	tools_*.go                    需要业务类型的插件：商品卡片（products）、图的编号和引用（shots）、
//	                              收割（harvest）、交付网页（deliver、deliver_links）、发帖草稿（draft），
//	                              和接到子包的胶水（browser、workspace）。每个都是一个小结构，带着自己的状态和 plugin()
//
// 子包，每个只依赖比它更底层的：
//
//	agent/             核心：循环、调模型、工具分发和跳过规则、收尾、done；Plugin 和 5 个钩子
//	guard/             上限类插件：连续失败、重复动作、剩几步提醒
//	followups/         回答后面的后续建议：切掉 <followups> 块，整理好交给页面
//	skills/            编进程序的 skill 文件，和读它们的工具 skill_read
//	tool/              工具的公共底层：Result、Spec、DecodeArgs、Image、AbortError
//	tool/browsertool/  浏览器工具和它管的旧快照、旧截图的压缩
//	tool/sandboxtool/  工作台工具：bash read_file write_file edit_file，按对话租用沙箱
//	tool/videotool/    save_video：在浏览器里拿视频，存进工作台
//	video/             视频的纯逻辑：各网站的格式（douyin）、挑清晰度（Pick）、分段下载（Fetch）
//	links/             链接清洗（Clean）、比较（Key）、交付网页里的链接核对（Fix）
//
// # 一轮对话怎么走
//
//	Chat（chat.go）
//	  → newChatTask：拼 taskOptions（模型、浏览器、沙箱、草稿、日志、事件出口），newTask 把插件装起来
//	  → task.run → agent.Run（agent/run.go），每一步：
//	      BeforeModel 钩子（旧快照换成占位语）→ 调模型 → 要了工具：runCalls 顺序执行（每个执行完调 AfterTool 钩子）
//	      → commit（回复和截图记进消息）→ EndOfTurn 钩子（连续失败、重复动作、剩几步）→ 下一步
//	      直到模型调用 done、直接回答，或者上限逼它收尾；最终回答出来时调 OnAnswer 钩子（存图、切后续建议）
//	  → 过程由 presenter（task_events.go，agent.Observer）推给页面，每条消息由 chatRecorder（agent.Recorder）记进对话日志
//
// # 新增一个能力要改哪里
//
//   - 一个新工具：写一个返回 agent.Plugin 的函数（要业务状态的写成 tools_*.go 里的小结构），在 newTask 的插件列表里加一行
//   - 一个新的上限或提醒：在 guard 包里写一个 EndOfTurn 插件，在 newTask 里按优先级放进去
//   - 核心（agent/）不用动
//
// 还没有搬进 tool.Spec 的：每个调用给用户看的一句话（task_events.go 的 describeCall）认识每一个工具，
// 新工具不加也行，页面上显示工具名
package purchaseai
