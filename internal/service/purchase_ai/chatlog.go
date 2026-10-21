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
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/llm"
)

// 对话日志（上下文管理概念 1）：服务端按对话存一份只追加的日志，存的是模型视角的完整记录，
// 不是页面的 UI 消息。发给模型的上下文（project）和页面上显示的（pageMessages）都是从它投影出来的。
// 参考 pi 的会话文件：每条一个条目，用 parent_id 串成树；编辑、重试从更早的节点长出新分支。
//
// 一轮「那 bose 的呢？」追加的条目：
//
//	user（用户的话）→ assistant（工具调用）→ tool（结果）→ … → assistant 或 done 的结果（回答）
//
// 页面上的东西不另存一份：只有运行时才知道的（调用的摘要、做好的商品卡片、回答引用的图）放在
// 消息的 Details 里，和 pi 工具结果的 details 一样，发给模型时不带

// 条目的种类。现在只有消息；以后的压缩摘要也是一种条目
const (
	EntryMessage = "message" // 一条模型消息
)

// storedVersion 是存储格式的版本。存下来的东西是对未来的承诺：llm.Message 可以随便改，
// 改了以后旧的行要能读回来，读的时候按版本转换
const storedVersion = 1

// Entry 是日志里的一条
type Entry struct {
	ID        string // 服务端生成的短 id，页面拿它当 parent_id
	ParentID  string // 上一条的 ID；空表示根
	Kind      string
	Message   *StoredMessage // Kind 为 message 时有
	CreatedAt time.Time
}

