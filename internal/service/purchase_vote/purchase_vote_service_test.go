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

package purchasevote

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/noticequeue"
	"github.com/apache/answer/internal/service/purchase"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	myErrors "github.com/segmentfault/pacman/errors"
	"github.com/stretchr/testify/require"
)

const (
	testQuestionID = "10010000000000001"
	testAuthorID   = "1"
	testVoterID    = "2"
)

// fakeQuestionRepo answers GetQuestion from fields. It embeds the interface
// so it satisfies QuestionRepo without writing out its forty methods: calling
// any other one panics on the nil interface, which would mean Vote touched
// something it should not.
type fakeQuestionRepo struct {
	questioncommon.QuestionRepo
	question *entity.Question
	exist    bool
	err      error
}

func (f *fakeQuestionRepo) GetQuestion(_ context.Context, _ string) (*entity.Question, bool, error) {
	return f.question, f.exist, f.err
}

// voteCall is what Vote passed to the repo, so a test can check the arguments.
type voteCall struct {
	questionID, userID string
	choice, closeAt    int
}

// fakePurchaseRepo returns a fixed result from Vote and records each call.
type fakePurchaseRepo struct {
	purchase.PurchaseRepo
	detail     *entity.PurchaseDetail
	justClosed bool
	err        error
	calls      []voteCall
}

func (f *fakePurchaseRepo) Vote(_ context.Context, questionID, userID string, choice, closeAt int) (
	*entity.PurchaseDetail, bool, error) {
	f.calls = append(f.calls, voteCall{questionID, userID, choice, closeAt})
	return f.detail, f.justClosed, f.err
}

// fakeNoticeQueue keeps every message sent to it instead of delivering it.
type fakeNoticeQueue struct {
	noticequeue.Service
	sent []*schema.NotificationMsg
}

func (f *fakeNoticeQueue) Send(_ context.Context, msg *schema.NotificationMsg) {
	f.sent = append(f.sent, msg)
}

// newTestService wires the service to a question by testAuthorID and a repo
// whose Vote succeeds with one buy vote. Tests change the fakes they care about.
func newTestService() (*PurchaseVoteService, *fakeQuestionRepo, *fakePurchaseRepo, *fakeNoticeQueue) {
	questionRepo := &fakeQuestionRepo{
		question: &entity.Question{ID: testQuestionID, UserID: testAuthorID, Status: entity.QuestionStatusAvailable},
		exist:    true,
	}
	purchaseRepo := &fakePurchaseRepo{
		detail: &entity.PurchaseDetail{QuestionID: testQuestionID, BuyCount: 1},
	}
	noticeQueue := &fakeNoticeQueue{}
	return NewPurchaseVoteService(purchaseRepo, questionRepo, noticeQueue), questionRepo, purchaseRepo, noticeQueue
}

func newVoteReq(choice int) *schema.PurchaseVoteReq {
	return &schema.PurchaseVoteReq{QuestionID: testQuestionID, Choice: choice, UserID: testVoterID}
}

// requireHTTPError asserts err is a pacman error with this status code and
// reason key, which is what the controller turns into the response.
func requireHTTPError(t *testing.T, err error, code int, wantReason string) {
	t.Helper()

	var pacmanErr *myErrors.Error
	require.True(t, errors.As(err, &pacmanErr), "expected a pacman error, got %v", err)
	require.Equal(t, code, pacmanErr.Code)
	require.Equal(t, wantReason, pacmanErr.Reason)
}

