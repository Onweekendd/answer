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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/skills"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/sandboxtool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/videotool"
	"github.com/segmentfault/pacman/log"
)

// agentTool 把告诉模型的工具说明和执行它的代码放在一起。
// 每个工具都是"原始的 JSON 参数进、toolResult 出"，循环按名字分发，不用认识具体的工具。
type agentTool struct {
	def llm.Tool
	run func(ctx context.Context, arguments string) (toolResult, error)
}

// agentRun 是一次任务的状态。PurchaseAIService 被所有任务共用，属于某一次任务的东西放在这里。
type agentRun struct {
	llm    llm.Client
	limits agentLimits

	// 浏览器这一侧的状态和工具（tool/browsertool）：开着哪个标签页、最新快照、编号的名字。
	// 嵌入进来，r.TabID、r.Open 这些直接可用
	browsertool.TaskState

	step  int // 现在是第几步（从 0 开始），记录每一步用
	steps []stepRecord

	images   ImageStore    // 存商品卡片的图；nil 就不存，卡片没有图
	products []ProductCard // 做好的商品卡片
	pictures []picture     // 看过的图，编号是下标加 1；回答引用了才存（tools_shots.go）
	draft    *draftContext // 对话页才有：可以把对话整理成帖子草稿；nil 就没有 draft_post
	callID   string        // 正在执行的调用的 id（tool_start 里那个），挂起的事件要带上它

	// 工作台这一侧的状态和工具（tool/sandboxtool）。Workspaces 为 nil 表示没有沙箱，不给工作台工具
	sandboxtool.Bench
	// 产物（tools_deliver.go）。artifactStore 为 nil 就交付不了：case 运行器要自己给一个
	artifactStore ArtifactStore
	artifacts     []Artifact
	artifactAt    map[string]int  // 工作台里的路径 → artifacts 的下标：再交付同一个文件就是更新它
	seenLinks     map[string]bool // 这次见过的网址（links.Key）：打开过的页面、收割清单。交付的网页只该链到这些

	// rec 把这一轮的消息记进对话日志（chat_recorder.go）。nil 表示不记：帖子的后台分析、case 运行器
	rec   *chatRecorder
	shots []Shot // 回答引用、已经存好的图（answerDetails），run 交给结果

	// 推给前端的事件（阶段 3）。events 为 nil 表示没人在听
	events        chan<- Event
	showReasoning bool     // 思考也推给用户
	redact        []string // 推给用户的文字里要换掉的词

	failures failureCounter
	loops    loopDetector
}

func newAgentRun(client llm.Client, b Browser, limits agentLimits) *agentRun {
	r := &agentRun{
		llm:       client,
		limits:    limits,
		TaskState: browsertool.TaskState{Browser: b, Timeout: limits.toolTimeout, LabelsOf: elementLabels},
		failures:  failureCounter{limit: limits.maxFailures},
		loops:     loopDetector{window: limits.loopWindow, nudgeAt: limits.loopNudgeAt, stopAt: limits.loopStopAt},
	}
	// 图的编号是整次任务共用的（截图、看原图、工作台里的图），浏览器工具编号时问 Agent
	r.TaskState.NumberPicture = r.numberPicture
	r.Bench.NumberPicture = r.numberPicture
	return r
}

const (
	toolSkill    = "skill_read"
	toolOpen     = browsertool.NameOpen
	toolNavigate = browsertool.NameNavigate
	toolSnapshot = browsertool.NameSnapshot
	toolRead     = browsertool.NameRead
	toolShot     = browsertool.NameShot
	toolClick    = browsertool.NameClick
	toolHover    = browsertool.NameHover
	toolType     = browsertool.NameType
	toolPress    = browsertool.NamePress
	toolScroll   = browsertool.NameScroll
	toolProduct  = "pick_product"
	toolImage    = "view_image"
	toolDraft    = "draft_post"
	toolDone     = "done"

	toolHarvest   = "harvest"
	toolVideo     = videotool.Name
	toolBash      = sandboxtool.NameBash
	toolReadFile  = sandboxtool.NameReadFile
	toolWriteFile = sandboxtool.NameWriteFile
	toolEditFile  = sandboxtool.NameEditFile
	toolDeliver   = "deliver"
)

