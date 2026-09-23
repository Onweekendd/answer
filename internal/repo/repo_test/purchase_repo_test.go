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

package repo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/repo/purchase"
	"github.com/apache/answer/internal/repo/question"
	"github.com/apache/answer/internal/repo/unique"
	purchaseservice "github.com/apache/answer/internal/service/purchase"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// newPurchaseRepos wires the two repos under test against the shared test database.
func newPurchaseRepos(t *testing.T) (purchaseservice.PurchaseRepo, questioncommon.QuestionRepo) {
	t.Helper()

	uniqueIDRepo := unique.NewUniqueIDRepo(testDataSource)
	purchaseRepo := purchase.NewPurchaseRepo(testDataSource)
	questionRepo := question.NewQuestionRepo(testDataSource, uniqueIDRepo, purchaseRepo)
	return purchaseRepo, questionRepo
}

// newTestQuestion returns an unsaved question owned by a fake user.
// AddQuestionWithPurchase fills in the ID.
func newTestQuestion(title string) *entity.Question {
	now := time.Now()

	return &entity.Question{
		UserID:           "1",
		Title:            title,
		OriginalText:     "original text",
		ParsedText:       "<p>parsed text</p>",
		Status:           entity.QuestionStatusAvailable,
		CreatedAt:        now,
		UpdatedAt:        now,
		PostUpdateTime:   now,
		LastEditUserID:   "0",
		AcceptedAnswerID: "0",
		LastAnswerID:     "0",
		RevisionID:       "0",
	}
}

func newTestDetail(price float64, images string) *entity.PurchaseDetail {
	return &entity.PurchaseDetail{
		Price:    price,
		Currency: "CNY",
		Link:     "https://item.jd.com/1.html",
		Images:   images,
	}
}

// withSession runs fn inside a transaction, the way the question repo calls
// UpsertWithSession in production.
func withSession(t *testing.T, fn func(session *xorm.Session) error) error {
	t.Helper()

	_, err := testDataSource.DB.Transaction(func(session *xorm.Session) (any, error) {
		return nil, fn(session)
	})
	return err
}

func Test_purchaseRepo_UpsertWithSession(t *testing.T) {
	purchaseRepo, questionRepo := newPurchaseRepos(t)
	ctx := context.TODO()

	// Arrange: one question that already has a purchase detail
	question := newTestQuestion("upsert test")
	err := questionRepo.AddQuestionWithPurchase(ctx, question, newTestDetail(5999, `["a.png"]`))
	require.NoError(t, err)

	t.Run("第二次 upsert 更新同一行而不是插入新行", func(t *testing.T) {
		require.NoError(t, withSession(t, func(session *xorm.Session) error {
			return purchaseRepo.UpsertWithSession(session, &entity.PurchaseDetail{
				QuestionID: question.ID,
				Price:      5499,
				Images:     `["b.png"]`,
				AIStatus:   entity.PurchaseAIStatusFinished,
			})
		}))

		// Assert: TODO 2 —— GetByQuestionID 取回来，exist 为 true，价格是 5499、images 是新的
		//         再断言表里只有一行：用 testDataSource.DB.Where("question_id = ?", ...).Count(...)
		detail, exist, err := purchaseRepo.GetByQuestionID(ctx, question.ID)
		require.NoError(t, err)
		require.True(t, exist)
		require.Equal(t, 5499.0, detail.Price)
		require.Equal(t, `["b.png"]`, detail.Images)

		questionCount, err := testDataSource.DB.Where("question_id = ?", question.ID).Count(&entity.PurchaseDetail{})
		require.NoError(t, err)
		require.Equal(t, int64(1), questionCount)
	})

	t.Run("同样的内容写两次结果不变（幂等）", func(t *testing.T) {
		d := &entity.PurchaseDetail{QuestionID: question.ID, Price: 5499, Currency: "CNY", Images: `["b.png"]`}

		for i := 0; i < 2; i++ {
			require.NoError(t, withSession(t, func(session *xorm.Session) error {
				return purchaseRepo.UpsertWithSession(session, d)
			}))
		}

		detail, exist, err := purchaseRepo.GetByQuestionID(ctx, question.ID)
		require.NoError(t, err)
		require.True(t, exist)
		require.Equal(t, 5499.0, detail.Price)
		require.Equal(t, `["b.png"]`, detail.Images)

		questionCount, err := testDataSource.DB.Where("question_id = ?", question.ID).Count(&entity.PurchaseDetail{})
		require.NoError(t, err)
		require.Equal(t, int64(1), questionCount)

	})
}

