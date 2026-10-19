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

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func sseContext(ctx context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/answer/api/v1/purchase/ai/chat", nil).WithContext(ctx)
	return c, rec
}

func Test_writeSSE(t *testing.T) {
	t.Run("每条事件一行 data，channel 关了写 [DONE]", func(t *testing.T) {
		// Arrange
		events := make(chan purchaseai.Event, 2)
		events <- purchaseai.Event{Type: purchaseai.EventText, Delta: "第一行\n第二行"}
		events <- purchaseai.Event{Type: purchaseai.EventDone}
		close(events)
		c, rec := sseContext(context.Background())

		// Act
		writeSSE(c, events, time.Hour)

		// Assert
		require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
		require.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))
		require.Equal(t,
			"data: {\"type\":\"text\",\"delta\":\"第一行\\n第二行\"}\n\n"+
				"data: {\"type\":\"done\"}\n\n"+
				"data: [DONE]\n\n",
			rec.Body.String())
	})

	t.Run("等事件的时候按时发心跳", func(t *testing.T) {
		// Arrange：一条事件都不来，过一会儿才关
		events := make(chan purchaseai.Event)
		go func() {
			time.Sleep(50 * time.Millisecond)
			close(events)
		}()
		c, rec := sseContext(context.Background())

		// Act
		writeSSE(c, events, 10*time.Millisecond)

		// Assert
		require.Contains(t, rec.Body.String(), ": ping\n\n")
		require.True(t, strings.HasSuffix(rec.Body.String(), "data: [DONE]\n\n"))
	})

	t.Run("用户断开就返回，不再等 channel", func(t *testing.T) {
		// Arrange
		ctx, cancel := context.WithCancel(context.Background())
		c, rec := sseContext(ctx)
		cancel()

		// Act：channel 永远不关，靠请求的 ctx 退出
		writeSSE(c, make(chan purchaseai.Event), time.Hour)

		// Assert
		require.NotContains(t, rec.Body.String(), "[DONE]")
	})
}
