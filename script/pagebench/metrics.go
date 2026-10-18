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
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Metrics 是从快照文本里算出来的指标，只有数字，不含页面内容，可以提交进仓库当基线
type Metrics struct {
	Lines      int `json:"lines"`      // 输出了几行（几个编号元素）
	Chars      int `json:"chars"`      // 一共多少个字
	Duplicates int `json:"duplicates"` // 去掉编号后和前面某一行完全一样的行，比如轮播图的复制品
	Empty      int `json:"empty"`      // 没有任何文字的行，比如 [3]<a>
	Clipped    int `json:"clipped"`    // 文字碰到长度上限被截断的行（以 … 结尾）
}

var (
	// 一行的格式：[12]<a id=x>文字。lineIndex 去掉开头的编号，剩下的部分用来比较是否重复
	lineIndex = regexp.MustCompile(`^\[\d+\]`)
	// 只有标签、没有文字：[3]<a> 或 [3]<button aria-label=关闭>
	emptyLine = regexp.MustCompile(`^\[\d+\]<[^>]*>$`)
)

// Analyze 统计快照的 elements 文本
func Analyze(elements string) Metrics {
	var m Metrics
	if elements == "" {
		return m
	}
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(elements, "\n") {
		m.Lines++
		m.Chars += utf8.RuneCountInString(line)
		body := lineIndex.ReplaceAllString(line, "")
		if seen[body] {
			m.Duplicates++
		}
		seen[body] = true
		if emptyLine.MatchString(line) {
			m.Empty++
		}
		if strings.HasSuffix(line, "…") {
			m.Clipped++
		}
	}
	m.Chars += m.Lines - 1 // 行与行之间的换行符
	return m
}

// 跳到这些页面说明被拦下来了，要人来处理
var (
	blockedHosts  = []string{"login.", "passport.", "sec.", "verify."}
	blockedTitles = []string{"登录", "验证", "安全", "滑块"}
)

// BlockedReason 判断跳转后是不是落在了登录页或验证码页。是的话返回原因，否则返回空字符串
func BlockedReason(pageURL, title string) string {
	if u, err := url.Parse(pageURL); err == nil {
		for _, prefix := range blockedHosts {
			if strings.HasPrefix(u.Hostname(), prefix) {
				return "redirected to " + u.Hostname()
			}
		}
		if strings.Contains(strings.ToLower(u.Path), "login") {
			return "redirected to a login page"
		}
	}
	for _, word := range blockedTitles {
		if strings.Contains(title, word) {
			return "page title mentions " + word
		}
	}
	return ""
}

// Redact 把 words 里的每个词（比如账号昵称）替换成 [已屏蔽]，再写到磁盘上
func Redact(text string, words []string) string {
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			text = strings.ReplaceAll(text, w, "[已屏蔽]")
		}
	}
	return text
}
