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

// Package tool 是 Agent 各类工具共用的最底层：工具的结果、参数解析、截图的格式。
// 每类工具一个子包（browsertool、sandboxtool、videotool），只依赖这里，互相之间按需依赖；
// purchase_ai 把它们拼成工具清单，交给 Agent 循环。
//
// 每个工具都是「原始的 JSON 参数进、Result 出」，循环按名字分发，不用认识具体的工具
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Result 是一次工具调用的结果里，各类工具都会用到的部分。
// purchase_ai 的 toolResult 在它之外还有商品卡片、产物、done 的回答这些只有个别工具才有的东西
type Result struct {
	Text string // 给模型看的
	// Display 是给用户看的一句话（tool_end 的 summary）。由每个工具自己写，只放白名单里的东西：
	// 页面的正文、元素列表、截图都不放。空的显示"完成"，失败的不看它
	Display     string
	Failed      bool // 这次没做成，模型换个做法也许能成
	PageChanged bool // 页面可能变了，同一轮后面的编号已经过期
	Snapshot    bool // 这是一份快照，下一轮要换成占位语

	// Image 是截图的 data URI。工具消息里放不了图，它放在这一轮全部工具回复之后的一条用户消息里，
	// 下一轮开始时和旧快照一样换成占位语，只留最新一张
	Image string

	URL string // 执行完以后所在的网址（知道的话），给每一步的记录用
}

// Spec 是一个工具：告诉模型的名字、说明、参数，和执行它的函数
type Spec struct {
	Name        string
	Description string
	Args        any // 参数的结构体，用来生成 JSON Schema（llm.SchemaFor）
	Run         func(ctx context.Context, arguments string) (Result, error)
}

// Image 是一张图：插件的截图、下载的原图、工作台里的图片文件
type Image struct {
	Data   string `json:"data"` // base64，不带 data: 前缀
	Mime   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"` // 截图时所在的页面
}

// ErrBrowserGone 表示插件断开了或者没连上。模型写不了回答的错误才让循环立刻停，
// 浏览器没了模型还能根据已经查到的写回答，所以循环收到它走收尾
var ErrBrowserGone = errors.New("purchase ai: browser is gone")

// DecodeArgs 解析模型写的参数。它不保证是合法的 JSON，所以失败时告诉模型正确的格式
func DecodeArgs(arguments string, v any, example string) *Result {
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	if err := json.Unmarshal([]byte(arguments), v); err != nil {
		// 参数在半截断掉：模型一次要输出的太多（一整个网页），超过了输出上限被截断。
		// 只说「不是合法的 JSON」，模型会以为是格式错了，原样再写一遍，再被截断一次（2026-10-22 连着三次）
		if strings.Contains(err.Error(), "unexpected end of JSON input") {
			return &Result{Failed: true, Text: fmt.Sprintf("参数在第 %d 个字符处断掉了：一次要写的内容太长，输出被截断。"+
				"分几次写：先用 write_file 写前一部分，再用 edit_file 或者 bash 的 cat >> 往后补；表格这类重复的内容也可以用脚本生成。", len(arguments))}
		}
		return &Result{Failed: true, Text: "参数不是合法的 JSON，格式是 " + example + "。"}
	}
	return nil
}

// OneLine 把所有空白（包括换行）压成一个空格
func OneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ClipRunes 最多留 n 个字（按字符，不按字节：中文一个字三个字节）
func ClipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
