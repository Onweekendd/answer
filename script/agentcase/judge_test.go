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

package main

import (
	"strings"
	"testing"

	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	"github.com/stretchr/testify/require"
)

// 京东详情页那个 case 的一次运行：搜索 → 点第一个商品 → 新标签页里的详情页
func detailRun(answer string) *purchaseai.TaskResult {
	return &purchaseai.TaskResult{
		Text:    answer,
		Success: true,
		Steps: []purchaseai.TaskStep{
			{Tool: "open", URL: "https://www.jd.com/"},
			{Tool: "type", URL: "https://search.jd.com/Search?keyword=iphone%2017"},
			{Tool: "click", URL: "https://item.jd.com/100012345.html"},
			{Tool: "done"},
		},
	}
}

var detailCase = Case{
	Name:   "京东详情页",
	Expect: Expect{Visited: []string{"search.jd.com", "item.jd.com"}, Used: []string{"type"}, MinPrices: 1},
}

func Test_judge(t *testing.T) {
	t.Run("条件全部满足，返回空", func(t *testing.T) {
		// Arrange
		res := detailRun("店铺是 Apple 产品京东自营旗舰店，价格 ¥5999")

		// Act
		problems := judge(detailCase, res, nil)

		// Assert
		require.Empty(t, problems)
	})

	t.Run("没到过要求的网址，那一条里写着这个网址", func(t *testing.T) {
		// Arrange：停在了搜索结果页，没进详情页
		res := detailRun("价格 ¥5999")
		res.Steps = res.Steps[:2]

		// Act
		problems := judge(detailCase, res, nil)

		// Assert
		require.Len(t, problems, 1)
		require.Contains(t, problems[0], "item.jd.com")
	})

	t.Run("工具执行失败的那一步不算用过", func(t *testing.T) {
		// Arrange：type 失败了，模型改成直接 navigate 到搜索网址
		res := detailRun("价格 ¥5999")
		res.Steps[1] = purchaseai.TaskStep{Tool: "type", Failed: true, URL: "https://www.jd.com/"}
		res.Steps = append(res.Steps, purchaseai.TaskStep{Tool: "navigate", URL: "https://search.jd.com/Search?keyword=x"})

		// Act
		problems := judge(detailCase, res, nil)

		// Assert
		require.Len(t, problems, 1)
		require.Contains(t, problems[0], "type")
	})

	t.Run("价格不够，那一条里写着要求的和实际的个数", func(t *testing.T) {
		// Arrange
		c := Case{Name: "1688 起批价", Expect: Expect{MinPrices: 3}}
		res := &purchaseai.TaskResult{Text: "第一个 ¥3.50，第二个 ¥4.20，第三个没看到价格"}

		// Act
		problems := judge(c, res, nil)

		// Assert
		require.Len(t, problems, 1)
		require.Contains(t, problems[0], "2")
		require.Contains(t, problems[0], "3")
	})

	t.Run("几条都没满足时全部列出来，不在第一条就返回", func(t *testing.T) {
		// Arrange：只打开了首页，回答里也没有价格
		res := detailRun("没找到")
		res.Steps = res.Steps[:1]

		// Act
		problems := judge(detailCase, res, nil)

		// Assert：两个网址、type、价格，一共 4 条
		require.Len(t, problems, 4)
		all := strings.Join(problems, "\n")
		require.Contains(t, all, "search.jd.com")
		require.Contains(t, all, "item.jd.com")
	})

	t.Run("模型说做成了也不算数，只看痕迹", func(t *testing.T) {
		// Arrange
		res := detailRun("做好了")
		res.Success = true

		// Act
		problems := judge(detailCase, res, nil)

		// Assert
		require.Len(t, problems, 1)
	})

	t.Run("seen：某一份快照里出现过就算，哪一份都行", func(t *testing.T) {
		// Arrange：第一份快照里"销量"还没选中，第二份里选中了
		c := Case{Name: "淘宝排序", Expect: Expect{Seen: []string{"aria-selected=true>销量"}}}
		snapshots := []string{
			"[25]<li role=tab aria-selected=true>综合\n[26]<li role=tab aria-selected=false>销量",
			"[25]<li role=tab aria-selected=false>综合\n[26]<li role=tab aria-selected=true>销量",
		}

		// Act
		passed := judge(c, &purchaseai.TaskResult{}, snapshots)
		failed := judge(c, &purchaseai.TaskResult{}, snapshots[:1])

		// Assert
		require.Empty(t, passed)
		require.Len(t, failed, 1)
		require.Contains(t, failed[0], "销量")
	})

	t.Run("没有任何条件的 case，什么都不检查", func(t *testing.T) {
		// Act
		problems := judge(Case{Name: "随便看看"}, &purchaseai.TaskResult{}, nil)

		// Assert
		require.Empty(t, problems)
	})
}

func Test_countPrices(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"人民币符号在前", "第一个是 Apple iPhone 17，¥5999", 1},
		{"全角符号、符号后面有空格", "到手价 ￥ 5499", 1},
		{"元在后面，前面有没有空格都算", "原价 5999元，国补后 5499 元", 2},
		{"千分位逗号去掉以后是同一个价格", "¥5,499，也就是 5499 元", 1},
		{"小数", "起批价 ¥3.50、¥4.2、¥12.80", 3},
		{"重复出现的价格只算一次", "¥3.50、¥4.20、¥3.50", 2},
		{"不带单位的数字不算", "iPhone 17 256GB，支持 24 期免息，月销 8000+", 0},
		{"上次淘宝回答里的一段", "多家天猫和授权店都是 5499 国补后，中国联通自营店 5399 国补后；标 ¥3928 的是个人店", 1},
		{"空回答", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := countPrices(tc.text)

			// Assert
			require.Equal(t, tc.want, got, tc.text)
		})
	}
}

func Test_judge_Card(t *testing.T) {
	cardCase := Case{Name: "京东卡片", Expect: Expect{MinCards: 1}}

	t.Run("卡片够数、链接在去过的网站上，通过", func(t *testing.T) {
		// Arrange
		res := detailRun("见卡片 1")
		res.Products = []purchaseai.ProductCard{{ID: 1, URL: "https://item.jd.com/100012345.html"}}

		// Act
		problems := judge(cardCase, res, nil)

		// Assert
		require.Empty(t, problems)
	})

	t.Run("没做卡片", func(t *testing.T) {
		// Act
		problems := judge(cardCase, detailRun("¥5999"), nil)

		// Assert
		require.Len(t, problems, 1)
		require.Contains(t, problems[0], "0 张")
	})

	t.Run("卡片的链接在没去过的网站上", func(t *testing.T) {
		// Arrange
		res := detailRun("见卡片 1")
		res.Products = []purchaseai.ProductCard{{ID: 1, URL: "https://item.taobao.com/item.htm?id=1"}}

		// Act
		problems := judge(cardCase, res, nil)

		// Assert
		require.Len(t, problems, 1)
		require.Contains(t, problems[0], "item.taobao.com")
	})
}
