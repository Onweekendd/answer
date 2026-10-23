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
	"strings"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// Config 是一个 Agent 的配置。入口（Chat、RunTask）各自拼一个
type Config struct {
	Model llm.Client

	// MaxSteps 是步数上限：一步 = 调用一次模型。到了就走收尾。
	// 这条是核心自己管的，因为循环要知道什么时候停；提醒「还剩几步」是插件的事（Turn.Left）
	MaxSteps  int
	MaxTokens int // 每次调用模型能输出多少。0 就用 llm 包的默认
	Timeouts  Timeouts

	// Plugins 的顺序有意义：工具发给模型的顺序、EndOfTurn 的评估顺序，都是注册顺序
	Plugins []Plugin

	Recorder Recorder // 把每条消息记下来；nil 就不记
	Observer Observer // 把过程告诉外面；nil 就没人听
}

// Timeouts 是核心管的两个期限。调一次工具的期限归工具自己（browsertool 的 TaskState.Timeout）
type Timeouts struct {
	Run   time.Duration // 整次任务
	Model time.Duration // 每次调用模型
}

// Recorder 是核心对「把消息记进对话日志」的全部要求：每往对话里加一条消息，就告诉它一下。
// 核心不认识对话日志。根包的 chatRecorder 来实现它
type Recorder interface {
	Keep(ctx context.Context, m llm.Message, meta Meta)
}

// Meta 是随消息记下来的附加信息
type Meta struct {
	PromptTokens int  // 发出这条之前，这次请求的输入有多少 token（assistant 消息才有）
	Synthetic    bool // 不是模型或用户写的，是系统追加的提醒、截图

	// 以下在工具回复上
	ToolName string
	Summary  string // 这个调用写成的一句人话；被跳过的、done 没有
	Failed   bool
	Display  string
	// Extras 在工具回复上是工具结果的 Extras，在最终回答那一条上是 Answer.Extras
	Extras map[string]any

	// Final 标出带着这一轮回答的那一条：没有它就分不清失败的、被跳过的、做成了的调用谁带着回答
	Final bool
}

// Observer 是核心对「把过程告诉外面」的全部要求。根包的 Chat 来实现它，推给页面
type Observer interface {
	// Describe 把一个调用写成人话，在执行之前问。被跳过的调用不问，也不发事件
	Describe(call llm.ToolCall) string
	Reasoning(ctx context.Context, text string)
	ToolStart(ctx context.Context, id string, call llm.ToolCall, summary string)
	ToolEnd(ctx context.Context, id string, call llm.ToolCall, res tool.Result)
}

// Result 是一次任务的结果
type Result struct {
	Text      string // 给用户的回答，已经过 OnAnswer 钩子
	Success   bool   // done 说做成了；直接回答也算；走了收尾就是 false
	Finish    Reason // 为什么被迫结束的；空表示正常结束
	Steps     []Step
	Reasoning string // 最后一次模型调用的思考，只用于回答为空时排查
	// Extras 是 OnAnswer 钩子放进来的产出（存下来的图、后续建议……），和记进日志的那份一样
	Extras map[string]any
}

// Step 记录执行过的一次工具调用，用于排查问题和 case 判定。不记 Text：快照里可能有昵称、地址
type Step struct {
	Step      int
	Tool      string
	Arguments string
	Failed    bool
	URL       string
	Duration  time.Duration
}

// Agent 是一个装配好的 Agent。一个 Agent 只跑一次任务：插件的状态在闭包里，跑完就扔
type Agent struct {
	cfg    Config
	reg    *Registry
	byName map[string]tool.Spec
	defs   []llm.Tool // 发给模型的工具定义，按注册顺序
}

// New 装配插件，整理出工具索引和发给模型的定义。工具重名、参数生成不出 JSON Schema 这类错误在这里报
func New(cfg Config) (*Agent, error) {
	reg, err := build(cfg.Plugins)
	if err != nil {
		return nil, err
	}
	a := &Agent{cfg: cfg, reg: reg, byName: make(map[string]tool.Spec, len(reg.tools))}
	for _, spec := range reg.tools {
		schema, err := llm.SchemaFor(spec.Args)
		if err != nil {
			return nil, err
		}
		a.byName[spec.Name] = spec
		a.defs = append(a.defs, llm.Tool{Name: spec.Name, Description: spec.Description, Parameters: schema})
	}
	return a, nil
}

// Tools 是装配好的工具，按注册顺序，也就是发给模型的顺序。给测试和排查用
func (a *Agent) Tools() []tool.Spec { return append([]tool.Spec(nil), a.reg.tools...) }

// Defs 是发给模型的工具定义，按注册顺序
func (a *Agent) Defs() []llm.Tool { return append([]llm.Tool(nil), a.defs...) }

// hasDone 说这次任务有没有 done：有的话，模型回了文字要提醒它调用 done；没有，直接回答就是答案
func (a *Agent) hasDone() bool {
	_, ok := a.byName[DoneName]
	return ok
}

// chat 调用一次模型，期限是 Timeouts.Model。
// 整次任务还活着、只是这一次到期了，用同样的请求再试一次；其他错误直接返回
func (a *Agent) chat(ctx context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	if req.MaxTokens == 0 {
		req.MaxTokens = a.cfg.MaxTokens
	}
	once := func() (*llm.ChatResp, error) {
		callCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeouts.Model)
		defer cancel()
		return a.cfg.Model.Chat(callCtx, req)
	}

	resp, err := once()
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		resp, err = once()
	}
	if err != nil {
		return nil, err
	}
	// 循环里的每一步和收尾都经过这里，思考在这里发一次就都覆盖到了
	if a.cfg.Observer != nil {
		if text := strings.TrimSpace(resp.Reasoning); text != "" {
			a.cfg.Observer.Reasoning(ctx, text)
		}
	}
	return resp, nil
}

// forModel 是这一次要发给模型的消息：把历史交给各个 BeforeModel 钩子依次处理。
// 钩子拿到的是浅拷贝，换掉一条消息的 Parts 不会伤到历史
func (a *Agent) forModel(messages []llm.Message) []llm.Message {
	out := append([]llm.Message(nil), messages...)
	for _, fn := range a.reg.beforeModel {
		out = fn(out)
	}
	return out
}

// keep 把一条消息接到对话后面，有记录器时同时记下。
// 循环里所有往 messages 里加消息的地方都走这里，日志才和模型实际看到的一致
func (a *Agent) keep(ctx context.Context, messages []llm.Message, m llm.Message, meta Meta) []llm.Message {
	if a.cfg.Recorder != nil {
		a.cfg.Recorder.Keep(ctx, m, meta)
	}
	return append(messages, m)
}
