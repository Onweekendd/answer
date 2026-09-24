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

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/service/purchase"
	"github.com/apache/answer/pkg/uid"
	"github.com/segmentfault/pacman/errors"
	"xorm.io/xorm"
)

// purchaseRepo purchase detail repository
type purchaseRepo struct {
	data *data.Data
}

func dbError(err error) error {
	// a nil in must stay nil, so callers can write "return dbError(err)"
	if err == nil {
		return nil
	}
	return errors.InternalServer(reason.DatabaseError).
		WithError(err).
		WithStack()
}

// NewPurchaseRepo new purchase detail repository
func NewPurchaseRepo(data *data.Data) purchase.PurchaseRepo {
	return &purchaseRepo{data: data}
}

// UpsertWithSession inserts or updates the purchase detail inside the caller's transaction.
func (pr *purchaseRepo) UpsertWithSession(session *xorm.Session, detail *entity.PurchaseDetail) error {
	detail.QuestionID = uid.DeShortID(detail.QuestionID)

	exists, err := session.ID(detail.QuestionID).Exist(&entity.PurchaseDetail{})

	if err != nil {
		return dbError(err)
	}

	if exists {
		_, err = session.ID(detail.QuestionID).Cols("price", "currency", "link", "images").Update(detail)
	} else {
		_, err = session.Insert(detail)
	}

	if err != nil {
		return dbError(err)
	}

	return nil
}

// GetByQuestionID get purchase detail by question id
func (pr *purchaseRepo) GetByQuestionID(ctx context.Context, questionID string) (
	detail *entity.PurchaseDetail, exist bool, err error) {
	questionID = uid.DeShortID(questionID)
	detail = &entity.PurchaseDetail{}

	exist, err = pr.data.DB.Context(ctx).ID(questionID).Get(detail)
	if err != nil {
		return nil, false, dbError(err)
	}

	if !exist {
		return nil, false, nil
	}

	return detail, exist, nil
}

// BatchGetByQuestionIDs get purchase details by question ids in one query
func (pr *purchaseRepo) BatchGetByQuestionIDs(ctx context.Context, questionIDs []string) (
	details map[string]*entity.PurchaseDetail, err error) {
	details = make(map[string]*entity.PurchaseDetail, len(questionIDs))
	if len(questionIDs) == 0 {
		return details, nil
	}
	ids := make([]string, 0, len(questionIDs))
	for _, qid := range questionIDs {
		ids = append(ids, uid.DeShortID(qid))
	}

	list := make([]*entity.PurchaseDetail, 0, len(ids))
	err = pr.data.DB.Context(ctx).In("question_id", ids).Find(&list)
	if err != nil {
		return nil, dbError(err)
	}

	for _, d := range list {
		details[d.QuestionID] = d
	}

	return details, nil
}

func (pr *purchaseRepo) UpdateAIStatus(ctx context.Context, questionID string, status int) (err error) {
	questionID = uid.DeShortID(questionID)

	_, err = pr.data.DB.Context(ctx).ID(questionID).Cols("ai_status").Update(&entity.PurchaseDetail{
		AIStatus: status,
	})

	return dbError(err)
}
