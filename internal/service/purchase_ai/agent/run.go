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
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/segmentfault/pacman/log"
)

// TagScreenshot 标出 Agent 自己截的图所在的用户消息。BeforeModel 钩子按它认出旧截图
const TagScreenshot = "screenshot"

type callIDKey struct{}

// WithCallID 给 ctx 带上一个调用的 id，核心执行每个工具之前做的就是这件事。测试单个工具时用它
func WithCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callIDKey{}, id)
}

// CallID 是正在执行的这个调用的 id（tool_start 事件里那个）。挂起的事件要带上它，页面才知道是哪张卡片
func CallID(ctx context.Context) string {
	id, _ := ctx.Value(callIDKey{}).(string)
	return id
}

// state 是一次任务里循环自己的状态。每个任务一个，不共享
type state struct {
	messages  []llm.Message
	step      int  // 现在是第几步，从 0 开始
	reminded  bool // 已经提醒过一次「调用 done」
	steps     []Step
	reasoning string // 最后一次模型调用的思考
}

// Run 跑一次任务，直到模型调用 done、直接回答，或者走到收尾。
// messages 是开头的对话（系统提示词、历史、用户的问题）。
//
// 一步 = 调用一次模型，每一步按这个顺序走：
//
//	BeforeModel 钩子 → 调模型 → 没要工具：reply（回答 / 提醒调用 done / 空回复）
//	                         → 要了工具：runCalls（顺序执行，AfterTool 钩子）→ commit（回复和截图记进消息）
//	                           → 工具说「不能继续」→ 收尾；有 Terminate → 结束；EndOfTurn 钩子 → 提醒或收尾
//
// 每个工具调用都必须有一条工具消息回复，跳过的、done 也一样：
// 否则服务商会拒绝这段对话，而对话后面还要接着发（追问、收尾）。
func (a *Agent) Run(ctx context.Context, messages []llm.Message) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeouts.Run)
	defer cancel()

	// 先复制一份，循环只改自己的数组。不复制的话，调用方的切片还有空余容量时，append 会写进它的底层数组
	st := &state{messages: slices.Clone(messages)}

	for step := 0; step < a.cfg.MaxSteps; step++ {
		st.step = step
		resp, err := a.chat(ctx, &llm.ChatReq{Messages: a.forModel(st.messages), Tools: a.defs})
		if err != nil {
			return nil, err
		}
		st.reasoning = resp.Reasoning

		if len(resp.ToolCalls) == 0 {
			if res, err := a.reply(ctx, st, resp); res != nil || err != nil {
				return res, err
			}
			continue
		}

		st.messages = a.keep(ctx, st.messages, assistantMessage(resp), Meta{PromptTokens: resp.Usage.PromptTokens})
		outcomes, err := a.runCalls(ctx, st, resp.ToolCalls)
		var abort *tool.AbortError
		if err != nil && !errors.As(err, &abort) {
			return nil, err
		}
		final := a.commit(ctx, st, resp.ToolCalls, outcomes)

		// 工具说不能继续了（浏览器断开）：这一轮每个调用都已经有回复，对话是完整的，走收尾
		if abort != nil {
			return a.finish(ctx, st, Finish{Reason: Reason(abort.Reason), Prompt: abort.Prompt})
		}
		if final != nil {
			return &Result{Text: final.Text, Success: final.Success, Steps: st.steps, Extras: final.Extras}, nil
		}

		if f := a.judge(ctx, st, Turn{Step: step, Left: a.cfg.MaxSteps - step - 1, Calls: resp.ToolCalls, Results: outcomes}); f != nil {
			return a.finish(ctx, st, *f)
		}
	}

	return a.finish(ctx, st, Finish{Reason: ReasonSteps, Prompt: StepsPrompt})
}

