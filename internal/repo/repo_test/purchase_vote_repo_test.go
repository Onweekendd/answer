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
	"testing"

	"github.com/apache/answer/internal/entity"
	purchaseservice "github.com/apache/answer/internal/service/purchase"
	"github.com/stretchr/testify/require"
)

// voteCloseAt is the threshold the tests pass to Vote, same as production.
const voteCloseAt = 10

// newVoteQuestion creates a fresh purchase post, so each case starts from zero votes.
func newVoteQuestion(t *testing.T, title string) string {
	t.Helper()

	_, questionRepo := newPurchaseRepos(t)
	question := newTestQuestion(title)
	err := questionRepo.AddQuestionWithPurchase(context.TODO(), question, newTestDetail(5999, `["a.png"]`))
	require.NoError(t, err)
	return question.ID
}

// countVotes returns how many vote rows a post has, read straight from the table.
func countVotes(t *testing.T, questionID string) int64 {
	t.Helper()

	count, err := testDataSource.DB.Where("question_id = ?", questionID).Count(&entity.PurchaseVote{})
	require.NoError(t, err)
	return count
}

func Test_purchaseRepo_Vote(t *testing.T) {
	purchaseRepo, _ := newPurchaseRepos(t)
	ctx := context.TODO()

	t.Run("第一次投「买」", func(t *testing.T) {
		// Arrange
		questionID := newVoteQuestion(t, "vote first")

		// Act
		detail, justClosed, err := purchaseRepo.Vote(ctx, questionID, "101", entity.PurchaseVoteBuy, voteCloseAt)

		// Assert
		require.NoError(t, err)
		require.False(t, justClosed)
		require.Equal(t, 1, detail.BuyCount)
		require.Equal(t, 0, detail.WaitCount)
		require.Equal(t, 0, detail.SkipCount)
		require.EqualValues(t, 1, countVotes(t, questionID))
	})

	t.Run("从「买」改成「不买」", func(t *testing.T) {
		// Arrange: TODO 新建帖子，用户 101 先投一次「买」
		questionID := newVoteQuestion(t, "vote change")
		_, _, err := purchaseRepo.Vote(ctx, questionID, "101", entity.PurchaseVoteBuy, voteCloseAt)
		require.NoError(t, err)

		// Act: TODO 同一个用户改投「不买」
		detail, justClosed, err := purchaseRepo.Vote(ctx, questionID, "101", entity.PurchaseVoteSkip, voteCloseAt)
		require.NoError(t, err)

		// Assert: TODO buy_count 回到 0，skip_count 是 1，投票表里还是只有 1 行
		require.NoError(t, err)
		require.False(t, justClosed)
		require.Equal(t, 0, detail.BuyCount)
		require.Equal(t, 1, detail.SkipCount)
		require.EqualValues(t, 1, countVotes(t, questionID))
	})

	t.Run("重复投同一个选项，计数不变", func(t *testing.T) {
		// Arrange: TODO 新建帖子，用户 101 投一次「不买」
		questionID := newVoteQuestion(t, "vote same")
		_, _, err := purchaseRepo.Vote(ctx, questionID, "101", entity.PurchaseVoteSkip, voteCloseAt)
		require.NoError(t, err)
		// Act: TODO 再投一次「不买」
		detail, justClosed, err := purchaseRepo.Vote(ctx, questionID, "101", entity.PurchaseVoteSkip, voteCloseAt)
		require.NoError(t, err)

		// Assert: TODO skip_count 还是 1，投票表里还是 1 行
		require.NoError(t, err)
		require.False(t, justClosed)
		require.Equal(t, 0, detail.BuyCount)
		require.Equal(t, 1, detail.SkipCount)
		require.EqualValues(t, 1, countVotes(t, questionID))
	})

	t.Run("帖子不存在", func(t *testing.T) {
		// Act: TODO 对一个不存在的 ID（比如 "10010000000000999"）投票
		detail, _, err := purchaseRepo.Vote(ctx, "10010000000000999", "101", entity.PurchaseVoteSkip, voteCloseAt)

		// Assert: TODO 用 require.ErrorIs 断言返回的是 purchaseservice.ErrPurchaseNotFound
		//         detail 是 nil
		require.ErrorIs(t, err, purchaseservice.ErrPurchaseNotFound)
		require.Nil(t, detail)
	})

	t.Run("第 10 票关闭投票，只有这一票拿到 justClosed", func(t *testing.T) {
		// Arrange: TODO 新建帖子，用 for 循环让 9 个不同的用户投票，每一票都断言 justClosed 是 false
		//          用户 ID 可以用 strconv.Itoa(200 + i)
		questionID := newVoteQuestion(t, "vote close")

		for i := range 9 {
			_, justClosed, err := purchaseRepo.Vote(ctx, questionID, strconv.Itoa(200+i), entity.PurchaseVoteBuy, voteCloseAt)

			require.False(t, justClosed)
			require.NoError(t, err)
		}

		// Act: TODO 第 10 个用户投票
		detail, justClosed, err := purchaseRepo.Vote(ctx, questionID, "209", entity.PurchaseVoteBuy, voteCloseAt)

		// Assert: TODO justClosed 是 true，detail.VoteClosed 是 true，三个计数加起来是 10
		require.NoError(t, err)
		require.True(t, justClosed)
		require.True(t, detail.VoteClosed)
		require.Equal(t, voteCloseAt, int(countVotes(t, questionID)))
	})

	t.Run("关闭后再投被拒绝，计数不变", func(t *testing.T) {
		// Arrange: TODO 新建帖子，投满 10 票
		questionID := newVoteQuestion(t, "vote after close")

		for i := range 10 {
			_, _, err := purchaseRepo.Vote(ctx, questionID, strconv.Itoa(200+i), entity.PurchaseVoteBuy, voteCloseAt)

			require.NoError(t, err)
		}

		// Act: TODO 第 11 个用户投票
		_, _, err := purchaseRepo.Vote(ctx, questionID, "210", entity.PurchaseVoteBuy, voteCloseAt)

		// Assert: TODO 返回 ErrVoteClosed；重新用 GetByQuestionID 读出来，三个计数加起来还是 10
		require.ErrorIs(t, err, purchaseservice.ErrVoteClosed)
		require.Equal(t, voteCloseAt, int(countVotes(t, questionID)))
	})
}

