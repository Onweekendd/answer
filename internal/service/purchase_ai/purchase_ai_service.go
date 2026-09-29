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

// Package purchaseai turns a purchase request into an AI buying recommendation
// posted as a comment from the bot account.
package purchaseai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/pkg/converter"
	"github.com/apache/answer/pkg/uid"
	"github.com/segmentfault/pacman/errors"

	"github.com/apache/answer/internal/service/comment"
	"github.com/apache/answer/internal/service/config"
	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase"
	"github.com/apache/answer/internal/service/purchaseaiqueue"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	"github.com/apache/answer/internal/service/uploader"
	"github.com/apache/answer/internal/service/webreader"
	"github.com/apache/answer/internal/service/websearch"
)

// PurchaseAIService orchestrates the analysis. It owns no data of its own and
// reaches every other domain through that domain's service, never its repo.
type PurchaseAIService struct {
	questionCommon  *questioncommon.QuestionCommon
	purchaseService *purchase.PurchaseService
	commentService  *comment.CommentService
	configService   *config.ConfigService
	llmClient       llm.Client
	queueService    purchaseaiqueue.Service
	searchProvider  websearch.Provider
	pageReader      webreader.Reader
	uploaderService uploader.UploaderService
}

// NewPurchaseAIService new purchase ai service
func NewPurchaseAIService(
	questionCommon *questioncommon.QuestionCommon,
	purchaseService *purchase.PurchaseService,
	commentService *comment.CommentService,
	configService *config.ConfigService,
	llmClient llm.Client,
	queueService purchaseaiqueue.Service,
	searchProvider websearch.Provider,
	pageReader webreader.Reader,
	uploaderService uploader.UploaderService,
) *PurchaseAIService {
	service := &PurchaseAIService{
		questionCommon:  questionCommon,
		purchaseService: purchaseService,
		commentService:  commentService,
		configService:   configService,
		llmClient:       llmClient,
		queueService:    queueService,
		searchProvider:  searchProvider,
		pageReader:      pageReader,
		uploaderService: uploaderService,
	}
	// the queue calls handlers with a fresh context, so an analysis outlives the
	// request that asked for it instead of dying when that request returns
	queueService.RegisterHandler(service.handleMsg)
	return service
}

// Enqueue schedules an analysis and returns immediately. Analyze itself blocks
// on the provider for tens of seconds, which is far too long to hold a request.
func (pas *PurchaseAIService) Enqueue(ctx context.Context, questionID string) {
	pas.queueService.Send(ctx, &schema.PurchaseAIMsg{QuestionID: questionID})
}

// handleMsg adapts a queue message to Analyze.
func (pas *PurchaseAIService) handleMsg(ctx context.Context, msg *schema.PurchaseAIMsg) error {
	return pas.Analyze(ctx, msg.QuestionID)
}

// Analyze asks the model whether this purchase is worth making and posts the
// answer as a comment from the bot account. It is one blocking call to the
// provider, so callers should not hold a user request open waiting for it.
func (pas *PurchaseAIService) Analyze(ctx context.Context, questionID string) (err error) {
	botID, err := pas.configService.GetStringValue(ctx, constant.AIBotUserIDKey)
	if err != nil {
		return err
	}
	if botID == "" {
		return errors.BadRequest(reason.AINotConfigured)
	}

	question, err := pas.questionCommon.Info(ctx, questionID, "")
	if err != nil {
		return err
	}

	err = pas.purchaseService.MarkProcessing(ctx, questionID)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = pas.purchaseService.MarkFailed(ctx, questionID, err)
		}
	}()

	product := pas.productFor(ctx, questionID, question.Purchase)

	messages := []llm.Message{
		llm.Text(llm.RoleSystem, buildSystemPrompt(time.Now())),
		llm.Text(llm.RoleUser, buildUserPrompt(question, product)),
	}

	resp, err := pas.runAgent(ctx, messages)
	if err != nil {
		return err
	}

	originalText := resp.Content

	if strings.TrimSpace(originalText) == "" {
		return errors.InternalServer(reason.AIRequestFailed).
			WithError(fmt.Errorf("model returned empty content, reasoning length: %d", len(resp.Reasoning))).
			WithStack()
	}

	_, err = pas.commentService.AddComment(ctx, &schema.AddCommentReq{
		// the controller deshortens this id before calling AddComment, and
		// AddComment stores whatever it is given, so do the same here
		ObjectID:     uid.DeShortID(questionID),
		OriginalText: originalText,
		// AddCommentReq.Check() renders the markdown, but it only runs inside
		// BindAndCheck, which a service to service call never reaches
		ParsedText: converter.Markdown2HTML(originalText),
		UserID:     botID,
		// CanAdd says the bot may comment; the other two only decide which
		// buttons the response offers a viewer, and there is no viewer here
		CanAdd:    true,
		CanEdit:   false,
		CanDelete: false,
	})

	if err != nil {
		return err
	}

	return pas.purchaseService.MarkFinished(ctx, questionID)
}
