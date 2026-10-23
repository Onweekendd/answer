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
	stderrors "errors"

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
		_, err = session.ID(detail.QuestionID).Cols("price", "currency", "link", "images", "product").Update(detail)
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

func (pr *purchaseRepo) UpdateProduct(ctx context.Context, questionID, product string) (err error) {
	questionID = uid.DeShortID(questionID)

	_, err = pr.data.DB.Context(ctx).ID(questionID).Cols("product").Update(&entity.PurchaseDetail{
		Product: product,
	})

	return dbError(err)
}

// GetVoteChoices implements [purchase.PurchaseRepo].
func (pr *purchaseRepo) GetVoteChoices(ctx context.Context, questionID string, userIDs []string) (
	choices map[string]int, err error) {
	choices = make(map[string]int, len(userIDs))
	ids := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return choices, nil
	}

	votes := make([]*entity.PurchaseVote, 0, len(ids))
	err = pr.data.DB.Context(ctx).Where("question_id = ?", uid.DeShortID(questionID)).
		In("user_id", ids).Find(&votes)
	if err != nil {
		return nil, dbError(err)
	}
	for _, v := range votes {
		choices[v.UserID] = v.Choice
	}
	return choices, nil
}

// Vote implements [purchase.PurchaseRepo].
func (pr *purchaseRepo) Vote(ctx context.Context, questionID string, userID string, choice int, closeAt int) (detail *entity.PurchaseDetail, justClosed bool, err error) {
	questionID = uid.DeShortID(questionID)
	detail = &entity.PurchaseDetail{}

	// ① 开事务
	_, err = pr.data.DB.Transaction(func(session *xorm.Session) (any, error) {
		session = session.Context(ctx)
		//② FOR UPDATE 锁住帖子
		exist, err := session.ID(questionID).ForUpdate().Get(detail)
		if err != nil {
			return nil, err
		}

		if !exist {
			return nil, purchase.ErrPurchaseNotFound
		}
		//   已关闭 → 返回错误（自动回滚）
		if detail.VoteClosed {
			return nil, purchase.ErrVoteClosed
		}
		//③ 查这个人原来的票

		oldVote := &entity.PurchaseVote{}
		voted, err := session.Where("question_id = ? AND user_id = ?", questionID, userID).Get(oldVote)
		if err != nil {
			return nil, err
		}

		prevChoice := oldVote.Choice

		if voted && prevChoice == choice {
			return nil, nil
		}

		//④ 写票（插入或更新 choice）
		if voted {
			oldVote.Choice = choice
			_, err = session.Where("question_id = ? AND user_id = ?", questionID, userID).Cols("choice").Update(oldVote)

			if err != nil {
				return nil, err
			}

			_, err = session.ID(questionID).Decr(voteCountColumn(prevChoice)).Update(&entity.PurchaseDetail{})
			if err != nil {
				return nil, err
			}

		} else {
			oldVote.QuestionID = questionID
			oldVote.UserID = userID
			oldVote.Choice = choice

			_, err = session.Insert(oldVote)

			if err != nil {
				return nil, err
			}

		}
		//⑤ Incr / Decr 改计数
		_, err = session.ID(questionID).Incr(voteCountColumn(choice)).Update(&entity.PurchaseDetail{})
		if err != nil {
			return nil, err
		}

		//⑥ CAS 关闭：UPDATE ... WHERE vote_closed = false AND 总票数 >= 10
		affected, err := session.
			Where("question_id = ? AND vote_closed = ? AND buy_count + wait_count + skip_count >= ?",
				questionID, false, closeAt).
			Cols("vote_closed").
			Update(&entity.PurchaseDetail{VoteClosed: true})

		if err != nil {
			return nil, err
		}

		//   受影响行数 == 1 → justClosed
		if affected == 1 {
			justClosed = true
		}
		//⑦ 返回最新票数；事务结束，锁释放

		fresh := &entity.PurchaseDetail{}
		_, err = session.ID(questionID).Get(fresh)
		if err != nil {
			return nil, err
		}

		detail = fresh

		return nil, nil
	})

	if err != nil {
		// 哨兵错误原样返回，service 才能用 errors.Is 认出来
		if stderrors.Is(err, purchase.ErrPurchaseNotFound) || stderrors.Is(err, purchase.ErrVoteClosed) {
			return nil, false, err
		}
		return nil, false, dbError(err)
	}

	return detail, justClosed, nil
}

// voteCountColumn maps a vote choice to its counter column on purchase_detail.
func voteCountColumn(choice int) string {
	switch choice {
	case entity.PurchaseVoteBuy:
		return "buy_count"
	case entity.PurchaseVoteWait:
		return "wait_count"
	case entity.PurchaseVoteSkip:
		return "skip_count"
	}

	return ""
}
