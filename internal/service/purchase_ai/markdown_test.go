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
	"strings"
	"testing"

	"github.com/apache/answer/pkg/converter"
	"github.com/stretchr/testify/require"
)

func Test_fixNumberRanges(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "数字范围", in: "68~74 元/公斤", want: "68～74 元/公斤"},
		{name: "带空格的范围", in: "68 ~ 74", want: "68～74"},
		{name: "一段里有两个范围", in: "68~74 元/公斤，约 34~37 元/斤", want: "68～74 元/公斤，约 34～37 元/斤"},
		{name: "连续的范围", in: "1~2~3", want: "1～2～3"},
		{name: "小数", in: "0.5~1.5 kg", want: "0.5～1.5 kg"},
		{name: "删除线不动", in: "~~原价 2599~~ 现价 2099", want: "~~原价 2599~~ 现价 2099"},
		{name: "链接里的波浪线不动", in: "[x](https://example.com/~user/1)", want: "[x](https://example.com/~user/1)"},
		{name: "前面不是数字的不动", in: "约 ~70 元", want: "约 ~70 元"},
		{name: "跨行的不动", in: "68\n~74", want: "68\n~74"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, fixNumberRanges(tt.in))
		})
	}
}

func Test_fixNumberRanges_rendersWithoutStrikethrough(t *testing.T) {
	// Arrange: the reply that showed up struck out on the site
	reply := "国内牛肉零售参考价也就 68~74 元/公斤，约 34~37 元/斤。"

	// Act
	before := converter.Markdown2HTML(reply)
	after := converter.Markdown2HTML(fixNumberRanges(reply))

	// Assert
	require.Contains(t, before, "<del>", "the bug this guards against")
	require.NotContains(t, after, "<del>")
	require.True(t, strings.Contains(after, "68～74") && strings.Contains(after, "34～37"))
}
