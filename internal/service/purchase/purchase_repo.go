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

package purchase

import (
	"context"
	"errors"

	"github.com/apache/answer/internal/entity"
	"xorm.io/xorm"
)

// Errors PurchaseRepo.Vote returns as they are, so the service can tell them
// apart with errors.Is and turn them into a reason for the user.
var (
	ErrPurchaseNotFound = errors.New("purchase not found")
	ErrVoteClosed       = errors.New("purchase vote closed")
)

// PurchaseRepo purchase detail repository.
// Defined here (consumer side) and implemented in internal/repo/purchase.
type PurchaseRepo interface {
	// UpsertWithSession inserts or updates the purchase detail inside an
	// existing transaction. The caller (question repo) owns the session.
	UpsertWithSession(session *xorm.Session, detail *entity.PurchaseDetail) (err error)
	// GetByQuestionID returns the detail for one question.
	GetByQuestionID(ctx context.Context, questionID string) (detail *entity.PurchaseDetail, exist bool, err error)
	// BatchGetByQuestionIDs returns details for many questions in one query, keyed by question ID.
	// Questions without a detail row are simply absent from the map.
	BatchGetByQuestionIDs(ctx context.Context, questionIDs []string) (details map[string]*entity.PurchaseDetail, err error)
	// UpdateAIStatus writes the ai_status column only. It does not report whether a
	// row matched: the affected count means different things on mysql and postgres.
	UpdateAIStatus(ctx context.Context, questionID string, status int) (err error)
	// UpdateProduct writes the product column only. product is the recognized
	// product as JSON; "" means not recognized yet.
	UpdateProduct(ctx context.Context, questionID string, product string) (err error)
	// GetVoteChoices returns the choice each of userIDs holds on the post, in one
	// query. Users without a vote, and empty IDs, are absent from the map.
	GetVoteChoices(ctx context.Context, questionID string, userIDs []string) (choices map[string]int, err error)
	// Vote records userID's buy / wait / skip choice on a purchase post, one
	// vote per user: voting again changes the choice, and the same choice
	// again changes nothing. Counts are updated in the same transaction while
	// the post row is locked. justClosed is true only for the vote that brings
	// the total to closeAt and closes voting. Returns ErrPurchaseNotFound when
	// the post has no purchase detail and ErrVoteClosed once voting is closed.
	Vote(ctx context.Context, questionID, userID string, choice, closeAt int) (
		detail *entity.PurchaseDetail, justClosed bool, err error)
}
