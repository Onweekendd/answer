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
	"fmt"
	"os"
	"strconv"
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

// chatNotFoundText 是对话不存在、不是这个人的、或者 parent_id 对不上时给用户看的话
const chatNotFoundText = "找不到这段对话，刷新页面再试试。"

// Chat 回答对话里的一条新消息，过程和结果都从返回的 channel 里出来，最后一条是 done 或 error，然后 channel 关闭。
//
// 历史不由页面送来，而是从对话日志里取（上下文管理概念 1、2）：页面只送这一条新消息和它接在哪里（parent_id），
// 这里按 parent_id 走出那条分支，project 成模型的历史，跑完把这一轮的每条消息记进日志。
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

		chatID, _ := strconv.ParseInt(req.ChatID, 10, 64) // 校验过是数字
		path, err := pas.chatPath(ctx, chatID, req)
		if err != nil {
			if ctx.Err() == nil {
				log.Warnf("purchase ai: cannot load chat %d: %v", chatID, err)
			}
			send(Event{Type: EventError, Message: chatNotFoundText})
			return
		}

		browserOn := pas.browser.Connected()
		r := newAgentRun(pas.llmClient, pas.browser, defaultLimits)
		r.events = out
		r.showReasoning = pas.chatOptions.showReasoning
		r.redact = pas.chatOptions.redact
		r.images = pas.uploaderService
		r.draft = &draftContext{userID: req.UserID, candidates: draftCandidates(path, req.Message), store: pas.drafts}
		r.workspaces = pas.workspaces
		r.chatID = chatID
		r.userID = req.UserID
		r.rec = newChatRecorder(pas.chatLog, chatID, req.ParentID)
		defer r.closeTabs()

		messages := []llm.Message{llm.Text(llm.RoleSystem, pas.chatPrompt(browserOn))}
		messages = append(messages, project(path)...)
		userEntry := ""
		if req.Message != nil {
			user := userMessage(req.Message)
			userEntry = r.rec.record(ctx, Entry{Kind: EntryMessage, Message: storeMessage(user, messageMeta{})})
			messages = append(messages, user)
		}
		// 日志和页面里的图都是网址，模型服务商访问不到我们的服务器，发之前换成 data URI
		messages = pas.loadImages(ctx, messages)

		res, err := r.run(ctx, messages, browserOn)
		var finals []Event
		if err != nil {
			if ctx.Err() == nil {
				log.Errorf("purchase ai: chat failed, userID: %s, err: %v", req.UserID, err)
			}
			finals = []Event{{Type: EventError, Message: chatFailedText}}
		} else {
			if res.Finish != "" {
				log.Infof("purchase ai: chat finished early (%s) after %d tool calls", res.Finish, len(res.Steps))
			}
			finals = r.answerEvents(res)
		}

		// 先把这一轮记完（最后一条是 view：页面上看到的样子，打开历史对话时重放），再告诉页面条目的 id，
		// 页面下一条消息拿 leaf 当 parent_id。顺序不能反：页面拿到 id 马上就可能发下一条
		r.shown = append(r.shown, finals...)
		r.rec.record(ctx, Entry{Kind: EntryView, View: &StoredView{V: storedVersion, Events: r.shown}})
		if err := r.rec.close(ctx); err != nil {
			log.Errorf("purchase ai: cannot save chat %d: %v", chatID, err)
		} else {
			send(Event{Type: EventSaved, Saved: &SavedIDs{User: userEntry, Leaf: r.rec.leaf()}})
		}
		for _, ev := range finals {
			send(ev)
		}
		if err == nil {
			send(Event{Type: EventDone})
		}
	}()
	return out
}

// chatPath 取出这个对话、按 parent_id 走出这一轮接着的那条分支。
// 没有新消息就是重新生成：parent_id 必须是一条用户消息，在它下面重跑一遍
func (pas *PurchaseAIService) chatPath(ctx context.Context, chatID int64, req *schema.PurchaseAIChatReq) ([]Entry, error) {
	entries, found, err := pas.chatLog.Entries(ctx, chatID, req.UserID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("chat %d is not the user's", chatID)
	}
	path, err := pathTo(entries, req.ParentID)
	if err != nil {
		return nil, err
	}
	if req.Message == nil {
		if n := len(path); n == 0 || path[n-1].Message == nil || path[n-1].Message.Role != llm.RoleUser {
			return nil, fmt.Errorf("regenerate needs a user message, parent is %q", req.ParentID)
		}
	}
	return path, nil
}

// chatPrompt 是对话的系统提示词：每轮现做（里面有今天的日期），不存进日志
func (pas *PurchaseAIService) chatPrompt(browserOn bool) string {
	prompt := buildSystemPrompt(time.Now(), browserOn)
	if pas.workspaces != nil {
		prompt += "\n\n" + workspaceRules
	}
	return prompt
}

// userMessage 是用户这一条消息：第一段是用户的话（pageMessages 按这个顺序取回给页面），后面是截图。
// 图是网址，记进日志的就是网址；发给模型前 loadImages 再换成 data URI
func userMessage(m *schema.PurchaseAIChatMessage) llm.Message {
	parts := []llm.Part{{Text: m.Text}}
	if len(m.Images) > 0 {
		parts = append(parts, llm.Part{Text: screenshotsHeader})
		for _, url := range m.Images {
			parts = append(parts, llm.Part{ImageURL: url})
		}
		// 网址也给模型：整理帖子草稿时，配图要填网址
		parts = append(parts, llm.Part{Text: "这些截图的网址依次是：" + strings.Join(m.Images, " ")})
	}
	return llm.Message{Role: llm.RoleUser, Parts: parts}
}

// loadImages 把消息里的网址图片换成 data URI。换不了的（文件没了）换成一句话，别让整段对话发不出去。
// 只换新的切片里的 Parts，不改传进来的消息：它们的 Parts 可能和别处共用
func (pas *PurchaseAIService) loadImages(ctx context.Context, messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	for i, m := range messages {
		out[i] = m
		if !hasURLImage(m.Parts) {
			continue
		}
		parts := make([]llm.Part, 0, len(m.Parts))
		for _, p := range m.Parts {
			if p.ImageURL == "" || strings.HasPrefix(p.ImageURL, "data:") {
				parts = append(parts, p)
				continue
			}
			if loaded := pas.imageParts(ctx, []string{p.ImageURL}); len(loaded) > 0 {
				parts = append(parts, loaded[0])
			} else {
				parts = append(parts, llm.Part{Text: imageOmitted})
			}
		}
		out[i].Parts = parts
	}
	return out
}

func hasURLImage(parts []llm.Part) bool {
	for _, p := range parts {
		if p.ImageURL != "" && !strings.HasPrefix(p.ImageURL, "data:") {
			return true
		}
	}
	return false
}

// answerEvents 是一轮最后推给页面的回答和后续建议。
// 回答是模型写的，也可能复述页面内容，和思考一样过一遍打码和屏蔽词；
// 图片在打码之后放进去：网址里的数字不能被当成手机号；
// 后续建议先切出来单独发：页面上是按钮，不是回答的一部分
func (r *agentRun) answerEvents(res *agentResult) []Event {
	answer, followups := splitFollowups(res.Text)
	if len(res.Followups) > 0 {
		followups = cleanFollowups(append(res.Followups, followups...))
	}
	events := []Event{{Type: EventText, Delta: withShots(r.view(fixNumberRanges(answer)), res.Shots)}}
	if len(followups) > 0 {
		for i, f := range followups {
			followups[i] = r.view(f)
		}
		events = append(events, Event{Type: EventFollowups, Followups: followups})
	}
	return events
}
