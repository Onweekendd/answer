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
	"regexp"
	"slices"
	"strings"
)

// 后续建议：回答写完，模型觉得用户多半还会接着问，就在最后用 <followups> 包几句用户可能会说的话，
// 页面上显示成按钮，点一下直接发出去。拿不准、要听大家意见时，「帮我发到社区」就是其中一句。
// 回答是整段发给页面的，所以建议块在服务端切掉，用户看不到标签

const (
	maxFollowups   = 3
	followupRunes  = 30 // 一句建议最多几个字：按钮放不下长句
	followupsOpen  = "<followups>"
	followupsClose = "</followups>"
)

// followupItem 是块里的一句 <q>…</q>
var followupItem = regexp.MustCompile(`(?s)<q>(.*?)</q>`)

// splitFollowups 把回答里的 <followups> 块切出来，返回去掉块的回答和里面的建议（最多 maxFollowups 句）。
// 写得不规矩也认：没有 <q> 就一行一句；结束标签丢了（回答被截断）就切到结尾
func splitFollowups(text string) (string, []string) {
	start := strings.Index(text, followupsOpen)
	if start < 0 {
		return text, nil
	}
	body := text[start+len(followupsOpen):]
	rest := ""
	if end := strings.Index(body, followupsClose); end >= 0 {
		rest = body[end+len(followupsClose):]
		body = body[:end]
	}
	answer := strings.TrimSpace(strings.TrimSpace(text[:start]) + "\n\n" + strings.TrimSpace(rest))

	var lines []string
	if items := followupItem.FindAllStringSubmatch(body, -1); len(items) > 0 {
		for _, m := range items {
			lines = append(lines, m[1])
		}
	} else {
		lines = strings.Split(body, "\n")
	}

	var followups []string
	for _, line := range lines {
		// 模型偶尔还是写成列表：去掉开头的列表符号
		line = oneLine(strings.TrimLeft(strings.TrimSpace(line), "-*•·0123456789.、) "))
		if line == "" || slices.Contains(followups, line) {
			continue
		}
		if clipped := clipRunes(line, followupRunes); clipped != line {
			line = clipped + "…"
		}
		followups = append(followups, line)
		if len(followups) == maxFollowups {
			break
		}
	}
	return answer, followups
}

// followupRules 只在对话里有：帖子分析是一条评论，没有人点按钮
const followupRules = `后续建议：
- 回答写完以后，如果用户多半还会接着问，在回答的最后写一个 <followups> 块，里面每句用 <q></q> 包起来，最多 3 句，是用户接下来可能会对你说的话，站在用户的角度写，每句不超过 20 个字。例如：
<followups>
<q>帮我发到社区，让大家投票</q>
<q>京东和天猫哪家更划算？</q>
</followups>
- 拿不准该不该买、需要听听别人意见时，把「帮我发到社区，让大家投票」放进去。
- 没有值得接着问的就不写；用户已经说要发帖、你已经整理了帖子草稿时也不写。`

// followupsInDone 只在有浏览器工具时加上：这时回答是 done 的 text，模型常常把建议只想在思考里
const followupsInDone = "\n- 用 done 结束时，<followups> 块写在 done 的 text 最后：只写在思考里，用户看不到。"