// StoredMessage 是存下来的一条模型消息。和 llm.Message 分开定义，字段带 json tag，
// 存储格式就不会跟着代码里的类型变
type StoredMessage struct {
	V          int          `json:"v"`
	Role       string       `json:"role"`
	Parts      []StoredPart `json:"parts,omitempty"`
	ToolCalls  []StoredCall `json:"tool_calls,omitempty"`
	Reasoning  string       `json:"reasoning,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	// ToolName：tool 消息是哪个工具的结果。llm.Message 里没有，投影要靠它认出快照
	ToolName string `json:"tool_name,omitempty"`
	// Synthetic：循环自己加的话（提醒调用 done、还剩几步、截图那条消息），不是用户说的。页面不显示
	Synthetic bool `json:"synthetic,omitempty"`
	// PromptTokens：assistant 消息那次调用的上下文有多少 token，第二批压缩判断要不要压时用
	PromptTokens int `json:"prompt_tokens,omitempty"`
	// Details：只给页面看的东西，Message() 不带它，模型永远看不到
	Details *Details `json:"details,omitempty"`
}

// Details 是一条消息上只给页面看的东西（pi 工具结果的 details）：运行时才知道、事后从消息里算不出来的。
// 打开历史对话时，页面上的那条回答从消息和它们的 Details 重放出来（replayTurn）。
// 怎么显示（打码、回答的排版）不存，重放时现做：规则改了，旧对话跟着变
type Details struct {
	// 以下在 tool 结果上，一个调用一份

	// Summary 是这次调用写成的人话（tool_start），还没打码。要查当时那份快照里的元素名，事后算不出来。
	// 空的表示页面上没有这张卡片：跳过了的调用、done
	Summary string `json:"summary,omitempty"`
	Failed  bool   `json:"failed,omitempty"`  // 没做成（tool_end）
	Display string `json:"display,omitempty"` // 做成了的一句话（tool_end），空的显示「完成」；还没打码
	// Product 是 pick_product 做好的卡片：链接和图是插件从页面上取的
	Product *ProductCard `json:"product,omitempty"`
	// Draft 是 draft_post 交给用户确认的草稿：草稿 id、候选图是运行时定的
	Draft *DraftInput `json:"draft,omitempty"`

	// 以下在带着这一轮回答的那一条上：做成了的 done、draft_post 的结果，或者直接回答、收尾的 assistant

	// Final 标出带着回答的那一条。没有它就分不清：失败的、被跳过的、做成了的 done 都可能没有别的 Details。
	// 一轮里找不到它，就是这一轮没跑完（模型出错、用户关了页面）
	Final bool `json:"final,omitempty"`
	// Shots 是回答引用了、存到服务器的图
	Shots []Shot `json:"shots,omitempty"`
}

// StoredPart 是消息的一段：文字或者一张图的网址。截图这类 data: 图片不存，见 storeParts
type StoredPart struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
}

// StoredCall 是一次工具调用
type StoredCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatLog 是对话日志的存取，定义在用它的这一边；实现在 internal/repo/purchase_chat
type ChatLog interface {
	// Entries 返回 userID 的对话 chatID 的全部条目，按写入的先后。对话不存在或者不是他的，found 为 false
	Entries(ctx context.Context, chatID int64, userID string) (entries []Entry, found bool, err error)
	// Append 按顺序追加条目
	Append(ctx context.Context, chatID int64, entries []Entry) error
}

// newEntryID 生成一个条目 id：e_ 加 12 个随机的小写字母和数字，一个对话里撞上的概率可以不管
func newEntryID() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "e_" + string(b)
}

// imageOmitted 是截图这类 data: 图片在日志里的样子。base64 一张就一两百 KB，不存；
// 回答引用了的图已经存到服务器了，网址在那条回答的 Details.Shots 里
const imageOmitted = "（这里原来有一张图，没有保存）"

// messageMeta 是 llm.Message 里没有、存的时候要带上的东西
type messageMeta struct {
	ToolName     string
	Synthetic    bool
	PromptTokens int
	Details      *Details
}

// storeMessage 把一条模型消息变成存储格式
func storeMessage(m llm.Message, meta messageMeta) *StoredMessage {
	s := &StoredMessage{
		V:            storedVersion,
		Role:         m.Role,
		Parts:        storeParts(m.Parts),
		Reasoning:    m.Reasoning,
		ToolCallID:   m.ToolCallID,
		ToolName:     meta.ToolName,
		Synthetic:    meta.Synthetic,
		PromptTokens: meta.PromptTokens,
		Details:      meta.Details,
	}
	for _, c := range m.ToolCalls {
		s.ToolCalls = append(s.ToolCalls, StoredCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments})
	}
	return s
}

// storeParts 留下文字和网址图片（用户上传的截图），data: 图片换成一句话
func storeParts(parts []llm.Part) []StoredPart {
	out := make([]StoredPart, 0, len(parts))
	for _, p := range parts {
		switch {
		case p.ImageURL == "":
			out = append(out, StoredPart{Text: p.Text})
		case strings.HasPrefix(p.ImageURL, "data:"):
			out = append(out, StoredPart{Text: imageOmitted})
		default:
			out = append(out, StoredPart{Image: p.ImageURL})
		}
	}
	return out
}

// Message 把存储格式变回模型消息。图片还是网址：发给模型之前要换成 data URI（loadImages），
// 因为上传的图在我们自己的服务器上，模型服务商访问不到
func (s *StoredMessage) Message() llm.Message {
	m := llm.Message{Role: s.Role, Reasoning: s.Reasoning, ToolCallID: s.ToolCallID}
	for _, p := range s.Parts {
		m.Parts = append(m.Parts, llm.Part{Text: p.Text, ImageURL: p.Image})
	}
	for _, c := range s.ToolCalls {
		m.ToolCalls = append(m.ToolCalls, llm.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments})
	}
	return m
}

var errNoSuchEntry = errors.New("purchase ai: no such entry in the chat")

// pathTo 从 leafID 沿着 parent_id 走回根，按从根到叶的顺序返回这条分支上的条目。
//
// 上下文：
//   - 谁调用：每一轮对话开始时 chatPath（chat.go）：Entries 取出这个对话的全部条目 → pathTo(entries, req.ParentID)
//     → 结果交给 project 拼成历史发给模型，最后一条就是这一轮新消息的 parent
//   - entries：整个对话的所有条目，按写入先后，所有分支混在一起。比如问了一次、又重新生成了一次：
//     {ID: "e1", ParentID: ""}    用户「XM5 值不值」
//     {ID: "e2", ParentID: "e1"}  assistant 调用 done
//     {ID: "e3", ParentID: "e2"}  done 的结果
//     {ID: "e4", ParentID: "e1"}  assistant 重新生成的回答（和 e2 是兄弟）
//     {ID: "e5", ParentID: "e4"}  done 的结果
//   - leafID：请求里的 parent_id，前端从上一轮的 saved 事件里拿到的 leaf。
//     pathTo(entries, "e5") 应该得到 [e1 e4 e5]，e2、e3 是另一条分支，不能混进来
//   - leafID 为空：新对话的第一条消息，前面什么都没有，返回 nil, nil
//   - leafID 是前端传上来的，可能是乱填的、或者别的对话里的 id：找不到就返回包着 errNoSuchEntry 的错误
//     （用 fmt.Errorf("%w: %s", errNoSuchEntry, id)，Chat 那边靠 errors.Is 认出它，回「找不到这段对话」）
//   - 数据库里的东西不能全信：如果 parent_id 绕成了环（a 的父是 b、b 的父是 a），也要返回错误，不能死循环
//
// TODO(精华，你来写)，按这个顺序：
//  1. leafID 为空返回 nil, nil
//  2. 建一个 map[string]Entry，按 ID 能一步找到条目（类比 TS 的 new Map(entries.map(e => [e.id, e]))）
//  3. 从 leafID 开始循环：查 map → 找不到报错 → 走过的再走到就是环，报错 → 放进结果 → 跳到它的 ParentID，
//     ParentID 为空就到根了
//  4. 收集的顺序是从叶到根，最后倒过来（Go 没有 reverse 方法：slices.Reverse，或者两个下标往中间换）
//
// 思考题：为什么第 2 步要先建 map，而不是每走一步都在 entries 里从头找一遍？一个聊了很久的对话有几千条
func pathTo(entries []Entry, leafID string) ([]Entry, error) {
	entryMap := make(map[string]Entry, len(entries))
	for _, e := range entries {
		entryMap[e.ID] = e
	}

	res := make([]Entry, 0)
	seen := map[string]bool{}

	// 空 id 是走到了根；不为空却查不到、或者又走回走过的条目，都是日志有问题
	for id := leafID; id != ""; {
		e, ok := entryMap[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", errNoSuchEntry, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("purchase ai: entry %s is its own ancestor", id)
		}
		seen[id] = true

		res = append(res, e)
		id = e.ParentID
	}

	slices.Reverse(res)
	return res, nil
}
