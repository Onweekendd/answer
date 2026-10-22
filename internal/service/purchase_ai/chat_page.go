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
	"strconv"
	"strings"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/segmentfault/pacman/errors"
)

// 页面投影：打开一段历史对话时，页面要的消息也从日志里出（上下文管理概念 1 的「一份日志，两种投影」）。
// 页面上只有两种消息：用户说的话，和每一轮的回答。回答不另存，从那一轮的消息和它们的 Details 重放出来，
// 和当时推给页面的事件一样（replayTurn）

// PageMessage 是页面上的一条消息
type PageMessage struct {
	ID       string   `json:"id"`        // 条目 id：用户消息是它那条 user 条目，回答是那一轮的最后一条（leaf）
	ParentID string   `json:"parent_id"` // 页面上的上一条消息；编辑、重新生成会让同一条有好几个下一条
	Role     string   `json:"role"`      // user / assistant
	Text     string   `json:"text,omitempty"`
	Images   []string `json:"images,omitempty"`
	Events   []Event  `json:"events,omitempty"` // 回答：按顺序重放这些事件得到那条回答
	// CreatedAt 是 unix 秒
	CreatedAt int64 `json:"created_at"`
}

// ChatHistory 返回一段对话在页面上的全部消息，所有分支都在里面
func (pas *PurchaseAIService) ChatHistory(ctx context.Context, req *schema.PurchaseAIChatHistoryReq) ([]PageMessage, error) {
	chatID, _ := strconv.ParseInt(req.ChatID, 10, 64)
	entries, found, err := pas.chatLog.Entries(ctx, chatID, req.UserID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.NotFound(reason.ObjectNotFound)
	}
	return pageMessages(entries, pas.chatOptions), nil
}

// pageMessages 从日志里挑出页面上的消息。日志是一棵树，页面上的消息是这样从里面认出来的：
//   - 用户说的话：role 为 user、不是循环自己加的提醒（Synthetic）
//   - 回答：用户消息下面，每个不是用户消息的孩子开始一轮，顺着往下走到头（下面没有了，或者只剩用户消息）。
//     走过的这一串就是这一轮，最后一条是 leaf，也是页面上这条回答的 id。重新生成会在同一条用户消息下面
//     再长一串，所以一条用户消息下面可以有好几条回答（页面上的 < 1/2 >）
//
// 输出的顺序：每条用户消息后面紧跟着它的回答；接在回答后面的下一条用户消息写得更晚，排在后面。
// 页面加载时 parent 总在 child 前面
func pageMessages(entries []Entry, opts chatOptions) []PageMessage {
	byID := make(map[string]Entry, len(entries))
	children := make(map[string][]Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
		children[e.ParentID] = append(children[e.ParentID], e)
	}
	said := func(e Entry) bool {
		return e.Message != nil && e.Message.Role == llm.RoleUser && !e.Message.Synthetic
	}
	// turnFrom 从一轮的第一条走到它的 leaf
	turnFrom := func(first Entry) []Entry {
		turn := []Entry{first}
		seen := map[string]bool{first.ID: true}
		for cur := first; ; {
			next, ok := Entry{}, false
			for _, c := range children[cur.ID] {
				if !said(c) && !seen[c.ID] {
					next, ok = c, true
					break
				}
			}
			if !ok {
				return turn
			}
			seen[next.ID] = true
			turn = append(turn, next)
			cur = next
		}
	}
	// pageParent 是用户消息在页面上的上一条：接在回答后面就是那条回答（所在那一轮的 leaf），
	// 接在用户消息后面（那条没有回答，比如进程重启了）就是它
	pageParent := func(e Entry) string {
		p, ok := byID[e.ParentID]
		switch {
		case !ok:
			return ""
		case said(p):
			return p.ID
		default:
			turn := turnFrom(p)
			return turn[len(turn)-1].ID
		}
	}

	r := &agentRun{}
	opts.apply(r)
	out := make([]PageMessage, 0)
	for _, e := range entries {
		if !said(e) {
			continue
		}
		out = append(out, userPage(e, pageParent(e)))
		for _, c := range children[e.ID] {
			if said(c) {
				continue
			}
			turn := turnFrom(c)
			leaf := turn[len(turn)-1]
			out = append(out, PageMessage{
				ID:        leaf.ID,
				ParentID:  e.ID,
				Role:      "assistant",
				Events:    r.replayTurn(turn),
				CreatedAt: leaf.CreatedAt.Unix(),
			})
		}
	}
	return out
}

