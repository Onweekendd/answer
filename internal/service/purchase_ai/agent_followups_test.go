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
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_splitFollowups(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		answer    string
		followups []string
	}{
		{
			name:      "规矩的写法：块被切掉，建议按顺序拿出来",
			in:        "京东自营 4299。\n\n<followups>\n<q>帮我发到社区，让大家投票</q>\n<q>天猫呢？</q>\n</followups>",
			answer:    "京东自营 4299。",
			followups: []string{"帮我发到社区，让大家投票", "天猫呢？"},
		},
		{
			name:   "没有块：原样返回",
			in:     "就这样。",
			answer: "就这样。",
		},
		{
			name:      "没用 <q>、写成了列表：一行一句，去掉列表符号",
			in:        "价格偏高。\n<followups>\n- 帮我发到社区\n2. 等双 11 会便宜多少？\n</followups>",
			answer:    "价格偏高。",
			followups: []string{"帮我发到社区", "等双 11 会便宜多少？"},
		},
		{
			name:      "开头的数字是内容时不去掉，只去掉真正的列表符号",
			in:        "x<followups><q>27 寸 4K 和 2K 高刷怎么选</q></followups>\n",
			answer:    "x",
			followups: []string{"27 寸 4K 和 2K 高刷怎么选"},
		},
		{
			name:      "写成编号列表的：去掉编号，保留内容里的数字",
			in:        "x<followups>\n1. 27 寸够不够大？\n2、换 32 寸呢\n</followups>",
			answer:    "x",
			followups: []string{"27 寸够不够大？", "换 32 寸呢"},
		},
		{
			name:      "结束标签丢了：切到结尾",
			in:        "价格偏高。\n<followups>\n<q>帮我发到社区</q>",
			answer:    "价格偏高。",
			followups: []string{"帮我发到社区"},
		},
		{
			name:      "最多 3 句，重复的只算一次",
			in:        "x<followups><q>a</q><q>a</q><q>b</q><q>c</q><q>d</q></followups>",
			answer:    "x",
			followups: []string{"a", "b", "c"},
		},
		{
			name:      "块后面还有字：留在回答里",
			in:        "前面\n<followups><q>a</q></followups>\n后面",
			answer:    "前面\n\n后面",
			followups: []string{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			answer, followups := splitFollowups(tt.in)

			// Assert
			assert.Equal(t, tt.answer, answer)
			assert.Equal(t, tt.followups, followups)
		})
	}

	t.Run("太长的一句截短", func(t *testing.T) {
		// Act
		_, followups := splitFollowups("<followups><q>" + strings.Repeat("长", 40) + "</q></followups>")

		// Assert
		assert.Equal(t, []string{strings.Repeat("长", followupRunes) + "…"}, followups)
	})
}

func Test_buildSystemPrompt_followups(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	// Act
	chat := buildSystemPrompt(now, true)
	post := buildPostPrompt(now, true)

	// Assert：对话里有后续建议，帖子分析（一条评论）没有
	assert.Contains(t, chat, "<followups>")
	assert.NotContains(t, post, "<followups>")
}

func Test_run_doneFollowups(t *testing.T) {
	// Arrange：模型用 done 结束，后续建议放在 followups 参数里
	args, _ := json.Marshal(map[string]any{
		"text": "价格偏高", "success": true,
		"followups": []string{"帮我发到社区，让大家投票", "2000 以内有什么推荐？"},
	})
	model := &fakeLLM{replies: []*llm.ChatResp{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: toolDone, Arguments: string(args)}}}}}
	r := newAgentRun(model, newFakeBrowser(), testLimits())

	// Act
	res, err := r.run(context.Background(), []llm.Message{llm.Text(llm.RoleUser, "值不值")}, true)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "价格偏高", res.Text)
	assert.Equal(t, []string{"帮我发到社区，让大家投票", "2000 以内有什么推荐？"}, res.Followups)
}

func Test_cleanFollowups(t *testing.T) {
	// Act：done 参数里的和 <followups> 块里的合在一起
	got := cleanFollowups([]string{"a", " ", "a", "1. b", "c", "d"})

	// Assert
	assert.Equal(t, []string{"a", "b", "c"}, got)
}
