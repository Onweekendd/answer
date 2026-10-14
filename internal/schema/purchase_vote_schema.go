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

// PurchaseVoteReq is one buy / wait / skip vote on a purchase post.
type PurchaseVoteReq struct {
	QuestionID string `validate:"required" json:"question_id"`
	// Choice is entity.PurchaseVoteBuy / Wait / Skip. The service checks it
	// again, because the AI bot votes without going through the controller.
	Choice int `validate:"required,oneof=1 2 3" json:"choice"`
	// UserID is filled by the controller, never by the client.
	UserID string `json:"-"`
}

// PurchaseVoteResp is the post's tally after the vote, for the ratio bar.
type PurchaseVoteResp struct {
	BuyCount  int `json:"buy_count"`
	WaitCount int `json:"wait_count"`
	SkipCount int `json:"skip_count"`
	// MyChoice is the choice the voter now holds, so the button stays lit.
	MyChoice int  `json:"my_choice"`
	Closed   bool `json:"closed"`
}
