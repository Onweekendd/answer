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
	"time"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	purchasechat "github.com/apache/answer/internal/service/purchase_chat"
	"github.com/segmentfault/pacman/errors"
	"xorm.io/xorm"
)

type purchaseChatRepo struct {
	data *data.Data
}

// NewPurchaseChatRepo new purchase chat repository
func NewPurchaseChatRepo(data *data.Data) purchasechat.PurchaseChatRepo {
	return &purchaseChatRepo{data: data}
}

func dbError(err error) error {
	if err == nil {
		return nil
	}
	return errors.InternalServer(reason.DatabaseError).WithError(err).WithStack()
}

// AddChat inserts the conversation and fills in its id.
func (r *purchaseChatRepo) AddChat(ctx context.Context, chat *entity.PurchaseChat) error {
	_, err := r.data.DB.Context(ctx).Insert(chat)
	return dbError(err)
}

// GetChat get one conversation by id
func (r *purchaseChatRepo) GetChat(ctx context.Context, id int64) (*entity.PurchaseChat, bool, error) {
	chat := &entity.PurchaseChat{}
	exist, err := r.data.DB.Context(ctx).ID(id).Get(chat)
	if err != nil {
		return nil, false, dbError(err)
	}
	return chat, exist, nil
}

// ListChats returns userID's conversations, the most recently used first.
func (r *purchaseChatRepo) ListChats(ctx context.Context, userID string, limit int) ([]*entity.PurchaseChat, error) {
	chats := make([]*entity.PurchaseChat, 0)
	err := r.data.DB.Context(ctx).Where("user_id = ?", userID).
		Desc("updated_at", "id").Limit(limit).Find(&chats)
	return chats, dbError(err)
}

// SaveMessage inserts the message or overwrites the one with the same
// (chat_id, message_id), and bumps the conversation's updated_at, in one transaction.
func (r *purchaseChatRepo) SaveMessage(ctx context.Context, msg *entity.PurchaseChatMessage) error {
	_, err := r.data.DB.Transaction(func(session *xorm.Session) (any, error) {
		session = session.Context(ctx)
		cond := &entity.PurchaseChatMessage{ChatID: msg.ChatID, MessageID: msg.MessageID}
		exists, err := session.Exist(cond)
		if err != nil {
			return nil, err
		}
		if exists {
			_, err = session.Where("chat_id = ? AND message_id = ?", msg.ChatID, msg.MessageID).
				Cols("parent_id", "content").Update(msg)
		} else {
			_, err = session.Insert(msg)
		}
		if err != nil {
			return nil, err
		}
		_, err = session.ID(msg.ChatID).Cols("updated_at").
			Update(&entity.PurchaseChat{UpdatedAt: time.Now()})
		return nil, err
	})
	return dbError(err)
}

// ListMessages returns a conversation's messages in the order they were first saved.
func (r *purchaseChatRepo) ListMessages(ctx context.Context, chatID int64) ([]*entity.PurchaseChatMessage, error) {
	msgs := make([]*entity.PurchaseChatMessage, 0)
	err := r.data.DB.Context(ctx).Where("chat_id = ?", chatID).Asc("id").Find(&msgs)
	return msgs, dbError(err)
}
