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
	"testing"

	"github.com/apache/answer/internal/entity"
	"github.com/stretchr/testify/require"
)

func feedIDs(t *testing.T, viewerID string) map[string]bool {
	t.Helper()

	_, questionRepo := newPurchaseRepos(t)
	list, err := questionRepo.GetRandomQuestionList(context.TODO(), viewerID, 1000)
	require.NoError(t, err)

	ids := make(map[string]bool, len(list))
	for _, q := range list {
		ids[q.ID] = true
	}
	return ids
}

func Test_questionRepo_GetRandomQuestionList(t *testing.T) {
	purchaseRepo, questionRepo := newPurchaseRepos(t)
	ctx := context.TODO()

	t.Run("limit 限制条数", func(t *testing.T) {
		for _, title := range []string{"feed limit a", "feed limit b", "feed limit c"} {
			newVoteQuestion(t, title)
		}

		list, err := questionRepo.GetRandomQuestionList(ctx, "", 2)

		require.NoError(t, err)
		require.Len(t, list, 2)
	})

	t.Run("排除自己的帖子，已投过的帖子仍在", func(t *testing.T) {
		own := newTestQuestion("feed own")
		own.UserID = "777"
		require.NoError(t, questionRepo.AddQuestionWithPurchase(ctx, own, newTestDetail(1, `["a.png"]`)))
		voted := newVoteQuestion(t, "feed voted")
		_, _, err := purchaseRepo.Vote(ctx, voted, "777", entity.PurchaseVoteBuy, voteCloseAt)
		require.NoError(t, err)

		ids := feedIDs(t, "777")

		require.False(t, ids[own.ID])
		require.True(t, ids[voted])
	})

	t.Run("游客能看到所有开放的帖子", func(t *testing.T) {
		open := newVoteQuestion(t, "feed guest")

		require.True(t, feedIDs(t, "")[open])
	})

	t.Run("投票已关闭的帖子不进 feed", func(t *testing.T) {
		closed := newVoteQuestion(t, "feed closed")
		for i := range voteCloseAt {
			_, _, err := purchaseRepo.Vote(ctx, closed, string(rune('a'+i)), entity.PurchaseVoteBuy, voteCloseAt)
			require.NoError(t, err)
		}

		require.False(t, feedIDs(t, "")[closed])
	})
}
