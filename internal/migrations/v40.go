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

package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/answer/internal/entity"
	"xorm.io/xorm"
)

// purchaseChatMessageV40 is purchase_chat_message as v40 created it: the chat page's
// own JSON for each message. The page no longer uses it (the conversation's log in
// purchase_chat_entry replaced it, v42), so the entity is gone and the table's
// shape is kept here for this migration.
type purchaseChatMessageV40 struct {
	ID        int64     `xorm:"not null pk autoincr BIGINT(20) id"`
	ChatID    int64     `xorm:"not null UNIQUE(chat_message) BIGINT(20) chat_id"`
	MessageID string    `xorm:"not null UNIQUE(chat_message) VARCHAR(64) message_id"`
	ParentID  string    `xorm:"not null default '' VARCHAR(64) parent_id"`
	Content   string    `xorm:"not null MEDIUMTEXT content"`
	CreatedAt time.Time `xorm:"created not null default CURRENT_TIMESTAMP TIMESTAMP created_at"`
	UpdatedAt time.Time `xorm:"updated not null default CURRENT_TIMESTAMP TIMESTAMP updated_at"`
}

func (purchaseChatMessageV40) TableName() string {
	return "purchase_chat_message"
}

// addPurchaseChat creates the tables that kept the chat page's conversations.
func addPurchaseChat(ctx context.Context, x *xorm.Engine) error {
	err := x.Context(ctx).Sync(new(entity.PurchaseChat), new(purchaseChatMessageV40))
	if err != nil {
		return fmt.Errorf("sync purchase chat tables failed: %w", err)
	}
	return nil
}
