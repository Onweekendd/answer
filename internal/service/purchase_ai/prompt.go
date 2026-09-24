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
	"fmt"
	"strings"

	"github.com/apache/answer/internal/schema"
)

// maxContentRunes caps how much of the post body reaches the model. Counted in
// runes, not bytes, so a cut never lands inside a multi-byte character.
// A real token budget arrives with the context builder; this is the cheap version.
const maxContentRunes = 2000

// maxAdviceRunes is what the prompt asks the model to stay under. The comment
// table takes more, but a wall of text reads badly next to human replies.
const maxAdviceRunes = 500

// buildSystemPrompt describes the job. It has no search tool yet, so the last
// two rules matter most: without them the model states market prices and review
// scores it has no way to know, in a confident voice.
func buildSystemPrompt() string {
	return fmt.Sprintf(`你是一个购物决策顾问，在一个叫「买不买」的社区里回答用户「这个东西我该不该买」。

请给出一个明确结论，只能是以下三者之一，并放在回答的第一行，用加粗标出：
- **买** —— 值得现在买
- **再等等** —— 东西不错但时机不对（新款将至、大促临近、价格偏高）
- **不买** —— 不建议买

结论之后用 2 到 4 条理由说明，每条一行，从这些角度考虑：
- 价格是否合理（相对于这类商品的常见定位）
- 用户描述的需求是否真的需要这个东西
- 是否有明显更划算的替代选择
- 使用频率、可替代性、是否冲动消费

硬性要求：
1. 用中文回答，用 Markdown 格式（会作为社区评论渲染）。
2. 控制在 %d 字以内，说重点，不要客套和总结段。
3. **你没有联网能力。** 绝对不要编造具体的市场价、竞品报价、评测结论、销量、库存、促销时间。你只知道用户提供的信息和你的常识。
4. 涉及需要查证的事实时，明确说明这一点，例如「建议自己比一下各平台当前价格」，而不是给出一个你并不知道的数字。
5. 不要重复用户已经写过的内容。`, maxAdviceRunes)
}

// buildUserPrompt lays out what is known about this one post. Fields the author
// left empty are omitted rather than sent as zero values: a line reading
// "价格：0.00 CNY" would tell the model the item is free.
func buildUserPrompt(question *schema.QuestionInfoResp) string {
	var b strings.Builder

	fmt.Fprintf(&b, "标题：%s\n", question.Title)

	if purchase := question.Purchase; purchase != nil {
		if purchase.Price > 0 {
			currency := purchase.Currency
			if currency == "" {
				currency = "CNY"
			}
			fmt.Fprintf(&b, "价格：%.2f %s\n", purchase.Price, currency)
		}
		if purchase.Link != "" {
			fmt.Fprintf(&b, "商品链接：%s\n", purchase.Link)
		}
	}

	if names := tagNames(question.Tags); len(names) > 0 {
		fmt.Fprintf(&b, "品类：%s\n", strings.Join(names, "、"))
	}

	if content := strings.TrimSpace(question.Content); content != "" {
		fmt.Fprintf(&b, "\n用户的描述：\n%s\n", truncateRunes(content, maxContentRunes))
	}

	return b.String()
}

func tagNames(tags []*schema.TagResp) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != nil && tag.DisplayName != "" {
			names = append(names, tag.DisplayName)
		}
	}
	return names
}

// truncateRunes cuts s to at most max runes. Slicing a string cuts bytes, which
// splits a Chinese character into garbage, so the conversion is the point.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…（内容过长已截断）"
}
