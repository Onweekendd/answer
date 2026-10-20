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
	"strconv"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/segmentfault/pacman/errors"
)

// 页面投影：打开一段历史对话时，页面要的消息也从日志里出（上下文管理概念 1 的「一份日志，两种投影」）。
// 页面上只有两种消息：用户说的话，和每一轮的回答（重放那一轮推过的事件，和当时看到的一样）。
// 中间的工具调用、结果、提醒都不是页面上的消息

// PageMessage 是页面上的一条消息
type PageMessage struct {
	ID       string   `json:"id"`        // 条目 id：用户消息是它那条 user 条目，回答是那一轮的 view 条目
	ParentID string   `json:"parent_id"` // 页面上的上一条消息；编辑、重新生成会让同一条有好几个下一条
	Role     string   `json:"role"`      // user / assistant
	Text     string   `json:"text,omitempty"`
	Images   []string `json:"images,omitempty"`
	Events   []Event  `json:"events,omitempty"` // 回答：按顺序重放这些事件得到那条回答
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
	return pageMessages(entries), nil
}

// pageMessages 从日志里挑出页面上的消息。条目在日志里的 parent 往往不是页面上的消息
// （回答的 view 接在最后一个工具结果后面），所以每条的页面 parent 是往上找到的第一条页面消息
func pageMessages(entries []Entry) []PageMessage {
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	onPage := func(e Entry) bool {
		return e.View != nil || (e.Message != nil && e.Message.Role == llm.RoleUser && !e.Message.Synthetic)
	}
	pageParent := func(e Entry) string {
		seen := map[string]bool{}
		for id := e.ParentID; id != "" && !seen[id]; {
			seen[id] = true
			p, ok := byID[id]
			if !ok {
				return ""
			}
			if onPage(p) {
				return p.ID
			}
			id = p.ParentID
		}
		return ""
	}

	out := make([]PageMessage, 0)
	for _, e := range entries {
		if !onPage(e) {
			continue
		}
		pm := PageMessage{ID: e.ID, ParentID: pageParent(e)}
		if e.View != nil {
			pm.Role = "assistant"
			pm.Events = e.View.Events
		} else {
			// userMessage 的约定：第一段是用户的话，图片是网址；截图说明、网址列表是写给模型的，不给页面
			pm.Role = "user"
			for i, p := range e.Message.Parts {
				switch {
				case p.Image != "":
					pm.Images = append(pm.Images, p.Image)
				case i == 0:
					pm.Text = p.Text
				}
			}
		}
		out = append(out, pm)
	}
	return out
}
