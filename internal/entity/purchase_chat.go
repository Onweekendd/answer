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

package entity

import "time"

// PurchaseChat is one conversation on the chat page. Its messages live in
// purchase_chat_message.
type PurchaseChat struct {
	ID     int64  `xorm:"not null pk autoincr BIGINT(20) id"`
	UserID string `xorm:"not null default 0 BIGINT(20) INDEX user_id"`
	Title  string `xorm:"not null default '' VARCHAR(100) title"`
	// SandboxID is the CubeSandbox sandbox the agent works in for this conversation;
	// empty until the agent first needs one. See internal/service/sandbox.
	SandboxID string    `xorm:"not null default '' VARCHAR(64) sandbox_id"`
	CreatedAt time.Time `xorm:"created not null default CURRENT_TIMESTAMP TIMESTAMP created_at"`
	UpdatedAt time.Time `xorm:"updated not null default CURRENT_TIMESTAMP TIMESTAMP updated_at"`
}

// TableName purchase chat table name
func (PurchaseChat) TableName() string {
	return "purchase_chat"
}

// PurchaseChatMessage is one message of a chat, kept as the page's own JSON so
// the page gets back exactly what it showed: text, thinking, steps and cards.
// MessageID is the page's id for the message; (chat_id, message_id) is unique,
// so saving the same message again overwrites it.
type PurchaseChatMessage struct {
	ID        int64     `xorm:"not null pk autoincr BIGINT(20) id"`
	ChatID    int64     `xorm:"not null UNIQUE(chat_message) BIGINT(20) chat_id"`
	MessageID string    `xorm:"not null UNIQUE(chat_message) VARCHAR(64) message_id"`
	ParentID  string    `xorm:"not null default '' VARCHAR(64) parent_id"`
	Content   string    `xorm:"not null MEDIUMTEXT content"`
	CreatedAt time.Time `xorm:"created not null default CURRENT_TIMESTAMP TIMESTAMP created_at"`
	UpdatedAt time.Time `xorm:"updated not null default CURRENT_TIMESTAMP TIMESTAMP updated_at"`
}

// TableName purchase chat message table name
func (PurchaseChatMessage) TableName() string {
	return "purchase_chat_message"
}
