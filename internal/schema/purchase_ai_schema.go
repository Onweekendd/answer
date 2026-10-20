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

// PurchaseAIChatReq is one turn of the chat. The server keeps the conversation
// (purchase_chat_entry), so the client sends only the new message and where it
// goes: ParentID is the entry it follows, the leaf of the turn before, or empty
// for a conversation's first message. Without a Message the turn regenerates the
// answer to the user message ParentID names.
type PurchaseAIChatReq struct {
	ChatID   string                 `json:"chat_id" validate:"required,numeric"`
	ParentID string                 `json:"parent_id" validate:"max=32"`
	Message  *PurchaseAIChatMessage `json:"message"`
	// UserID is filled by the controller, never by the client.
	UserID string `json:"-"`
}

// PurchaseAIChatHistoryReq asks for a conversation as the page shows it.
type PurchaseAIChatHistoryReq struct {
	ChatID string `validate:"required,numeric" form:"chat_id"`
	UserID string `json:"-"`
}

// PurchaseAIChatMessage is the user's new message. Images are urls returned by
// POST /file, the same rule as a post's screenshots.
type PurchaseAIChatMessage struct {
	Text   string   `json:"text" validate:"max=8000"`
	Images []string `json:"images" validate:"max=5,dive,url"`
}

// Check wants something to answer: a message with words or images, or a parent
// to regenerate the answer of.
func (req *PurchaseAIChatReq) Check() (errFields []*validator.FormErrorField, err error) {
	if (req.Message == nil && req.ParentID == "") ||
		(req.Message != nil && strings.TrimSpace(req.Message.Text) == "" && len(req.Message.Images) == 0) {
		return append(errFields, &validator.FormErrorField{
			ErrorField: "messages",
			ErrorMsg:   reason.RequestFormatError,
		}), errors.BadRequest(reason.RequestFormatError)
	}
	return nil, nil
}
