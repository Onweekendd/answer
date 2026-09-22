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

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// newTestEngine opens an in-memory sqlite engine with only the tables
// ensureBotUser touches. The sqlite driver is registered by internal/base/data.
func newTestEngine(t *testing.T) *xorm.Engine {
	t.Helper()
	x, err := xorm.NewEngine("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = x.Close() })
	require.NoError(t, x.Sync(new(entity.User), new(entity.Config)))
	return x
}

// getBotUser / getBotConfig are small readers used by the Assert sections.
func getBotUser(t *testing.T, x *xorm.Engine) (*entity.User, bool) {
	t.Helper()
	u := &entity.User{}
	exists, err := x.Where("username = ?", constant.AIBotUsername).Get(u)
	require.NoError(t, err)
	return u, exists
}

func getBotConfig(t *testing.T, x *xorm.Engine) (*entity.Config, bool) {
	t.Helper()
	c := &entity.Config{}
	exists, err := x.Where("`key` = ?", constant.AIBotUserIDKey).Get(c)
	require.NoError(t, err)
	return c, exists
}

func TestAddPurchaseDetail(t *testing.T) {
	// Arrange
	x := newTestEngine(t)

	// Act
	err := addPurchaseDetail(context.TODO(), x)

	// Assert
	require.NoError(t, err)

	tableExists, err := x.IsTableExist(new(entity.PurchaseDetail))
	require.NoError(t, err)
	assert.True(t, tableExists, "purchase_detail table should be created")

	bot, botExists := getBotUser(t, x)
	require.True(t, botExists, "bot user should be created")
	assert.Equal(t, entity.UserStatusAvailable, bot.Status)
	assert.NotEmpty(t, bot.Pass, "bot must have a (random) password hash")

	cfg, cfgExists := getBotConfig(t, x)
	require.True(t, cfgExists, "bot user id should be stored in config")
	assert.Equal(t, bot.ID, cfg.Value)
}

func TestEnsureBotUserIdempotent(t *testing.T) {
	// Arrange
	x := newTestEngine(t)

	// Act
	err1 := ensureBotUser(context.TODO(), x)
	err2 := ensureBotUser(context.TODO(), x)

	// Assert
	require.NoError(t, err1)
	require.NoError(t, err2)

	userCount, err := x.Count(&entity.User{Username: constant.AIBotUsername})
	require.NoError(t, err)
	assert.EqualValues(t, 1, userCount, "running twice must not create a second bot user")

	cfgCount, err := x.Count(&entity.Config{Key: constant.AIBotUserIDKey})
	require.NoError(t, err)
	assert.EqualValues(t, 1, cfgCount, "running twice must not create a second config row")
}

func TestEnsureBotUserReusesExistingUser(t *testing.T) {
	// Arrange: a bot user already exists (e.g. previous run failed after inserting it)
	x := newTestEngine(t)
	existing := &entity.User{
		Username:     constant.AIBotUsername,
		Pass:         "whatever",
		EMail:        "ai-assistant@localhost",
		MailStatus:   1,
		NoticeStatus: 1,
		Status:       entity.UserStatusAvailable,
		Rank:         1,
		DisplayName:  "AI Assistant",
	}
	_, err := x.Insert(existing)
	require.NoError(t, err)

	// Act
	err = ensureBotUser(context.TODO(), x)

	// Assert
	require.NoError(t, err)

	userCount, err := x.Count(&entity.User{Username: constant.AIBotUsername})
	require.NoError(t, err)
	assert.EqualValues(t, 1, userCount, "must reuse the existing bot user, not create another")

	cfg, cfgExists := getBotConfig(t, x)
	require.True(t, cfgExists)
	assert.Equal(t, existing.ID, cfg.Value, "config must point at the pre-existing user's ID")
}
