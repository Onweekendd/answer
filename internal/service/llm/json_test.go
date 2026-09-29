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

package llm

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// scriptedClient answers from a list of replies and keeps a copy of each
// request, so a test can check what was sent on every attempt.
type scriptedClient struct {
	replies []string
	err     error
	reqs    []ChatReq
}

func (c *scriptedClient) Chat(_ context.Context, req *ChatReq) (*ChatResp, error) {
	snapshot := *req
	snapshot.Messages = append([]Message(nil), req.Messages...)
	c.reqs = append(c.reqs, snapshot)
	if c.err != nil {
		return nil, c.err
	}
	if len(c.replies) == 0 {
		return nil, errors.New("scriptedClient: no reply left")
	}
	reply := c.replies[0]
	c.replies = c.replies[1:]
	return &ChatResp{Content: reply}, nil
}

// parseSmall is a tiny "schema": a whole number below 100. It makes T an int,
// which shows the zero value on failure is 0 rather than nil.
func parseSmall(content string) (int, error) {
	n, err := strconv.Atoi(content)
	if err != nil {
		return 0, errors.New("not a whole number")
	}
	if n >= 100 {
		return 0, errors.New("must be below 100")
	}
	return n, nil
}

func startReq() *ChatReq {
	return &ChatReq{Messages: []Message{Text(RoleSystem, "sys"), Text(RoleUser, "pick a number")}}
}

func TestChatJSON(t *testing.T) {
	t.Run("第一次就合格：只请求一次，并且打开 JSON 模式", func(t *testing.T) {
		// Arrange
		client := &scriptedClient{replies: []string{"42"}}

		// Act
		n, err := ChatJSON(context.Background(), client, startReq(), 2, parseSmall)

		// Assert
		require.NoError(t, err)
		require.Equal(t, 42, n)
		require.Len(t, client.reqs, 1)
		require.True(t, client.reqs[0].JSONOutput, "the caller did not set it, ChatJSON must")
	})

	t.Run("不合格时带着原输出和原因重试", func(t *testing.T) {
		// Arrange
		client := &scriptedClient{replies: []string{"150", "7"}}

		// Act
		n, err := ChatJSON(context.Background(), client, startReq(), 2, parseSmall)

		// Assert
		require.NoError(t, err)
		require.Equal(t, 7, n)
		require.Len(t, client.reqs, 2)

		retry := client.reqs[1].Messages
		require.Len(t, retry, 4)
		require.Equal(t, RoleAssistant, retry[2].Role)
		require.Equal(t, "150", retry[2].Parts[0].Text, "the model sees its own rejected reply")
		require.Equal(t, RoleUser, retry[3].Role)
		require.Contains(t, retry[3].Parts[0].Text, "must be below 100", "and why it was rejected")
	})

	t.Run("用完次数返回最后一次的原因和零值", func(t *testing.T) {
		// Arrange
		client := &scriptedClient{replies: []string{"abc", "150"}}

		// Act
		n, err := ChatJSON(context.Background(), client, startReq(), 2, parseSmall)

		// Assert
		require.Equal(t, 0, n)
		require.ErrorContains(t, err, "must be below 100")
		require.Len(t, client.reqs, 2)
	})

	t.Run("模型调用失败时不重试", func(t *testing.T) {
		// Arrange
		client := &scriptedClient{err: errors.New("provider down")}

		// Act
		_, err := ChatJSON(context.Background(), client, startReq(), 3, parseSmall)

		// Assert
		require.ErrorContains(t, err, "provider down")
		require.Len(t, client.reqs, 1)
	})

	t.Run("不会改到调用方的请求", func(t *testing.T) {
		// Arrange
		client := &scriptedClient{replies: []string{"150", "7"}}
		req := startReq()

		// Act
		_, err := ChatJSON(context.Background(), client, req, 2, parseSmall)

		// Assert
		require.NoError(t, err)
		require.Len(t, req.Messages, 2)
		require.False(t, req.JSONOutput)
	})

	t.Run("次数小于 1 时仍然请求一次", func(t *testing.T) {
		// Arrange
		client := &scriptedClient{replies: []string{"5"}}

		// Act
		n, err := ChatJSON(context.Background(), client, startReq(), 0, parseSmall)

		// Assert
		require.NoError(t, err)
		require.Equal(t, 5, n)
		require.Len(t, client.reqs, 1)
	})
}
