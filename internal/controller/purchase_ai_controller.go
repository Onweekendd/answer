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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/apache/answer/internal/base/handler"
	"github.com/apache/answer/internal/base/middleware"
	"github.com/apache/answer/internal/schema"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	"github.com/gin-gonic/gin"
)

// PurchaseAIController purchase ai controller
type PurchaseAIController struct {
	purchaseAIService *purchaseai.PurchaseAIService
}

// NewPurchaseAIController new controller
func NewPurchaseAIController(
	purchaseAIService *purchaseai.PurchaseAIService,
) *PurchaseAIController {
	return &PurchaseAIController{
		purchaseAIService: purchaseAIService,
	}
}

// Analyze analyze a purchase request
// @Summary analyze a purchase request
// @Description ask the model whether the item is worth buying; the advice is posted as a comment
// @Tags PurchaseAI
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param data body schema.PurchaseAIAnalyzeReq true "question id"
// @Success 200 {object} handler.RespBody
// @Router /answer/api/v1/purchase/ai/analyze [post]
func (pac *PurchaseAIController) Analyze(ctx *gin.Context) {
	req := &schema.PurchaseAIAnalyzeReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}

	// the analysis takes tens of seconds, so this only queues it. The caller polls
	// purchase.ai_status to find out how it went.
	pac.purchaseAIService.Enqueue(ctx, req.QuestionID)
	handler.HandleResponse(ctx, nil, nil)
}

// Identify read the product off just-uploaded screenshots
// @Summary read the product off just-uploaded screenshots
// @Description ask the vision model what product the screenshots show, so the post form can fill in the price and tags; product is null when they show none
// @Tags PurchaseAI
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param data body schema.PurchaseAIIdentifyReq true "uploaded screenshot urls"
// @Success 200 {object} handler.RespBody{data=schema.PurchaseAIIdentifyResp}
// @Router /answer/api/v1/purchase/ai/identify [post]
func (pac *PurchaseAIController) Identify(ctx *gin.Context) {
	req := &schema.PurchaseAIIdentifyReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}
	req.UserID = middleware.GetLoginUserIDFromContext(ctx)

	// unlike Analyze this waits for the result: it is one vision call of a few
	// seconds, and the form needs it. The request's context goes along, so the
	// call stops when the user removes the screenshots and the form cancels.
	resp, err := pac.purchaseAIService.Identify(ctx, req)
	handler.HandleResponse(ctx, err, resp)
}

// sseHeartbeat 是多久发一次心跳。nginx 默认 60 秒收不到数据就断开，Agent 等一次模型回复可能就要二三十秒
const sseHeartbeat = 15 * time.Second

// Chat answer the latest message of a conversation, streaming the steps
// @Summary chat with the buying agent
// @Description server-sent events: reasoning, tool_start, tool_end, text, then done or error, then data: [DONE]
// @Tags PurchaseAI
// @Accept json
// @Produce text/event-stream
// @Security ApiKeyAuth
// @Param data body schema.PurchaseAIChatReq true "the conversation so far"
// @Router /answer/api/v1/purchase/ai/chat [post]
func (pac *PurchaseAIController) Chat(ctx *gin.Context) {
	req := &schema.PurchaseAIChatReq{}
	// 参数不对时还没开始流，照常回 JSON 错误
	if handler.BindAndCheck(ctx, req) {
		return
	}
	req.UserID = middleware.GetLoginUserIDFromContext(ctx)

	// 用请求的 context：用户关掉页面、或者这个 handler 返回时它被取消，Agent 跟着停
	events := pac.purchaseAIService.Chat(ctx.Request.Context(), req)
	writeSSE(ctx, events, sseHeartbeat)
}

// ChatHistory a conversation as the page shows it, rebuilt from its log
// @Summary a conversation's messages as the page shows them, every branch included
// @Tags PurchaseAI
// @Produce json
// @Security ApiKeyAuth
// @Param chat_id query string true "conversation id"
// @Success 200 {object} handler.RespBody{data=[]purchaseai.PageMessage}
// @Router /answer/api/v1/purchase/ai/chat/history [get]
func (pac *PurchaseAIController) ChatHistory(ctx *gin.Context) {
	req := &schema.PurchaseAIChatHistoryReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}
	req.UserID = middleware.GetLoginUserIDFromContext(ctx)
	resp, err := pac.purchaseAIService.ChatHistory(ctx, req)
	handler.HandleResponse(ctx, err, resp)
}

// VoteDraft the AI casts the vote it kept with a draft
// @Summary the AI casts the vote it kept with a draft, on the post the user published from it
// @Tags PurchaseAI
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param data body schema.PurchaseDraftVoteReq true "draft and post"
// @Success 200 {object} handler.RespBody
// @Router /answer/api/v1/purchase/ai/draft/vote [post]
func (pac *PurchaseAIController) VoteDraft(ctx *gin.Context) {
	req := &schema.PurchaseDraftVoteReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}
	req.UserID = middleware.GetLoginUserIDFromContext(ctx)
	err := pac.purchaseAIService.VoteDraft(ctx, req)
	handler.HandleResponse(ctx, err, nil)
}

// writeSSE 把事件一条条写成 SSE，直到 channel 关闭（最后写 data: [DONE]）或者用户断开。
// 写网络只在这一个 goroutine 里：同时等事件和心跳，ResponseWriter 不用加锁（概念 2）
func writeSSE(ctx *gin.Context, events <-chan purchaseai.Event, heartbeat time.Duration) {
	w := ctx.Writer
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // 让 nginx 别攒着
	w.WriteHeader(http.StatusOK)
	w.Flush() // 先把响应头发出去，前端马上知道请求成功了

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				w.Flush()
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			// json.Marshal 把换行转义成 \n，一条事件就是一行 data
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			w.Flush()
		case <-ticker.C:
			// 冒号开头的行是注释，前端直接跳过，只为让连接上有数据在走
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			w.Flush()
		case <-ctx.Request.Context().Done():
			return
		}
	}
}
