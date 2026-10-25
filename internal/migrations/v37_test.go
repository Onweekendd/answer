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

package migrations

import (
	"context"
	"testing"
	"time"

	"github.com/apache/answer/internal/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// purchaseDetailV36 is purchase_detail as v36 left it, before the vote columns.
type purchaseDetailV36 struct {
	QuestionID string    `xorm:"not null pk BIGINT(20) question_id"`
	CreatedAt  time.Time `xorm:"created not null default CURRENT_TIMESTAMP TIMESTAMP created_at"`
	UpdatedAt  time.Time `xorm:"updated TIMESTAMP updated_at"`
	Product    string    `xorm:"not null default '' TEXT product"`
	Price      float64   `xorm:"not null default 0.00 DECIMAL(10,2) price"`
	Link       string    `xorm:"not null default '' VARCHAR(255) link"`
	Images     string    `xorm:"not null default '' TEXT images"`
	Currency   string    `xorm:"not null default '' VARCHAR(10) currency"`
	AIStatus   int       `xorm:"not null default 0 SMALLINT ai_status"`
}

func (purchaseDetailV36) TableName() string {
	return "purchase_detail"
}

func newV36Engine(t *testing.T) *xorm.Engine {
	t.Helper()
	x, err := xorm.NewEngine("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = x.Close() })
	require.NoError(t, x.Sync(new(purchaseDetailV36)))
	return x
}

func TestAddPurchaseVote_existingPostStartsOpenWithNoVotes(t *testing.T) {
	// Arrange
	x := newV36Engine(t)
	_, err := x.Insert(&purchaseDetailV36{QuestionID: "1", AIStatus: entity.PurchaseAIStatusFinished})
	require.NoError(t, err)

	// Act
	err = addPurchaseVote(context.TODO(), x)

	// Assert
	require.NoError(t, err)
	got := &entity.PurchaseDetail{}
	exists, err := x.Where("question_id = ?", "1").Get(got)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, 0, got.BuyCount)
	assert.Equal(t, 0, got.WaitCount)
	assert.Equal(t, 0, got.SkipCount)
	assert.False(t, got.VoteClosed)
	assert.Equal(t, entity.PurchaseAIStatusFinished, got.AIStatus, "existing columns must be kept")
}

func TestAddPurchaseVote_oneVotePerUserPerPost(t *testing.T) {
	// Arrange
	x := newV36Engine(t)
	require.NoError(t, addPurchaseVote(context.TODO(), x))
	_, err := x.Insert(&entity.PurchaseVote{QuestionID: "1", UserID: "2", Choice: entity.PurchaseVoteBuy})
	require.NoError(t, err)

	// Act
	_, sameUserErr := x.Insert(&entity.PurchaseVote{QuestionID: "1", UserID: "2", Choice: entity.PurchaseVoteSkip})
	_, otherUserErr := x.Insert(&entity.PurchaseVote{QuestionID: "1", UserID: "3", Choice: entity.PurchaseVoteSkip})

	// Assert
	require.Error(t, sameUserErr, "a second vote by the same user must hit the primary key")
	require.NoError(t, otherUserErr)
	count, err := x.Count(&entity.PurchaseVote{QuestionID: "1"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}

func TestAddPurchaseVote_idempotent(t *testing.T) {
	// Arrange
	x := newV36Engine(t)

	// Act
	err1 := addPurchaseVote(context.TODO(), x)
	err2 := addPurchaseVote(context.TODO(), x)

	// Assert
	require.NoError(t, err1)
	require.NoError(t, err2)
}
