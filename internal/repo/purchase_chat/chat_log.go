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

package purchasechat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	"xorm.io/xorm"
)

// chatLog keeps each conversation's log in purchase_chat_entry.
type chatLog struct {
	data *data.Data
}

// NewChatLog new chat log
func NewChatLog(data *data.Data) purchaseai.ChatLog {
	return &chatLog{data: data}
}

// Entries 取出一个对话的全部条目（读），见 ChatLog 接口。
//
//   - 谁调用：每一轮对话开始时 PurchaseAIService.chatPath（chat.go），交给 pathTo 走出一条分支、
//     再交给 project 拼成历史发给模型；打开旧对话时 ChatHistory（chat_page.go），交给 pageMessages 给页面重放
//   - chatID 是前端传上来的，谁都可以随便填一个数字，所以先确认 purchase_chat 里这个对话是 userID 的；
//     不是就返回 nil, false, nil，不算错误（调用方会回「找不到这段对话」）
//   - 按自增的 id 排，不按 created_at：一轮里的几十条可能在同一秒写进去
//   - 某一行解不开就整个返回错误：日志坏了，不能悄悄少一条
func (r *chatLog) Entries(ctx context.Context, chatID int64, userID string) ([]purchaseai.Entry, bool, error) {
	// ① 对话是不是 userID 的：chat_id 是前端传上来的，不能信
	found, err := r.data.DB.Context(ctx).Where("id = ? AND user_id = ?", chatID, userID).Exist(&entity.PurchaseChat{})
	if err != nil {
		return nil, false, dbError(err)
	}
	if !found {
		return nil, false, nil
	}

	// ② 这个对话的全部条目，按写入的先后
	rows := make([]entity.PurchaseChatEntry, 0)
	err = r.data.DB.Context(ctx).Where("chat_id = ?", chatID).Asc("id").Find(&rows)
	if err != nil {
		return nil, false, dbError(err)
	}

	// ③ 每一行解开 body，变成 Entry
	res := make([]purchaseai.Entry, 0, len(rows))
	for _, row := range rows {
		e, err := fromRow(&row)
		if err != nil {
			return nil, false, err
		}

		res = append(res, e)
	}

	return res, true, nil
}

// Append 把一批条目追加进日志（写），见 ChatLog 接口。
//
//   - 谁调用：chatRecorder（recorder.go），每条立刻写，失败的攒着下次一起写，所以 entries 可能是一条也可能是几条。
//     ID 和 ParentID 已经由 recorder 串好了，这里只管按顺序存
//   - 每个条目在 purchase_chat_entry 插一行；再把 purchase_chat 的 updated_at 改成现在，
//     侧边栏的对话列表按它倒序（ListChats），刚聊过的排最前
//   - 放在一个事务里：插到一半断了，链上就少了中间几条，下一轮 pathTo 会走不通
//   - 一条一条插而不是一次插一批：Entries 靠自增 id 排先后，逐条插保证 id 和顺序一致
//   - 表上有 UNIQUE(chat_id, entry_id)：同一个条目再插一次会报错，这是有意的
func (r *chatLog) Append(ctx context.Context, chatID int64, entries []purchaseai.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	rows := make([]*entity.PurchaseChatEntry, 0)

	for _, e := range entries {
		row, err := toRow(chatID, e)
		if err != nil {
			return err
		}

		rows = append(rows, row)
	}

	_, err := r.data.DB.Transaction(func(session *xorm.Session) (any, error) {
		session = session.Context(ctx)

		for _, row := range rows {
			if _, err := session.Insert(row); err != nil {
				return nil, err
			}
		}

		_, err := session.ID(chatID).Cols("updated_at").Update(&entity.PurchaseChat{})
		if err != nil {
			return nil, err
		}

		return nil, nil
	})

	if err != nil {
		return dbError(err)
	}

	return nil
}

func toRow(chatID int64, e purchaseai.Entry) (*entity.PurchaseChatEntry, error) {
	var body any
	switch e.Kind {
	case purchaseai.EntryMessage:
		body = e.Message
	default:
		return nil, fmt.Errorf("purchase chat: unknown entry kind %q", e.Kind)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &entity.PurchaseChatEntry{ChatID: chatID, EntryID: e.ID, ParentID: e.ParentID, Kind: e.Kind, Body: string(raw)}, nil
}

func fromRow(row *entity.PurchaseChatEntry) (purchaseai.Entry, error) {
	e := purchaseai.Entry{ID: row.EntryID, ParentID: row.ParentID, Kind: row.Kind, CreatedAt: row.CreatedAt}
	var err error
	switch row.Kind {
	case purchaseai.EntryMessage:
		e.Message = &purchaseai.StoredMessage{}
		err = json.Unmarshal([]byte(row.Body), e.Message)
	default:
		err = fmt.Errorf("unknown entry kind %q", row.Kind)
	}
	if err != nil {
		return purchaseai.Entry{}, fmt.Errorf("purchase chat: entry %s: %w", row.EntryID, err)
	}
	return e, nil
}