// reply 处理模型这一步没要工具的情况：是最终回答、该提醒它调用 done，还是空回复。
// 返回的 *Result 非 nil 表示任务结束；两个都是 nil 表示接着下一步
func (a *Agent) reply(ctx context.Context, st *state, resp *llm.ChatResp) (*Result, error) {
	msg := assistantMessage(resp)
	empty := strings.TrimSpace(AnswerText(resp)) == ""
	if empty {
		msg.Parts = []llm.Part{{Text: EmptyReply}}
	}
	// 只回了文字：已经提醒过一次、这是最后一步（提醒了也没有步数去调用 done），或者根本没有 done，
	// 就当作答案；否则提醒它调用 done。空回复当不了答案：最后再问一次，不给工具，让它把回答写出来
	last := st.step == a.cfg.MaxSteps-1
	final := !empty && (!a.hasDone() || st.reminded || last)
	meta := Meta{PromptTokens: resp.Usage.PromptTokens}
	var ans *Answer
	if final {
		// 记进日志之前就要知道它是答案：答案引用的图要存好，和它一起记下（日志只追加，记了就改不了）
		ans = a.answer(ctx, AnswerText(resp))
		meta.Final, meta.Extras = true, ans.Extras
	}
	st.messages = a.keep(ctx, st.messages, msg, meta)

	if final {
		return &Result{Text: ans.Text, Success: true, Steps: st.steps, Reasoning: resp.Reasoning, Extras: ans.Extras}, nil
	}
	if !a.hasDone() || st.reminded || last {
		return a.finish(ctx, st, Finish{Reason: ReasonEmpty, Prompt: EmptyPrompt})
	}
	reminder := DoneReminder
	if empty {
		reminder = EmptyReminder
	}
	st.messages = a.keep(ctx, st.messages, llm.Text(llm.RoleUser, reminder), Meta{Synthetic: true})
	st.reminded = true
	return nil, nil
}

// runCalls 按顺序执行这一轮的调用，返回和 calls 一样长、一一对应的结果。
// 页面变了、失败了、done 了，或者工具说「不能继续」，后面的调用都跳过，但也有一条回复。
// 返回的 error 是 *tool.AbortError 时，结果仍然是完整的；别的 error 让任务立刻结束。
func (a *Agent) runCalls(ctx context.Context, st *state, calls []llm.ToolCall) ([]Outcome, error) {
	// 长度直接给满，按下标填：跳过的调用也要占住自己的位置
	out := make([]Outcome, len(calls))

	// skip 就是那个标识：空字符串表示继续执行；不为空表示后面的都跳过，内容就是跳过时回复的话
	skip := ""
	// 工具说不能继续时记下这个错误，等每个调用都有了结果再一起返回
	var abort error

	for i, call := range calls {
		if skip != "" {
			out[i] = Outcome{Result: tool.Result{Text: skip}, Skipped: true}
			continue
		}

		// 跳过的调用不发事件，用户只看到真的做了的事；done 不是一张工具卡片，它的回答走 text
		id := fmt.Sprintf("%d-%d", st.step, i)
		shown := call.Name != DoneName && a.cfg.Observer != nil
		summary := ""
		if shown {
			summary = a.cfg.Observer.Describe(call)
			a.cfg.Observer.ToolStart(ctx, id, call, summary)
		}

		res, err := a.runTool(WithCallID(ctx, id), st, call)
		var stop *tool.AbortError
		if errors.As(err, &stop) {
			res = tool.Result{Failed: true, Text: stop.Text}
			skip, abort = SkippedText, err
		} else if err != nil {
			return nil, err
		}
		out[i] = Outcome{Result: res, Summary: summary}
		if shown {
			a.cfg.Observer.ToolEnd(ctx, id, call, res)
		}
		if abort != nil {
			continue
		}
		switch {
		case res.Terminate:
			skip = SkippedAfterDoneText
		case res.Failed:
			skip = SkippedAfterFailText
		case res.PageChanged:
			skip = SkippedText
		}
	}
	return out, abort
}

// runTool 执行一次调用。模型编出来的工具名告诉它，不算出错，它可以自己改正。执行之后记下这一步
func (a *Agent) runTool(ctx context.Context, st *state, call llm.ToolCall) (tool.Result, error) {
	spec, ok := a.byName[call.Name]
	if !ok {
		log.Warnf("purchase ai: model called unknown tool %q", call.Name)
		return tool.Result{Failed: true, Text: "没有这个工具：" + call.Name}, nil
	}
	started := time.Now()
	res, err := spec.Run(ctx, call.Arguments)
	if err == nil {
		for _, fn := range a.reg.afterTool {
			fn(ctx, call, &res)
		}
	}
	st.steps = append(st.steps, Step{
		Step:      st.step,
		Tool:      call.Name,
		Arguments: call.Arguments,
		Failed:    res.Failed || err != nil,
		URL:       res.URL,
		Duration:  time.Since(started),
	})
	return res, err
}

// final 是这一轮里某个工具（done、draft_post）交出来的最终回答
type final struct {
	Text    string
	Success bool
	Extras  map[string]any
}

