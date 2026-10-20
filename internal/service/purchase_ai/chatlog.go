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
//	user（用户的话）→ assistant（工具调用）→ tool（结果）→ … → assistant（回答）→ view（页面要的东西）

// 条目的种类
const (
	EntryMessage = "message" // 一条模型消息，进模型的上下文
	EntryView    = "view"    // 这一轮推给页面的事件，只给页面看，模型永远看不到
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
	View      *StoredView    // Kind 为 view 时有
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

// StoredView 是一轮推给页面的事件，打开历史对话时页面用它们重放出那条回答
type StoredView struct {
	V      int     `json:"v"`
	Events []Event `json:"events"`
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
// 回答引用了的图已经存到服务器了，网址在 view 里
const imageOmitted = "（这里原来有一张图，没有保存）"

// messageMeta 是 llm.Message 里没有、存的时候要带上的东西
type messageMeta struct {
	ToolName     string
	Synthetic    bool
	PromptTokens int
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
// leafID 为空返回空（新对话的第一条消息）。找不到、或者 parent_id 绕成了环都是错误
func pathTo(entries []Entry, leafID string) ([]Entry, error) {
	if leafID == "" {
		return nil, nil
	}
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	var path []Entry
	seen := map[string]bool{}
	for id := leafID; id != ""; {
		e, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", errNoSuchEntry, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("purchase ai: entry %s is its own ancestor", id)
		}
		seen[id] = true
		path = append(path, e)
		id = e.ParentID
	}
	// 收集的时候是从叶到根，倒过来
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}
