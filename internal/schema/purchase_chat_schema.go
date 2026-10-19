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

package schema

import "encoding/json"

// PurchaseChatCreateReq starts a conversation; the page sends the first question as the title.
type PurchaseChatCreateReq struct {
	Title  string `json:"title"`
	UserID string `json:"-"`
}

// PurchaseChatCreateResp is the new conversation's id.
type PurchaseChatCreateResp struct {
	ID string `json:"id"`
}

// PurchaseChatItem is one row of the conversation list.
type PurchaseChatItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
}

// PurchaseChatGetReq asks for one conversation with its messages.
type PurchaseChatGetReq struct {
	ID     string `validate:"required" form:"id"`
	UserID string `json:"-"`
}

// PurchaseChatResp is one conversation with its messages in the order they were first saved.
type PurchaseChatResp struct {
	ID       string                     `json:"id"`
	Title    string                     `json:"title"`
	Messages []*PurchaseChatMessageItem `json:"messages"`
}

// PurchaseChatMessageItem is a saved message. Message is the page's JSON, as it was sent.
type PurchaseChatMessageItem struct {
	ParentID string          `json:"parent_id"`
	Message  json.RawMessage `json:"message" swaggertype:"object"`
}

// PurchaseChatMessageReq saves one message; saving the same message_id again overwrites it.
type PurchaseChatMessageReq struct {
	ChatID    string          `validate:"required" json:"chat_id"`
	MessageID string          `validate:"required,max=64" json:"message_id"`
	ParentID  string          `validate:"max=64" json:"parent_id"`
	Message   json.RawMessage `validate:"required" json:"message" swaggertype:"object"`
	UserID    string          `json:"-"`
}
