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
	"strconv"
	"strings"

	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
)

const (
	// listLimit is how many recent conversations the list shows.
	listLimit = 50
	// maxTitleRunes keeps the title to what fits on one line of the list.
	maxTitleRunes = 40
	defaultTitle  = "新对话"
)

// PurchaseChatRepo stores conversations.
// Defined here (consumer side) and implemented in internal/repo/purchase_chat.
type PurchaseChatRepo interface {
	AddChat(ctx context.Context, chat *entity.PurchaseChat) (err error)
	// ListChats returns userID's conversations, the most recently used first.
	ListChats(ctx context.Context, userID string, limit int) (chats []*entity.PurchaseChat, err error)
}

// PurchaseChatService keeps the chat page's list of conversations. What was said
// in them is the conversation's log, kept and read by the purchase_ai service.
type PurchaseChatService struct {
	repo PurchaseChatRepo
}

// NewPurchaseChatService new purchase chat service
func NewPurchaseChatService(repo PurchaseChatRepo) *PurchaseChatService {
	return &PurchaseChatService{repo: repo}
}

// Create starts a conversation for req.UserID.
func (s *PurchaseChatService) Create(ctx context.Context, req *schema.PurchaseChatCreateReq) (
	*schema.PurchaseChatCreateResp, error) {
	chat := &entity.PurchaseChat{UserID: req.UserID, Title: chatTitle(req.Title)}
	if err := s.repo.AddChat(ctx, chat); err != nil {
		return nil, err
	}
	return &schema.PurchaseChatCreateResp{ID: strconv.FormatInt(chat.ID, 10)}, nil
}

// List returns userID's recent conversations.
func (s *PurchaseChatService) List(ctx context.Context, userID string) ([]*schema.PurchaseChatItem, error) {
	chats, err := s.repo.ListChats(ctx, userID, listLimit)
	if err != nil {
		return nil, err
	}
	items := make([]*schema.PurchaseChatItem, 0, len(chats))
	for _, c := range chats {
		items = append(items, &schema.PurchaseChatItem{
			ID:        strconv.FormatInt(c.ID, 10),
			Title:     c.Title,
			UpdatedAt: c.UpdatedAt.Unix(),
		})
	}
	return items, nil
}

// chatTitle is the first line of the first question, cut to fit the list.
func chatTitle(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultTitle
	}
	if r := []rune(line); len(r) > maxTitleRunes {
		return string(r[:maxTitleRunes]) + "…"
	}
	return line
}