// userPage 是页面上的一条用户消息。userMessage 的约定：第一段是用户的话，图片是网址；
// 截图说明、网址列表是写给模型的，不给页面
func userPage(e Entry, parent string) PageMessage {
	pm := PageMessage{ID: e.ID, ParentID: parent, Role: "user", CreatedAt: e.CreatedAt.Unix()}
	for i, p := range e.Message.Parts {
		switch {
		case p.Image != "":
			pm.Images = append(pm.Images, p.Image)
		case i == 0:
			pm.Text = p.Text
		}
	}
	return pm
}

// replayTurn 把一轮的消息重放成页面上的那条回答：和当时推给页面的事件一样（pageMessages 用）。
//
// 上下文：
//   - 谁调用：打开一段历史对话时 GET /purchase/ai/chat/history → ChatHistory → pageMessages，
//     每一轮调一次。返回的事件交给页面，页面用直播时同一套代码（frontend/web/src/core/chat/stream.ts 的
//     replayEvents）折叠成那条回答
//   - r：只有打码要用的 redact、要不要显示思考的 showReasoning，是一个「空的」agentRun：没有浏览器、
//     没有模型。用它是为了直接用直播时那几个方法（view、answerEvents），保证两边一样
//   - turn：一轮里 Agent 记下的全部消息，按顺序，不含开头那条用户消息。一轮典型的样子（下标从 0 数）：
//     0 assistant  Reasoning「先看京东」 ToolCalls[c1 open {"url":"https://www.jd.com"}]
//     1 tool       ToolCallID c1  Details{Summary:"打开 www.jd.com", Display:"打开了「京东」"}
//     2 assistant  ToolCalls[c2 pick_product {...}, c3 snapshot {}]
//     3 tool       c2  Details{Summary:"把「XM5」记下来", Display:"记下了「XM5」", Product:&ProductCard{...}}
//     4 tool       c3  Details{Summary:"查看页面", Display:"页面上有 2 个可以操作的元素"}
//     5 user       Synthetic「这是第 2 步截的图」（截图、提醒这类，页面不显示）
//     6 assistant  ToolCalls[c4 done {"text":"买 [图1：券后价]","followups":["那 bose 呢"]}]
//     7 tool       c4  Details{Final:true, Shots:[{ID:1, Image:"https://…/uploads/agent/1.jpg"}]}（done 没有 Summary）
//
// 直播时这一轮推出去的事件，就是要重放出来的（agent.go 的 loop、runCalls，chat.go 的 Chat）：
//   - 每条 assistant 消息是一步，step 从 0 数。思考：showReasoning 开着、Reasoning 不空，发 reasoning，Delta 过 r.view
//   - 这一步的每个调用 i：它的结果（ToolCallID 对得上的那条 tool 消息）Details.Summary 不空才有卡片
//     （跳过的、done 没有），id 是 fmt.Sprintf("%d-%d", step, i)，依次发：
//     tool_start（Name 是工具名，Args.Summary 是 r.view(Summary)）→ 有 Product 发 product → 有 Artifact 发 artifact → 有 Draft 发
//     tool_suspend（ID、Name 和 tool_start 一样，Input 是 Draft）→ tool_end（Result.Summary 是
//     r.view(resultSummary(toolResult{Failed, Display}))，Result.OK 是 !Failed 的指针）
//   - 最后是回答：找 Details.Final 为 true 的那条，交给 r.answerEvents(&agentResult{Text, Followups, Shots})，
//     它发 text 和 followups。Shots 就是那条的 Details.Shots，Text 和 Followups 看它是哪种：
//     ① assistant（直接回答、收尾）：Text 是 answerText(&llm.ChatResp{Content: 它的文字})
//     ② done 的结果：按 ToolCallID 找回那个调用，Text、Followups 从它的参数里解出来（json.Unmarshal 到 doneArgs）
//     ③ draft_post 的结果：Text 是 draftAnswer
//     找不到 Final：这一轮没跑完（模型出错、用户关了页面），发一条 error，Message 是 chatFailedText
//
// 做法：先扫一遍，按调用 ID 记下结果、找到 Final 那条；再按步发思考和卡片；最后拼出回答。
//
// 测试：chat_page_test.go。TestReplayTurn_* 一条规则一个；TestReplayTurn_SameAsLive 是总验收：
// 真的跑一轮 Chat，收下推给页面的事件，再从日志里重放，两边必须一模一样
func (r *agentRun) replayTurn(turn []Entry) []Event {
	// 先扫一遍：调用 ID → 它的结果，调用 ID → 调用本身（done 的回答在参数里），带着回答的那条
	results := map[string]*StoredMessage{}
	calls := map[string]StoredCall{}
	var final *StoredMessage
	for _, e := range turn {
		m := e.Message
		if m == nil {
			continue
		}
		if m.Role == llm.RoleTool {
			results[m.ToolCallID] = m
		}
		for _, c := range m.ToolCalls {
			calls[c.ID] = c
		}
		if m.Details != nil && m.Details.Final {
			final = m
		}
	}

	// 每条 assistant 是一步：先思考，再是这一步每个调用的卡片（runCalls 发卡片的顺序）
	var out []Event
	step := 0
	for _, e := range turn {
		m := e.Message
		if m == nil || m.Role != llm.RoleAssistant {
			continue
		}
		if text := strings.TrimSpace(m.Reasoning); r.showReasoning && text != "" {
			out = append(out, Event{Type: EventReasoning, Delta: r.view(text)})
		}
		for i, c := range m.ToolCalls {
			res := results[c.ID]
			if res == nil || res.Details == nil || res.Details.Summary == "" {
				continue // 跳过了的、done：页面上没有卡片
			}
			d := res.Details
			id := fmt.Sprintf("%d-%d", step, i)
			out = append(out, Event{Type: EventToolStart, ID: id, Name: c.Name, Args: &ToolView{Summary: r.view(d.Summary)}})
			if d.Product != nil {
				out = append(out, Event{Type: EventProduct, Product: d.Product})
			}
			if d.Artifact != nil {
				out = append(out, Event{Type: EventArtifact, Artifact: d.Artifact})
			}
			if d.Draft != nil {
				out = append(out, Event{Type: EventToolSuspend, ID: id, Name: c.Name, Input: d.Draft})
			}
			ok := !d.Failed
			out = append(out, Event{Type: EventToolEnd, ID: id, Result: &ToolView{Summary: r.view(resultSummary(toolResult{Failed: d.Failed, Display: d.Display})), OK: &ok}})
		}
		step++
	}

	// 回答：没有 Final 就是这一轮没跑完
	if final == nil {
		return append(out, Event{Type: EventError, Message: chatFailedText})
	}
	res := &agentResult{Shots: final.Details.Shots}
	switch {
	case final.Role == llm.RoleAssistant: // 直接回答、收尾
		text := ""
		if len(final.Parts) > 0 {
			text = final.Parts[0].Text
		}
		res.Text = answerText(&llm.ChatResp{Content: text})
	case final.ToolName == toolDraft:
		res.Text = draftAnswer
	default: // done：回答在调用的参数里
		var args doneArgs
		_ = json.Unmarshal([]byte(calls[final.ToolCallID].Arguments), &args)
		res.Text, res.Followups = args.Text, args.Followups
	}
	return append(out, r.answerEvents(res)...)
}
