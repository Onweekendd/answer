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

// Entries returns all of userID's conversation's entries in the order they were
// written; found is false when the conversation does not exist or is someone else's.
func (r *chatLog) Entries(ctx context.Context, chatID int64, userID string) ([]purchaseai.Entry, bool, error) {
	found, err := r.data.DB.Context(ctx).Where("id = ? AND user_id = ?", chatID, userID).Exist(&entity.PurchaseChat{})
	if err != nil || !found {
		return nil, false, dbError(err)
	}
	rows := make([]*entity.PurchaseChatEntry, 0)
	if err := r.data.DB.Context(ctx).Where("chat_id = ?", chatID).Asc("id").Find(&rows); err != nil {
		return nil, false, dbError(err)
	}
	entries := make([]purchaseai.Entry, 0, len(rows))
	for _, row := range rows {
		e, err := fromRow(row)
		if err != nil {
			return nil, false, err
		}
		entries = append(entries, e)
	}
	return entries, true, nil
}

// Append inserts the entries in one transaction and marks the conversation as just used.
func (r *chatLog) Append(ctx context.Context, chatID int64, entries []purchaseai.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	rows := make([]*entity.PurchaseChatEntry, 0, len(entries))
	for _, e := range entries {
		row, err := toRow(chatID, e)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	_, err := r.data.DB.Transaction(func(session *xorm.Session) (any, error) {
		session = session.Context(ctx)
		// one by one, not a bulk insert: the auto-increment id is what keeps them in order
		for _, row := range rows {
			if _, err := session.Insert(row); err != nil {
				return nil, err
			}
		}
		_, err := session.ID(chatID).Cols("updated_at").Update(&entity.PurchaseChat{})
		return nil, err
	})
	return dbError(err)
}

func toRow(chatID int64, e purchaseai.Entry) (*entity.PurchaseChatEntry, error) {
	var body any
	switch e.Kind {
	case purchaseai.EntryMessage:
		body = e.Message
	case purchaseai.EntryView:
		body = e.View
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
	case purchaseai.EntryView:
		e.View = &purchaseai.StoredView{}
		err = json.Unmarshal([]byte(row.Body), e.View)
	default:
		err = fmt.Errorf("unknown entry kind %q", row.Kind)
	}
	if err != nil {
		return purchaseai.Entry{}, fmt.Errorf("purchase chat: entry %s: %w", row.EntryID, err)
	}
	return e, nil
}
