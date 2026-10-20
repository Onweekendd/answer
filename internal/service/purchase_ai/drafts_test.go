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
	"encoding/json"
	"errors"
	"testing"

	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDrafts 把草稿记在 map 里
type fakeDrafts struct {
	saved map[string]*Draft
	err   error
}

func (f *fakeDrafts) SaveDraft(_ context.Context, id string, d *Draft) error {
	if f.err != nil {
		return f.err
	}
	f.saved[id] = d
	return nil
}

func (f *fakeDrafts) GetDraft(_ context.Context, id string) (*Draft, bool, error) {
	d, ok := f.saved[id]
	return d, ok, nil
}

func (f *fakeDrafts) DeleteDraft(_ context.Context, id string) error {
	delete(f.saved, id)
	return nil
}

const (
	upload1 = "http://site/uploads/post/a.jpg"
	shot1   = "http://site/uploads/agent/b.jpg"
)

func draftRun(store DraftStore) (*agentRun, chan Event) {
	r := newToolRun(newFakeBrowser())
	r.draft = &draftContext{userID: "7", candidates: []string{upload1, shot1}, store: store}
	r.callID = "3-0"
	events := make(chan Event, 4)
	r.events = events
	return r, events
}

func Test_draftPost(t *testing.T) {
	t.Run("整理好草稿，挂起交给页面：AI 的那一票只记在服务端", func(t *testing.T) {
		// Arrange
		store := &fakeDrafts{saved: map[string]*Draft{}}
		r, events := draftRun(store)

		// Act
		res, err := r.draftPost(context.Background(), `{"title":"iPad Air M3 现在买还是等双 11？",
			"content":"预算 4000，主要记笔记。京东自营 4299。","images":["`+shot1+`","http://evil/x.jpg"],
			"price":4299,"link":"https://item.jd.com/100.html?spm=x","category":"数码","vote":"wait"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.True(t, res.Done)
		assert.Equal(t, draftAnswer, res.Answer)
		ev := <-events
		assert.Equal(t, EventToolSuspend, ev.Type)
		assert.Equal(t, "3-0", ev.ID)
		assert.Equal(t, toolDraft, ev.Name)
		in := ev.Input
		require.NotNil(t, in)
		assert.Equal(t, []string{shot1}, in.Images, "对话里没出现过的图不要")
		assert.Equal(t, []string{upload1, shot1}, in.Candidates)
		assert.Equal(t, "https://item.jd.com/100.html", in.Link)
		assert.Equal(t, entity.PurchaseVoteWait, in.Vote)
		assert.Equal(t, &Draft{UserID: "7", Vote: entity.PurchaseVoteWait}, store.saved[in.DraftID])
	})

	t.Run("一张图都没挑对时用对话里的前几张", func(t *testing.T) {
		// Arrange
		r, events := draftRun(&fakeDrafts{saved: map[string]*Draft{}})

		// Act
		_, _ = r.draftPost(context.Background(), `{"title":"这个显示器值不值","content":"x","images":["nope"],"vote":"buy"}`)

		// Assert
		assert.Equal(t, []string{upload1, shot1}, (<-events).Input.Images)
	})

	t.Run("标题太短、vote 不对时让模型改", func(t *testing.T) {
		for _, args := range []string{
			`{"title":"买吗","content":"x","vote":"buy"}`,
			`{"title":"这个显示器值不值","content":"x","vote":"maybe"}`,
		} {
			// Arrange
			store := &fakeDrafts{saved: map[string]*Draft{}}
			r, _ := draftRun(store)

			// Act
			res, _ := r.draftPost(context.Background(), args)

			// Assert
			assert.True(t, res.Failed, args)
			assert.Empty(t, store.saved)
		}
	})

	t.Run("存不了草稿就不挂起", func(t *testing.T) {
		// Arrange
		r, events := draftRun(&fakeDrafts{err: errors.New("cache down")})

		// Act
		res, _ := r.draftPost(context.Background(), `{"title":"这个显示器值不值","content":"x","vote":"buy"}`)

		// Assert
		assert.True(t, res.Failed)
		assert.False(t, res.Done)
		assert.Empty(t, events)
	})
}

func Test_draftCandidates(t *testing.T) {
	// Act
	path := []Entry{
		msgEntry("u1", "", userMessage(&schema.PurchaseAIChatMessage{Text: "值不值", Images: []string{upload1}}), messageMeta{}),
		viewEntry("v1", "u1", Event{Type: EventText, Delta: "看图\n\n![图1](" + shot1 + ")\n*图1：价格*\n别的站的图 ![x](https://cdn.example.com/a.jpg)"}),
	}
	got := draftCandidates(path, &schema.PurchaseAIChatMessage{Text: "帮我发到社区", Images: []string{upload1}})

	// Assert
	assert.Equal(t, []string{upload1, shot1}, got)
}

func Test_tools_draft(t *testing.T) {
	t.Run("插件没连着时只有 draft_post", func(t *testing.T) {
		// Arrange
		r, _ := draftRun(&fakeDrafts{saved: map[string]*Draft{}})

		// Act
		_, defs, err := r.tools(false)

		// Assert
		require.NoError(t, err)
		require.Len(t, defs, 1)
		assert.Equal(t, toolDraft, defs[0].Name)
	})

	t.Run("帖子分析没有 draft_post", func(t *testing.T) {
		// Act
		tools, _, err := newToolRun(newFakeBrowser()).tools(true)

		// Assert
		require.NoError(t, err)
		assert.NotContains(t, tools, toolDraft)
	})
}

func Test_run_draftOffline(t *testing.T) {
	t.Run("插件没连着、用户要发帖：调用 draft_post 以后就结束，回答是固定的那句", func(t *testing.T) {
		// Arrange
		args, _ := json.Marshal(map[string]any{"title": "这个显示器值不值", "content": "x", "vote": "skip"})
		model := &fakeLLM{replies: []*llm.ChatResp{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: toolDraft, Arguments: string(args)}}}}}
		r := newAgentRun(model, newFakeBrowser(), testLimits())
		r.draft = &draftContext{userID: "7", store: &fakeDrafts{saved: map[string]*Draft{}}}

		// Act
		res, err := r.run(context.Background(), []llm.Message{llm.Text(llm.RoleUser, "帮我发到社区")}, false)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, draftAnswer, res.Text)
		assert.Len(t, model.reqs, 1)
	})

	t.Run("插件没连着、普通提问：直接回答就是答案，不催它调用 done", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: "价格偏高"}}}
		r := newAgentRun(model, newFakeBrowser(), testLimits())
		r.draft = &draftContext{userID: "7", store: &fakeDrafts{saved: map[string]*Draft{}}}

		// Act
		res, err := r.run(context.Background(), []llm.Message{llm.Text(llm.RoleUser, "值不值")}, false)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "价格偏高", res.Text)
		assert.Len(t, model.reqs, 1)
	})
}

func Test_draftPost_noPictures(t *testing.T) {
	// Arrange：对话里一张图都没有
	r, events := draftRun(&fakeDrafts{saved: map[string]*Draft{}})
	r.draft.candidates = draftCandidates(nil, nil)

	// Act
	_, _ = r.draftPost(context.Background(), `{"title":"这个显示器值不值","content":"x","vote":"buy"}`)

	// Assert：编成 JSON 是 []，不是 null，页面才认得这份草稿
	raw, err := json.Marshal((<-events).Input)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"images":[]`)
	assert.Contains(t, string(raw), `"candidates":[]`)
}
