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
	"github.com/apache/answer/internal/base/handler"
	"github.com/apache/answer/internal/base/middleware"
	"github.com/apache/answer/internal/schema"
	purchasechat "github.com/apache/answer/internal/service/purchase_chat"
	"github.com/gin-gonic/gin"
)

// PurchaseChatController keeps the chat page's conversations
type PurchaseChatController struct {
	purchaseChatService *purchasechat.PurchaseChatService
}

// NewPurchaseChatController new controller
func NewPurchaseChatController(purchaseChatService *purchasechat.PurchaseChatService) *PurchaseChatController {
	return &PurchaseChatController{purchaseChatService: purchaseChatService}
}

// List my recent conversations
// @Summary list my recent conversations
// @Tags PurchaseChat
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} handler.RespBody{data=[]schema.PurchaseChatItem}
// @Router /answer/api/v1/purchase/chats [get]
func (pcc *PurchaseChatController) List(ctx *gin.Context) {
	resp, err := pcc.purchaseChatService.List(ctx, middleware.GetLoginUserIDFromContext(ctx))
	handler.HandleResponse(ctx, err, resp)
}

// Create start a conversation
// @Summary start a conversation
// @Tags PurchaseChat
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param data body schema.PurchaseChatCreateReq true "title"
// @Success 200 {object} handler.RespBody{data=schema.PurchaseChatCreateResp}
// @Router /answer/api/v1/purchase/chat [post]
func (pcc *PurchaseChatController) Create(ctx *gin.Context) {
	req := &schema.PurchaseChatCreateReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}
	req.UserID = middleware.GetLoginUserIDFromContext(ctx)
	resp, err := pcc.purchaseChatService.Create(ctx, req)
	handler.HandleResponse(ctx, err, resp)
}
