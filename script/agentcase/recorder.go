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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/llm"
)

// recorder 包在真正的模型客户端外面，记下每次请求和回复，循环那边不知道自己被包了（概念 5）
type recorder struct {
	inner llm.Client
	calls []recordedCall
	usage llm.Usage
	// after 在每次调用记下来以后执行：实时打印、把记录写盘。运行被中途打断时，已经写下的就是全部线索
	after func(call recordedCall)
}

type recordedCall struct {
	Messages []llm.Message
	Response *llm.ChatResp `json:",omitempty"`
	Error    string        `json:",omitempty"`
	Duration time.Duration
}

func (r *recorder) Chat(ctx context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	started := time.Now()
	resp, err := r.inner.Chat(ctx, req)

	// 复制消息这一层：循环之后会把旧快照的 Parts 换成占位语，那是写进它自己数组里的消息的，
	// 不复制的话，这里存的也会跟着变成占位语，可当时模型看到的是完整快照。
	// Parts 不用复制：循环只整个换掉 Parts，不往里写
	call := recordedCall{Messages: slices.Clone(req.Messages), Response: resp, Duration: time.Since(started)}
	if err != nil {
		call.Error = err.Error()
	}
	if resp != nil {
		r.usage.PromptTokens += resp.Usage.PromptTokens
		r.usage.CompletionTokens += resp.Usage.CompletionTokens
		r.usage.TotalTokens += resp.Usage.TotalTokens
	}
	r.calls = append(r.calls, call)
	if r.after != nil {
		r.after(call)
	}
	return resp, err
}

// snapshots 取出这次运行里模型看到过的每一份快照，按出现顺序，重复的只留一份。
// 每次调用的消息都是压缩之前复制下来的，所以旧快照在当时那次调用里还是完整的
func (r *recorder) snapshots() []string {
	var out []string
	seen := map[string]bool{}
	for _, call := range r.calls {
		snapshotCalls := map[string]bool{} // 调用 snapshot 的 ToolCall.ID
		for _, msg := range call.Messages {
			for _, tc := range msg.ToolCalls {
				if tc.Name == "snapshot" {
					snapshotCalls[tc.ID] = true
				}
			}
			if msg.Role != llm.RoleTool || !snapshotCalls[msg.ToolCallID] || len(msg.Parts) == 0 {
				continue
			}
			text := msg.Parts[0].Text
			if !seen[text] {
				seen[text] = true
				out = append(out, text)
			}
		}
	}
	return out
}

// runRecord 是一次运行写到磁盘上的全部内容
type runRecord struct {
	Case     string
	Attempt  int
	Task     string
	Outcome  string
	Problems []string `json:",omitempty"`
	Error    string   `json:",omitempty"`
	Result   any      `json:",omitempty"`
	Usage    llm.Usage
	Calls    []recordedCall
}

// save 把一次运行写成 JSON：图片换成一句说明，屏蔽词替换掉，只有自己能读（0600）
func save(path string, rec runRecord, redact []string) error {
	rec.Calls = slices.Clone(rec.Calls) // rec 是值传递，但 Calls 的数组和 recorder 共用，下面要往里写
	for i := range rec.Calls {
		rec.Calls[i].Messages = withoutImages(rec.Calls[i].Messages)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // 不然 < 会变成 <，屏蔽词里有 < & 时就替换不到
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(redactText(buf.String(), redact)), 0o600)
}

// withoutImages 把图片换成一句说明：base64 又大又没法看，而且图里可能有个人信息。
// 换的是新的 Parts，不改原来的（原来的和循环里的消息共用）
func withoutImages(messages []llm.Message) []llm.Message {
	out := slices.Clone(messages)
	for i, msg := range out {
		if !slices.ContainsFunc(msg.Parts, func(p llm.Part) bool { return p.ImageURL != "" }) {
			continue
		}
		parts := make([]llm.Part, len(msg.Parts))
		for j, p := range msg.Parts {
			parts[j] = p
			if p.ImageURL != "" {
				parts[j] = llm.Part{Text: fmt.Sprintf("[图片，%d 个字符，没有保存]", len(p.ImageURL))}
			}
		}
		out[i].Parts = parts
	}
	return out
}

// redactText 把 words 里的每个词（比如账号昵称）替换成 [已屏蔽]。空词和只有空白的词跳过
func redactText(text string, words []string) string {
	for _, w := range words {
		if strings.TrimSpace(w) == "" {
			continue
		}
		text = strings.ReplaceAll(text, w, "[已屏蔽]")
	}
	return text
}

// loadRedact 读屏蔽词：一行一个，文件不存在就当没有
func loadRedact(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.ReplaceAll(string(raw), "\r", ""), "\n")
}
