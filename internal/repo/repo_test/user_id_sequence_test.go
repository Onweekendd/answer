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
	"github.com/apache/answer/internal/repo/user"
	"github.com/stretchr/testify/require"
)

// Test_userRepo_AddUser_afterInit reproduces the production bug: the test
// database is built by InitDB, which inserts the admin and the AI bot with
// explicit ids, just like the install wizard on the server. Before the fix,
// PostgreSQL handed the next user id 1 and the insert failed on the primary key.
// It runs on SQLite too, where it passes either way.
func Test_userRepo_AddUser_afterInit(t *testing.T) {
	t.Run("初始化之后，新用户第一次插入就成功，ID 比现有的都大", func(t *testing.T) {
		// Arrange: other tests add users too, so the current largest id is read, not assumed
		userRepo := user.NewUserRepo(testDataSource)
		var maxID int64
		_, err := testDataSource.DB.SQL(`SELECT COALESCE(MAX(id), 0) FROM "user"`).Get(&maxID)
		require.NoError(t, err)
		newUser := &entity.User{
			Username:    "sequence_check",
			Pass:        "sequence_check",
			EMail:       "sequence_check@example.com",
			MailStatus:  entity.EmailStatusAvailable,
			Status:      entity.UserStatusAvailable,
			DisplayName: "sequence_check",
		}

		// Act: one try only; before the fix the first try is the one that failed
		err = userRepo.AddUser(context.TODO(), newUser)

		// Assert
		require.NoError(t, err)
		id, err := strconv.ParseInt(newUser.ID, 10, 64)
		require.NoError(t, err)
		require.Greater(t, id, maxID)
	})
}
