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

	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// toolResult 是一次工具调用的结果。它有两个读者：Text 给模型看，其余字段给循环看。
type toolResult struct {
	Text string // 给模型看的
	// Display 是给用户看的一句话（tool_end 的 summary）。由每个工具自己写，只放白名单里的东西：
	// 页面的正文、元素列表、截图都不放（阶段 3 概念 3）。空的显示"完成"，失败的不看它
	Display     string
	Failed      bool // 这次没做成，模型换个做法也许能成（概念 3）
	Skipped     bool // 没有执行：前面的动作改变了页面，或者任务已经结束（概念 1）
	PageChanged bool // 页面可能变了，同一轮后面的编号已经过期（概念 1）
	Snapshot    bool // 这是一份快照，下一轮要换成占位语（概念 2）
	Done        bool // 模型调用了 done（概念 7）

	// Image 是截图的 data URI。工具消息里放不了图，它放在这一轮全部工具回复之后的一条用户消息里，
	// 下一轮开始时和旧快照一样换成占位语，只留最新一张（阶段 2 概念 5）
	Image string

	Answer  string // done 的回答
	Success bool   // done 说要求的事有没有做成
	// Followups 是 done 给的后续建议，没清洗过
	Followups []string

	URL      string // 执行完以后所在的网址（知道的话），给每一步的记录用
	PageHash string // 执行这个动作时的页面状态：最新快照的哈希加上滚动位置，循环检测用（概念 5）

	// 页面上的这张卡片，记进日志的 Details（chat_log.go）
	Summary  string       // tool_start 那句话，runCalls 发卡片时填；跳过的、done 没有
	Product  *ProductCard // pick_product 做好的卡片
	Draft    *DraftInput  // draft_post 交给用户的草稿
	Artifact *Artifact    // deliver 交给用户的产物
}

// details 是记进日志的那份只给页面看的东西：页面上有卡片的才有。带着回答的那份另外由 answerDetails 补上
func (res toolResult) details() *Details {
	if res.Summary == "" && res.Product == nil && res.Draft == nil && res.Artifact == nil {
		return nil
	}
	return &Details{Summary: res.Summary, Failed: res.Failed, Display: res.Display, Product: res.Product, Draft: res.Draft, Artifact: res.Artifact}
}

// finishReason 是走到收尾的原因（概念 4）。空字符串表示模型自己调用了 done，或者直接回答了。
type finishReason string

const (
	finishSteps       finishReason = "steps"        // 步数用完了
	finishFailures    finishReason = "failures"     // 连续失败
	finishLoop        finishReason = "loop"         // 一直在重复同一个动作
	finishEmpty       finishReason = "empty"        // 提醒过以后还是回了空回复（只有思考，没有正文）
	finishBrowserGone finishReason = "browser_gone" // 做到一半插件断开了
)

// errBrowserGone 表示插件断开了或者没连上。模型写不了回答的错误才让循环立刻停，
// 这个不是：前面查到的信息都还在，走收尾（概念 7）。
var errBrowserGone = tool.ErrBrowserGone

// stepRecord 记录执行过的一次工具调用，用于排查问题、case 判定，以后也给前端展示过程。
// 不记 Text：快照里可能有昵称、地址。
type stepRecord struct {
	Step      int
	Tool      string
	Arguments string
	Failed    bool
	URL       string
	Duration  time.Duration
}

// agentResult 是一次任务的结果
type agentResult struct {
	Text    string
	Success bool         // done 说做成了；直接回答也算；走了收尾就是 false
	Finish  finishReason // 为什么结束的；空表示正常结束
	// Followups 是 done 参数里的后续建议；直接回答时它们在 Text 的 <followups> 块里
	Followups []string
	Steps     []stepRecord
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
