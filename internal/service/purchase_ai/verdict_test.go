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
	"testing"

	"github.com/apache/answer/internal/entity"
	"github.com/stretchr/testify/require"
)

func Test_parseVerdict(t *testing.T) {
	tests := []struct {
		name   string
		advice string
		want   int
	}{
		// the first three are the openings of real replies in the local database
		{"加粗的买", "**买**\n\n- 价格已经比官方价便宜一截", entity.PurchaseVoteBuy},
		{"加粗的不买", "**不买**\n\n- 价格和宣传对不上", entity.PurchaseVoteSkip},
		{"再等等带说明", "**再等等** —— 东西该买，但这个价不是现在该付的价", entity.PurchaseVoteWait},
		{"前面有空行", "\n\n  **买** —— 值得现在买", entity.PurchaseVoteBuy},
		{"标题格式", "## 不买", entity.PurchaseVoteSkip},
		{"不买不能被认成买", "不买，理由如下", entity.PurchaseVoteSkip},
		{"第一行不是结论", "这款耳机我查了三个平台。\n**买**", 0},
		{"空回复", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parseVerdict(tt.advice))
		})
	}
}
