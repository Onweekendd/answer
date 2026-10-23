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

package guard

import (
	"fmt"
	"testing"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/stretchr/testify/require"
)

func tc(id, name, args string) llm.ToolCall { return llm.ToolCall{ID: id, Name: name, Arguments: args} }

func TestFailureCounter(t *testing.T) {
	ok := agent.Outcome{Result: tool.Result{Text: "ok"}}
	failed := agent.Outcome{Result: tool.Result{Failed: true}}
	skipped := agent.Outcome{Skipped: true}

	t.Run("连续失败到上限时返回 true", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act & Assert
		require.False(t, c.record([]agent.Outcome{failed}))
		require.False(t, c.record([]agent.Outcome{failed}))
		require.True(t, c.record([]agent.Outcome{failed}))
	})

	t.Run("中间成功一次就清零", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act
		c.record([]agent.Outcome{failed, failed})
		c.record([]agent.Outcome{ok})

		// Assert
		require.False(t, c.record([]agent.Outcome{failed, failed}))
		require.True(t, c.record([]agent.Outcome{failed}))
	})

	t.Run("同一轮里先失败两次、最后成功，不算连续失败", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act
		c.record([]agent.Outcome{failed, failed, ok})

		// Assert
		require.False(t, c.record([]agent.Outcome{failed, failed}))
	})

	t.Run("跳过的调用不算成功也不算失败", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act
		c.record([]agent.Outcome{failed, failed})

		// Assert：skipped 不清零，后面那次失败凑够 3 次
		require.True(t, c.record([]agent.Outcome{skipped, failed}))
	})
}

// ⑧-1 概念 5
func TestFingerprint(t *testing.T) {
	click5 := tc("c1", "click", `{"index":5}`)

	t.Run("参数只是空格不同，算同一个动作", func(t *testing.T) {
		require.Equal(t, fingerprint(click5, "h1"), fingerprint(tc("c2", "click", `{ "index": 5 }`), "h1"))
	})

	t.Run("调用 id 不同不影响", func(t *testing.T) {
		require.Equal(t, fingerprint(click5, "h1"), fingerprint(tc("c9", "click", `{"index":5}`), "h1"))
	})

	t.Run("参数不同、工具不同、页面不同，指纹都不同", func(t *testing.T) {
		base := fingerprint(click5, "h1")

		require.NotEqual(t, base, fingerprint(tc("c1", "click", `{"index":6}`), "h1"))
		require.NotEqual(t, base, fingerprint(tc("c1", "type", `{"index":5}`), "h1"))
		require.NotEqual(t, base, fingerprint(click5, "h2"))
	})
}

// ⑧-2 概念 5
func TestLoopDetector(t *testing.T) {
	newDetector := func() *loopDetector { return &loopDetector{window: 10, nudgeAt: 3, stopAt: 5} }

	t.Run("同一个指纹：第 3 次提醒，第 4 次不再提醒，第 5 次收尾", func(t *testing.T) {
		// Arrange
		d := newDetector()

		// Act
		var got []loopVerdict
		for range 5 {
			got = append(got, d.record("A"))
		}

		// Assert
		require.Equal(t, []loopVerdict{loopNone, loopNone, loopNudge, loopNone, loopStop}, got)
	})

	t.Run("来回切换也能发现：A B A B A", func(t *testing.T) {
		// Arrange
		d := newDetector()

		// Act
		var got []loopVerdict
		for _, fp := range []string{"A", "B", "A", "B", "A"} {
			got = append(got, d.record(fp))
		}

		// Assert
		require.Equal(t, []loopVerdict{loopNone, loopNone, loopNone, loopNone, loopNudge}, got)
	})

	t.Run("超出窗口的旧记录不算", func(t *testing.T) {
		// Arrange：A 出现两次以后，隔了 10 个别的动作
		d := newDetector()
		d.record("A")
		d.record("A")
		for i := range 10 {
			d.record(fmt.Sprintf("other-%d", i))
		}

		// Act
		verdict := d.record("A")

		// Assert
		require.Equal(t, loopNone, verdict)
	})

	t.Run("每次都不一样就永远没问题", func(t *testing.T) {
		// Arrange
		d := newDetector()

		// Act & Assert
		for i := range 30 {
			require.Equal(t, loopNone, d.record(fmt.Sprintf("fp-%d", i)))
		}
	})
}

func TestCanonicalArgs(t *testing.T) {
	t.Run("空格和键的顺序不影响", func(t *testing.T) {
		require.Equal(t, canonicalArgs(`{"b":1,"a":"x"}`), canonicalArgs(`{ "a": "x", "b": 1 }`))
	})

	t.Run("不是 JSON 就原样返回", func(t *testing.T) {
		require.Equal(t, "oops", canonicalArgs("oops"))
	})
}
