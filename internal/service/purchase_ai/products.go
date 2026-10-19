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
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/segmentfault/pacman/log"
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
	URL       string `json:"url"`             // 清洗过的链接，见 cleanProductURL
	Image     string `json:"image,omitempty"` // 存下来的商品图的网址；没截到、没存成就没有
}

// ImageStore 存 Agent 截的图，返回给用户看的网址。uploader.UploaderService 实现了它（存在本地 uploads/agent）；
// 换成对象存储（Garage）时只换实现
type ImageStore interface {
	SaveAgentImage(ctx context.Context, data []byte) (string, error)
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
	URL   string          `json:"url"`
	Title string          `json:"title"`
	Image *screenshotData `json:"image"` // 没找到够大的图时为 null
}

// pickProduct 把一个商品做成卡片。不改变页面，同一轮后面的调用照常执行
func (r *agentRun) pickProduct(ctx context.Context, arguments string) (toolResult, error) {
	var args productArgs
	if res := decodeArgs(arguments, &args, `{"title": "...", "price": "¥588"} 或 {"index": 3, "title": "...", "price": "¥588"}`); res != nil {
		return *res, nil
	}
	if oneLine(args.Title) == "" {
		return toolResult{Failed: true, Text: "参数无效：title 不能为空。"}, nil
	}
	if res := r.requireTab(); res != nil {
		return *res, nil
	}
	req := map[string]any{"maxWidth": productImageWidth, "quality": productImageQuality}
	if args.Index != nil {
		req["index"] = *args.Index
	}
	var data productData
	if res, err := r.call(ctx, r.tabID, "product", req, &data); res != nil {
		return *res, err
	}
	link, ok := cleanProductURL(data.URL)
	if !ok {
		return toolResult{Failed: true, Text: "这张卡片的链接是广告跳转或者不是网页地址，用户点了打不开对的商品。点进商品详情页，再不填 index 调用 pick_product。"}, nil
	}

	// 同一个商品再做一次，就更新原来那张，不重复
	at := -1
	for i, p := range r.products {
		if p.URL == link {
			at = i
		}
	}
	if at < 0 && len(r.products) >= maxProducts {
		return toolResult{Failed: true, Text: fmt.Sprintf("已经有 %d 张卡片了，不能再加。挑最值得看的写进回答就行。", maxProducts)}, nil
	}

	card := ProductCard{
		Title:     r.cardText(args.Title),
		Shop:      r.cardText(args.Shop),
		Spec:      r.cardText(args.Spec),
		Price:     r.cardText(args.Price),
		PriceNote: r.cardText(args.PriceNote),
		Note:      r.cardText(args.Note),
		URL:       link, // 不过 view：打码会把 11 位的商品 id 当成手机号
		Image:     r.saveImage(ctx, data.Image),
	}
	if at >= 0 {
		card.ID = r.products[at].ID
		r.products[at] = card
	} else {
		card.ID = len(r.products) + 1
		r.products = append(r.products, card)
	}
	r.emit(ctx, Event{Type: EventProduct, Product: &card})

	text := fmt.Sprintf("已记下「%s」（%s）。用户会在回答上方看到它的商品图和价格，点一下就打开商品页。回答里提到它直接写商品名，不要自己写链接。",
		card.Title, link)
	if card.Image == "" {
		text += "没有截到商品图。"
	}
	return toolResult{Text: text, Display: "记下了「" + card.Title + "」"}, nil
}

// cardText 是卡片上的一个字段：压成一行、截短，再过打码和屏蔽词（模型写的，可能复述页面内容）
func (r *agentRun) cardText(s string) string {
	s = oneLine(s)
	if clipped := clipRunes(s, productTextRunes); clipped != s {
		s = clipped + "…"
	}
	return r.view(s)
}

// saveImage 存下商品图，返回网址。存不了就没有图：卡片照样有用，不算这次调用失败
func (r *agentRun) saveImage(ctx context.Context, shot *screenshotData) string {
	if r.images == nil || shot == nil || shot.Data == "" {
		return ""
	}
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		log.Warnf("purchase ai: bad product image from the extension: %v", err)
		return ""
	}
	link, err := r.images.SaveAgentImage(ctx, data)
	if err != nil {
		log.Warnf("purchase ai: cannot save product image: %v", err)
		return ""
	}
	return link
}

// ---------- 链接清洗 ----------

// productParams 是各个网站的商品链接里要留下的参数，别的（spm、追踪 id、搜索词）都去掉：
// 它们可能带着共享浏览器那个账号的信息，用户点开也用不上。没列出的网站去掉 trackingParams
var productParams = map[string][]string{
	"item.taobao.com":          {"id"},
	"detail.tmall.com":         {"id", "skuId"},
	"chaoshi.detail.tmall.com": {"id", "skuId"},
	"detail.tmall.hk":          {"id", "skuId"},
	"item.jd.com":              {},
	"item.jd.hk":               {},
	"www.ikea.cn":              {},
	"detail.1688.com":          {},
}

// trackingParams 是没列出的网站上要去掉的参数（前缀）
var trackingParams = []string{"spm", "scm", "utm_", "pvid", "abbucket", "ali_trackid", "ali_refid", "priceTId", "trackInfo", "xsec_"}

// redirectHosts 是广告跳转的域名：链接里只有加密的参数，看不出是哪个商品，跳过去的地方也不一定对
var redirectHosts = []string{"click.simba.taobao.com", "s.click.taobao.com", "ccc-x.jd.com", "union-click.jd.com"}

// cleanProductURL 去掉商品链接里不需要的参数和锚点。不是 http(s) 的、广告跳转的，返回 false
func cleanProductURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range redirectHosts {
		if host == h {
			return "", false
		}
	}
	u.Fragment = ""
	u.User = nil
	query := u.Query()
	if keep, ok := productParams[host]; ok {
		kept := url.Values{}
		for _, k := range keep {
			if v := query.Get(k); v != "" {
				kept.Set(k, v)
			}
		}
		query = kept
	} else {
		for k := range query {
			for _, p := range trackingParams {
				if strings.HasPrefix(k, p) {
					query.Del(k)
				}
			}
		}
	}
	u.RawQuery = query.Encode()
	return u.String(), true
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
