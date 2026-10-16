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

// newBadgeEngine is a database as the badge feature left it: the three badge tables with a
// row each, and a notification table holding a badge notification next to an ordinary one.
func newBadgeEngine(t *testing.T) *xorm.Engine {
	t.Helper()
	x, err := xorm.NewEngine("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = x.Close() })

	for _, ddl := range []string{
		`CREATE TABLE badge (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE badge_group (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE badge_award (id INTEGER PRIMARY KEY, badge_id INTEGER)`,
		`INSERT INTO badge (id, name) VALUES (1, 'b')`,
		`INSERT INTO badge_group (id, name) VALUES (1, 'g')`,
		`INSERT INTO badge_award (id, badge_id) VALUES (1, 1)`,
	} {
		_, err := x.Exec(ddl)
		require.NoError(t, err)
	}

	require.NoError(t, x.Sync(new(entity.Notification)))
	_, err = x.Insert(
		&entity.Notification{UserID: "1", ObjectID: "5", Content: `{"object_info":{"object_type":"badge_award","title":"t"}}`},
		&entity.Notification{UserID: "1", ObjectID: "6", Content: `{"object_info":{"object_type":"question","title":"q"}}`},
	)
	require.NoError(t, err)
	return x
}

func tableExists(t *testing.T, x *xorm.Engine, name string) bool {
	t.Helper()
	ok, err := x.IsTableExist(name)
	require.NoError(t, err)
	return ok
}

func TestDropBadges(t *testing.T) {
	t.Run("三张徽章表都被删掉", func(t *testing.T) {
		// Arrange
		x := newBadgeEngine(t)

		// Act
		err := dropBadges(context.TODO(), x)

		// Assert
		require.NoError(t, err)
		for _, name := range []string{"badge", "badge_group", "badge_award"} {
			require.False(t, tableExists(t, x, name), name)
		}
	})

	t.Run("只删徽章通知，其他通知原样保留", func(t *testing.T) {
		// Arrange
		x := newBadgeEngine(t)

		// Act
		require.NoError(t, dropBadges(context.TODO(), x))

		// Assert
		list := make([]*entity.Notification, 0)
		require.NoError(t, x.Find(&list))
		require.Len(t, list, 1)
		require.Equal(t, "6", list[0].ObjectID)
	})

	t.Run("可以重复执行", func(t *testing.T) {
		// Arrange
		x := newBadgeEngine(t)

		// Act
		err1 := dropBadges(context.TODO(), x)
		err2 := dropBadges(context.TODO(), x)

		// Assert
		require.NoError(t, err1)
		require.NoError(t, err2)
	})

	t.Run("从没建过徽章表的新库也不报错", func(t *testing.T) {
		// Arrange: a fresh install no longer creates the badge tables at all
		x, err := xorm.NewEngine("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = x.Close() })
		require.NoError(t, x.Sync(new(entity.Notification)))

		// Act
		err = dropBadges(context.TODO(), x)

		// Assert
		require.NoError(t, err)
	})

	t.Run("没有通知表时跳过清理，不报错", func(t *testing.T) {
		// Arrange
		x, err := xorm.NewEngine("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = x.Close() })

		// Act
		err = dropBadges(context.TODO(), x)

		// Assert
		require.NoError(t, err)
	})
}
