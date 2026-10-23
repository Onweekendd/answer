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

// Package guard 是防止 Agent 打转或者没完没了的几个插件：连续失败、重复动作、快用完步数的提醒。
// 步数上限本身是核心管的（agent.Config.MaxSteps），因为循环要知道什么时候停；这里管的是
// 「到了某个时候说一句话」和「发现不对劲就要求收尾」。
//
// 每个限制是一个插件，参数和提醒的文字都在这里，入口把要用的挑出来写进插件列表。
// 顺序有意义：EndOfTurn 按注册顺序评估，第一个要求收尾的赢，后面的不再被问。
// 入口的顺序是 Failures、Loops、Warn（见 purchaseai 的 assemble）
package guard

import (
	"encoding/json"
	"fmt"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
)

const (
	// ReasonFailures 是连续失败太多次的收尾原因
	ReasonFailures agent.Reason = "failures"
	// ReasonLoop 是一直在重复同一个动作的收尾原因
	ReasonLoop agent.Reason = "loop"
)

const (
	// FailuresPrompt 是连续失败收尾时追加给模型的话
	FailuresPrompt = "浏览器操作连续失败，不再继续。请根据已经看到的信息作答，并说明哪些信息没能查到。"
	// LoopPrompt 是重复动作收尾时追加给模型的话
	LoopPrompt = "你一直在重复同样的操作，不再继续。请根据已经看到的信息作答，并说明哪些信息没能查到。"
)

// Failures 在工具连续失败 limit 次时要求收尾。跳过的调用不算成功也不算失败
func Failures(limit int) agent.Plugin {
	return func(r *agent.Registry) {
		c := &failureCounter{limit: limit}
		r.EndOfTurn(func(t agent.Turn) agent.Verdict {
			if c.record(t.Results) {
				return agent.Verdict{Finish: &agent.Finish{
					Reason: ReasonFailures,
					Prompt: FailuresPrompt,
				}}
			}
			return agent.Verdict{}
		})
	}
}

// Loops 发现同一个动作（工具 + 参数 + 当时外部世界的状态）在最近 window 次里出现 nudgeAt 次就提醒，
// 出现 stopAt 次就要求收尾。只看真的执行了的调用
func Loops(window, nudgeAt, stopAt int) agent.Plugin {
	return func(r *agent.Registry) {
		d := &loopDetector{window: window, nudgeAt: nudgeAt, stopAt: stopAt}
		r.EndOfTurn(func(t agent.Turn) agent.Verdict {
			var v agent.Verdict
			for i, call := range t.Calls {
				if t.Results[i].Skipped {
					continue
				}
				switch d.record(fingerprint(call, t.Results[i].State)) {
				case loopNudge:
					v.Messages = append(v.Messages, llm.Text(llm.RoleUser, loopNudgeText(call, nudgeAt)))
				case loopStop:
					v.Finish = &agent.Finish{
						Reason: ReasonLoop,
						Prompt: LoopPrompt,
					}
					return v
				}
			}
			return v
		})
	}
}

// Warn 在正好还剩 at 步时提醒一次。追加在对话最后，不改系统提示词，前缀缓存才能命中
func Warn(at int) agent.Plugin {
	return func(r *agent.Registry) {
		r.EndOfTurn(func(t agent.Turn) agent.Verdict {
			if t.Left != at {
				return agent.Verdict{}
			}
			return agent.Verdict{Messages: []llm.Message{llm.Text(llm.RoleUser, WarnText(t.Left))}}
		})
	}
}

// WarnText 是剩 remaining 步时追加的话
func WarnText(remaining int) string {
	return fmt.Sprintf("只剩 %d 步了。信息够了就调用 done 作答；不够，就用剩下的步数补上最关键的一项。", remaining)
}

// failureCounter 数连续失败了几次
type failureCounter struct {
	limit int // 连续失败几次就该收尾
	n     int // 现在连续失败了几次
}

// record 按顺序记下一轮的结果，连续失败到 limit 时返回 true。跳过的不算成功也不算失败
func (c *failureCounter) record(results []agent.Outcome) bool {
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

// loopDetector 记下最近几次动作的指纹，发现同一个指纹反复出现就提醒、再多就收尾
type loopDetector struct {
	window  int // 只看最近几次
	nudgeAt int // 第几次出现时提醒
	stopAt  int // 第几次出现时收尾
	recent  []string
}

// fingerprint 是一个动作的指纹：工具 + 整理过的参数 + 做这个动作时的状态
func fingerprint(call llm.ToolCall, state string) string {
	args := canonicalArgs(call.Arguments)
	// 三段之间加上分隔符，打印出来是 click:{"index":5}@3f9a1c07…，排查时一眼能分开
	return fmt.Sprintf("%s:%s@%s", call.Name, args, state)
}

// record 记下一个指纹，数它在最近 window 次里出现了几次。
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

// loopNudgeText 在同一个动作重复出现时追加
func loopNudgeText(call llm.ToolCall, times int) string {
	return fmt.Sprintf("你已经在同一个页面状态下第 %d 次执行 %s(%s)，页面没有出现新的内容。"+
		"换个做法，比如换一个元素、换一个搜索词、换一个网站，或者根据已有的信息调用 done 作答。",
		times, call.Name, canonicalArgs(call.Arguments))
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
