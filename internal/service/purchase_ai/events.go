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
)

// 推给前端的事件（阶段 3 概念 3）。字段和 frontend/web/src/core/chat/mapper.ts 的 ChatEvent 一一对应，改一边就要改另一边。
const (
	EventReasoning = "reasoning" // 模型这一步的思考，按步骤整段发
	EventText      = "text"      // 回答
	EventToolStart = "tool_start"
	EventToolEnd   = "tool_end"
	EventProduct   = "product" // 一张商品卡片，见 products.go
	EventError     = "error"
	EventDone      = "done"
)

// Event 是一条事件。没用到的字段不输出
type Event struct {
	Type    string       `json:"type"`
	Delta   string       `json:"delta,omitempty"`   // reasoning、text
	ID      string       `json:"id,omitempty"`      // tool_start、tool_end：同一个调用的两条事件 id 一样
	Name    string       `json:"name,omitempty"`    // tool_start：工具名
	Args    *ToolView    `json:"args,omitempty"`    // tool_start：要做什么
	Result  *ToolView    `json:"result,omitempty"`  // tool_end：做得怎么样
	Message string       `json:"message,omitempty"` // error：给用户看的一句话
	Product *ProductCard `json:"product,omitempty"` // product
}

// ToolView 是一次工具调用给用户看的样子。只有这两个字段：给模型看的 Text 里有页面内容，
// 共享浏览器上还有那个账号的昵称和上一个人的搜索，一律不推（白名单，概念 3）
type ToolView struct {
	Summary string `json:"summary"`
	OK      *bool  `json:"ok,omitempty"` // 只在 tool_end 里有
}

// emit 把事件交给另一头（SSE）。没人在听（帖子的后台分析、case 运行器）就什么都不做。
// 发送一定要和 ctx.Done() 一起等：用户走了，handler 不再收，不这样写这个 goroutine 就永远卡在这里（概念 2）
func (r *agentRun) emit(ctx context.Context, ev Event) {
	if r.events == nil {
		return
	}
	select {
	case r.events <- ev:
	case <-ctx.Done():
	}
}

// emitReasoning 发这一步的思考。思考是模型自己写的，可能复述页面内容，没法用白名单，
// 只能打码、过屏蔽词兜底（概念 3 补充），所以有开关
func (r *agentRun) emitReasoning(ctx context.Context, resp *llm.ChatResp) {
	if !r.showReasoning {
		return
	}
	if text := strings.TrimSpace(resp.Reasoning); text != "" {
		r.emit(ctx, Event{Type: EventReasoning, Delta: r.view(text)})
	}
}

func (r *agentRun) emitToolStart(ctx context.Context, id string, call llm.ToolCall) {
	r.emit(ctx, Event{Type: EventToolStart, ID: id, Name: call.Name, Args: &ToolView{Summary: r.view(r.describeCall(call.Name, call.Arguments))}})
}

func (r *agentRun) emitToolEnd(ctx context.Context, id string, res toolResult) {
	ok := !res.Failed
	r.emit(ctx, Event{Type: EventToolEnd, ID: id, Result: &ToolView{Summary: r.view(resultSummary(res)), OK: &ok}})
}

// view 是所有推给用户的文字的唯一出口：打码，再过屏蔽词
func (r *agentRun) view(s string) string {
	return redactWords(maskPrivate(s), r.redact)
}

// resultSummary 是 tool_end 里的一句话。失败的不说具体原因：报错里有"盖在上面的是 …"这类页面内容
func resultSummary(res toolResult) string {
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
func (r *agentRun) describeCall(name, arguments string) string {
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
			return "读「" + r.label(*a.Index) + "」的全部文字"
		}
		return "读页面上的文字"
	case toolShot:
		if a.Index != nil {
			return "截图看「" + r.label(*a.Index) + "」"
		}
		return "截图看页面"
	case toolClick:
		return "点击「" + r.labelOf(a.Index) + "」"
	case toolHover:
		return "把鼠标移到「" + r.labelOf(a.Index) + "」上"
	case toolType:
		if a.Submit {
			return "输入「" + a.Text + "」并回车"
		}
		return "输入「" + a.Text + "」"
	case toolPress:
		if say, ok := pressKeys[a.Key]; ok {
			return say
		}
		return "按 " + a.Key
	case toolCite:
		var c struct {
			Caption string `json:"caption"`
		}
		_ = json.Unmarshal([]byte(arguments), &c)
		return "截图存证：" + clipRunes(oneLine(c.Caption), labelMaxRunes)
	case toolCiteImg:
		var c struct {
			Caption string `json:"caption"`
		}
		_ = json.Unmarshal([]byte(arguments), &c)
		return "保存原图：" + clipRunes(oneLine(c.Caption), labelMaxRunes)
	case toolProduct:
		var p struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal([]byte(arguments), &p)
		return "把「" + clipRunes(oneLine(p.Title), labelMaxRunes) + "」记下来"
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

func (r *agentRun) labelOf(index *int) string {
	if index == nil {
		return "元素"
	}
	return r.label(*index)
}

// label 是最新快照里 index 号元素的名字，找不到就说编号
func (r *agentRun) label(index int) string {
	if name := r.labels[index]; name != "" {
		return name
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
