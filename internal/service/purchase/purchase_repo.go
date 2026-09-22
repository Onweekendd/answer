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

	"github.com/apache/answer/internal/entity"
	"xorm.io/xorm"
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
}
