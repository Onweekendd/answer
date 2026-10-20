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
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/service/sandbox"
)

// sandboxStore keeps which sandbox each conversation works in, in purchase_chat.sandbox_id.
type sandboxStore struct {
	data *data.Data
}

// NewSandboxStore new sandbox store
func NewSandboxStore(data *data.Data) sandbox.Store {
	return &sandboxStore{data: data}
}

// SandboxOf returns the sandbox recorded for userID's conversation; found is false when
// the conversation does not exist or belongs to someone else.
func (s *sandboxStore) SandboxOf(ctx context.Context, chatID int64, userID string) (string, bool, error) {
	chat := &entity.PurchaseChat{}
	found, err := s.data.DB.Context(ctx).Where("id = ? AND user_id = ?", chatID, userID).
		Cols("sandbox_id").Get(chat)
	if err != nil {
		return "", false, dbError(err)
	}
	return chat.SandboxID, found, nil
}

// SwapSandbox records newID only while the recorded sandbox is still old, in one UPDATE:
// the database compares and sets atomically, so of two writers racing from the same
// old value exactly one wins. It leaves updated_at alone: the chat list is ordered by
// when the user last talked, not by when the agent got a sandbox.
func (s *sandboxStore) SwapSandbox(ctx context.Context, chatID int64, old, newID string) (bool, error) {
	n, err := s.data.DB.Context(ctx).Where("id = ? AND sandbox_id = ?", chatID, old).
		Cols("sandbox_id").NoAutoTime().Update(&entity.PurchaseChat{SandboxID: newID})
	if err != nil {
		return false, dbError(err)
	}
	return n == 1, nil
}
