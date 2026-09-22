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
	"fmt"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/pkg/random"
	"golang.org/x/crypto/bcrypt"
	"xorm.io/xorm"
)

func addPurchaseDetail(ctx context.Context, x *xorm.Engine) error {
	// 1. create purchase_detail table
	if err := x.Context(ctx).Sync(new(entity.PurchaseDetail)); err != nil {
		return fmt.Errorf("sync purchase_detail failed: %w", err)
	}
	// 2. bot user + config (shared with fresh install)
	return ensureBotUser(ctx, x)
}

// ensureBotUser makes sure the AI bot account exists and its ID is stored in config.
// It is idempotent: safe to run on every upgrade and on fresh install.
func ensureBotUser(ctx context.Context, x *xorm.Engine) error {
	bot := &entity.User{}
	exists, err := x.Context(ctx).Where("username = ?", constant.AIBotUsername).Get(bot)
	if err != nil {
		return fmt.Errorf("get bot user failed: %w", err)
	}
	if !exists {
		// random password so the account can never be logged into via password
		hash, err := bcrypt.GenerateFromPassword([]byte(random.Username()), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash bot password failed: %w", err)
		}
		bot = &entity.User{
			Username:     constant.AIBotUsername,
			Pass:         string(hash),
			EMail:        "ai-assistant@localhost",
			MailStatus:   1,
			NoticeStatus: 1,
			Status:       entity.UserStatusAvailable,
			Rank:         1,
			DisplayName:  "AI Assistant",
		}
		// ID is autoincr; xorm writes the generated ID back into bot.ID
		if _, err = x.Context(ctx).Insert(bot); err != nil {
			return fmt.Errorf("insert bot user failed: %w", err)
		}
	}

	cfg := &entity.Config{}
	exists, err = x.Context(ctx).Where("`key` = ?", constant.AIBotUserIDKey).Get(cfg)
	if err != nil {
		return fmt.Errorf("get bot config failed: %w", err)
	}
	if !exists {
		cfg = &entity.Config{Key: constant.AIBotUserIDKey, Value: bot.ID}
		if _, err = x.Context(ctx).Insert(cfg); err != nil {
			return fmt.Errorf("insert bot config failed: %w", err)
		}
	}
	return nil
}
