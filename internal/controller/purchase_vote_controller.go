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
	purchasevote "github.com/apache/answer/internal/service/purchase_vote"
	"github.com/gin-gonic/gin"
)

// PurchaseVoteController purchase vote controller
type PurchaseVoteController struct {
	purchaseVoteService *purchasevote.PurchaseVoteService
}

// NewPurchaseVoteController new controller
func NewPurchaseVoteController(
	purchaseVoteService *purchasevote.PurchaseVoteService,
) *PurchaseVoteController {
	return &PurchaseVoteController{
		purchaseVoteService: purchaseVoteService,
	}
}

// Vote vote buy / wait / skip on a purchase post
// @Summary vote buy / wait / skip on a purchase post
// @Description one vote per user, changeable until the post reaches the vote limit; the author cannot vote
// @Tags PurchaseVote
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param data body schema.PurchaseVoteReq true "question id and choice: 1 buy, 2 wait, 3 skip"
// @Success 200 {object} handler.RespBody{data=schema.PurchaseVoteResp}
// @Router /answer/api/v1/purchase/vote [post]
func (pvc *PurchaseVoteController) Vote(ctx *gin.Context) {
	req := &schema.PurchaseVoteReq{}
	if handler.BindAndCheck(ctx, req) {
		return
	}
	req.UserID = middleware.GetLoginUserIDFromContext(ctx)

	resp, err := pvc.purchaseVoteService.Vote(ctx, req)
	handler.HandleResponse(ctx, err, resp)
}
