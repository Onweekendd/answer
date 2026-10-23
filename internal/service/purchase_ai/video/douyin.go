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

package video

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// 抖音（2026-10-22 试通）：视频页一打开，页面自己会调 aweme/detail 接口，返回里有视频的信息和
// 签过名的 CDN 地址。我们在用户的浏览器里旁听这个返回（插件 netcapture），不自己发请求：
// 请求要页面现算的 a_bogus 签名和 cookie，抖音常改算法。
// 这个接口很乱（一份 12 万字，大部分是开关、风控、推荐用的字段），只取用得上的几个，别的一概不管

// DouyinPage 是抖音视频页，第 1 组是视频的 id
var DouyinPage = regexp.MustCompile(`^https://www\.douyin\.com/video/(\d{6,25})(?:[/?#]|$)`)

// DouyinDetailAPI 是视频页调的接口。旁听时只抄它：别的接口（user/settings 这类）带着账号信息
const DouyinDetailAPI = "/aweme/v1/web/aweme/detail/"

// douyinHosts 是抖音视频文件所在的 CDN
var douyinHosts = []string{"zjcdn.com", "douyinvod.com", "douyincdn.com"}

// DouyinPageOf 是视频 id 对应的视频页：出处
func DouyinPageOf(id string) string {
	return "https://www.douyin.com/video/" + id
}

// DouyinHostOK 说一个地址能不能下载：https，并且在抖音的视频 CDN 上
func DouyinHostOK(link string) bool {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range douyinHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// aweme 是一个抖音作品，只取用得上的字段。字段随时可能缺，翻译时都要容得下
type aweme struct {
	ID     string `json:"aweme_id"`
	Desc   string `json:"desc"` // 文案，标题和 #话题 都在里面
	Author struct {
		Nickname string `json:"nickname"`
	} `json:"author"`
	Video struct {
		Duration int       `json:"duration"`  // 毫秒
		PlayAddr playAddr  `json:"play_addr"` // 默认那一档，一般是原画
		BitRate  []awemeBR `json:"bit_rate"`  // 各档清晰度
	} `json:"video"`
}

// awemeBR 是一档清晰度
type awemeBR struct {
	GearName string   `json:"gear_name"` // 比如 normal_540_0、normal_1080_0
	PlayAddr playAddr `json:"play_addr"`
}

// playAddr 是一个视频文件
type playAddr struct {
	URLList  []string `json:"url_list"`
	DataSize int      `json:"data_size"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
}

func (p playAddr) variant(name string) Variant {
	return Variant{Name: name, URLs: p.URLList, Size: p.DataSize, Width: p.Width, Height: p.Height}
}

// ParseDouyin 从旁听到的接口返回里找出 id 这个视频。找不到（被验证码挡住了、返回的是别的视频）返回 false
func ParseDouyin(body []byte, id string) (Source, bool) {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return Source{}, false
	}
	for _, a := range findAwemes(v) {
		if a.ID != id {
			continue
		}
		s := Source{
			Site: "douyin", ID: a.ID, Page: DouyinPageOf(a.ID),
			Title: a.Desc, Author: a.Author.Nickname,
			Duration: time.Duration(a.Video.Duration) * time.Millisecond,
			Default:  a.Video.PlayAddr.variant("default"),
		}
		for _, br := range a.Video.BitRate {
			s.Variants = append(s.Variants, br.PlayAddr.variant(br.GearName))
		}
		return s, true
	}
	return Source{}, false
}

// findAwemes 在任意 JSON 里找带 aweme_id 和 video.play_addr 的对象。不写死路径：
// 视频页的接口是 aweme_detail，搜索接口是一个列表里套着 aweme_info，外面包的层随时会变
func findAwemes(v any) []aweme {
	var out []aweme
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if _, ok := x["aweme_id"]; ok {
				if video, ok := x["video"].(map[string]any); ok && video["play_addr"] != nil {
					raw, _ := json.Marshal(x)
					var a aweme
					if json.Unmarshal(raw, &a) == nil {
						out = append(out, a)
						return
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}
