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

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// 这个文件是插件系统的接口，照 pi 的 ExtensionAPI 来
// （pi/packages/coding-agent/src/core/extensions/types.ts 第 1592 行起），只取 5 个钩子。
// 每个钩子的注释写了它在循环里的哪个位置被调用、谁在用。

// Plugin 是一个插件：一个函数，核心把注册器交给它，它想要什么就自己注册。
// 状态放在函数的闭包里（见 doc.go）
type Plugin func(*Registry)

// Registry 是核心交给插件的注册器。核心保存所有插件注册的东西，循环在固定的时机取出来调用。
// 同一个钩子上，先注册的先被调用
type Registry struct {
	tools       []tool.Spec
	afterTool   []func(ctx context.Context, call llm.ToolCall, res *tool.Result)
	beforeModel []func(msgs []llm.Message) []llm.Message
	onAnswer    []func(ctx context.Context, a *Answer)
	endOfTurn   []func(t Turn) Verdict

	errs []error // 装配错误（工具重名……）。注册方法没法返回 error，攒在这里，由 New 一起报
}

// Tool 注册一个工具。注册顺序就是发给模型的顺序（前缀缓存靠它稳定）。
// 重名是装配错了：记下错误，New 会返回
//
// 不给模型的工具怎么办：入口不装对应的插件就行（没连插件的任务不装 browser），不需要「这个工具要什么才能用」的枚举。
// done 是 Done 插件注册的，放在最后
func (r *Registry) Tool(spec tool.Spec) {
	for _, t := range r.tools {
		if t.Name == spec.Name {
			r.errs = append(r.errs, fmt.Errorf("agent: tool %q is registered twice", spec.Name))
			return
		}
	}
	r.tools = append(r.tools, spec)
}

// AfterTool 在一个工具执行完、结果还没交给模型之前调用，每个执行了的调用一次（被跳过的不调）。
// 插件可以读，也可以改 res（pi 的 afterToolCall 一样）。
//
// 谁用：浏览器插件（记下带快照的调用 id）；链接插件（有 URL 的成功结果记下来）
func (r *Registry) AfterTool(fn func(ctx context.Context, call llm.ToolCall, res *tool.Result)) {
	r.afterTool = append(r.afterTool, fn)
}

// BeforeModel 在每次请求模型前调用，返回这一次要发出去的消息。
// 返回值只用于这一次请求，核心不把它写回历史，也不进日志；
// 多个插件：上一个的返回值是下一个的输入。
//
// 参数是消息列表的浅拷贝：换掉一条消息的 Parts 是安全的，往一条消息的 Parts 里写不是
// （Parts 的底层数组和历史共用）。
//
// 谁用：浏览器插件，只留最新一份快照、最新一张截图。
// 怎么找到旧快照：用 ToolCallID（AfterTool 里记下）。截图消息没有 ToolCallID，
// 核心追加它时打上 Tag（TagScreenshot），插件按 Tag 找。
func (r *Registry) BeforeModel(fn func(msgs []llm.Message) []llm.Message) {
	r.beforeModel = append(r.beforeModel, fn)
}

// OnAnswer 在确定「这是最终回答」的那一刻调用，记进日志之前。插件可以改文字、往 Extras 里放产出。
// 来源有三个：模型直接回答、调用 done、被迫收尾后的回答。
//
// 为什么在记日志之前：日志只追加，记了就改不了。Extras 和回答一起记进日志，打开历史对话时直接读出来。
// 没有 error 返回：存图失败回答照给。
// 谁用：图片插件（存回答 [图N] 引用的图，Extras["shots"]）；followups 插件（切块、清洗，Extras["followups"]）
func (r *Registry) OnAnswer(fn func(ctx context.Context, a *Answer)) {
	r.onAnswer = append(r.onAnswer, fn)
}

// EndOfTurn 在一轮的工具都执行完、回复和截图都记进消息之后调用，返回要追加的提醒，或者「走收尾」。
// 任务已经结束（Terminate）和工具说不能继续（tool.AbortError）的轮次不会走到这里。
//
// 谁用：guard 包的 Failures、Loops、Warn（剩几步提醒），交付插件的「剩 10 步提醒做成果」。
// 步数上限本身是核心管的（Config.MaxSteps），因为循环要知道什么时候停。
//
// 多个插件的叠加规则：按注册顺序调用，每个返回的提醒都追加；第一个返回 Finish 的赢，后面的不再调用
// （连续失败收尾时不会再发循环提醒和步数提醒）。所以装配列表的顺序是规则，guard 的测试把它固定住。
func (r *Registry) EndOfTurn(fn func(t Turn) Verdict) {
	r.endOfTurn = append(r.endOfTurn, fn)
}

// ───────────────────────────── 钩子的参数和返回值 ─────────────────────────────

// Answer 是最终回答。插件直接改它
type Answer struct {
	Text   string         // 给用户的回答。插件可以改（followups 在这里切掉标签）
	Extras map[string]any // 插件附带的产出，和回答一起记进日志、交给页面
}

// Turn 是一轮发生的事实，EndOfTurn 据此判断
type Turn struct {
	Step    int            // 第几步，从 0 开始
	Left    int            // 还剩几步（上限由核心配置，见 Config.MaxSteps）
	Calls   []llm.ToolCall // 这一轮模型要的调用
	Results []Outcome      // 和 Calls 一一对应
}

// Outcome 是一个调用的结果。被跳过的（前面的动作改变了页面、失败了、done 了）也在里面
type Outcome struct {
	tool.Result
	Skipped bool
	Summary string // 这个调用写成的一句人话（Observer.Describe 给的），被跳过的没有
}

// Verdict 是 EndOfTurn 的决定
type Verdict struct {
	// Messages 是要追加到对话里的提醒，核心把它们标成 Synthetic 记进日志
	Messages []llm.Message
	// Finish 非 nil 就是走收尾，不再继续
	Finish *Finish
}

// Finish 是被迫收尾的原因和收尾时追加给模型的话。
// 话由要求收尾的插件自己带（guard.Failures 带「连续失败……」），不集中放在核心里
type Finish struct {
	Reason Reason
	Prompt string
}

// Reason 是走到收尾的原因。核心自己只用下面几个，插件和工具（tool.AbortError）可以有自己的
type Reason string

const (
	ReasonSteps Reason = "steps" // 步数用完了
	ReasonEmpty Reason = "empty" // 提醒过以后模型还是回了空回复
)

// build 把所有插件装配进注册器，返回装配错误
func build(plugins []Plugin) (*Registry, error) {
	r := &Registry{}
	for _, p := range plugins {
		p(r)
	}
	return r, errors.Join(r.errs...)
}