// toolNeed 是一个工具要什么才能用
type toolNeed int

const (
	needBrowser           toolNeed = iota // 插件连着
	needDraft                             // 对话页（r.draft 不为 nil）
	needSandbox                           // 有沙箱（r.workspaces 不为 nil）
	needBrowserAndSandbox                 // 收割：从页面上收，放进沙箱
)

type skillArgs struct {
	Path string `json:"path" description:"skill 里的文件路径，比如 site-guide/SKILL.md、site-guide/references/taobao.md"`
}

type doneArgs struct {
	Text    string `json:"text" description:"给用户的完整回答，用 Markdown"`
	Success bool   `json:"success" description:"用户要求的事有没有做成。没查到、被登录页或验证码挡住时填 false"`
	// 后续建议放在单独的参数里：写进 text 末尾的 <followups> 块，模型常常只想在思考里（2026-10-20 实测两次）
	Followups []string `json:"followups,omitempty" description:"用户接下来可能会对你说的话，最多 3 句，站在用户的角度写，每句不超过 20 个字，页面上显示成按钮。拿不准该不该买时放「帮我发到社区，让大家投票」；没有值得接着问的就不填"`
}

// toolEntry 是工具清单里的一行
type toolEntry struct {
	name        string
	need        toolNeed
	description string
	args        any
	run         func(context.Context, string) (toolResult, error)
}

// tools 返回这次能用的工具。顺序固定：工具定义在请求的最前面，顺序一变前缀缓存就全部失效。
// 每个工具要什么才能用写在 need 里：插件没连着时只剩 draft_post 和沙箱工具（对话页上也能整理草稿、加工文件）；
// 没有 r.draft 就没有 draft_post，没有沙箱就没有沙箱工具，收割两样都要
func (r *agentRun) tools(browserOn bool) (map[string]agentTool, []llm.Tool, error) {
	browser, sandbox := r.TaskState.Specs(), r.Bench.Specs()
	video := &videotool.Tool{TaskState: &r.TaskState, SeeLink: r.seeLink}
	fromSpec := func(need toolNeed, spec tool.Spec) toolEntry {
		return toolEntry{spec.Name, need, spec.Description, spec.Args, adapt(spec.Run)}
	}
	list := []toolEntry{
		{toolSkill, needBrowser, "读取 skill 里的文件。系统提示词里列出了可用的 skill：先读它的 SKILL.md，再按里面说的读需要的 reference。" +
			"去一个网站操作之前，先读这个网站的 reference。", skillArgs{}, r.skillRead},
		fromSpec(needBrowser, browser[toolOpen]),
		fromSpec(needBrowser, browser[toolNavigate]),
		fromSpec(needBrowser, browser[toolSnapshot]),
		fromSpec(needBrowser, browser[toolRead]),
		fromSpec(needBrowser, browser[toolShot]),
		fromSpec(needBrowser, browser[toolClick]),
		fromSpec(needBrowser, browser[toolHover]),
		fromSpec(needBrowser, browser[toolType]),
		fromSpec(needBrowser, browser[toolPress]),
		fromSpec(needBrowser, browser[toolScroll]),
		{toolProduct, needBrowser, "记下一个值得推荐给用户的商品：用户会在回答下方看到它的商品图、价格，点一下就打开商品页。链接和图由系统从页面上取，你只写标题、价格这些看到的东西。" +
			"在商品详情页上不填 index；在搜索结果里填那张商品卡片的编号。只记看过价格的商品，最多 6 个。", productArgs{}, r.pickProduct},
		{toolImage, needBrowser, "下载页面上一张图片的原图来看，比截图清楚得多：商品主图、详情页里的参数图和说明图、买家秀、价格走势图。" +
			"填 index 取那个元素里最大的图，不填取屏幕上最大的图。和 screenshot 共用编号「图N」，也能在回答里引用给用户看。" +
			"价格区、表格这类不是图片的，或者下载失败了，用 screenshot。", viewImageArgs{}, r.viewImage},
		// 工作台（概念 4）。harvest 从页面上收卡片放进沙箱；下面四个只碰沙箱
		{toolHarvest, needBrowserAndSandbox, "把当前页面（搜索结果、瀑布流）上的卡片一次收下来：边滚动边收，去掉广告和重复的，缩略图放进工作台的 /home/user/harvest/，" +
			"返回一张编了号的总览图。要从很多图里挑参考、做拼图、做图墙时用它，不要一张张截图。收完看总览图挑，编号 = 文件名 = items.json 里的 n。", harvestArgs{}, r.withWorkspace(r.harvest)},
		fromSpec(needBrowserAndSandbox, video.Spec(&r.Bench)),
		fromSpec(needSandbox, sandbox[toolBash]),
		fromSpec(needSandbox, sandbox[toolReadFile]),
		fromSpec(needSandbox, sandbox[toolWriteFile]),
		fromSpec(needSandbox, sandbox[toolEditFile]),
		{toolDeliver, needSandbox, "把工作台里做好的网页交给用户：用户会在回答下方看到一张卡片，点开是网页预览，还能保存成图片发给家人朋友。" +
			"用户要的对比表、清单、行程卡这类东西，做成网页、用 shot 截图看过没问题以后，一定要用它交付；中间稿、脚本、素材不要交付。" +
			"只能交付 .html。网页里引用的工作台里的图会自动内嵌；商品、结论要带上你打开过的商品页和原文链接（<a href>），会自动在新标签页打开；改了以后再交付同一个 path 就是更新它。", deliverArgs{}, r.withWorkspace(r.deliver)},
		{toolDraft, needDraft, "把这段对话整理成一篇发到社区请大家投票的帖子草稿，交给用户确认。只在用户要求发帖时调用（比如「帮我发到社区」）。" +
			"调用以后这次回答就结束了：用户会在页面上修改、发布或者放弃，不用你再说什么。", draftArgs{}, r.draftPost},
		{toolDone, needBrowser, "任务结束时调用：text 是给用户的完整回答，success 表示用户要求的事有没有做成。调用以后不能再操作浏览器。", doneArgs{}, r.done},
	}

	tools := make(map[string]agentTool, len(list))
	defs := make([]llm.Tool, 0, len(list))
	for _, t := range list {
		if !r.has(t.need, browserOn) {
			continue
		}
		schema, err := llm.SchemaFor(t.args)
		if err != nil {
			return nil, nil, err
		}
		def := llm.Tool{Name: t.name, Description: t.description, Parameters: schema}
		tools[t.name] = agentTool{def: def, run: t.run}
		defs = append(defs, def)
	}
	return tools, defs, nil
}

