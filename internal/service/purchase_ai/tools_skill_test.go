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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_skillRead(t *testing.T) {
	t.Run("还没打开网页也能读，读到的是文件全文，不算改变页面", func(t *testing.T) {
		// Arrange
		r := newAgentRun(&fakeLLM{}, newFakeBrowser(), testLimits())

		// Act
		res, err := r.skillRead(context.Background(), `{"path":"site-guide/references/taobao.md"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed)
		require.False(t, res.PageChanged)
		require.Contains(t, res.Text, "# 淘宝")
	})

	t.Run("路径写错时告诉模型这个 skill 里有哪些文件", func(t *testing.T) {
		// Arrange
		r := newAgentRun(&fakeLLM{}, newFakeBrowser(), testLimits())

		// Act
		res, err := r.skillRead(context.Background(), `{"path":"site-guide/references/pdd.md"}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Contains(t, res.Text, "site-guide/references/taobao.md")
		require.Contains(t, res.Text, "site-guide/SKILL.md")
	})

	t.Run("skill 名字也写错时，告诉模型路径的格式", func(t *testing.T) {
		// Arrange
		r := newAgentRun(&fakeLLM{}, newFakeBrowser(), testLimits())

		// Act
		res, err := r.skillRead(context.Background(), `{"path":"nope/SKILL.md"}`)

		// Assert
		require.NoError(t, err)
		require.True(t, res.Failed)
		require.Contains(t, res.Text, "site-guide/SKILL.md")
	})
}

func Test_buildSystemPrompt_skills(t *testing.T) {
	t.Run("能用浏览器时，系统提示词里列出每个 skill 的路径和描述，但不放正文", func(t *testing.T) {
		// Act
		prompt := buildSystemPrompt(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), true)

		// Assert
		require.Contains(t, prompt, "site-guide/SKILL.md：")
		require.Contains(t, prompt, "skill_read")
		require.NotContains(t, prompt, "references/taobao.md")
	})

	t.Run("没有浏览器时不列 skill：没有工具，读不了", func(t *testing.T) {
		// Act
		prompt := buildSystemPrompt(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), false)

		// Assert
		require.NotContains(t, prompt, "site-guide")
	})
}
