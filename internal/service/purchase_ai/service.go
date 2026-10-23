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

package purchaseai

import (
	"github.com/apache/answer/internal/service/config"
	"github.com/apache/answer/internal/service/llm"
	purchasevote "github.com/apache/answer/internal/service/purchase_vote"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	"github.com/apache/answer/internal/service/uploader"
)

// PurchaseAIService supports chat, screenshot identification and post drafts. It owns no data of its own and
// reaches every other domain through that domain's service, never its repo.
type PurchaseAIService struct {
	questionCommon  *questioncommon.QuestionCommon
	configService   *config.ConfigService
	llmClient       llm.Client
	browser         Browser
	uploaderService uploader.UploaderService
	voteService     *purchasevote.PurchaseVoteService
	chatOptions     chatOptions
	drafts          DraftStore
	workspaces      Workspaces // nil 表示没有沙箱
	chatLog         ChatLog
}

// NewPurchaseAIService new purchase ai service
func NewPurchaseAIService(
	questionCommon *questioncommon.QuestionCommon,
	configService *config.ConfigService,
	llmClient llm.Client,
	browser Browser,
	uploaderService uploader.UploaderService,
	voteService *purchasevote.PurchaseVoteService,
	drafts DraftStore,
	workspaces Workspaces,
	chatLog ChatLog,
) *PurchaseAIService {
	service := &PurchaseAIService{
		questionCommon:  questionCommon,
		configService:   configService,
		llmClient:       llmClient,
		browser:         browser,
		uploaderService: uploaderService,
		voteService:     voteService,
		chatOptions:     chatOptionsFromEnv(),
		drafts:          drafts,
		workspaces:      workspaces,
		chatLog:         chatLog,
	}
	return service
}

// taskOptions is the base configuration for one agent run of this service:
// model, browser and the image stores. Chat tasks start from here, so none of them
// can forget one (a missing image store is no error: cards and answers just quietly have no pictures).
func (pas *PurchaseAIService) taskOptions(browserOn bool) taskOptions {
	return taskOptions{
		llm:           pas.llmClient,
		browser:       pas.browser,
		browserOn:     browserOn,
		limits:        defaultLimits,
		images:        pas.uploaderService,
		resources:     pas.uploaderService,
		artifactStore: pas.uploaderService,
	}
}