func TestPurchaseVoteService_Vote(t *testing.T) {
	ctx := context.TODO()

	t.Run("投票成功，返回最新票数和我投的选项", func(t *testing.T) {
		// Arrange
		s, _, purchaseRepo, _ := newTestService()
		purchaseRepo.detail = &entity.PurchaseDetail{BuyCount: 3, WaitCount: 2, SkipCount: 1}

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteWait))

		// Assert
		require.NoError(t, err)
		require.Equal(t, &schema.PurchaseVoteResp{
			BuyCount:  3,
			WaitCount: 2,
			SkipCount: 1,
			MyChoice:  entity.PurchaseVoteWait,
			Closed:    false,
		}, resp)
		require.Equal(t, []voteCall{{testQuestionID, testVoterID, entity.PurchaseVoteWait, voteCloseAt}},
			purchaseRepo.calls, "votes once, with the service's close threshold")
	})

	t.Run("普通的一票不发通知", func(t *testing.T) {
		// Arrange
		s, _, _, noticeQueue := newTestService()

		// Act
		_, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.NoError(t, err)
		require.Empty(t, noticeQueue.sent)
	})

	t.Run("这一票关闭了投票：返回 closed，只通知作者一次", func(t *testing.T) {
		// Arrange
		s, _, purchaseRepo, noticeQueue := newTestService()
		purchaseRepo.detail = &entity.PurchaseDetail{BuyCount: 10, VoteClosed: true}
		purchaseRepo.justClosed = true

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.NoError(t, err)
		require.True(t, resp.Closed)
		require.Equal(t, []*schema.NotificationMsg{{
			TriggerUserID:       testAuthorID,
			ReceiverUserID:      testAuthorID,
			Type:                schema.NotificationTypeInbox,
			ObjectID:            testQuestionID,
			ObjectType:          constant.QuestionObjectType,
			NotificationAction:  constant.NotificationPurchaseVoteClosed,
			NoNeedPushAllFollow: true,
		}}, noticeQueue.sent)
	})

	t.Run("投票已经关闭的帖子再投，不再通知", func(t *testing.T) {
		// Arrange
		s, _, purchaseRepo, noticeQueue := newTestService()
		purchaseRepo.detail, purchaseRepo.err = nil, purchase.ErrVoteClosed

		// Act
		_, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.Error(t, err)
		require.Empty(t, noticeQueue.sent)
	})

	t.Run("choice 不是 1、2、3，不查库直接 400", func(t *testing.T) {
		for _, choice := range []int{0, 4, -1} {
			// Arrange
			s, _, purchaseRepo, _ := newTestService()

			// Act
			resp, err := s.Vote(ctx, newVoteReq(choice))

			// Assert
			require.Nil(t, resp)
			requireHTTPError(t, err, http.StatusBadRequest, reason.RequestFormatError)
			require.Empty(t, purchaseRepo.calls, "choice %d", choice)
		}
	})

	t.Run("帖子不存在，404", func(t *testing.T) {
		// Arrange
		s, questionRepo, purchaseRepo, _ := newTestService()
		questionRepo.question, questionRepo.exist = nil, false

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.Nil(t, resp)
		requireHTTPError(t, err, http.StatusNotFound, reason.QuestionNotFound)
		require.Empty(t, purchaseRepo.calls)
	})

	t.Run("帖子已删除，和不存在一样 404", func(t *testing.T) {
		// Arrange
		s, questionRepo, purchaseRepo, _ := newTestService()
		questionRepo.question.Status = entity.QuestionStatusDeleted

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.Nil(t, resp)
		requireHTTPError(t, err, http.StatusNotFound, reason.QuestionNotFound)
		require.Empty(t, purchaseRepo.calls)
	})

	t.Run("作者给自己的帖子投票，403", func(t *testing.T) {
		// Arrange
		s, _, purchaseRepo, _ := newTestService()
		req := newVoteReq(entity.PurchaseVoteBuy)
		req.UserID = testAuthorID

		// Act
		resp, err := s.Vote(ctx, req)

		// Assert
		require.Nil(t, resp)
		requireHTTPError(t, err, http.StatusForbidden, reason.PurchaseVoteOwnPost)
		require.Empty(t, purchaseRepo.calls)
	})

	t.Run("查帖子出错，原样返回", func(t *testing.T) {
		// Arrange
		s, questionRepo, purchaseRepo, _ := newTestService()
		dbErr := errors.New("question db down")
		questionRepo.err = dbErr

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.Nil(t, resp)
		require.ErrorIs(t, err, dbErr)
		require.Empty(t, purchaseRepo.calls)
	})

	t.Run("投票已关闭，400", func(t *testing.T) {
		// Arrange
		s, _, purchaseRepo, _ := newTestService()
		purchaseRepo.detail, purchaseRepo.err = nil, purchase.ErrVoteClosed

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.Nil(t, resp)
		requireHTTPError(t, err, http.StatusBadRequest, reason.PurchaseVoteClosed)
	})

	t.Run("帖子存在但不是买不买帖，404", func(t *testing.T) {
		// Arrange
		s, _, purchaseRepo, _ := newTestService()
		purchaseRepo.detail, purchaseRepo.err = nil, purchase.ErrPurchaseNotFound

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.Nil(t, resp)
		requireHTTPError(t, err, http.StatusNotFound, reason.QuestionNotFound)
	})

	t.Run("投票时数据库出错，原样返回而不是 panic", func(t *testing.T) {
		// Arrange: the repo hands back a nil detail with the error, as it does for real
		s, _, purchaseRepo, _ := newTestService()
		dbErr := errors.New("vote db down")
		purchaseRepo.detail, purchaseRepo.err = nil, dbErr

		// Act
		resp, err := s.Vote(ctx, newVoteReq(entity.PurchaseVoteBuy))

		// Assert
		require.Nil(t, resp)
		require.ErrorIs(t, err, dbErr)
	})
}
