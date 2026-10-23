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
	"fmt"
	"strings"

	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/links"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
)

// 商品卡片（阶段 3）：Agent 看中的商品做成一张卡片推给用户，用户点一下就能打开，自己核实价格。
// 链接和商品图都由插件从页面上取，模型只写标题、价格这些它读到的东西：
// 让模型自己写链接，商品 id 错一位就是别的商品（"不要编造链接"管不住）

// ProductCard 是一张商品卡片（product 事件）。字段和 frontend/web/src/core/chat/mapper.ts 的 ProductCard 对应
type ProductCard struct {
	ID        int    `json:"id"` // 第几个，从 1 开始；同一个商品再记一次时用它找到原来那张
	Title     string `json:"title"`
	Shop      string `json:"shop,omitempty"`
	Spec      string `json:"spec,omitempty"`
	Price     string `json:"price,omitempty"`
	PriceNote string `json:"priceNote,omitempty"`
	Note      string `json:"note,omitempty"`
	URL       string `json:"url"`             // 清洗过的链接，见 links.Clean
	Image     string `json:"image,omitempty"` // 存下来的商品图的网址；没截到、没存成就没有
}

// ImageStore 存 Agent 截的图，返回给用户看的网址。uploader.UploaderService 实现了它（存在本地 uploads/agent）；
// 换成对象存储（Garage）时只换实现
type ImageStore interface {
	SaveAgentImage(ctx context.Context, data []byte) (string, error)
}

// productBook 是这次任务记下的商品卡片
type productBook struct {
	state *browsertool.TaskState
	pres  *presenter
	pics  *pictures // 存卡片上的图
	cards []ProductCard
}

// plugin 装 pick_product
func (b *productBook) plugin() agent.Plugin {
	return func(r *agent.Registry) {
		r.Tool(tool.Spec{Name: toolProduct, Description: "记下一个值得推荐给用户的商品：用户会在回答下方看到它的商品图、价格，点一下就打开商品页。链接和图由系统从页面上取，你只写标题、价格这些看到的东西。" +
			"在商品详情页上不填 index；在搜索结果里填那张商品卡片的编号。只记看过价格的商品，最多 6 个。", Args: productArgs{}, Run: b.state.Track(b.pick)})
	}
}

type productArgs struct {
	Index     *int   `json:"index,omitempty" description:"在搜索结果里挑时，填那张商品卡片在最新快照里的编号；在商品详情页上不填，用的是当前页面"`
	Title     string `json:"title" description:"商品名，写短一点：品牌、型号、关键规格"`
	Shop      string `json:"shop,omitempty" description:"店铺名，比如 无印良品MUJI官方旗舰店"`
	Spec      string `json:"spec,omitempty" description:"价格对应的规格，比如 200x230cm；页面上看不出是哪个规格就不填"`
	Price     string `json:"price" description:"页面上看到的价格，比如 ¥588；只显示了一部分就照样写，比如 ¥4??"`
	PriceNote string `json:"priceNote,omitempty" description:"价格的条件，比如 券后、会员价、每满2000减200。只写条件，price 里写过的价格不要再写"`
	Note      string `json:"note,omitempty" description:"一句话：为什么推荐它，或者要注意什么"`
}

const (
	maxProducts         = 6   // 一次回答最多几张卡片：再多用户就不看了
	productImageWidth   = 480 // 卡片上的图只是缩略图，不用看清小字
	productImageQuality = 80
	productTextRunes    = 80 // 卡片上每个字段最多几个字
)

// productData 是插件 product 命令的结果
type productData struct {
	URL   string      `json:"url"`
	Title string      `json:"title"`
	Image *tool.Image `json:"image"` // 没找到够大的图时为 null
}

