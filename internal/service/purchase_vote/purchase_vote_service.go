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
	stderrors "errors"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/noticequeue"
	"github.com/apache/answer/internal/service/purchase"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	"github.com/segmentfault/pacman/errors"
)

// voteCloseAt is the vote count that closes voting on a post.
const voteCloseAt = 10

// PurchaseVoteService holds the voting rules for purchase posts. People and
// the AI bot both vote through it, so the rules live in one place.
type PurchaseVoteService struct {
	purchaseRepo             purchase.PurchaseRepo
	questionRepo             questioncommon.QuestionRepo
	notificationQueueService noticequeue.Service
}

// NewPurchaseVoteService new purchase vote service
func NewPurchaseVoteService(
	purchaseRepo purchase.PurchaseRepo,
	questionRepo questioncommon.QuestionRepo,
	notificationQueueService noticequeue.Service,
) *PurchaseVoteService {
	return &PurchaseVoteService{
		purchaseRepo:             purchaseRepo,
		questionRepo:             questionRepo,
		notificationQueueService: notificationQueueService,
	}
}

// Vote records req.UserID's choice on a purchase post.
func (s *PurchaseVoteService) Vote(ctx context.Context, req *schema.PurchaseVoteReq) (*schema.PurchaseVoteResp, error) {
	switch req.Choice {
	case entity.PurchaseVoteBuy, entity.PurchaseVoteWait, entity.PurchaseVoteSkip:
	default:
		return nil, errors.BadRequest(reason.RequestFormatError)
	}

	question, exist, err := s.questionRepo.GetQuestion(ctx, req.QuestionID)
	if err != nil {
		return nil, err
	}

	if !exist || question.Status == entity.QuestionStatusDeleted {
		return nil, errors.NotFound(reason.QuestionNotFound)
	}

	if question.UserID == req.UserID {
		return nil, errors.Forbidden(reason.PurchaseVoteOwnPost)
	}

	detail, justClosed, err := s.purchaseRepo.Vote(ctx, req.QuestionID, req.UserID, req.Choice, voteCloseAt)

	if stderrors.Is(err, purchase.ErrVoteClosed) {
		return nil, errors.BadRequest(reason.PurchaseVoteClosed)
	}

	if stderrors.Is(err, purchase.ErrPurchaseNotFound) {
		return nil, errors.NotFound(reason.QuestionNotFound)
	}

	if err != nil {
		return nil, err
	}

	if justClosed {
		// The author is the trigger as well, so the inbox line does not reveal who cast the closing vote.
		s.notificationQueueService.Send(ctx, &schema.NotificationMsg{
			TriggerUserID:       question.UserID,
			ReceiverUserID:      question.UserID,
			Type:                schema.NotificationTypeInbox,
			ObjectID:            question.ID,
			ObjectType:          constant.QuestionObjectType,
			NotificationAction:  constant.NotificationPurchaseVoteClosed,
			NoNeedPushAllFollow: true,
		})
	}

	return &schema.PurchaseVoteResp{
		BuyCount:  detail.BuyCount,
		WaitCount: detail.WaitCount,
		SkipCount: detail.SkipCount,
		MyChoice:  req.Choice,
		Closed:    detail.VoteClosed,
	}, nil
}
