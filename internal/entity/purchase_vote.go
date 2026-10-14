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

// PurchaseVote is one user's buy / wait / skip vote on a purchase post.
// The (question_id, user_id) primary key is what keeps it to one vote per
// person, even when two requests from the same user race each other.
type PurchaseVote struct {
	QuestionID string    `xorm:"not null pk BIGINT(20) question_id"`
	UserID     string    `xorm:"not null pk BIGINT(20) user_id"`
	Choice     int       `xorm:"not null default 0 SMALLINT choice"`
	CreatedAt  time.Time `xorm:"created not null default CURRENT_TIMESTAMP TIMESTAMP created_at"`
	UpdatedAt  time.Time `xorm:"updated TIMESTAMP updated_at"`
}

// TableName purchase vote table name
func (PurchaseVote) TableName() string {
	return "purchase_vote"
}