func Test_purchaseRepo_GetByQuestionID(t *testing.T) {
	purchaseRepo, questionRepo := newPurchaseRepos(t)
	ctx := context.TODO()

	t.Run("存在时返回详情", func(t *testing.T) {
		// TODO 4 —— 建一个带 purchase 的 question，取回来断言字段
		question := newTestQuestion("get by question id test")
		err := questionRepo.AddQuestionWithPurchase(ctx, question, newTestDetail(5999, `["a.png"]`))
		require.NoError(t, err)

		detail, exist, err := purchaseRepo.GetByQuestionID(ctx, question.ID)
		require.NoError(t, err)
		require.True(t, exist)
		require.Equal(t, 5999.0, detail.Price)
		require.Equal(t, `["a.png"]`, detail.Images)
	})

	t.Run("不存在时返回 exist=false 且不报错", func(t *testing.T) {

		detail, exist, err := purchaseRepo.GetByQuestionID(ctx, "10010000000000999")
		require.NoError(t, err)
		require.False(t, exist)
		require.Nil(t, detail)
	})
}

func Test_purchaseRepo_BatchGetByQuestionIDs(t *testing.T) {
	purchaseRepo, questionRepo := newPurchaseRepos(t)
	ctx := context.TODO()

	// Arrange: two questions with different prices, so a mixed-up mapping would show
	question1 := newTestQuestion("batch get test 1")
	err := questionRepo.AddQuestionWithPurchase(ctx, question1, newTestDetail(5999, `["a.png"]`))
	require.NoError(t, err)

	question2 := newTestQuestion("batch get test 2")
	err = questionRepo.AddQuestionWithPurchase(ctx, question2, newTestDetail(6999, `["b.png"]`))
	require.NoError(t, err)

	missingID := "10010000000000999"

	t.Run("返回的 map 只包含存在的那些", func(t *testing.T) {
		// Act
		purchaseMap, err := purchaseRepo.BatchGetByQuestionIDs(ctx,
			[]string{question1.ID, question2.ID, missingID})

		// Assert
		require.NoError(t, err)
		require.Len(t, purchaseMap, 2)
		require.Equal(t, 5999.0, purchaseMap[question1.ID].Price)
		require.Equal(t, `["a.png"]`, purchaseMap[question1.ID].Images)
		require.Equal(t, 6999.0, purchaseMap[question2.ID].Price)

		// 不存在的 ID 不能占一个零值的位置，否则列表接口会返回 {price:0} 而不是 null
		_, ok := purchaseMap[missingID]
		require.False(t, ok)
	})

	t.Run("传空切片返回空 map 且不查库", func(t *testing.T) {
		// Act
		purchaseMap, err := purchaseRepo.BatchGetByQuestionIDs(ctx, []string{})

		// Assert: 空 map 而不是 nil —— 调用方直接按 key 取，不必判空
		require.NoError(t, err)
		require.NotNil(t, purchaseMap)
		require.Empty(t, purchaseMap)
	})
}

func Test_questionRepo_AddQuestionWithPurchase_Rollback(t *testing.T) {
	// The whole point of the extension table: if the detail cannot be written,
	// the question must not be left behind either.
	_, questionRepo := newPurchaseRepos(t)
	ctx := context.TODO()

	// Arrange: currency is VARCHAR(10), so a longer value is rejected by the database
	const title = "rollback test"
	question := newTestQuestion(title)
	badDetail := newTestDetail(1, `["a.png"]`)
	badDetail.Currency = strings.Repeat("A", 20)

	// Act
	err := questionRepo.AddQuestionWithPurchase(ctx, question, badDetail)

	// Assert
	require.Error(t, err)

	count, err := testDataSource.DB.Where("title = ?", title).Count(&entity.Question{})
	require.NoError(t, err)
	require.EqualValues(t, 0, count, "question 也必须一起回滚")

	detailCount, err := testDataSource.DB.Where("question_id = ?", question.ID).
		Count(&entity.PurchaseDetail{})
	require.NoError(t, err)
	require.EqualValues(t, 0, detailCount)
}
