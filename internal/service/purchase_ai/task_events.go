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
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
)

// 推给前端的事件（阶段 3 概念 3）。字段和 frontend/web/src/core/chat/mapper.ts 的 ChatEvent 一一对应，改一边就要改另一边。
const (
	EventReasoning = "reasoning" // 模型这一步的思考，按步骤整段发
	EventText      = "text"      // 回答
	EventToolStart = "tool_start"
	EventToolEnd   = "tool_end"
	EventProduct   = "product"  // 一张商品卡片，见 tools_products.go
	EventArtifact  = "artifact" // 一个交给用户的产物，见 tools_deliver.go
	// EventToolSuspend 是一个要用户拍板的调用：Agent 停在这里，把 input 交给页面，见 tools_draft.go
	EventToolSuspend = "tool_suspend"
	// EventFollowups 是回答后面的几句后续建议，页面上点一下就发出去，见 followups 包
	EventFollowups = "followups"
	EventError     = "error"
	// EventSaved 告诉页面这一轮记进日志的条目 id（上下文管理概念 5）：页面下一条消息的 parent_id 是 leaf，
	// 重新生成这一轮时 parent_id 是 user。在 done 之前、回答之前发
	EventSaved = "saved"
	EventDone  = "done"
)

// Event 是一条事件。没用到的字段不输出
type Event struct {
	Type     string       `json:"type"`
	Delta    string       `json:"delta,omitempty"`    // reasoning、text
	ID       string       `json:"id,omitempty"`       // tool_start、tool_end：同一个调用的两条事件 id 一样
	Name     string       `json:"name,omitempty"`     // tool_start：工具名
	Args     *ToolView    `json:"args,omitempty"`     // tool_start：要做什么
	Result   *ToolView    `json:"result,omitempty"`   // tool_end：做得怎么样
	Message  string       `json:"message,omitempty"`  // error：给用户看的一句话
	Product  *ProductCard `json:"product,omitempty"`  // product
	Artifact *Artifact    `json:"artifact,omitempty"` // artifact
	Input    *DraftInput  `json:"input,omitempty"`    // tool_suspend：交给用户确认的内容（tool_input）
	// followups：用户接下来可能会说的话，最多 3 句
	Followups []string  `json:"followups,omitempty"`
	Saved     *SavedIDs `json:"saved,omitempty"` // saved
}

// SavedIDs 是一轮记进日志的条目 id
type SavedIDs struct {
	User string `json:"user,omitempty"` // 这一轮的用户消息；重新生成时没有新的用户消息，为空
	Leaf string `json:"leaf"`           // 这一轮的最后一条
}

// ToolView 是一次工具调用给用户看的样子。只有这两个字段：给模型看的 Text 里有页面内容，
// 共享浏览器上还有那个账号的昵称和上一个人的搜索，一律不推（白名单，概念 3）
type ToolView struct {
	Summary string `json:"summary"`
	OK      *bool  `json:"ok,omitempty"` // 只在 tool_end 里有
}

// presenter 把一次任务的过程推给页面，所有推给用户的文字都过它的 view。
// 它是核心（agent）的观察者（agent.Observer），也是挂起、商品卡片、产物这几种事件的出口
type presenter struct {
	events chan<- Event // nil 表示没人在听（帖子的后台分析、case 运行器）
	opts   chatOptions  // 打码、思考要不要给
	// state 取元素的名字，把「点击 44 号元素」写成「点击「Phone17」」。回放历史对话时是 nil：用不到
	state *browsertool.TaskState
}

// emit 把事件交给另一头（SSE）。没人在听就什么都不做。
// 发送一定要和 ctx.Done() 一起等：用户走了，handler 不再收，不这样写这个 goroutine 就永远卡在这里（概念 2）
func (p *presenter) emit(ctx context.Context, ev Event) {
	if p.events == nil {
		return
	}
	sendEvent(ctx, p.events, ev)
}

