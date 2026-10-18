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
	"strings"
	"testing"

	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/require"
)

func Test_userMessage(t *testing.T) {
	t.Run("截图直接作为图片放进第一条用户消息，不再单独调用模型识别", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{}
		pas := newProductForService(t, model, &fakePurchaseRepo{})
		question := &schema.QuestionInfoResp{
			Title:    "iPhone 17 值得买吗",
			Purchase: &schema.PurchaseInfo{Price: 5999, Images: []string{"u"}},
		}

		// Act
		msg := pas.userMessage(context.Background(), question)

		// Assert
		require.Empty(t, model.reqs)
		require.Equal(t, llm.RoleUser, msg.Role)
		require.Len(t, msg.Parts, 3)
		require.Contains(t, msg.Parts[0].Text, "标题：iPhone 17 值得买吗")
		require.Equal(t, screenshotsHeader, msg.Parts[1].Text)
		require.True(t, strings.HasPrefix(msg.Parts[2].ImageURL, "data:image/"))
	})

	t.Run("帖子里存过的商品信息照样写成文字", func(t *testing.T) {
		// Arrange
		pas := newProductForService(t, &fakeLLM{}, &fakePurchaseRepo{})
		question := &schema.QuestionInfoResp{
			Title:    "x",
			Purchase: &schema.PurchaseInfo{Product: &schema.PurchaseProduct{Name: "用户确认过的商品", Confidence: 0.9}},
		}

		// Act
		msg := pas.userMessage(context.Background(), question)

		// Assert：没有截图就只有一段文字
		require.Len(t, msg.Parts, 1)
		require.Contains(t, msg.Parts[0].Text, "- 名称：用户确认过的商品")
	})

	t.Run("截图都读不出来时只发文字", func(t *testing.T) {
		// Arrange
		pas := newProductForService(t, &fakeLLM{}, &fakePurchaseRepo{})
		question := &schema.QuestionInfoResp{Title: "x", Purchase: &schema.PurchaseInfo{Images: []string{"missing"}}}

		// Act
		msg := pas.userMessage(context.Background(), question)

		// Assert
		require.Len(t, msg.Parts, 1)
	})

	t.Run("不是买不买帖子（没有 purchase）也能组出消息", func(t *testing.T) {
		// Arrange
		pas := newProductForService(t, &fakeLLM{}, &fakePurchaseRepo{})

		// Act
		msg := pas.userMessage(context.Background(), &schema.QuestionInfoResp{Title: "x"})

		// Assert
		require.Len(t, msg.Parts, 1)
	})
}
