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

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	purchasechat "github.com/apache/answer/internal/service/purchase_chat"
	"github.com/segmentfault/pacman/errors"
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

// ListChats returns userID's conversations, the most recently used first.
func (r *purchaseChatRepo) ListChats(ctx context.Context, userID string, limit int) ([]*entity.PurchaseChat, error) {
	chats := make([]*entity.PurchaseChat, 0)
	err := r.data.DB.Context(ctx).Where("user_id = ?", userID).
		Desc("updated_at", "id").Limit(limit).Find(&chats)
	return chats, dbError(err)
}
