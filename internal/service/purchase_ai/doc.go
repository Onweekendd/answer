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
// 根包的文件按前缀分组，同一组的文件排在一起：
//
//	service.go、service_task.go   入口：PurchaseAIService（controller 调）、RunTask（case 运行器调）
//	post_*.go                     帖子分析：从截图识别商品（identify）、给出买不买的结论（verdict）
//	chat*.go                      对话页：一轮对话（chat）、打开历史对话（page）、对话日志（log、projection、recorder）
//	agent*.go                     Agent 循环：调模型、分发工具、防打转（loop）、事件（events）、提示词（prompt）、
//	                              给模型的固定文字（text）、回答后面的建议（followups）、回答的排版（markdown）
//	tools.go                      工具清单：按固定顺序把各个工具拼起来，按名字分发，记下每一步
//	tools_*.go                    挂在 Agent 上的工具：商品卡片（products）、图的编号和看原图（shots）、
//	                              收割（harvest）、交付网页（deliver）、发帖草稿（draft），和接到子包的胶水（browser、workspace）
//
// 子包，每个只依赖比它更底层的：
//
//	tool/              工具的公共底层：Result、Spec、DecodeArgs、Image
//	tool/browsertool/  浏览器工具：open navigate snapshot read screenshot click hover type press scroll
//	tool/sandboxtool/  工作台工具：bash read_file write_file edit_file，按对话租用沙箱
//	tool/videotool/    save_video：在浏览器里拿视频，存进工作台
//	video/             视频的纯逻辑：各网站的格式（douyin）、挑清晰度（Pick）、分段下载（Fetch）
//	links/             链接清洗（Clean）、比较（Key）、交付网页里的链接核对（Fix）
//	skills/            编进程序的 skill 文件，和读取它们
//
// # 一轮对话怎么走
//
//	Chat（chat.go）
//	  → newRun：一个 agentRun，嵌入 browsertool.TaskState 和 sandboxtool.Bench
//	  → run（agent.go）：调模型 → 模型要调的工具 → runTool（tools.go）按名字找到工具执行 → 结果回给模型 → ……
//	    → 直到模型调用 done，或者步数、时间、失败次数用完走收尾
//	  → 事件（agent_events.go）一路推给页面，消息记进对话日志（chat_recorder.go）
//
// 为什么 agentRun 和挂在它上面的工具还在根包：Go 的方法必须和类型写在同一个包里。
// 能独立出去的状态（浏览器、工作台）已经各自成了子包，嵌入进来；
// 剩下的工具要用到整次任务共用的东西（商品卡片、图的编号、产物、对话页的草稿），以后要拆，先要把这些挪进一个单独的包
package purchaseai
