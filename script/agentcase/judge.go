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
	"fmt"
	"regexp"
	"slices"
	"strings"

	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
)

// judge 判定一次运行有没有通过：返回没有满足的条件，每条一句话；空切片表示通过。
// 只看痕迹（网址、执行过的动作、看到过的快照、回答），不看 res.Success：那是模型给自己打的分。
// 每条没满足的都记下来，不在第一条就返回：表格里要一次看全是哪几条没满足
func judge(c Case, res *purchaseai.TaskResult, snapshots []string) []string {
	var problems []string

	// ① 每个要求的网址，都在某一步的网址里出现过
	for _, want := range c.Expect.Visited {
		visited := slices.ContainsFunc(res.Steps, func(s purchaseai.TaskStep) bool {
			return strings.Contains(s.URL, want)
		})
		if !visited {
			problems = append(problems, "没有到过 "+want)
		}
	}

	// ② 每个要求的工具，都成功执行过至少一次；失败了的那一步不算
	for _, tool := range c.Expect.Used {
		used := slices.ContainsFunc(res.Steps, func(s purchaseai.TaskStep) bool {
			return s.Tool == tool && !s.Failed
		})
		if !used {
			problems = append(problems, "没有成功执行过 "+tool)
		}
	}

	// ③ 每段要求的文字，都在某一份快照里出现过
	for _, text := range c.Expect.Seen {
		seen := slices.ContainsFunc(snapshots, func(snap string) bool {
			return strings.Contains(snap, text)
		})
		if !seen {
			problems = append(problems, "快照里没有出现过 "+text)
		}
	}

	// ④ 回答里的价格够数
	if got := countPrices(res.Text); got < c.Expect.MinPrices {
		problems = append(problems, fmt.Sprintf("回答里只有 %d 个价格，要求至少 %d 个", got, c.Expect.MinPrices))
	}

	return problems
}

// price 是带单位的数字：¥ 或 ￥ 在前（后面可以有空格），或者"元"在后（前面可以有空格）。
// 数字可以带千分位逗号和小数。两种写法各有一个分组，匹配上的那个不为空
var price = regexp.MustCompile(`[¥￥]\s*(\d[\d,]*(?:\.\d+)?)|(\d[\d,]*(?:\.\d+)?)\s*元`)

// countPrices 数回答里有几个不同的价格。不带单位的数字不算：256GB、iPhone 17、24 期免息
func countPrices(text string) int {
	seen := map[string]bool{}
	for _, m := range price.FindAllStringSubmatch(text, -1) {
		// m[0] 是整段匹配，m[1] 是 ¥ 后面的数字，m[2] 是"元"前面的数字
		number := m[1]
		if number == "" {
			number = m[2]
		}
		// 去掉逗号再比较：5,499 和 5499 是同一个价格
		seen[strings.ReplaceAll(number, ",", "")] = true
	}
	return len(seen)
}
