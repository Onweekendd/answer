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

package schema

import (
	"strings"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/base/validator"
	"github.com/segmentfault/pacman/errors"
)

// PurchaseAIAnalyzeReq asks for an AI opinion on one purchase request.
type PurchaseAIAnalyzeReq struct {
	QuestionID string `validate:"required" json:"question_id"`
	// UserID is filled by the controller, never by the client.
	UserID string `json:"-"`
}

// PurchaseProduct is what the vision model read off the screenshots of one
// post: the one product the post is about. Anything the screenshots do not
// show is left empty rather than guessed.
//
// The post form submits it back, so the validate tags hold a submitted one to
// the same limits parseProduct puts on a reading; keep the two in step. A tag
// cannot name a Go constant, hence the repeated numbers.
type PurchaseProduct struct {
	// Name is required because a reading without one means no product, which
	// is sent as a null product rather than an empty one. notblank comes
	// first: it never fails, it only turns an all-space value into "" for
	// required to reject.
	Name  string `json:"name" validate:"notblank,required,lte=200"`
	Brand string `json:"brand" validate:"lte=100"`
	Model string `json:"model" validate:"lte=100"`
	// Tags are 1 to 3 short categories, broad to narrow, such as 数码 and
	// 游戏机. Unlike the other fields they are the model's judgement rather
	// than text read off the screenshots; the post form offers them as tags.
	// 35 is TagItem.DisplayName's limit.
	Tags     []string `json:"tags" validate:"max=3,dive,required,lte=35"`
	Specs    []string `json:"specs" validate:"max=10,dive,required,lte=100"`
	Platform string   `json:"platform" validate:"omitempty,oneof=淘宝 天猫 京东 拼多多 其他"`
	Shop     string   `json:"shop" validate:"lte=100"`
	// Official, Price and ListPrice are pointers because their zero values
	// are real answers: nil means the screenshots did not show it, while
	// false or 0 would claim a non-official shop or a free item.
	Official  *bool    `json:"official"`
	Price     *float64 `json:"price" validate:"omitempty,gte=0"`
	ListPrice *float64 `json:"list_price" validate:"omitempty,gte=0"`
	// Promotions are the deals shown on the page, one per entry, such as
	// "满2000减100" or "国家补贴".
	Promotions []string `json:"promotions" validate:"max=10,dive,required,lte=100"`
	// Confidence is the model's own 0 to 1 estimate of how right the whole
	// reading is. It is a hint for when to ask the user, not a probability.
	Confidence float64 `json:"confidence" validate:"gte=0,lte=1"`
	// Notes says what the model could not read or was unsure of, such as a
	// price hidden behind a popup.
	Notes string `json:"notes" validate:"lte=300"`
}

// PurchaseAIIdentifyReq asks what product some just-uploaded screenshots show,
// before the post exists.
type PurchaseAIIdentifyReq struct {
	// same rule as PurchaseInfo.Images: identify must accept whatever a post can hold
	Images []string `json:"images" validate:"required,min=1,max=5,dive,url"`
	// UserID is filled by the controller, never by the client.
	UserID string `json:"-"`
}

// PurchaseAIIdentifyResp is the reading. Product is null when the screenshots
// show no product, which is an answer rather than an error.
type PurchaseAIIdentifyResp struct {
	Product *PurchaseProduct `json:"product"`
}

// PurchaseAIChatReq is one turn of the chat: the whole conversation so far,
// oldest first, ending with the user's new message. The server keeps no
// history yet, so the client sends it back each time.
type PurchaseAIChatReq struct {
	Messages []PurchaseAIChatMessage `json:"messages" validate:"required,min=1,max=40,dive"`
	// ChatID is the saved conversation this turn belongs to, if any. The agent keeps one
	// sandbox per conversation; without it the sandbox lives for this turn only.
	ChatID string `json:"chat_id" validate:"omitempty,numeric"`
	// UserID is filled by the controller, never by the client.
	UserID string `json:"-"`
}

// PurchaseAIChatMessage is one message. Images are urls returned by POST /file,
// the same rule as a post's screenshots; an assistant message has none.
type PurchaseAIChatMessage struct {
	Role   string   `json:"role" validate:"required,oneof=user assistant"`
	Text   string   `json:"text" validate:"max=8000"`
	Images []string `json:"images" validate:"max=5,dive,url"`
}

// Check wants the conversation to end with something from the user to answer.
func (req *PurchaseAIChatReq) Check() (errFields []*validator.FormErrorField, err error) {
	n := len(req.Messages)
	if n == 0 || req.Messages[n-1].Role != "user" ||
		(strings.TrimSpace(req.Messages[n-1].Text) == "" && len(req.Messages[n-1].Images) == 0) {
		return append(errFields, &validator.FormErrorField{
			ErrorField: "messages",
			ErrorMsg:   reason.RequestFormatError,
		}), errors.BadRequest(reason.RequestFormatError)
	}
	return nil, nil
}
