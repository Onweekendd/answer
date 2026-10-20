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
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/apache/answer/internal/service/llm"
)

// runAgent 让模型操作浏览器，直到它调用 done、直接回答，或者走到收尾。
// browserOn 为 false 时不给任何工具，模型只能直接回答（概念 7 的降级）。
func (pas *PurchaseAIService) runAgent(ctx context.Context, messages []llm.Message, browserOn bool) (*agentResult, error) {
	r := newAgentRun(pas.llmClient, pas.browser, defaultLimits)
	r.images = pas.uploaderService
	defer r.closeTabs()
	return r.run(ctx, messages, browserOn)
}

// run 跑完一次任务，结果带上做好的商品卡片和回答引用的图。循环里有好几个出口，在这里统一带上
func (r *agentRun) run(ctx context.Context, messages []llm.Message, browserOn bool) (*agentResult, error) {
	res, err := r.loop(ctx, messages, browserOn)
	if res != nil {
		res.Products = r.products
		res.Shots = r.citedShots(ctx, res.Text)
	}
	return res, err
}

// loop 是 Agent 循环。一步 = 调用一次模型；每一步按 ①～⑨ 的顺序走。
//
// 每个工具调用都必须有一条工具消息回复，跳过的、done 也一样：
// 否则服务商会拒绝这段对话，而对话后面还要接着发（追问、收尾）。
func (r *agentRun) loop(ctx context.Context, messages []llm.Message, browserOn bool) (*agentResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.limits.runTimeout)
	defer cancel()

	// 先复制一份，循环只改自己的数组。不复制的话，调用方的切片还有空余容量时，append 会写进它的底层数组；
	// 以后传进来的历史里带着快照时，① 还会改掉调用方存着的那几条。
	// 只复制了消息这一层，Parts 还是共用的，所以 ① 只能整个换掉 Parts，不能往里写
	messages = slices.Clone(messages)

	var tools map[string]agentTool
	var defs []llm.Tool
	if browserOn || r.draft != nil || r.workspaces != nil {
		var err error
		tools, defs, err = r.tools(browserOn)
		if err != nil {
			return nil, err
		}
	}

	var snapshotAt []int   // 快照的工具消息在 messages 里的下标
	var screenshotAt []int // Agent 自己截的图所在的用户消息的下标；用户上传的截图不在里面，永远保留
	reminded := false      // 已经提醒过一次"调用 done"

	for step := 0; step < r.limits.maxSteps; step++ {
		r.step = step

		// ① 旧快照换成占位语，只留最新的一份；截图也一样
		compactSnapshots(messages, snapshotAt)
		compactScreenshots(messages, screenshotAt)

		// ② 调用模型
		resp, err := r.chat(ctx, &llm.ChatReq{Messages: messages, Tools: defs})
		if err != nil {
			return nil, err
		}
		msg := assistantMessage(resp)
		empty := len(resp.ToolCalls) == 0 && strings.TrimSpace(answerText(resp)) == ""
		if empty {
			msg.Parts = []llm.Part{{Text: emptyReply}}
		}
		messages = append(messages, msg)

		if len(resp.ToolCalls) == 0 {
			// ③ 只回了文字：没给工具、已经提醒过一次，或者这是最后一步（提醒了也没有步数去调用 done），
			// 就当作答案；否则提醒它调用 done。空回复当不了答案：最后再问一次，不给工具，让它把回答写出来
			// 插件没连着时只有 draft_post，没有 done：直接回答就是答案
			if !browserOn || reminded || step == r.limits.maxSteps-1 {
				if empty {
					return r.finish(ctx, messages, finishEmpty)
				}
				return &agentResult{Text: answerText(resp), Success: true, Steps: r.steps, Reasoning: resp.Reasoning}, nil
			}
			reminder := doneReminder
			if empty {
				reminder = emptyReminder
			}
			messages = append(messages, llm.Text(llm.RoleUser, reminder))
			reminded = true
			continue
		}

		// ④ 按顺序执行这一轮的调用，页面变了就停（⑤ 在执行过程中被调用）
		results, err := r.runCalls(ctx, tools, resp.ToolCalls)
		if err != nil && !errors.Is(err, errBrowserGone) {
			return nil, err
		}
		if len(results) != len(resp.ToolCalls) {
			return nil, fmt.Errorf("purchase ai: runCalls returned %d results for %d calls", len(results), len(resp.ToolCalls))
		}
		for i, call := range resp.ToolCalls {
			if results[i].Snapshot {
				snapshotAt = append(snapshotAt, len(messages))
			}
			messages = append(messages, toolMessage(call.ID, results[i].Text))
		}
		// 截图放在这一轮全部工具回复的后面：assistant 调用了几个工具，紧跟着的就必须是这几条回复
		for _, res := range results {
			if res.Image != "" {
				screenshotAt = append(screenshotAt, len(messages))
				messages = append(messages, screenshotMessage(step, res.Image))
			}
		}

		// ⑥ 插件断开了：这一轮每个调用都已经有回复，对话是完整的，走收尾
		if errors.Is(err, errBrowserGone) {
			return r.finish(ctx, messages, finishBrowserGone)
		}

		if done, ok := doneResult(results); ok {
			return &agentResult{Text: done.Answer, Success: done.Success, Steps: r.steps, Followups: done.Followups}, nil
		}

		// ⑦ 连续失败
		if r.failures.record(results) {
			return r.finish(ctx, messages, finishFailures)
		}

		// ⑧ 循环检测：只看真的执行了的调用
		for i, call := range resp.ToolCalls {
			if results[i].Skipped {
				continue
			}
			switch r.loops.record(fingerprint(call, results[i].PageHash)) {
			case loopNudge:
				messages = append(messages, llm.Text(llm.RoleUser, loopNudgeText(call, r.limits.loopNudgeAt)))
			case loopStop:
				return r.finish(ctx, messages, finishLoop)
			}
		}

		// ⑨ 正好剩 warnAt 步时提醒一次。追加在最后，不改系统提示词，前缀缓存才能命中
		restSteps := r.limits.maxSteps - step - 1
		if restSteps == r.limits.warnAt {
			messages = append(messages, llm.Text(llm.RoleUser, stepWarningText(restSteps)))
		}
	}

	return r.finish(ctx, messages, finishSteps)
}

