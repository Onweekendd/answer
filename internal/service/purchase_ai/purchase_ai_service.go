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
	"github.com/segmentfault/pacman/log"

	"github.com/apache/answer/internal/service/comment"
	"github.com/apache/answer/internal/service/config"
	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase"
	purchasevote "github.com/apache/answer/internal/service/purchase_vote"
	"github.com/apache/answer/internal/service/purchaseaiqueue"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	"github.com/apache/answer/internal/service/uploader"
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
	browser         Browser
	uploaderService uploader.UploaderService
	voteService     *purchasevote.PurchaseVoteService
	chatOptions     chatOptions
}

// NewPurchaseAIService new purchase ai service
func NewPurchaseAIService(
	questionCommon *questioncommon.QuestionCommon,
	purchaseService *purchase.PurchaseService,
	commentService *comment.CommentService,
	configService *config.ConfigService,
	llmClient llm.Client,
	queueService purchaseaiqueue.Service,
	browser Browser,
	uploaderService uploader.UploaderService,
	voteService *purchasevote.PurchaseVoteService,
) *PurchaseAIService {
	service := &PurchaseAIService{
		questionCommon:  questionCommon,
		purchaseService: purchaseService,
		commentService:  commentService,
		configService:   configService,
		llmClient:       llmClient,
		queueService:    queueService,
		browser:         browser,
		uploaderService: uploaderService,
		voteService:     voteService,
		chatOptions:     chatOptionsFromEnv(),
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

	// checked once up front: without the extension the model gets no tools and
	// answers from what it knows, saying the prices are not checked live
	browserOn := pas.browser.Connected()
	messages := []llm.Message{
		llm.Text(llm.RoleSystem, buildSystemPrompt(time.Now(), browserOn)),
		pas.userMessage(ctx, question),
	}

	result, err := pas.runAgent(ctx, messages, browserOn)
	if err != nil {
		return err
	}
	if result.Finish != "" {
		log.Infof("purchase ai: finished early (%s) after %d tool calls, questionID: %s", result.Finish, len(result.Steps), questionID)
	}

	originalText := fixNumberRanges(result.Text)

	if strings.TrimSpace(originalText) == "" {
		return errors.InternalServer(reason.AIRequestFailed).
			WithError(fmt.Errorf("model returned empty content, reasoning length: %d", len(result.Reasoning))).
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

	pas.voteAsBot(ctx, questionID, botID, originalText)

	return pas.purchaseService.MarkFinished(ctx, questionID)
}

// userMessage is the post as the agent first sees it: what the author typed,
// then the screenshots themselves. The model reads the images directly instead
// of a JSON summary made by an earlier call, which saves that call and keeps
// what a summary drops (coupons, the selected SKU, small print). The images sit
// in the first message, so every later step resends them inside the cached
// prefix.
//
// A product stored with the post (one the author confirmed in the old form) is
// still written out, as a second opinion beside the images.
func (pas *PurchaseAIService) userMessage(ctx context.Context, question *schema.QuestionInfoResp) llm.Message {
	var stored *schema.PurchaseProduct
	var images []string
	if question.Purchase != nil {
		stored = question.Purchase.Product
		images = question.Purchase.Images
	}

	parts := []llm.Part{{Text: buildUserPrompt(question, stored)}}
	if imageParts := pas.imageParts(ctx, images); len(imageParts) > 0 {
		parts = append(parts, llm.Part{Text: screenshotsHeader})
		parts = append(parts, imageParts...)
	}
	return llm.Message{Role: llm.RoleUser, Parts: parts}
}

// voteAsBot casts the advice's verdict as the bot's vote, through the same
// rules as a person's. The advice is already posted, so a vote that cannot be
// cast (no verdict on the first line, voting closed) is logged, not failed.
func (pas *PurchaseAIService) voteAsBot(ctx context.Context, questionID, botID, advice string) {
	choice := parseVerdict(advice)
	if choice == 0 {
		log.Warnf("purchase ai: no verdict on the first line, the bot does not vote, questionID: %s", questionID)
		return
	}
	_, err := pas.voteService.Vote(ctx, &schema.PurchaseVoteReq{
		QuestionID: questionID,
		Choice:     choice,
		UserID:     botID,
	})
	if err != nil {
		log.Warnf("purchase ai: bot vote failed, questionID: %s, err: %v", questionID, err)
	}
}
