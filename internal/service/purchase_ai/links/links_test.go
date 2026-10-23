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

package links

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClean(t *testing.T) {
	cases := []struct {
		name, raw, want string
		ok              bool
	}{
		{"天猫只留 id 和 skuId", "https://detail.tmall.com/item.htm?id=6123&skuId=5566&spm=a1z10&ali_refid=x#detail", "https://detail.tmall.com/item.htm?id=6123&skuId=5566", true},
		{"淘宝只留 id", "https://item.taobao.com/item.htm?ns=1&abbucket=3&id=7788", "https://item.taobao.com/item.htm?id=7788", true},
		{"京东去掉全部参数", "https://item.jd.com/100012.html?bbtf=1&spm=x", "https://item.jd.com/100012.html", true},
		{"京东海外自营也去掉", "https://npcitem.jd.hk/100378972868.html?pcdk=abc", "https://npcitem.jd.hk/100378972868.html", true},
		{"没列出的网站只去掉追踪参数", "https://shop.example.com/p?sku=9&utm_source=a&spm=b", "https://shop.example.com/p?sku=9", true},
		{"广告跳转不要", "https://click.simba.taobao.com/cc_im?p=xx&s=yy", "", false},
		{"闲鱼只留 id", "https://www.goofish.com/item?spm=a21ybx.search.searchFeedList.14&id=1083647455304&categoryId=50023914", "https://www.goofish.com/item?id=1083647455304", true},
		{"1688 的广告跳转也不要", "https://dj.1688.com/ci_bb?a=7255&e=x", "", false},
		{"1688 商品页去掉参数", "http://detail.1688.com/offer/828190408511.html?sortType=&pageId=&abBizDataType=cbuOffer", "http://detail.1688.com/offer/828190408511.html", true},
		{"小红书留着 xsec_token，不然笔记打不开", "https://www.xiaohongshu.com/explore/6ad5?xsec_token=AB1=&xsec_source=pc_feed&source=web_explore_feed", "https://www.xiaohongshu.com/explore/6ad5?xsec_source=pc_feed&xsec_token=AB1%3D", true},
		{"不是网页地址不要", "javascript:alert(1)", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			got, ok := Clean(c.raw)

			// Assert
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestIsProductPage(t *testing.T) {
	cases := []struct {
		link string
		want bool
	}{
		{"https://item.jd.com/100378972868.html", true},
		{"https://search.jd.com/Search?keyword=ipad", false},
		{"https://chat.jd.com/index.action", false},
		{"https://mall.jd.com/index-1000000.html", false},
		{"https://item.taobao.com/item.htm?id=7788", true},
		{"https://s.taobao.com/search?q=ipad", false},
		{"https://detail.tmall.com/item.htm?id=6123&skuId=5566", true},
		{"https://muji.tmall.com/shop/view_shop.htm", false},
		{"https://www.ikea.cn/cn/zh/p/fjaellarnika-30459285/", true},
		{"https://www.ikea.cn/cn/zh/search/products/?q=x", false},
		{"http://detail.1688.com/offer/828190408511.html", true},
		{"https://s.1688.com/selloffer/offer_search.htm?keywords=x", false},
		{"https://air.1688.com/kapp/1688-search/pc-image-search/", false},
		{"https://www.goofish.com/item?id=1083647455304", true},
		{"https://www.goofish.com/search?q=ipad", false},
		{"https://shop.example.com/anything", true}, // 不认识的网站不管
	}
	for _, c := range cases {
		t.Run(c.link, func(t *testing.T) {
			assert.Equal(t, c.want, IsProductPage(c.link))
		})
	}
}

func TestComplete(t *testing.T) {
	opened := "https://www.xiaohongshu.com/explore/6a91?xsec_source=pc_search&xsec_token=AB%3D"
	cases := []struct {
		name, link, opened, want string
	}{
		{"裸链接补上 token", "https://www.xiaohongshu.com/explore/6a91", opened, opened},
		{"页面上已经有 token 的不动", "https://www.xiaohongshu.com/explore/6a91?xsec_token=PAGE", opened, "https://www.xiaohongshu.com/explore/6a91?xsec_token=PAGE"},
		{"不是同一篇笔记不补", "https://www.xiaohongshu.com/explore/other", opened, "https://www.xiaohongshu.com/explore/other"},
		{"没打开过不补", "https://www.xiaohongshu.com/explore/6a91", "", "https://www.xiaohongshu.com/explore/6a91"},
		{"打开的网址也没有 token 就没得补", "https://www.xiaohongshu.com/explore/6a91", "https://www.xiaohongshu.com/explore/6a91", "https://www.xiaohongshu.com/explore/6a91"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Complete(c.link, c.opened))
		})
	}
}

func TestCompleteText(t *testing.T) {
	// Arrange：打开过 6a91，没打开过 6a93
	opened := func(key string) string {
		if key == Key("https://www.xiaohongshu.com/explore/6a91") {
			return "https://www.xiaohongshu.com/explore/6a91?xsec_source=pc_search&xsec_token=AB%3D"
		}
		return ""
	}
	text := "吐槽最多的是这篇（https://www.xiaohongshu.com/explore/6a91 ，已开抢不到）；" +
		"[另一篇](https://www.xiaohongshu.com/explore/6a93)；结尾 https://www.xiaohongshu.com/explore/6a91。"

	// Act
	got := CompleteText(text, opened)

	// Assert：补全的链接前后的标点不被吞掉；没打开过的原样
	want := "吐槽最多的是这篇（https://www.xiaohongshu.com/explore/6a91?xsec_source=pc_search&xsec_token=AB%3D ，已开抢不到）；" +
		"[另一篇](https://www.xiaohongshu.com/explore/6a93)；结尾 https://www.xiaohongshu.com/explore/6a91?xsec_source=pc_search&xsec_token=AB%3D。"
	assert.Equal(t, want, got)
}
