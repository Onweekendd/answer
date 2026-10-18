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
	"encoding/json"
	"fmt"

	"github.com/apache/answer/internal/service/llm"
)

// 两种"卡住"：每一步都失败（failureCounter），和每一步都成功但没有进展（loopDetector）。

// failureCounter 数连续失败了几次（概念 3）
type failureCounter struct {
	limit int // 连续失败几次就该收尾
	n     int // 现在连续失败了几次
}

// record 按顺序记下一轮的结果，连续失败到 limit 时返回 true（⑦）。跳过的不算成功也不算失败
func (c *failureCounter) record(results []toolResult) bool {
	for _, res := range results {
		switch {
		case res.Skipped:
			// 没有执行，不能说明什么，计数保持原样
		case res.Failed:
			c.n++
		default:
			c.n = 0
		}
	}
	return c.n >= c.limit
}

// loopVerdict 是循环检测对一次动作的判断
type loopVerdict int

const (
	loopNone  loopVerdict = iota // 没问题
	loopNudge                    // 提醒模型换个做法
	loopStop                     // 走收尾
)

// loopDetector 记下最近几次动作的指纹，发现同一个指纹反复出现就提醒、再多就收尾（概念 5）
type loopDetector struct {
	window  int // 只看最近几次
	nudgeAt int // 第几次出现时提醒
	stopAt  int // 第几次出现时收尾
	recent  []string
}

// fingerprint 是一个动作的指纹（⑧）：工具 + 整理过的参数 + 做这个动作时的页面
func fingerprint(call llm.ToolCall, pageHash string) string {
	args := canonicalArgs(call.Arguments)
	// 三段之间加上分隔符，打印出来是 click:{"index":5}@3f9a1c07…，排查时一眼能分开
	return fmt.Sprintf("%s:%s@%s", call.Name, args, pageHash)
}

// record 记下一个指纹，数它在最近 window 次里出现了几次（⑧）。
// 不要求连续：在两个标签之间来回切换时，相邻两次的指纹并不一样
func (d *loopDetector) record(fp string) loopVerdict {
	d.recent = append(d.recent, fp)

	if len(d.recent) > d.window {
		d.recent = d.recent[len(d.recent)-d.window:]
	}

	count := 0
	for _, x := range d.recent {
		if x == fp {
			count++
		}
	}

	// 先判断收尾，再判断提醒；提醒用 ==，第 4 次就不再重复提醒
	switch {
	case count >= d.stopAt:
		return loopStop
	case count == d.nudgeAt:
		return loopNudge
	default:
		return loopNone
	}
}

// canonicalArgs 把模型写的参数整理成固定的写法：解析成 map 再序列化，
// encoding/json 序列化 map 时按键排序，空格和键的顺序就都不影响了。不是合法的 JSON 就原样返回
func canonicalArgs(arguments string) string {
	var v map[string]any
	if err := json.Unmarshal([]byte(arguments), &v); err != nil {
		return arguments
	}
	out, err := json.Marshal(v)
	if err != nil {
		return arguments
	}
	return string(out)
}
