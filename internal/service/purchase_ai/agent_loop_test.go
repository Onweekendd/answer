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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/require"
)

// 精华函数各自的测试，和 TODO 的编号对应

// ① 概念 2
func Test_compactSnapshots(t *testing.T) {
	newMessages := func() []llm.Message {
		return []llm.Message{
			llm.Text(llm.RoleSystem, "sys"),
			toolMessage("c1", "快照一"),
			toolMessage("c2", "已点击 [1]"),
			toolMessage("c3", "快照二"),
			toolMessage("c4", "快照三"),
		}
	}

	t.Run("只留最新的一份快照，其余换成占位语，别的消息不动", func(t *testing.T) {
		// Arrange
		messages := newMessages()

		// Act
		compactSnapshots(messages, []int{1, 3, 4})

		// Assert
		require.Equal(t, snapshotOmitted, messages[1].Parts[0].Text)
		require.Equal(t, snapshotOmitted, messages[3].Parts[0].Text)
		require.Equal(t, "快照三", messages[4].Parts[0].Text)
		require.Equal(t, "已点击 [1]", messages[2].Parts[0].Text)
		require.Equal(t, "c1", messages[1].ToolCallID)
		require.Len(t, messages[1].Parts, 1)
	})

	t.Run("调用两次结果一样，占位语换上以后不再变", func(t *testing.T) {
		// Arrange
		messages := newMessages()
		compactSnapshots(messages, []int{1, 3, 4})

		// Act
		compactSnapshots(messages, []int{1, 3, 4})

		// Assert
		require.Equal(t, snapshotOmitted, messages[1].Parts[0].Text)
		require.Equal(t, "快照三", messages[4].Parts[0].Text)
	})

	t.Run("只有一份或者没有快照时什么都不改", func(t *testing.T) {
		// Arrange
		messages := newMessages()

		// Act
		compactSnapshots(messages, []int{1})
		compactSnapshots(messages, nil)

		// Assert
		require.Equal(t, newMessages(), messages)
	})

	t.Run("不改动之前发出去的那份：Parts 的底层数组是共享的", func(t *testing.T) {
		// Arrange：sent 像 fakeLLM 那样复制了一份消息，Parts 和 messages 共用底层数组
		messages := newMessages()
		sent := append([]llm.Message(nil), messages...)

		// Act
		compactSnapshots(messages, []int{1, 4})

		// Assert
		require.Equal(t, "快照一", sent[1].Parts[0].Text)
	})
}

// ② 概念 6
func Test_chat(t *testing.T) {
	newRun := func(model *fakeLLM) *agentRun {
		limits := testLimits()
		limits.llmTimeout = 50 * time.Millisecond
		return newAgentRun(model, newFakeBrowser(), limits)
	}
	req := &llm.ChatReq{Messages: startMessages()}
	// 整次任务的 ctx 给 2 秒：没写好单次期限时，测试 2 秒后失败，不会一直卡住
	alive := func(t *testing.T) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		t.Cleanup(cancel)
		return ctx
	}

	t.Run("成功就直接返回，只调用一次", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: "好"}}}

		// Act
		resp, err := newRun(model).chat(context.Background(), req)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "好", resp.Content)
		require.Len(t, model.reqs, 1)
	})

	t.Run("这一次卡住了，用同样的请求重试一次", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{hang: 1, replies: []*llm.ChatResp{{Content: "第二次成功"}}}

		// Act
		resp, err := newRun(model).chat(alive(t), req)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "第二次成功", resp.Content)
		require.Len(t, model.reqs, 2)
		require.Equal(t, model.reqs[0].Messages, model.reqs[1].Messages)
	})

	t.Run("每次调用都有自己的期限，不会一直等下去", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{hang: 2}
		started := time.Now()

		// Act
		_, err := newRun(model).chat(alive(t), req)

		// Assert：两次各 50ms，远远不到测试的超时
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Len(t, model.reqs, 2)
		require.Less(t, time.Since(started), time.Second)
	})

	t.Run("整次任务的 ctx 结束了就不重试", func(t *testing.T) {
		// Arrange
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		model := &fakeLLM{hang: 1, replies: []*llm.ChatResp{{Content: "不该走到这里"}}}

		// Act
		_, err := newRun(model).chat(ctx, req)

		// Assert
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, model.reqs, 1)
	})

	t.Run("服务商报了别的错不重试", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{err: errors.New("401 invalid key")}

		// Act
		_, err := newRun(model).chat(context.Background(), req)

		// Assert
		require.EqualError(t, err, "401 invalid key")
		require.Len(t, model.reqs, 1)
	})
}