// sendEvent 把一条事件放进 channel，或者等到 ctx 取消就放弃。直接写 ch <- ev 的话，用户关了页面、
// 没人再收，缓冲满了以后这个 goroutine 就永远卡在这里（概念 2 规矩 2）
func sendEvent(ctx context.Context, ch chan<- Event, ev Event) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

// Describe 见 agent.Observer
func (p *presenter) Describe(call llm.ToolCall) string {
	return p.describeCall(call.Name, call.Arguments)
}

// Reasoning 发这一步的思考。思考是模型自己写的，可能复述页面内容，没法用白名单，
// 只能打码、过屏蔽词兜底（概念 3 补充），所以有开关
func (p *presenter) Reasoning(ctx context.Context, text string) {
	if p.opts.showReasoning {
		p.emit(ctx, Event{Type: EventReasoning, Delta: p.view(text)})
	}
}

// ToolStart 发一个调用开始了
func (p *presenter) ToolStart(ctx context.Context, id string, call llm.ToolCall, summary string) {
	p.emit(ctx, Event{Type: EventToolStart, ID: id, Name: call.Name, Args: &ToolView{Summary: p.view(summary)}})
}

// ToolEnd 发一个调用做完了
func (p *presenter) ToolEnd(ctx context.Context, id string, _ llm.ToolCall, res tool.Result) {
	ok := !res.Failed
	p.emit(ctx, Event{Type: EventToolEnd, ID: id, Result: &ToolView{Summary: p.view(resultSummary(res)), OK: &ok}})
}

// view 是所有推给用户的文字的唯一出口：打码，再过屏蔽词
func (p *presenter) view(s string) string {
	return redactWords(maskPrivate(s), p.opts.redact)
}

// cardText 是卡片上的一个字段：压成一行、截短，再过打码和屏蔽词（模型写的，可能复述页面内容）
func (p *presenter) cardText(s string) string {
	s = tool.OneLine(s)
	if clipped := tool.ClipRunes(s, productTextRunes); clipped != s {
		s = clipped + "…"
	}
	return p.view(s)
}

// resultSummary 是 tool_end 里的一句话。失败的不说具体原因：报错里有"盖在上面的是 …"这类页面内容
func resultSummary(res tool.Result) string {
	switch {
	case res.Failed:
		return "没做成，换个办法"
	case res.Display != "":
		return res.Display
	default:
		return "完成"
	}
}

// ---------- 每个调用给用户看的说法 ----------

// labelMaxRunes 是元素名字最多几个字：卡片的文字很长，用户认出是哪个就够了
const labelMaxRunes = 24

// describeCall 把一个调用写成人话（tool_start 的 summary）。参数是模型写的 JSON，格式由工具定义固定，
// 解析它不会因为改提示词而坏掉；解析失败就只说工具名
func (p *presenter) describeCall(name, arguments string) string {
	var a struct {
		Path      string `json:"path"`
		URL       string `json:"url"`
		Index     *int   `json:"index"`
		Text      string `json:"text"`
		Submit    bool   `json:"submit"`
		Key       string `json:"key"`
		Direction string `json:"direction"`
		Pages     int    `json:"pages"`
	}
	_ = json.Unmarshal([]byte(arguments), &a)

	switch name {
	case toolSkill:
		return "查阅网站使用指南：" + strings.TrimSuffix(path.Base(a.Path), ".md")
	case toolOpen, toolNavigate:
		return "打开 " + hostOf(a.URL)
	case toolSnapshot:
		return "查看页面"
	case toolRead:
		if a.Index != nil {
			return "读「" + p.label(*a.Index) + "」的全部文字"
		}
		return "读页面上的文字"
	case toolShot:
		if a.Index != nil {
			return "截图看「" + p.label(*a.Index) + "」"
		}
		return "截图看页面"
	case toolClick:
		return "点击「" + p.labelOf(a.Index) + "」"
	case toolHover:
		return "把鼠标移到「" + p.labelOf(a.Index) + "」上"
	case toolType:
		if a.Submit {
			return "输入「" + a.Text + "」并回车"
		}
		return "输入「" + a.Text + "」"
	case toolPress:
		if say, ok := browsertool.PressKeys[a.Key]; ok {
			return say
		}
		return "按 " + a.Key
	case toolDraft:
		return "整理帖子草稿"
	case toolImage:
		return "查看原图"
	case toolProduct:
		var t struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal([]byte(arguments), &t)
		return "把「" + clipRunes(oneLine(t.Title), labelMaxRunes) + "」记下来"
	case toolHarvest:
		return "把这一页的图收进工作台"
	case toolVideo:
		return "把抖音视频存进工作台"
	case toolDeliver:
		var d struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal([]byte(arguments), &d)
		return "交付「" + clipRunes(oneLine(d.Title), labelMaxRunes) + "」"
	case toolBash:
		var b struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal([]byte(arguments), &b)
		return "运行 " + clipRunes(oneLine(b.Command), labelMaxRunes)
	case toolReadFile:
		return "打开 " + path.Base(a.Path)
	case toolWriteFile:
		return "写入 " + path.Base(a.Path)
	case toolEditFile:
		return "修改 " + path.Base(a.Path)
	case toolScroll:
		pages := max(a.Pages, 1)
		if a.Direction == "up" {
			return fmt.Sprintf("向上滚动 %d 屏", pages)
		}
		return fmt.Sprintf("向下滚动 %d 屏", pages)
	default:
		return name
	}
}