// has 说这次任务有没有 need 要的东西
func (r *agentRun) has(need toolNeed, browserOn bool) bool {
	sandboxOn := r.Workspaces != nil
	switch need {
	case needDraft:
		return r.draft != nil
	case needSandbox:
		return sandboxOn
	case needBrowserAndSandbox:
		return browserOn && sandboxOn
	default:
		return browserOn
	}
}

// runTool 执行一次调用。模型编出来的工具名告诉它，不算出错，它可以自己改正。
// 执行之前记下当时的快照哈希（循环检测用），执行之后记下这一步。
func (r *agentRun) runTool(ctx context.Context, tools map[string]agentTool, call llm.ToolCall) (toolResult, error) {
	t, ok := tools[call.Name]
	if !ok {
		log.Warnf("purchase ai: model called unknown tool %q", call.Name)
		return toolResult{Failed: true, Text: "没有这个工具：" + call.Name}, nil
	}
	before := r.PageHash
	started := time.Now()
	res, err := t.run(ctx, call.Arguments)
	res.PageHash = before
	// 换了页面（open、navigate、点开了新页面）以后不一定拍快照，页面状态要跟着网址换：
	// 不然在几个网页上各 read 一次，指纹都是"read@上一份快照"，被当成原地打转
	// （2026-10-21 成都案例：熊猫基地官网、秘塔、必应、官网票务页各读一次，第 5 次 read 就被收尾）
	if res.PageChanged && res.URL != "" {
		r.PageHash = browsertool.PageState("url:"+res.URL, 0)
	}
	// 真的打开过的页面：交付的网页里只该链到这些（links.Fix）
	if res.URL != "" && !res.Failed {
		r.seeLink(res.URL)
	}

	r.steps = append(r.steps, stepRecord{
		Step:      r.step,
		Tool:      call.Name,
		Arguments: call.Arguments,
		Failed:    res.Failed || err != nil,
		URL:       res.URL,
		Duration:  time.Since(started),
	})
	return res, err
}

