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
	"os"
	"strings"
	"time"

	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/segmentfault/pacman/log"
)

// eventBuffer 是事件 channel 的缓冲：网络慢一点时 Agent 不用等每一次 Flush（概念 2）
const eventBuffer = 16

// chatFailedText 是出错时给用户看的话。不发 err.Error()：里面可能有模型服务商的地址、内部信息
const chatFailedText = "这次没能完成，请稍后再试。"

// chatOptions 是对话的配置，从环境变量读：
//   - PURCHASE_AI_SHOW_REASONING=false 不把思考推给用户（共享浏览器上线时按情况关掉）
//   - PURCHASE_AI_REDACT_FILE 屏蔽词文件，一行一个，比如共享浏览器登录的那个账号的昵称
type chatOptions struct {
	showReasoning bool
	redact        []string
}

func chatOptionsFromEnv() chatOptions {
	opts := chatOptions{showReasoning: os.Getenv("PURCHASE_AI_SHOW_REASONING") != "false"}
	if path := os.Getenv("PURCHASE_AI_REDACT_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			// 配了却读不到，说明配置错了：思考里可能会漏出要屏蔽的词，宁可不推思考
			log.Errorf("purchase ai: cannot read PURCHASE_AI_REDACT_FILE, reasoning is not shown: %v", err)
			opts.showReasoning = false
		}
		opts.redact = strings.Split(strings.ReplaceAll(string(raw), "\r", ""), "\n")
	}
	return opts
}

// Chat 回答对话里的最新一条消息，过程和结果都从返回的 channel 里出来，最后一条是 done 或 error，然后 channel 关闭。
//
// channel 由这里开的 goroutine 发送、也由它关闭，调用方只管收（概念 2 规矩 1）。
// ctx 一般是请求的 context：用户关掉页面、handler 返回时它被取消，Agent 停下、关掉开过的标签页，
// 每次发送也都和 ctx.Done() 一起等，所以没人收的时候不会卡住（规矩 2）
func (pas *PurchaseAIService) Chat(ctx context.Context, req *schema.PurchaseAIChatReq) <-chan Event {
	out := make(chan Event, eventBuffer)
	send := func(ev Event) {
		select {
		case out <- ev:
		case <-ctx.Done():
		}
	}

	go func() {
		defer close(out)
		// 这个 goroutine 里的 panic 不归 gin 的 recovery 管，不接住整个进程就退出了
		defer func() {
			if p := recover(); p != nil {
				log.Errorf("purchase ai: chat panicked: %v", p)
				send(Event{Type: EventError, Message: chatFailedText})
			}
		}()

		browserOn := pas.browser.Connected()
		messages := pas.chatMessages(ctx, req, browserOn)

		r := newAgentRun(pas.llmClient, pas.browser, defaultLimits)
		r.events = out
		r.showReasoning = pas.chatOptions.showReasoning
		r.redact = pas.chatOptions.redact
		defer r.closeTabs()

		res, err := r.run(ctx, messages, browserOn)
		if err != nil {
			if ctx.Err() == nil {
				log.Errorf("purchase ai: chat failed, userID: %s, err: %v", req.UserID, err)
			}
			send(Event{Type: EventError, Message: chatFailedText})
			return
		}
		if res.Finish != "" {
			log.Infof("purchase ai: chat finished early (%s) after %d tool calls", res.Finish, len(res.Steps))
		}
		// 回答是模型写的，也可能复述页面内容，和思考一样过一遍打码和屏蔽词
		send(Event{Type: EventText, Delta: r.view(fixNumberRanges(res.Text))})
		send(Event{Type: EventDone})
	}()
	return out
}

// chatMessages 把前端发来的对话变成模型的消息：系统提示词在最前面，用户消息带上截图
func (pas *PurchaseAIService) chatMessages(ctx context.Context, req *schema.PurchaseAIChatReq, browserOn bool) []llm.Message {
	messages := []llm.Message{llm.Text(llm.RoleSystem, buildSystemPrompt(time.Now(), browserOn))}
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			messages = append(messages, llm.Text(llm.RoleAssistant, m.Text))
			continue
		}
		parts := []llm.Part{{Text: m.Text}}
		if images := pas.imageParts(ctx, m.Images); len(images) > 0 {
			parts = append(parts, llm.Part{Text: screenshotsHeader})
			parts = append(parts, images...)
		}
		messages = append(messages, llm.Message{Role: llm.RoleUser, Parts: parts})
	}
	return messages
}
