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
	"time"

	"github.com/apache/answer/internal/service/purchase_ai/agent"
)

// agentResult 是一次任务的结果
type agentResult struct {
	Text    string
	Success bool         // done 说做成了；直接回答也算；走了收尾就是 false
	Finish  agent.Reason // 为什么结束的；空表示正常结束
	// Followups 是回答后面的后续建议，整理好了的（followups 插件），帖子分析和 case 运行器没有
	Followups []string
	Steps     []agent.Step
	// Products 是做好的商品卡片，不管怎么结束的都带上：收尾时卡片照样有用
	Products []ProductCard
	// Shots 是存下来的截图，回答里的 [图N] 指的就是它们
	Shots []Shot
	// Reasoning 是最后一次模型调用的思考过程，只用于回答为空时排查
	Reasoning string
}

// agentLimits 是各种上限。都是起步值，跑了 1c 的 case 以后再调；测试里会换成小的
type agentLimits struct {
	maxSteps    int // 一步 = 调用一次模型（概念 4）
	warnAt      int // 还剩几步时提醒收尾
	deliverAt   int // 有工作台时，还剩几步提醒开始做图、表、网页
	maxFailures int // 连续失败几次就收尾（概念 3）

	loopWindow  int // 循环检测看最近几次动作（概念 5）
	loopNudgeAt int // 同一个指纹出现第几次时提醒
	loopStopAt  int // 出现第几次时收尾

	// maxOutputTokens 是每次调用模型能输出多少（思考加正文）。不设就是 llm 包的默认 4096
	maxOutputTokens int

	runTimeout  time.Duration // 整次任务（概念 6）
	llmTimeout  time.Duration // 每次调用模型
	toolTimeout time.Duration // 每次调用插件
}

var defaultLimits = agentLimits{
	// 2026-10-19 从 20 提到 30：小红书一篇笔记要 4 步（点开、snapshot 加 read、截图加 Esc、回到列表），
	// 读三篇再加上读 skill、打开搜索页，20 步常常用完被迫收尾。
	// 2026-10-21 提到 50：做功课要去秘塔核实、点开官网原文，再读几篇小红书，30 步时打开了官网原文还没读就被收尾
	maxSteps: 50,
	warnAt:   3,
	// 2026-10-21 成都案例：46 步都在读网页，剩 4 步才开始做行程卡，只好用 Pillow 草草画一张。
	// 做一张图要写网页、截图、看一眼、改一次，至少 4～5 步，留 10 步
	deliverAt:   10,
	maxFailures: 3,

	loopWindow:  10,
	loopNudgeAt: 3,
	loopStopAt:  5,

	// 2026-10-22 从默认的 4096 提到 8192：写一个 10 KB 的网页（write_file 的参数）加上思考就超过 4096，
	// 参数被截在半截，是坏的 JSON，连写三次都一样，被当成连续失败收尾
	maxOutputTokens: 8192,

	runTimeout: 8 * time.Minute, // 30 步实测 3 分钟出头（2026-10-21），50 步要留出余量
	// 2026-10-22 从 60 秒提到 150 秒：输出能到 8192 个 token，一次写一整个网页，60 秒写不完
	llmTimeout:  150 * time.Second,
	toolTimeout: 30 * time.Second,
}
