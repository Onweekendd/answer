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

package repo_test

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/apache/answer/internal/entity"
	purchaseservice "github.com/apache/answer/internal/service/purchase"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm/schemas"
)

// requirePostgres skips the test unless it runs against PostgreSQL. These
// tests check row locks, which SQLite does not have: it serializes all writers
// and answers concurrent ones with "database is locked".
func requirePostgres(t *testing.T) {
	t.Helper()

	if testDataSource.DB.Dialect().URI().DBType != schemas.POSTGRES {
		t.Skip("needs PostgreSQL: set TEST_DB_DRIVER=postgres")
	}
}

// runConcurrently calls fn(0) … fn(n-1), each in its own goroutine, and
// returns once all of them have finished. Like Promise.all over n tasks.
//
// fn must not call require/assert: a failed require calls t.FailNow, which
// only works on the test's own goroutine. Write results into a slice by index
// instead, and assert after runConcurrently returns.
func runConcurrently(n int, fn func(i int)) {
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { fn(i) })
	}
	wg.Wait()
}

// countVotesByChoice counts a post's vote rows that hold one choice, read
// straight from purchase_vote, the source the counters must agree with.
func countVotesByChoice(t *testing.T, questionID string, choice int) int64 {
	t.Helper()

	count, err := testDataSource.DB.Where("question_id = ? AND choice = ?", questionID, choice).
		Count(&entity.PurchaseVote{})
	require.NoError(t, err)
	return count
}

// getVoteDetail reads a post's purchase detail back after the votes are in.
func getVoteDetail(t *testing.T, questionID string) *entity.PurchaseDetail {
	t.Helper()

	purchaseRepo, _ := newPurchaseRepos(t)
	detail, exist, err := purchaseRepo.GetByQuestionID(context.TODO(), questionID)
	require.NoError(t, err)
	require.True(t, exist)
	return detail
}

// requireCountsMatchVotes checks each counter on purchase_detail equals the
// number of vote rows with that choice: if any update was lost, they differ.
func requireCountsMatchVotes(t *testing.T, questionID string) {
	t.Helper()

	detail := getVoteDetail(t, questionID)
	require.EqualValues(t, countVotesByChoice(t, questionID, entity.PurchaseVoteBuy), detail.BuyCount, "buy_count")
	require.EqualValues(t, countVotesByChoice(t, questionID, entity.PurchaseVoteWait), detail.WaitCount, "wait_count")
	require.EqualValues(t, countVotesByChoice(t, questionID, entity.PurchaseVoteSkip), detail.SkipCount, "skip_count")
}

var voteChoices = []int{entity.PurchaseVoteBuy, entity.PurchaseVoteWait, entity.PurchaseVoteSkip}

func Test_purchaseRepo_Vote_concurrentVotesKeepCounts(t *testing.T) {
	requirePostgres(t)
	purchaseRepo, _ := newPurchaseRepos(t)
	ctx := context.TODO()

	// Arrange: closeAt is out of reach, so voting stays open the whole time
	const voters = 50
	questionID := newVoteQuestion(t, "vote concurrent counts")
	neverClose := voters + 1

	// Act 1: 50 users vote at the same moment
	firstErrs := make([]error, voters)
	runConcurrently(voters, func(i int) {
		_, _, firstErrs[i] = purchaseRepo.Vote(ctx, questionID, strconv.Itoa(300+i), voteChoices[i%3], neverClose)
	})

	// Act 2: all of them change their vote at the same moment, which runs the Decr path too
	changeErrs := make([]error, voters)
	runConcurrently(voters, func(i int) {
		_, _, changeErrs[i] = purchaseRepo.Vote(ctx, questionID, strconv.Itoa(300+i), voteChoices[(i+1)%3], neverClose)
	})

	// Assert
	for i := range voters {
		require.NoError(t, firstErrs[i], "first vote of user %d", 300+i)
		require.NoError(t, changeErrs[i], "changed vote of user %d", 300+i)
	}
	require.EqualValues(t, voters, countVotes(t, questionID), "one row per user")
	requireCountsMatchVotes(t, questionID)
}

func Test_purchaseRepo_Vote_concurrentVotesCloseExactlyOnce(t *testing.T) {
	requirePostgres(t)
	purchaseRepo, _ := newPurchaseRepos(t)
	ctx := context.TODO()

	// Arrange: 30 users rush a post that closes at 10 votes
	const voters = 30
	questionID := newVoteQuestion(t, "vote concurrent close")
	justClosed := make([]bool, voters)
	errs := make([]error, voters)

	// Act
	runConcurrently(voters, func(i int) {
		_, justClosed[i], errs[i] = purchaseRepo.Vote(ctx, questionID, strconv.Itoa(400+i), voteChoices[i%3], voteCloseAt)
	})

	// Assert: every vote either got in or was turned away as closed, nothing else
	successCount := 0
	closedCount := 0
	justClosedCount := 0
	for i := range voters {
		if errs[i] == nil {
			successCount++
		} else {
			require.ErrorIs(t, errs[i], purchaseservice.ErrVoteClosed, "user %d", 400+i)
			closedCount++
		}
		if justClosed[i] {
			justClosedCount++
		}
	}
	require.Equal(t, voteCloseAt, successCount, "votes that got in")
	require.Equal(t, voters-voteCloseAt, closedCount, "votes turned away as closed")
	require.Equal(t, 1, justClosedCount, "only the closing vote reports justClosed")

	require.EqualValues(t, voteCloseAt, countVotes(t, questionID), "only the votes before close are kept")
	requireCountsMatchVotes(t, questionID)
	require.True(t, getVoteDetail(t, questionID).VoteClosed)
}

func Test_purchaseRepo_Vote_concurrentSameUserKeepsOneVote(t *testing.T) {
	requirePostgres(t)
	purchaseRepo, _ := newPurchaseRepos(t)
	ctx := context.TODO()

	// Arrange: one user double-clicks across all three buttons
	const clicks = 20
	questionID := newVoteQuestion(t, "vote concurrent same user")
	errs := make([]error, clicks)

	// Act
	runConcurrently(clicks, func(i int) {
		_, _, errs[i] = purchaseRepo.Vote(ctx, questionID, "500", voteChoices[i%3], voteCloseAt)
	})

	// Assert: no click hit the primary key, and the user still holds exactly one vote
	for i := range clicks {
		require.NoError(t, errs[i], "click %d", i)
	}
	require.EqualValues(t, 1, countVotes(t, questionID), "one vote per user")
	requireCountsMatchVotes(t, questionID)
	detail := getVoteDetail(t, questionID)
	require.Equal(t, 1, detail.BuyCount+detail.WaitCount+detail.SkipCount)
}
