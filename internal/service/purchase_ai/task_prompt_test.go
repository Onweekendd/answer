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

package purchaseai

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_buildSystemPrompt(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.Local)

	t.Run("插件连着时教模型用浏览器查，并且只看不买", func(t *testing.T) {
		// Act
		prompt := buildSystemPrompt(now, true)

		// Assert
		require.Contains(t, prompt, "今天是 2026-09-27")
		for _, word := range []string{"snapshot", "done", "加入购物车", "验证码", "未能核实"} {
			require.Contains(t, prompt, word)
		}
		require.NotContains(t, prompt, "web_search")
	})

	t.Run("两种模式都带着看截图的规则：看不到的不要凭印象补", func(t *testing.T) {
		for _, browserOn := range []bool{true, false} {
			// Act
			prompt := buildSystemPrompt(now, browserOn)

			// Assert
			require.Contains(t, prompt, "如何看截图")
			require.Contains(t, prompt, "不要凭印象补上价格")
		}
	})

	t.Run("聊天不下买不买的结论，可以整理发帖草稿", func(t *testing.T) {
		// Act
		chat := buildSystemPrompt(now, true)

		// Assert
		require.Contains(t, chat, "社区投票决定")
		require.NotContains(t, chat, "放在回答的第一行")
		require.Contains(t, chat, "draft_post", "对话里可以帮用户发帖")
	})

	t.Run("插件没连上时不提工具，要求说明价格没有实时核实", func(t *testing.T) {
		// Act
		prompt := buildSystemPrompt(now, false)

		// Assert
		require.Contains(t, prompt, "没有实时核实")
		require.NotContains(t, prompt, "snapshot")
		require.NotContains(t, prompt, "done")
	})
}
