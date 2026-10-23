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

package agent

import (
	"regexp"
	"strings"

	"github.com/apache/answer/internal/service/llm"
)

// leakedDoneText 匹配模型当成文字写出来的 done 调用里的 text 参数。
// deepseek 在没给工具、对话里却全是工具调用时（收尾就是这样），有时会照着样子把调用 done 写成它内部的
// DSML 标记，而不是直接回答：<｜｜DSML｜｜ invoke name="done"> … <｜｜DSML｜｜ parameter name="text" …>回答</｜｜DSML｜｜ parameter>
var leakedDoneText = regexp.MustCompile(`(?s)invoke name="done">.*?parameter name="text"[^>]*>(.*?)</[^<>]*parameter>`)

// AnswerText 是模型回复里给用户的回答：正常就是 Content；模型把 done 写成了文字时，取出里面的 text
func AnswerText(resp *llm.ChatResp) string {
	if strings.Contains(resp.Content, "DSML") {
		if m := leakedDoneText.FindStringSubmatch(resp.Content); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return resp.Content
}

// LeakedCall 说模型是不是把 done 以外的工具调用写成了文字
func LeakedCall(resp *llm.ChatResp) bool {
	return strings.Contains(resp.Content, "DSML") && !leakedDoneText.MatchString(resp.Content)
}
