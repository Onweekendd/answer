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
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/require"
)

// hangingModel 前 hang 次调用一直卡到 ctx 结束，像一个不响应的服务商
type hangingModel struct {
	replies []*llm.ChatResp
	err     error
	hang    int
	reqs    []llm.ChatReq
}

func (f *hangingModel) Chat(ctx context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	snapshot := *req
	snapshot.Messages = append([]llm.Message(nil), req.Messages...)
	f.reqs = append(f.reqs, snapshot)

	if f.hang > 0 {
		f.hang--
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	if len(f.replies) == 0 {
		return nil, errors.New("no scripted reply left")
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	return reply, nil
}

func TestAgent_chat(t *testing.T) {
	newAgent := func(model llm.Client) *Agent {
		return &Agent{cfg: Config{Model: model, Timeouts: Timeouts{Run: time.Minute, Model: 50 * time.Millisecond}}, reg: &Registry{}}
	}
	req := &llm.ChatReq{Messages: start()}
	// 整次任务的 ctx 给 2 秒：没写好单次期限时，测试 2 秒后失败，不会一直卡住
	alive := func(t *testing.T) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		t.Cleanup(cancel)
		return ctx
	}

	t.Run("成功就直接返回，只调用一次", func(t *testing.T) {
		// Arrange
		model := &hangingModel{replies: []*llm.ChatResp{{Content: "好"}}}

		// Act
		resp, err := newAgent(model).chat(context.Background(), req)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "好", resp.Content)
		require.Len(t, model.reqs, 1)
	})

	t.Run("这一次卡住了，用同样的请求重试一次", func(t *testing.T) {
		// Arrange
		model := &hangingModel{hang: 1, replies: []*llm.ChatResp{{Content: "第二次成功"}}}

		// Act
		resp, err := newAgent(model).chat(alive(t), req)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "第二次成功", resp.Content)
		require.Len(t, model.reqs, 2)
		require.Equal(t, model.reqs[0].Messages, model.reqs[1].Messages)
	})

	t.Run("每次调用都有自己的期限，不会一直等下去", func(t *testing.T) {
		// Arrange
		model := &hangingModel{hang: 2}
		started := time.Now()

		// Act
		_, err := newAgent(model).chat(alive(t), req)

		// Assert：两次各 50ms，远远不到测试的超时
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Len(t, model.reqs, 2)
		require.Less(t, time.Since(started), time.Second)
	})

	t.Run("整次任务的 ctx 结束了就不重试", func(t *testing.T) {
		// Arrange
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		model := &hangingModel{hang: 1, replies: []*llm.ChatResp{{Content: "不该走到这里"}}}

		// Act
		_, err := newAgent(model).chat(ctx, req)

		// Assert
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, model.reqs, 1)
	})

	t.Run("服务商报了别的错不重试", func(t *testing.T) {
		// Arrange
		model := &hangingModel{err: errors.New("401 invalid key")}

		// Act
		_, err := newAgent(model).chat(context.Background(), req)

		// Assert
		require.EqualError(t, err, "401 invalid key")
		require.Len(t, model.reqs, 1)
	})

	t.Run("思考发给观察者", func(t *testing.T) {
		// Arrange
		obs := &fakeObserver{}
		a := newAgent(&hangingModel{replies: []*llm.ChatResp{{Content: "好", Reasoning: "先想想"}}})
		a.cfg.Observer = obs

		// Act
		_, err := a.chat(context.Background(), req)

		// Assert
		require.NoError(t, err)
		require.Equal(t, []string{"reasoning"}, obs.events)
	})
}