// pick 把一个商品做成卡片。不改变页面，同一轮后面的调用照常执行
func (b *productBook) pick(ctx context.Context, arguments string) (tool.Result, error) {
	var args productArgs
	if res := tool.DecodeArgs(arguments, &args, `{"title": "...", "price": "¥588"} 或 {"index": 3, "title": "...", "price": "¥588"}`); res != nil {
		return *res, nil
	}
	if tool.OneLine(args.Title) == "" {
		return tool.Result{Failed: true, Text: "参数无效：title 不能为空。"}, nil
	}
	if res := b.state.RequireTab(); res != nil {
		return *res, nil
	}
	req := map[string]any{"maxWidth": productImageWidth, "quality": productImageQuality}
	if args.Index != nil {
		req["index"] = *args.Index
	}
	var data productData
	if res, err := b.state.Call(ctx, b.state.TabID, "product", req, &data); res != nil {
		return *res, err
	}
	link, ok := links.Clean(data.URL)
	if !ok {
		return tool.Result{Failed: true, Text: "这张卡片的链接是广告跳转或者不是网页地址，用户点了打不开对的商品。点进商品详情页，再不填 index 调用 pick_product。"}, nil
	}
	if !links.IsProductPage(link) {
		return tool.Result{Failed: true, Text: fmt.Sprintf("这个链接（%s）不是商品页，是搜索结果、店铺或者客服页面，用户点了看不到这个商品。"+
			"在搜索结果里填那张商品卡片的编号，或者点进商品详情页以后不填 index 再记。", link)}, nil
	}

	// 同一个商品再做一次，就更新原来那张，不重复
	at := -1
	for i, p := range b.cards {
		if p.URL == link {
			at = i
		}
	}
	if at < 0 && len(b.cards) >= maxProducts {
		return tool.Result{Failed: true, Text: fmt.Sprintf("已经有 %d 张卡片了，不能再加。挑最值得看的写进回答就行。", maxProducts)}, nil
	}

	card := ProductCard{
		Title:     b.pres.cardText(args.Title),
		Shop:      b.pres.cardText(args.Shop),
		Spec:      b.pres.cardText(args.Spec),
		Price:     b.pres.cardText(args.Price),
		PriceNote: b.pres.cardText(args.PriceNote),
		Note:      b.pres.cardText(args.Note),
		URL:       link, // 不过 view：打码会把 11 位的商品 id 当成手机号
		Image:     b.pics.save(ctx, data.Image),
	}
	if at >= 0 {
		card.ID = b.cards[at].ID
		b.cards[at] = card
	} else {
		card.ID = len(b.cards) + 1
		b.cards = append(b.cards, card)
	}
	b.pres.emit(ctx, Event{Type: EventProduct, Product: &card})

	text := fmt.Sprintf("已记下「%s」（%s）。用户会在回答下方看到它的商品图和价格，点一下就打开商品页。回答里提到它直接写商品名，不要自己写链接。",
		card.Title, link)
	if card.Image == "" {
		text += "没有截到商品图。"
	}
	return tool.Result{Text: text, Display: "记下了「" + card.Title + "」", Extras: map[string]any{extraProduct: &card}}, nil
}

// markdownEscaper 转义标题里会被 Markdown 当成链接、强调的字符
var markdownEscaper = strings.NewReplacer(`[`, `\[`, `]`, `\]`, `*`, `\*`, `_`, `\_`, "`", "\\`")

// productsMarkdown 把卡片写成 Markdown 列表，接在帖子分析的评论后面：评论里显示不了卡片，
// 回答里提到的商品要有地方点开
func productsMarkdown(products []ProductCard) string {
	if len(products) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n**提到的商品**\n")
	for _, p := range products {
		fmt.Fprintf(&b, "\n%d. [%s](%s)", p.ID, markdownEscaper.Replace(p.Title), p.URL)
		for _, field := range []string{p.Shop, p.Spec, p.Price} {
			if field != "" {
				b.WriteString(" · " + markdownEscaper.Replace(field))
			}
		}
		if p.PriceNote != "" {
			b.WriteString("（" + markdownEscaper.Replace(p.PriceNote) + "）")
		}
	}
	return b.String()
}
