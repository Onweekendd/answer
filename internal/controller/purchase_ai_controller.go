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
