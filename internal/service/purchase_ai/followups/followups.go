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

// Package followups 是回答后面的「后续建议」：用户多半还会接着问的几句话，页面上显示成按钮。
// 它是一个插件（Plugin）：回答出来时把 <followups> 块切掉、整理好，放进 Extras 交给页面。
// 帖子分析是一条评论，没有人点按钮，不装这个插件，也不告诉模型这套规则
package followups

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// Extra 是 Answer.Extras 里放建议的 key，值是 []string
const Extra = "followups"

// Plugin 在最终回答出来时切掉 <followups> 块，建议放进 Extras
func Plugin() agent.Plugin {
	return func(r *agent.Registry) {
		r.OnAnswer(func(_ context.Context, a *agent.Answer) {
			text, items := Split(a.Text)
			a.Text = text
			if len(items) > 0 {
				a.Extras[Extra] = items
			}
		})
	}
}

// 后续建议：回答写完，模型觉得用户多半还会接着问，就在最后用 <followups> 包几句用户可能会说的话，
// 页面上显示成按钮，点一下直接发出去。拿不准、要听大家意见时，「帮我发到社区」就是其中一句。
// 回答是整段发给页面的，所以建议块在服务端切掉，用户看不到标签。
// 模型忘了写就算了：这只是辅助功能，不强加

const (
	maxFollowups   = 3
	followupRunes  = 30 // 一句建议最多几个字：按钮放不下长句
	followupsOpen  = "<followups>"
	followupsClose = "</followups>"
)

// followupItem 是块里的一句 <q>…</q>
var followupItem = regexp.MustCompile(`(?s)<q>(.*?)</q>`)

// listMarker 是一行开头的列表符号：「- 」「* 」「• 」「1. 」「2、」「3) 」。
// 只认符号本身，不能把开头的数字都去掉：「27 寸 4K 和 2K 怎么选」的 27 是内容（2026-10-20 线上实测）
var listMarker = regexp.MustCompile(`^\s*(?:[-*•·]\s*|\d+\s*[.、)）]\s*)`)

// Split 把回答里的 <followups> 块切出来，返回去掉块的回答和里面的建议（最多 maxFollowups 句）。
// 写得不规矩也认：没有 <q> 就一行一句；结束标签丢了（回答被截断）就切到结尾
func Split(text string) (string, []string) {
	before, after, ok := strings.Cut(text, followupsOpen)
	if !ok {
		return text, nil
	}
	body := after
	rest := ""
	if end := strings.Index(body, followupsClose); end >= 0 {
		rest = body[end+len(followupsClose):]
		body = body[:end]
	}
	answer := strings.TrimSpace(strings.TrimSpace(before) + "\n\n" + strings.TrimSpace(rest))

	var lines []string
	if items := followupItem.FindAllStringSubmatch(body, -1); len(items) > 0 {
		for _, m := range items {
			lines = append(lines, m[1])
		}
	} else {
		lines = strings.Split(body, "\n")
	}
	return answer, Clean(lines)
}

// Clean 整理建议：去掉列表符号和空的、重复的，太长的截短，最多 maxFollowups 句。
func Clean(lines []string) []string {
	var followups []string
	for _, line := range lines {
		// 模型偶尔还是写成列表：去掉开头的列表符号
		line = tool.OneLine(listMarker.ReplaceAllString(line, ""))
		if line == "" || slices.Contains(followups, line) {
			continue
		}
		if clipped := tool.ClipRunes(line, followupRunes); clipped != line {
			line = clipped + "…"
		}
		followups = append(followups, line)
		if len(followups) == maxFollowups {
			break
		}
	}
	return followups
}

// Rules 是系统提示词里的后续建议规则。只在对话里有：帖子分析是一条评论，没有人点按钮
const Rules = `后续建议：
- 回答写完以后，如果用户多半还会接着问，在回答的最后写一个 <followups> 块，里面每句用 <q></q> 包起来，最多 3 句，是用户接下来可能会对你说的话，站在用户的角度写，每句不超过 20 个字。例如：
<followups>
<q>帮我发到社区，让大家投票</q>
<q>京东和天猫哪家更划算？</q>
</followups>
- 拿不准该不该买、需要听听别人意见时，把「帮我发到社区，让大家投票」放进去。
- 没有值得接着问的就不写；用户已经说要发帖、你已经整理了帖子草稿时也不写。`