func Test_purchaseRepo_GetVoteChoices(t *testing.T) {
	purchaseRepo, _ := newPurchaseRepos(t)
	ctx := context.TODO()

	// Arrange: user 101 buys, user 102 skips, user 103 never votes
	questionID := newVoteQuestion(t, "vote choices")
	_, _, err := purchaseRepo.Vote(ctx, questionID, "101", entity.PurchaseVoteBuy, voteCloseAt)
	require.NoError(t, err)
	_, _, err = purchaseRepo.Vote(ctx, questionID, "102", entity.PurchaseVoteSkip, voteCloseAt)
	require.NoError(t, err)

	t.Run("每个人投的选项，没投的人不在 map 里", func(t *testing.T) {
		// Act
		choices, err := purchaseRepo.GetVoteChoices(ctx, questionID, []string{"101", "102", "103"})

		// Assert
		require.NoError(t, err)
		require.Equal(t, map[string]int{"101": entity.PurchaseVoteBuy, "102": entity.PurchaseVoteSkip}, choices)
	})

	t.Run("空 ID 被跳过，全是空的就不查库", func(t *testing.T) {
		// Act: a guest viewer and no configured bot
		choices, err := purchaseRepo.GetVoteChoices(ctx, questionID, []string{"", ""})

		// Assert
		require.NoError(t, err)
		require.NotNil(t, choices)
		require.Empty(t, choices)
	})
}
