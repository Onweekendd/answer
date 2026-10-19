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
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/require"
)

// stubLLM 按顺序给出回复
type stubLLM struct {
	replies []*llm.ChatResp
	err     error
}

func (s *stubLLM) Chat(context.Context, *llm.ChatReq) (*llm.ChatResp, error) {
	if s.err != nil {
		return nil, s.err
	}
	resp := s.replies[0]
	s.replies = s.replies[1:]
	return resp, nil
}

func Test_recorder(t *testing.T) {
	t.Run("循环之后把快照换成占位语，记录里还是当时发出去的完整快照", func(t *testing.T) {
		// Arrange
		rec := &recorder{inner: &stubLLM{replies: []*llm.ChatResp{{Content: "ok"}}}}
		messages := []llm.Message{llm.Text(llm.RoleUser, "任务"), llm.Text(llm.RoleTool, "[0]<a>完整快照")}

		// Act：像 compactSnapshots 那样，在调用之后整个换掉 Parts
		_, err := rec.Chat(context.Background(), &llm.ChatReq{Messages: messages})
		messages[1].Parts = []llm.Part{{Text: "（旧快照已省略）"}}

		// Assert
		require.NoError(t, err)
		require.Equal(t, "[0]<a>完整快照", rec.calls[0].Messages[1].Parts[0].Text)
	})

	t.Run("每次回复的 token 累加起来", func(t *testing.T) {
		// Arrange
		rec := &recorder{inner: &stubLLM{replies: []*llm.ChatResp{
			{Usage: llm.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}},
			{Usage: llm.Usage{PromptTokens: 200, CompletionTokens: 20, TotalTokens: 220}},
		}}}

		// Act
		for range 2 {
			_, _ = rec.Chat(context.Background(), &llm.ChatReq{})
		}

		// Assert
		require.Equal(t, llm.Usage{PromptTokens: 300, CompletionTokens: 30, TotalTokens: 330}, rec.usage)
		require.Len(t, rec.calls, 2)
	})

	t.Run("出错的调用也记下来，错误原样返回给循环", func(t *testing.T) {
		// Arrange
		boom := errors.New("boom")
		rec := &recorder{inner: &stubLLM{err: boom}}

		// Act
		_, err := rec.Chat(context.Background(), &llm.ChatReq{})

		// Assert
		require.ErrorIs(t, err, boom)
		require.Equal(t, "boom", rec.calls[0].Error)
	})
}

func Test_recorder_snapshots(t *testing.T) {
	t.Run("从每次调用的消息里取出快照，压缩掉的旧快照用当时那次调用里的完整版，重复的只留一份", func(t *testing.T) {
		// Arrange：第二次调用时，第一份快照已经被压缩成占位语
		ask := func(id string) llm.Message {
			msg := llm.Text(llm.RoleAssistant, "")
			msg.ToolCalls = []llm.ToolCall{{ID: id, Name: "snapshot"}}
			return msg
		}
		reply := func(id, text string) llm.Message {
			msg := llm.Text(llm.RoleTool, text)
			msg.ToolCallID = id
			return msg
		}
		click := llm.Text(llm.RoleAssistant, "")
		click.ToolCalls = []llm.ToolCall{{ID: "c1", Name: "click"}}
		rec := &recorder{calls: []recordedCall{
			{Messages: []llm.Message{ask("s1"), reply("s1", "第一页")}},
			{Messages: []llm.Message{ask("s1"), reply("s1", "（旧快照已省略）"), click, reply("c1", "已点击 [3]"), ask("s2"), reply("s2", "第二页")}},
		}}

		// Act
		got := rec.snapshots()

		// Assert：点击的回复不算快照
		require.Equal(t, []string{"第一页", "（旧快照已省略）", "第二页"}, got)
	})
}

func Test_withoutImages(t *testing.T) {
	t.Run("图片换成说明，原来的消息不变", func(t *testing.T) {
		// Arrange
		messages := []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{{Text: "看图"}, {ImageURL: "data:image/jpeg;base64,AAAA"}}}}

		// Act
		out := withoutImages(messages)

		// Assert
		require.Equal(t, "看图", out[0].Parts[0].Text)
		require.Empty(t, out[0].Parts[1].ImageURL)
		require.Contains(t, out[0].Parts[1].Text, "图片")
		require.Equal(t, "data:image/jpeg;base64,AAAA", messages[0].Parts[1].ImageURL)
	})
}

func Test_save(t *testing.T) {
	t.Run("写出的文件里没有屏蔽词、没有图片数据", func(t *testing.T) {
		// Arrange
		path := filepath.Join(t.TempDir(), "run", "京东搜索-1.json")
		rec := runRecord{
			Case: "京东搜索",
			Calls: []recordedCall{{Messages: []llm.Message{
				llm.Text(llm.RoleTool, "[3]<a>你好，小明<同学> 退出"),
				{Role: llm.RoleUser, Parts: []llm.Part{{ImageURL: "data:image/png;base64,SECRETPIXELS"}}},
			}}},
		}

		// Act
		err := save(path, rec, []string{"小明<同学>", "", " "})

		// Assert
		require.NoError(t, err)
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "小明")
		require.Contains(t, string(raw), "[已屏蔽]")
		require.NotContains(t, string(raw), "SECRETPIXELS")
		require.Equal(t, "data:image/png;base64,SECRETPIXELS", rec.Calls[0].Messages[1].Parts[0].ImageURL)
	})
}
