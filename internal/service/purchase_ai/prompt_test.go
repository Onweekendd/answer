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
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/schema"
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

	t.Run("聊天不下买不买的结论，帖子分析要把结论放在第一行（AI 拿它投票）", func(t *testing.T) {
		// Act
		chat := buildSystemPrompt(now, true)
		post := buildPostPrompt(now, true)

		// Assert
		require.Contains(t, chat, "社区投票决定")
		require.NotContains(t, chat, "放在回答的第一行")
		require.Contains(t, post, "放在回答的第一行")
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

func Test_buildUserPrompt(t *testing.T) {
	question := &schema.QuestionInfoResp{
		Title:    "Switch OLED 现在值得买吗",
		Purchase: &schema.PurchaseInfo{Price: 1999, Currency: "CNY"},
	}
	ptr := func(v float64) *float64 { return &v }

	t.Run("没有识别结果时不出现商品段落", func(t *testing.T) {
		// Act
		prompt := buildUserPrompt(question, nil)

		// Assert
		require.Contains(t, prompt, "价格：1999.00 CNY")
		require.NotContains(t, prompt, "截图识别出的商品")
	})

	t.Run("识别结果放在用户填写的后面，并说明冲突时以用户为准", func(t *testing.T) {
		// Arrange: the reading's price differs from what the author typed
		official := true
		product := &schema.PurchaseProduct{
			Name: "任天堂 Switch OLED 国行 白色", Brand: "任天堂", Model: "Switch OLED",
			Specs: []string{"国行", "白色"}, Platform: "京东", Shop: "任天堂京东自营旗舰店",
			Official: &official, Price: ptr(2099), ListPrice: ptr(2599),
			Promotions: []string{"满2000减100"}, Confidence: 0.9,
		}

		// Act
		prompt := buildUserPrompt(question, product)

		// Assert
		require.Contains(t, prompt, "以用户填写的为准")
		require.Less(t, strings.Index(prompt, "价格：1999.00 CNY"), strings.Index(prompt, "截图识别出的商品"))
		for _, line := range []string{
			"- 名称：任天堂 Switch OLED 国行 白色", "- 规格：国行、白色", "- 平台：京东",
			"- 官方店：是", "- 截图里的到手价：2099.00 元", "- 截图里的原价：2599.00 元", "- 优惠：满2000减100",
		} {
			require.Contains(t, prompt, line)
		}
		require.NotContains(t, prompt, "把握不大")
	})

	t.Run("截图里没有的字段不写，0 元和非官方店照写", func(t *testing.T) {
		// Arrange
		official := false
		product := &schema.PurchaseProduct{Name: "x", Official: &official, Price: ptr(0), Specs: []string{}, Confidence: 0.9}

		// Act
		prompt := buildUserPrompt(question, product)

		// Assert
		require.Contains(t, prompt, "- 官方店：否")
		require.Contains(t, prompt, "- 截图里的到手价：0.00 元")
		for _, label := range []string{"品牌：", "型号：", "规格：", "平台：", "店铺：", "原价：", "优惠：", "识别备注："} {
			require.NotContains(t, prompt, label)
		}
	})

	t.Run("置信度低时提醒模型只作参考", func(t *testing.T) {
		// Act
		prompt := buildUserPrompt(question, &schema.PurchaseProduct{Name: "x", Confidence: 0.3, Notes: "价格被弹窗挡住"})

		// Assert
		require.Contains(t, prompt, "把握不大")
		require.Contains(t, prompt, "- 识别备注：价格被弹窗挡住")
	})
}
