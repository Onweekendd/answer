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

package main

import (
	"errors"
	"testing"

	"github.com/apache/answer/internal/service/llm"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	pacmanerrors "github.com/segmentfault/pacman/errors"
	"github.com/stretchr/testify/require"
)

func Test_summarize(t *testing.T) {
	t.Run("跑不了的不进通过率的分母，平均值只算真的跑了的", func(t *testing.T) {
		// Arrange：通过 1 次、失败 1 次、插件没连上 1 次（根本没开始跑）
		steps := func(n int) *purchaseai.TaskResult {
			return &purchaseai.TaskResult{Steps: make([]purchaseai.TaskStep, n), Success: true}
		}
		attempts := []attempt{
			{outcome: outcomePass, ran: true, res: steps(10), selfPass: true, usage: llm.Usage{TotalTokens: 3000}},
			{outcome: outcomeFail, ran: true, res: steps(20), selfPass: true, usage: llm.Usage{TotalTokens: 5000}},
			{outcome: outcomeBlocked},
		}

		// Act
		s := summarize(attempts)

		// Assert
		require.Equal(t, 1, s.passed)
		require.Equal(t, 2, s.judged)
		require.Equal(t, 1, s.blocked)
		require.Equal(t, 2, s.ran)
		require.Equal(t, 2, s.selfPass)
		require.Equal(t, "15.0", avg(s.steps, s.ran))
		require.Equal(t, 4000, avgInt(s.tokens, s.ran))
	})
}

func Test_describe(t *testing.T) {
	t.Run("模型服务商的报错藏在 Answer 错误的 Err 里，要取出来，不能是空字符串", func(t *testing.T) {
		// Arrange：Answer 的错误类型 Message 为空，Error() 就是空字符串
		err := pacmanerrors.InternalServer("error.ai.request_failed").
			WithError(errors.New("error, status code: 400, message: invalid assistant message"))

		// Act
		got := describe(err)

		// Assert
		require.Equal(t, "error, status code: 400, message: invalid assistant message", got.Error())
	})
}