// ---------- 每个工具 ----------

// skillRead 读 skill 里的一个文件。不碰浏览器，还没打开网页时也能用
func (r *agentRun) skillRead(_ context.Context, arguments string) (toolResult, error) {
	var args skillArgs
	if res := decodeArgs(arguments, &args, `{"path": "site-guide/SKILL.md"}`); res != nil {
		return *res, nil
	}
	content, err := skills.Read(args.Path)
	if err != nil {
		skill, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(args.Path), "/"), "/")
		if files := skills.FilesOf(skill); len(files) > 0 {
			return toolResult{Failed: true, Text: fmt.Sprintf("没有 %s。这个 skill 里有：%s。", args.Path, strings.Join(files, "、"))}, nil
		}
		return toolResult{Failed: true, Text: fmt.Sprintf("没有 %s。路径的格式是 skill名/文件，比如 site-guide/SKILL.md。", args.Path)}, nil
	}
	return toolResult{Text: content}, nil
}

func (r *agentRun) done(_ context.Context, arguments string) (toolResult, error) {
	var args doneArgs
	if res := decodeArgs(arguments, &args, `{"text": "...", "success": true}`); res != nil {
		return *res, nil
	}
	if strings.TrimSpace(args.Text) == "" {
		return toolResult{Failed: true, Text: "参数无效：text 不能为空，要写上给用户的完整回答。"}, nil
	}
	return toolResult{Text: "已结束。", Done: true, Answer: args.Text, Success: args.Success, Followups: args.Followups}, nil
}

// ---------- 小工具 ----------

// 浏览器工具在 browsertool 里，返回的是 tool.Result；根包里的工具（商品卡片、收割、交付……）
// 用下面这几个包装，拿到的还是 toolResult

// screenshotData 是一张图：插件的截图、下载的原图、工作台里的图片文件，见 tool.Image
type screenshotData = tool.Image

// fromTool 把 tool.Result 换成 toolResult：tool.Result 是 toolResult 的一部分
func fromTool(t tool.Result) toolResult {
	return toolResult{Text: t.Text, Display: t.Display, Failed: t.Failed, PageChanged: t.PageChanged,
		Snapshot: t.Snapshot, Image: t.Image, URL: t.URL}
}

// lift 是 fromTool 的指针版：nil 还是 nil
func lift(t *tool.Result) *toolResult {
	if t == nil {
		return nil
	}
	res := fromTool(*t)
	return &res
}

// adapt 让一个返回 tool.Result 的工具能放进工具清单
func adapt(run func(context.Context, string) (tool.Result, error)) func(context.Context, string) (toolResult, error) {
	return func(ctx context.Context, arguments string) (toolResult, error) {
		res, err := run(ctx, arguments)
		return fromTool(res), err
	}
}

// call、callFor、requireTab 见 browsertool.TaskState 上同名的方法
func (r *agentRun) call(ctx context.Context, tabID int, cmd string, args, out any) (*toolResult, error) {
	res, err := r.Call(ctx, tabID, cmd, args, out)
	return lift(res), err
}

func (r *agentRun) callFor(ctx context.Context, timeout time.Duration, tabID int, cmd string, args, out any) (*toolResult, error) {
	res, err := r.CallFor(ctx, timeout, tabID, cmd, args, out)
	return lift(res), err
}

func (r *agentRun) requireTab() *toolResult {
	return lift(r.RequireTab())
}

// decodeArgs 见 tool.DecodeArgs
func decodeArgs(arguments string, v any, example string) *toolResult {
	return lift(tool.DecodeArgs(arguments, v, example))
}

// oneLine 把所有空白（包括换行）压成一个空格
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
