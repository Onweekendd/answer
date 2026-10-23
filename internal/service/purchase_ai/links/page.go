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

package links

import (
	"html"
	"regexp"
	"strings"
)

// 交付的网页里的链接。网页是模型写的，链接可能：
//   - 带着追踪参数：和商品卡片一样要洗掉（Clean）
//   - 点不动：iframe 里的普通链接想在框里打开，小红书、淘宝都不许被嵌进框里，结果是白屏；要 target="_blank"
//   - 是编的，或者是网页内容里注入的：用户在我们的产物里点到它，会以为是我们核实过的
//
// 所以交付时每个 <a href> 都洗一遍、补上 target 和 rel；再核对这次是不是真的见过它，
// 没见过的列给模型，由它去核实或者删掉，不替它删

var (
	anchorTag = regexp.MustCompile(`(?i)<a\b[^>]*>`)
	// anchorAttr 是 <a> 上要换掉的属性：href 洗过再写回去，target、rel 统一写
	anchorAttr = regexp.MustCompile(`(?i)\s(href|target|rel)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
)

// Report 是 Fix 看到的链接情况，给模型看
type Report struct {
	Unseen  []string        // 这次没见过的网址（洗过的）
	Dropped []string        // 点不了、去掉了的（javascript:、广告跳转）
	Keys    map[string]bool // 网页里链到的每个网址的 Key
}

// Fix 把网页里每个 <a href> 洗干净、补上 target="_blank" rel="noopener noreferrer"。
// 页内跳转（#第三天）不动；不是 http(s) 的去掉 href，文字留着。seen 说一个网址（Key）这次见没见过
func Fix(page []byte, seen func(key string) bool) ([]byte, Report) {
	report := Report{Keys: map[string]bool{}}
	out := anchorTag.ReplaceAllStringFunc(string(page), func(tag string) string {
		var href string
		found := false
		rest := anchorAttr.ReplaceAllStringFunc(tag, func(attr string) string {
			m := anchorAttr.FindStringSubmatch(attr)
			if strings.EqualFold(m[1], "href") {
				href, found = html.UnescapeString(strings.Trim(m[2], `"'`)), true
			}
			return ""
		})
		href = strings.TrimSpace(href)
		if !found || strings.HasPrefix(href, "#") {
			return tag
		}
		rest = rest[len("<a"):] // 下面重新拼开头
		clean, ok := Clean(href)
		if !ok {
			report.Dropped = append(report.Dropped, href)
			return "<a" + rest
		}
		key := Key(clean)
		if !report.Keys[key] {
			report.Keys[key] = true
			if !seen(key) {
				report.Unseen = append(report.Unseen, clean)
			}
		}
		return `<a href="` + html.EscapeString(clean) + `" target="_blank" rel="noopener noreferrer"` + rest
	})
	return []byte(out), report
}
