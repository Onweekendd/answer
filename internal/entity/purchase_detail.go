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

package entity

import "time"

const (
	PurchaseAIStatusNone       = 0
	PurchaseAIStatusProcessing = 1
	PurchaseAIStatusFinished   = 2
	PurchaseAIStatusFailed     = 3
)

const (
	PurchaseVoteBuy  = 1
	PurchaseVoteWait = 2
	PurchaseVoteSkip = 3
)

// PurchaseDetail purchase detail
type PurchaseDetail struct {
	QuestionID string    `xorm:"not null pk BIGINT(20) question_id"`
	CreatedAt  time.Time `xorm:"created not null default CURRENT_TIMESTAMP TIMESTAMP created_at"`
	UpdatedAt  time.Time `xorm:"updated TIMESTAMP updated_at"`
	Product    string    `xorm:"not null default '' TEXT product"`
	Price      float64   `xorm:"not null default 0.00 DECIMAL(10,2) price"`
	Link       string    `xorm:"not null default '' VARCHAR(255) link"`
	Images     string    `xorm:"not null default '' TEXT images"`
	Currency   string    `xorm:"not null default '' VARCHAR(10) currency"`
	AIStatus   int       `xorm:"not null default 0 SMALLINT ai_status"`
	BuyCount   int       `xorm:"not null default 0 INT buy_count"`
	WaitCount  int       `xorm:"not null default 0 INT wait_count"`
	SkipCount  int       `xorm:"not null default 0 INT skip_count"`
	VoteClosed bool      `xorm:"not null default false BOOL vote_closed"`
}

// TableName purchase detail table name
func (PurchaseDetail) TableName() string {
	return "purchase_detail"
}