// hostOf 只留网站的域名：网址的查询参数里常有 xsec_token 这类东西
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "网页"
	}
	return u.Host
}

func (p *presenter) labelOf(index *int) string {
	if index == nil {
		return "元素"
	}
	return p.label(*index)
}

// label 是最新快照里 index 号元素的名字，找不到就说编号
func (p *presenter) label(index int) string {
	if p.state != nil {
		if name := p.state.Labels[index]; name != "" {
			return name
		}
	}
	return fmt.Sprintf("%d 号元素", index)
}

// snapshotLine 是快照里的一行：[44]<a id=x>Phone17续航差
var snapshotLine = regexp.MustCompile(`^\[(\d+)\]<([^>]*)>(.*)$`)

// namedAttr 是没有文字的元素（输入框、图标按钮）用来当名字的属性
var namedAttr = regexp.MustCompile(`(?:aria-label|placeholder|title)=(\S+)`)

// elementLabels 从快照里取出每个编号的名字：有文字用文字，没有就用 aria-label、placeholder、title
func elementLabels(elements string) map[int]string {
	labels := map[int]string{}
	for line := range strings.SplitSeq(elements, "\n") {
		m := snapshotLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		index, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		name := strings.TrimSpace(m[3])
		if name == "" {
			if attr := namedAttr.FindStringSubmatch(m[2]); attr != nil {
				name = attr[1]
			}
		}
		if name != "" {
			if clipped := clipRunes(name, labelMaxRunes); clipped != name {
				name = clipped + "…"
			}
			labels[index] = name
		}
	}
	return labels
}

// ---------- 打码 ----------

// 和插件的 maskPrivate（frontend/extension/src/snapshot/privacy.ts）规则一样。
// Go 的正则不支持"前面不是数字"（?<!），所以先取出连续的数字，再看整段像不像
var (
	digitRun = regexp.MustCompile(`\d+[Xx]?`)
	phone    = regexp.MustCompile(`^1[3-9]\d{9}$`)
	idCard   = regexp.MustCompile(`^\d{17}[\dXx]$`)
	email    = regexp.MustCompile(`[\w.+-]+@[\w-]+(\.[\w-]+)+`)
)

func maskPrivate(s string) string {
	s = email.ReplaceAllString(s, "[邮箱]")
	return digitRun.ReplaceAllStringFunc(s, func(run string) string {
		switch {
		case idCard.MatchString(run):
			return "[身份证号]"
		case phone.MatchString(run):
			return "[手机号]"
		}
		return run
	})
}

// redactWords 把屏蔽词换掉：共享浏览器登录的那个账号的昵称这类，运营的人知道、写在配置里的
func redactWords(s string, words []string) string {
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			s = strings.ReplaceAll(s, w, "[已屏蔽]")
		}
	}
	return s
}