// commit 把这一轮的结果记进消息：先是每个调用的工具回复（按调用的顺序，一一对应），
// 再是截图（整批回复之后的一条用户消息）。assistant 调用了几个工具，紧跟着的就必须是这几条回复，
// 所以消息顺序的不变量只在这里维护。
// 这一轮有工具交出最终回答（Terminate）时返回它：回答引用的图在这里存好、和那条回复一起记下
func (a *Agent) commit(ctx context.Context, st *state, calls []llm.ToolCall, results []Outcome) *final {
	var done *final
	for i, call := range calls {
		res := results[i]
		meta := Meta{ToolName: call.Name, Summary: res.Summary, Failed: res.Failed, Display: res.Display, Extras: res.Extras}
		if res.Terminate && !res.Skipped {
			ans := a.answer(ctx, res.Answer)
			meta.Final = true
			meta.Extras = mergeExtras(res.Extras, ans.Extras)
			done = &final{Text: ans.Text, Success: res.Success, Extras: ans.Extras}
		}
		st.messages = a.keep(ctx, st.messages, toolMessage(call.ID, res.Text), meta)
	}
	for _, res := range results {
		if res.Image != "" {
			msg := screenshotMessage(st.step, res.Image)
			st.messages = a.keep(ctx, st.messages, msg, Meta{Synthetic: true})
		}
	}
	return done
}

// judge 依次问各个插件的 EndOfTurn：它们要追加的提醒都追加，直到有人要求收尾。返回非 nil 就是走收尾
func (a *Agent) judge(ctx context.Context, st *state, t Turn) *Finish {
	for _, fn := range a.reg.endOfTurn {
		v := fn(t)
		for _, m := range v.Messages {
			st.messages = a.keep(ctx, st.messages, m, Meta{Synthetic: true})
		}
		if v.Finish != nil {
			return v.Finish
		}
	}
	return nil
}

// finish 是被迫停下时的最后一步：追加 f.Prompt，不给工具再问一次模型，让它把回答写出来
func (a *Agent) finish(ctx context.Context, st *state, f Finish) (*Result, error) {
	st.messages = a.keep(ctx, st.messages, llm.Text(llm.RoleUser, f.Prompt), Meta{Synthetic: true})

	// 不给工具：模型只能用文字作答
	resp, err := a.chat(ctx, &llm.ChatReq{Messages: a.forModel(st.messages)})
	if err != nil {
		return nil, err
	}
	// 写成文字的不是 done、是别的工具调用（2026-10-21 成都案例写了一个 open）：那不是回答，AnswerText 也取不出来。
	// 再问一次，明说不能再调用工具
	if LeakedCall(resp) {
		st.messages = a.keep(ctx, st.messages, assistantMessage(resp), Meta{PromptTokens: resp.Usage.PromptTokens})
		st.messages = a.keep(ctx, st.messages, llm.Text(llm.RoleUser, LeakedCallText), Meta{Synthetic: true})
		if resp, err = a.chat(ctx, &llm.ChatReq{Messages: a.forModel(st.messages)}); err != nil {
			return nil, err
		}
	}
	// 收尾的回答也记进日志：下一轮要知道这一轮是怎么结束的
	ans := a.answer(ctx, AnswerText(resp))
	a.keep(ctx, st.messages, assistantMessage(resp), Meta{PromptTokens: resp.Usage.PromptTokens, Final: true, Extras: ans.Extras})

	return &Result{
		Text:      ans.Text,
		Success:   false, // 被迫停下的，要求的事没有做完
		Finish:    f.Reason,
		Steps:     st.steps,
		Reasoning: resp.Reasoning,
		Extras:    ans.Extras,
	}, nil
}

// answer 把一段文字作为最终回答：交给所有 OnAnswer 钩子处理，返回处理后的回答
func (a *Agent) answer(ctx context.Context, text string) *Answer {
	ans := &Answer{Text: text, Extras: map[string]any{}}
	for _, fn := range a.reg.onAnswer {
		fn(ctx, ans)
	}
	return ans
}

// assistantMessage 把模型的回复放回对话：调用了哪些工具、思考过程都要原样带回去
func assistantMessage(resp *llm.ChatResp) llm.Message {
	msg := llm.Text(llm.RoleAssistant, resp.Content)
	msg.ToolCalls = resp.ToolCalls
	msg.Reasoning = resp.Reasoning
	return msg
}

func toolMessage(callID, text string) llm.Message {
	msg := llm.Text(llm.RoleTool, text)
	msg.ToolCallID = callID
	return msg
}

// screenshotMessage 是放截图的那条用户消息：先说明这是什么，再放图
func screenshotMessage(step int, image string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Tag: TagScreenshot, Parts: []llm.Part{
		{Text: fmt.Sprintf("这是第 %d 步截的图，是页面现在的样子：", step+1)},
		{ImageURL: image},
	}}
}

func mergeExtras(a, b map[string]any) map[string]any {
	if len(a) == 0 {
		return b
	}
	out := make(map[string]any, len(a)+len(b))
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}