// ④ 概念 1：runCalls 的规则在 Test_run 里已经覆盖了（同一轮、页面变了、done、插件断开）

func Test_failureCounter(t *testing.T) {
	ok := toolResult{Text: "ok"}
	failed := toolResult{Failed: true}
	skipped := toolResult{Skipped: true}

	t.Run("连续失败到上限时返回 true", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act & Assert
		require.False(t, c.record([]toolResult{failed}))
		require.False(t, c.record([]toolResult{failed}))
		require.True(t, c.record([]toolResult{failed}))
	})

	t.Run("中间成功一次就清零", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act
		c.record([]toolResult{failed, failed})
		c.record([]toolResult{ok})

		// Assert
		require.False(t, c.record([]toolResult{failed, failed}))
		require.True(t, c.record([]toolResult{failed}))
	})

	t.Run("同一轮里先失败两次、最后成功，不算连续失败", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act
		c.record([]toolResult{failed, failed, ok})

		// Assert
		require.False(t, c.record([]toolResult{failed, failed}))
	})

	t.Run("跳过的调用不算成功也不算失败", func(t *testing.T) {
		// Arrange
		c := failureCounter{limit: 3}

		// Act
		c.record([]toolResult{failed, failed})

		// Assert：skipped 不清零，后面那次失败凑够 3 次
		require.True(t, c.record([]toolResult{skipped, failed}))
	})
}

// ⑧-1 概念 5
func Test_fingerprint(t *testing.T) {
	click5 := toolCall("c1", toolClick, `{"index":5}`)

	t.Run("参数只是空格不同，算同一个动作", func(t *testing.T) {
		require.Equal(t, fingerprint(click5, "h1"), fingerprint(toolCall("c2", toolClick, `{ "index": 5 }`), "h1"))
	})

	t.Run("调用 id 不同不影响", func(t *testing.T) {
		require.Equal(t, fingerprint(click5, "h1"), fingerprint(toolCall("c9", toolClick, `{"index":5}`), "h1"))
	})

	t.Run("参数不同、工具不同、页面不同，指纹都不同", func(t *testing.T) {
		base := fingerprint(click5, "h1")

		require.NotEqual(t, base, fingerprint(toolCall("c1", toolClick, `{"index":6}`), "h1"))
		require.NotEqual(t, base, fingerprint(toolCall("c1", toolType, `{"index":5}`), "h1"))
		require.NotEqual(t, base, fingerprint(click5, "h2"))
	})
}

// ⑧-2 概念 5
func Test_loopDetector(t *testing.T) {
	newDetector := func() *loopDetector { return &loopDetector{window: 10, nudgeAt: 3, stopAt: 5} }

	t.Run("同一个指纹：第 3 次提醒，第 4 次不再提醒，第 5 次收尾", func(t *testing.T) {
		// Arrange
		d := newDetector()

		// Act
		var got []loopVerdict
		for i := 0; i < 5; i++ {
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
		for i := 0; i < 10; i++ {
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
		for i := 0; i < 30; i++ {
			require.Equal(t, loopNone, d.record(fmt.Sprintf("fp-%d", i)))
		}
	})
}
