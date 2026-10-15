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

	"github.com/apache/answer/internal/entity"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

func newUserEngine(t *testing.T) *xorm.Engine {
	t.Helper()
	x, err := xorm.NewEngine("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = x.Close() })
	require.NoError(t, x.Sync(new(entity.User)))
	return x
}

// The PostgreSQL behaviour is covered in repo_test, which runs on a real
// PostgreSQL: Test_userRepo_AddUser_afterInit. These run on SQLite only.
func TestSyncUserIDSequence(t *testing.T) {
	t.Run("不是 PostgreSQL 时什么都不做，也不报错", func(t *testing.T) {
		// Arrange
		x := newUserEngine(t)
		_, err := x.Insert(&entity.User{ID: "7", Username: "admin", Pass: "x", EMail: "a@example.com"})
		require.NoError(t, err)

		// Act
		err = syncUserIDSequence(context.TODO(), x)

		// Assert
		require.NoError(t, err)
		users := make([]*entity.User, 0)
		require.NoError(t, x.Find(&users))
		require.Len(t, users, 1)
		require.Equal(t, "7", users[0].ID)
	})

	t.Run("可以重复执行", func(t *testing.T) {
		// Arrange: ensureBotUser runs it on every install and every upgrade
		x := newUserEngine(t)

		// Act
		err1 := syncUserIDSequence(context.TODO(), x)
		err2 := syncUserIDSequence(context.TODO(), x)

		// Assert
		require.NoError(t, err1)
		require.NoError(t, err2)
	})
}
