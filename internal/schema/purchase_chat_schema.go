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

// PurchaseDraftVoteReq asks the AI to cast the vote it kept with a draft, on
// the post the user published from that draft.
type PurchaseDraftVoteReq struct {
	DraftID    string `validate:"required,max=32" json:"draft_id"`
	QuestionID string `validate:"required" json:"question_id"`
	UserID     string `json:"-"`
}

// PurchaseDraftResultReq tells what the user did with a draft: published it (as the post
// QuestionID, titled Title) or dropped it. The chat remembers it, so the history shows it as decided.
type PurchaseDraftResultReq struct {
	DraftID    string `validate:"required,max=32" json:"draft_id"`
	Status     string `validate:"required,oneof=published dropped" json:"status"`
	QuestionID string `validate:"omitempty,max=32" json:"question_id"`
	Title      string `validate:"omitempty,max=300" json:"title"`
	UserID     string `json:"-"`
}

// PurchaseChatCreateReq starts a conversation; the page sends the first question as the title.
type PurchaseChatCreateReq struct {
	Title  string `json:"title"`
	UserID string `json:"-"`
}

// PurchaseChatCreateResp is the new conversation's id.
type PurchaseChatCreateResp struct {
	ID string `json:"id"`
}

// PurchaseChatItem is one row of the conversation list.
type PurchaseChatItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
}
