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

// Package links 管 Agent 交给用户的链接：商品卡片的链接、交付网页里的 <a href>。
// 从页面上取下来的网址常带着 spm、追踪 id 这类参数，可能带着共享浏览器那个账号的信息，用户点开也用不上，
// 交给用户之前都要洗一遍（Clean）；比较两个网址是不是同一页用 Key
package links

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// productParams 是各个网站的商品链接里要留下的参数，别的（spm、追踪 id、搜索词）都去掉。没列出的网站去掉 trackingParams
var productParams = map[string][]string{
	"item.taobao.com":          {"id"},
	"detail.tmall.com":         {"id", "skuId"},
	"chaoshi.detail.tmall.com": {"id", "skuId"},
	"detail.tmall.hk":          {"id", "skuId"},
	"item.jd.com":              {},
	"item.jd.hk":               {},
	"npcitem.jd.hk":            {}, // 海外自营的详情页，网址后面带着 pcdk=…
	"www.ikea.cn":              {},
	"detail.1688.com":          {},
	"www.goofish.com":          {"id"}, // 闲鱼，网址里还有 spm、categoryId
	// 小红书的笔记网址要带 xsec_token 才能打开，去掉了就是「当前笔记暂时无法浏览」（300031，2026-10-20 实测）
	"www.xiaohongshu.com": {"xsec_token", "xsec_source"},
}

// trackingParams 是没列出的网站上要去掉的参数（前缀）
var trackingParams = []string{"spm", "scm", "utm_", "pvid", "abbucket", "ali_trackid", "ali_refid", "priceTId", "trackInfo"}

// redirectHosts 是广告跳转的域名：链接里只有加密的参数，看不出是哪个商品，跳过去的地方也不一定对
var redirectHosts = []string{"click.simba.taobao.com", "s.click.taobao.com", "ccc-x.jd.com", "union-click.jd.com", "dj.1688.com"}

// productPages 是认识的网站的商品页网址（清洗过以后）。这些网站上的链接不是商品页的一律不要：
// 京东的搜索结果卡片里唯一的链接是客服，模型在搜索页上不填 index 记下的是搜索页（2026-10-19 实测）。
// 不认识的网站不管
var productPages = []struct {
	domains []string
	page    *regexp.Regexp
}{
	{[]string{"jd.com", "jd.hk"}, regexp.MustCompile(`^https?://(item\.jd\.com|item\.jd\.hk|npcitem\.jd\.hk)/\d+\.html$`)},
	{[]string{"taobao.com"}, regexp.MustCompile(`^https?://item\.taobao\.com/item\.htm\?id=\d+$`)},
	{[]string{"tmall.com", "tmall.hk"}, regexp.MustCompile(`^https?://(chaoshi\.)?detail\.tmall\.(com|hk)/item\.htm\?id=\d+(&skuId=\d+)?$`)},
	{[]string{"ikea.cn"}, regexp.MustCompile(`^https?://www\.ikea\.cn/cn/zh/p/[^/?]+/?$`)},
	{[]string{"1688.com"}, regexp.MustCompile(`^https?://detail\.1688\.com/offer/\d+\.html$`)},
	{[]string{"goofish.com"}, regexp.MustCompile(`^https?://www\.goofish\.com/item\?id=\d+$`)},
}

// IsProductPage 说一个清洗过的链接是不是商品页：认识的网站要对得上格式，不认识的都算
func IsProductPage(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, site := range productPages {
		for _, d := range site.domains {
			if host == d || strings.HasSuffix(host, "."+d) {
				return site.page.MatchString(link)
			}
		}
	}
	return true
}

// Clean 去掉链接里不需要的参数和锚点。不是 http(s) 的、广告跳转的，返回 false
func Clean(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if slices.Contains(redirectHosts, host) {
		return "", false
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

// Key 是比较两个网址是不是同一个页面用的：洗掉追踪参数，不管 http 还是 https、末尾的斜杠。
// 小红书笔记的 xsec_token 每次打开都不一样，比较时也不算。不是 http(s) 的返回空字符串
func Key(raw string) string {
	clean, ok := Clean(raw)
	if !ok {
		return ""
	}
	u, err := url.Parse(clean)
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Del("xsec_token")
	query.Del("xsec_source")
	key := strings.ToLower(u.Host) + strings.TrimSuffix(u.EscapedPath(), "/")
	if q := query.Encode(); q != "" {
		key += "?" + q
	}
	return key
}

// tokenParams 是缺了页面就打不开的参数：只有小红书用，别的网站这些参数都是追踪用的，不补
var tokenParams = []string{"xsec_token", "xsec_source"}

// Complete 用这次真的打开过的完整网址（opened）补全模型写的网址（link）：
// 模型抄网址时常把 ?xsec_token=… 当成噪音省掉，裸链接在小红书上是「当前笔记暂时无法浏览」。
// 只在两个网址是同一个页面（Key 相同）、link 没带 xsec_token、opened 带着的时候才补，其余原样返回
func Complete(link, opened string) string {
	if opened == "" || Key(link) == "" || Key(link) != Key(opened) {
		return link
	}
	l, err := url.Parse(strings.TrimSpace(link))
	if err != nil || l.Query().Get("xsec_token") != "" {
		return link
	}
	o, err := url.Parse(opened)
	if err != nil || o.Query().Get("xsec_token") == "" {
		return link
	}
	query := l.Query()
	for _, k := range tokenParams {
		if v := o.Query().Get(k); v != "" {
			query.Set(k, v)
		}
	}
	l.RawQuery = query.Encode()
	return l.String()
}

// bareURL 是文字里的一个网址：到空白、引号、尖括号、中文标点、括号、* 和反引号为止
var bareURL = regexp.MustCompile("https?://[^\\s<>\"'`*()（）【】「」『』“”‘’，。、；：！？]+")

// CompleteText 把一段回答里的每个网址都 Complete 一遍。opened 按 Key 给出这次打开过的完整网址，没有就返回空
func CompleteText(text string, opened func(key string) string) string {
	return bareURL.ReplaceAllStringFunc(text, func(link string) string {
		return Complete(link, opened(Key(link)))
	})
}