// compactSnapshots 把旧快照换成占位语，只留最新的一份（①）。
// 换的是整个 Parts，不往里面写：Parts 的底层数组和记下来的历史是共用的
func compactSnapshots(messages []llm.Message, snapshotAt []int) {
	if len(snapshotAt) <= 1 {
		return
	}

	for _, idx := range snapshotAt[:len(snapshotAt)-1] {
		messages[idx].Parts = []llm.Part{{Text: snapshotOmitted}}
	}

}

// compactScreenshots 把 Agent 自己截的旧截图换成占位语，只留最新一张，和 compactSnapshots 一样整个换掉 Parts
func compactScreenshots(messages []llm.Message, screenshotAt []int) {
	if len(screenshotAt) <= 1 {
		return
	}
	for _, idx := range screenshotAt[:len(screenshotAt)-1] {
		messages[idx].Parts = []llm.Part{{Text: screenshotOmitted}}
	}
}

// screenshotMessage 是放截图的那条用户消息：先说明这是什么，再放图
func screenshotMessage(step int, image string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Parts: []llm.Part{
		{Text: fmt.Sprintf("这是第 %d 步截的图，是页面现在的样子：", step+1)},
		{ImageURL: image},
	}}
}

// chat 调用一次模型，期限是 llmTimeout（②）。
// 整次任务还活着、只是这一次到期了，用同样的请求再试一次；其他错误直接返回
func (r *agentRun) chat(ctx context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	once := func() (*llm.ChatResp, error) {
		callCtx, cancel := context.WithTimeout(ctx, r.limits.llmTimeout)
		defer cancel()
		return r.llm.Chat(callCtx, req)
	}

	resp, err := once()
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		resp, err = once()
	}
	if err != nil {
		return nil, err
	}
	// 循环里的每一步和收尾都经过这里，思考在这里发一次就都覆盖到了
	r.emitReasoning(ctx, resp)
	return resp, nil
}

