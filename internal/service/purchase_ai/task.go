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

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/followups"
	"github.com/apache/answer/internal/service/purchase_ai/guard"
	"github.com/apache/answer/internal/service/purchase_ai/skills"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/sandboxtool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/videotool"
)

// 工具的名字：告诉模型的，也是日志、事件里记的
const (
	toolSkill    = skills.Name
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
	toolDone     = agent.DoneName

	toolHarvest   = "harvest"
	toolVideo     = videotool.Name
	toolBash      = sandboxtool.NameBash
	toolReadFile  = sandboxtool.NameReadFile
	toolWriteFile = sandboxtool.NameWriteFile
	toolEditFile  = sandboxtool.NameEditFile
	toolDeliver   = "deliver"
)

// 工具结果和最终回答的 Extras 里，各个插件放东西用的 key：核心不认识它们，只原样记进日志
const (
	extraProduct  = "product"  // *ProductCard，pick_product 做好的卡片
	extraDraft    = "draft"    // *DraftInput，draft_post 交给用户确认的草稿
	extraArtifact = "artifact" // *Artifact，deliver 交给用户的产物
	extraShots    = "shots"    // []Shot，回答引用、存下来的图
)

// taskOptions 是一次任务的配置，由入口（Chat、Analyze、RunTask）拼出来。
// 装哪些插件完全由这里决定：没连插件就没有浏览器的工具，没有沙箱就没有工作台的工具，不是对话就没有草稿
type taskOptions struct {
	llm       llm.Client
	browser   Browser
	browserOn bool // 插件连着：给浏览器的工具
	limits    agentLimits

	images        ImageStore    // 存回答引用的图、商品卡片的图；nil 就都没有图
	artifactStore ArtifactStore // 存交付的网页；nil 就交付不了
	workspaces    Workspaces    // 沙箱；nil 就没有工作台的工具
	chatID        int64         // 工作台按对话租用
	userID        string
	draft         *draftContext // 对话页才有：可以把对话整理成帖子草稿
	followups     bool          // 对话才有：回答后面的后续建议

	events  chan<- Event // 推给页面；nil 就没人听
	present chatOptions  // 推给用户之前的处理：打码、思考要不要给
	rec     *chatRecorder
}

// task 是装配好的一次任务：核心 Agent，加上各个插件的状态。
// 状态不在 Agent 上：商品卡片、看过的图、交付的产物各在自己的结构里，用到它们的插件拿着，
// 入口跑完以后从这里取结果（卡片、产物），测试从这里取单个工具
type task struct {
	agent   *agent.Agent
	pres    *presenter
	state   *browsertool.TaskState
	pics    *pictures
	cards   *productBook
	links   *linkBook
	arts    *deliverer
	harvest *harvester
	draft   *drafter // 不是对话时是 nil
	rec     *chatRecorder
}

// newTask 把插件按固定的顺序装起来。顺序有意义：
//   - 工具定义在请求的最前面，顺序一变前缀缓存就全部失效（browser、产品、图、收割、视频、工作台、交付、草稿、done）
//   - EndOfTurn 按顺序评估，第一个要求收尾的赢：连续失败、重复动作、剩几步、交付提醒
func newTask(o taskOptions) (*task, error) {
	state := &browsertool.TaskState{Browser: o.browser, Timeout: o.limits.toolTimeout, LabelsOf: elementLabels}
	pres := &presenter{events: o.events, opts: o.present, state: state}
	bench := &sandboxtool.Bench{Workspaces: o.workspaces, ChatID: o.chatID, UserID: o.userID}

	// 图的编号是整次任务共用的（截图、看原图、工作台里的图），浏览器工具和工作台工具编号时问它
	pics := &pictures{state: state, images: o.images}
	state.NumberPicture = pics.number
	bench.NumberPicture = pics.number

	cards := &productBook{state: state, pres: pres, pics: pics}
	links := &linkBook{products: cards}
	arts := &deliverer{bench: bench, store: o.artifactStore, pres: pres, links: links, products: cards}
	harvest := &harvester{state: state, bench: bench, pics: pics, links: links}
	video := &videotool.Tool{TaskState: state, SeeLink: links.see}

	browserOn, sandboxOn := o.browserOn, o.workspaces != nil
	l := o.limits
	var plugins []agent.Plugin
	add := func(on bool, p ...agent.Plugin) {
		if on {
			plugins = append(plugins, p...)
		}
	}
	add(browserOn, skills.Plugin(), browsertool.Plugin(state), cards.plugin())
	add(true, pics.plugin(browserOn))
	add(browserOn && sandboxOn, harvest.plugin(), videotool.Plugin(video, bench))
	add(sandboxOn, sandboxtool.Plugin(bench), arts.plugin())
	var draft *drafter
	if o.draft != nil {
		draft = &drafter{ctx: o.draft, pres: pres, products: cards}
		add(true, draft.plugin())
	}
	add(true, links.plugin())
	add(o.followups, followups.Plugin())
	add(true, guard.Failures(l.maxFailures), guard.Loops(l.loopWindow, l.loopNudgeAt, l.loopStopAt), guard.Warn(l.warnAt))
	add(sandboxOn, arts.reminder(l.deliverAt))
	add(browserOn, agent.Done())

	cfg := agent.Config{
		Model:     o.llm,
		MaxSteps:  l.maxSteps,
		MaxTokens: l.maxOutputTokens,
		Timeouts:  agent.Timeouts{Run: l.runTimeout, Model: l.llmTimeout},
		Plugins:   plugins,
		Observer:  pres,
	}
	if o.rec != nil {
		cfg.Recorder = o.rec
	}
	a, err := agent.New(cfg)
	if err != nil {
		return nil, err
	}
	return &task{agent: a, pres: pres, state: state, pics: pics, cards: cards, links: links, arts: arts, harvest: harvest, draft: draft, rec: o.rec}, nil
}

// run 跑完一次任务，结果带上做好的商品卡片和回答引用的图。核心有好几个出口，在这里统一带上
func (t *task) run(ctx context.Context, messages []llm.Message) (*agentResult, error) {
	res, err := t.agent.Run(ctx, messages)
	if err != nil {
		return nil, err
	}
	out := &agentResult{
		Text:      res.Text,
		Success:   res.Success,
		Finish:    res.Finish,
		Steps:     res.Steps,
		Products:  t.cards.cards,
		Reasoning: res.Reasoning,
	}
	out.Followups, _ = res.Extras[followups.Extra].([]string)
	out.Shots, _ = res.Extras[extraShots].([]Shot)
	return out, nil
}

// close 关掉这次任务开过的标签页。每个入口跑完都要调
func (t *task) close() { t.state.CloseTabs() }

// oneLine 把所有空白（包括换行）压成一个空格
func oneLine(s string) string { return tool.OneLine(s) }
