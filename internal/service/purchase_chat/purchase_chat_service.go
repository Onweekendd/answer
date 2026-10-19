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

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/segmentfault/pacman/errors"
)

const (
	// listLimit is how many recent conversations the list shows.
	listLimit = 50
	// maxTitleRunes keeps the title to what fits on one line of the list.
	maxTitleRunes = 40
	// maxMessageBytes caps one saved message. A long answer with its thinking
	// and steps is well under 100KB; this only stops abuse.
	maxMessageBytes = 512 << 10
	defaultTitle    = "新对话"
)

// PurchaseChatRepo stores conversations and their messages.
// Defined here (consumer side) and implemented in internal/repo/purchase_chat.
type PurchaseChatRepo interface {
	AddChat(ctx context.Context, chat *entity.PurchaseChat) (err error)
	GetChat(ctx context.Context, id int64) (chat *entity.PurchaseChat, exist bool, err error)
	// ListChats returns userID's conversations, the most recently used first.
	ListChats(ctx context.Context, userID string, limit int) (chats []*entity.PurchaseChat, err error)
	// SaveMessage inserts the message, or overwrites the one with the same
	// (chat_id, message_id), and marks the conversation as just used.
	SaveMessage(ctx context.Context, msg *entity.PurchaseChatMessage) (err error)
	// ListMessages returns a conversation's messages in the order they were first saved.
	ListMessages(ctx context.Context, chatID int64) (msgs []*entity.PurchaseChatMessage, err error)
}

// PurchaseChatService keeps the chat page's conversations. It stores what the
// page sends and gives it back; it does not read the messages.
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

// Get returns one of req.UserID's conversations with its messages.
func (s *PurchaseChatService) Get(ctx context.Context, req *schema.PurchaseChatGetReq) (*schema.PurchaseChatResp, error) {
	chat, err := s.ownChat(ctx, req.ID, req.UserID)
	if err != nil {
		return nil, err
	}
	msgs, err := s.repo.ListMessages(ctx, chat.ID)
	if err != nil {
		return nil, err
	}
	resp := &schema.PurchaseChatResp{
		ID:       strconv.FormatInt(chat.ID, 10),
		Title:    chat.Title,
		Messages: make([]*schema.PurchaseChatMessageItem, 0, len(msgs)),
	}
	for _, m := range msgs {
		resp.Messages = append(resp.Messages, &schema.PurchaseChatMessageItem{
			ParentID: m.ParentID,
			Message:  []byte(m.Content),
		})
	}
	return resp, nil
}

// SaveMessage saves one message into one of req.UserID's conversations.
func (s *PurchaseChatService) SaveMessage(ctx context.Context, req *schema.PurchaseChatMessageReq) error {
	if len(req.Message) > maxMessageBytes {
		return errors.BadRequest(reason.RequestFormatError)
	}
	chat, err := s.ownChat(ctx, req.ChatID, req.UserID)
	if err != nil {
		return err
	}
	return s.repo.SaveMessage(ctx, &entity.PurchaseChatMessage{
		ChatID:    chat.ID,
		MessageID: req.MessageID,
		ParentID:  req.ParentID,
		Content:   string(req.Message),
	})
}

// ownChat finds the conversation and checks it is userID's. Someone else's
// conversation looks the same as a missing one, so ids cannot be probed.
func (s *PurchaseChatService) ownChat(ctx context.Context, id, userID string) (*entity.PurchaseChat, error) {
	chatID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, errors.NotFound(reason.ObjectNotFound)
	}
	chat, exist, err := s.repo.GetChat(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if !exist || chat.UserID != userID {
		return nil, errors.NotFound(reason.ObjectNotFound)
	}
	return chat, nil
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