// runCalls 按顺序执行一轮的调用（④），返回和 calls 一样长、一一对应的结果。
// 页面变了、失败了、done 了，或者插件断开了，后面的调用都跳过，但也有一条回复
func (r *agentRun) runCalls(ctx context.Context, tools map[string]agentTool, calls []llm.ToolCall) ([]toolResult, error) {
	// 长度直接给满，按下标填：跳过的调用也要占住自己的位置
	toolRes := make([]toolResult, len(calls))

	// skip 就是那个标识：空字符串表示继续执行；不为空表示后面的都跳过，内容就是跳过时回复的话
	skip := ""
	// 插件断开时记下这个错误，等每个调用都有了结果再一起返回
	var gone error

	for i, call := range calls {
		if skip != "" {
			toolRes[i] = toolResult{Skipped: true, Text: skip}
			continue
		}

		// 跳过的调用不发事件，用户只看到真的做了的事；done 不是一张工具卡片，它的回答走 text
		id := fmt.Sprintf("%d-%d", r.step, i)
		shown := call.Name != toolDone
		if shown {
			r.emitToolStart(ctx, id, call)
		}

		r.callID = id
		res, err := r.runTool(ctx, tools, call)
		if errors.Is(err, errBrowserGone) {
			toolRes[i] = toolResult{Failed: true, Text: browserGoneText}
			if shown {
				r.emitToolEnd(ctx, id, toolRes[i])
			}
			skip = skippedText
			gone = err
			continue
		}
		if err != nil {
			return nil, err
		}

		toolRes[i] = res
		if shown {
			r.emitToolEnd(ctx, id, res)
		}
		switch {
		case res.Done:
			skip = skippedAfterDoneText
		case res.Failed:
			skip = skippedAfterFailText
		case res.PageChanged:
			skip = skippedText
		}
	}

	return toolRes, gone
}

// finish 是被迫停下时的最后一步（⑥）：说明原因，不给工具再问一次模型，让它把回答写出来
func (r *agentRun) finish(ctx context.Context, messages []llm.Message, reason finishReason) (*agentResult, error) {
	messages = append(messages, llm.Text(llm.RoleUser, finishPrompts[reason]))

	// 不给工具：模型只能用文字作答
	resp, err := r.chat(ctx, &llm.ChatReq{Messages: messages})
	if err != nil {
		return nil, err
	}

	return &agentResult{
		Text:      answerText(resp),
		Success:   false, // 被迫停下的，要求的事没有做完
		Finish:    reason,
		Steps:     r.steps,
		Reasoning: resp.Reasoning,
	}, nil
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

// leakedDoneText 匹配模型当成文字写出来的 done 调用里的 text 参数。
// deepseek 在没给工具、对话里却全是工具调用时（收尾就是这样），有时会照着样子把调用 done 写成它内部的
// DSML 标记，而不是直接回答：<｜｜DSML｜｜ invoke name="done"> … <｜｜DSML｜｜ parameter name="text" …>回答</｜｜DSML｜｜ parameter>
var leakedDoneText = regexp.MustCompile(`(?s)invoke name="done">.*?parameter name="text"[^>]*>(.*?)</[^<>]*parameter>`)

// answerText 是模型回复里给用户的回答：正常就是 Content；模型把 done 写成了文字时，取出里面的 text
func answerText(resp *llm.ChatResp) string {
	if strings.Contains(resp.Content, "DSML") {
		if m := leakedDoneText.FindStringSubmatch(resp.Content); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return resp.Content
}

// doneResult 找出这一轮里 done 的结果
func doneResult(results []toolResult) (toolResult, bool) {
	for _, res := range results {
		if res.Done {
			return res, true
		}
	}
	return toolResult{}, false
}
