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
	"time"

	"github.com/apache/answer/internal/base/data"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
)

// draftTTL is how long a draft waits for the user. A draft left longer than this
// can still be published from the page; the AI just does not vote on it.
const draftTTL = 30 * 24 * time.Hour

const draftKeyPrefix = "answer:purchase:draft:"

type draftRepo struct {
	data *data.Data
}

// NewDraftRepo keeps drafts in the cache
func NewDraftRepo(data *data.Data) purchaseai.DraftStore {
	return &draftRepo{data: data}
}

// SaveDraft keeps the draft for draftTTL
func (r *draftRepo) SaveDraft(ctx context.Context, id string, draft *purchaseai.Draft) error {
	raw, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	return dbError(r.data.Cache.SetString(ctx, draftKeyPrefix+id, string(raw), draftTTL))
}

// GetDraft returns the draft, or exist false when it is unknown or expired
func (r *draftRepo) GetDraft(ctx context.Context, id string) (*purchaseai.Draft, bool, error) {
	raw, exist, err := r.data.Cache.GetString(ctx, draftKeyPrefix+id)
	if err != nil || !exist {
		return nil, false, dbError(err)
	}
	draft := &purchaseai.Draft{}
	if err := json.Unmarshal([]byte(raw), draft); err != nil {
		return nil, false, nil
	}
	return draft, true, nil
}

// DeleteDraft forgets the draft
func (r *draftRepo) DeleteDraft(ctx context.Context, id string) error {
	return dbError(r.data.Cache.Del(ctx, draftKeyPrefix+id))
}
