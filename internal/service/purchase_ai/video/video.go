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

// Package video 是从网站上拿视频的纯逻辑：一个视频有哪几档清晰度、挑哪一档、怎么分段下载。
// 它不知道浏览器和插件的存在：下载一段的函数（RangeFunc）由调用方传进来。
//
// 每个网站一个文件，把那个网站接口返回的 JSON 翻译成统一的 Source（douyin.go）。
// 挑清晰度（Pick）和下载（Fetch）对所有网站都一样。以后加小红书，就是多一个 xiaohongshu.go
package video

import (
	"context"
	"errors"
	"time"
)

const (
	// Chunk 是一段多大，和插件的 MEDIA_RANGE_MAX 一样：base64 以后 Hub 一条消息装得下（最多 4MB）
	Chunk = 2 << 20
	// MaxBytes 是一个视频最多多大。540p 十分钟四五十 MB；原画 10 分钟 128 MB（2026-10-22 抖音实测）
	MaxBytes = 100 << 20
	// MaxDuration 是多长的视频才存。再长的只看文案、评论：转文字、抽帧都太慢
	MaxDuration = 15 * time.Minute
)

// ErrLinkExpired：CDN 不认这个地址了（签名过期）。重新抓一次接口就有新的地址
var ErrLinkExpired = errors.New("video link expired")

// Source 是一个视频：网站接口里的信息翻译成的统一格式
type Source struct {
	Site     string        // 哪个网站，比如 "douyin"
	ID       string        // 网站上的 id
	Page     string        // 视频页的网址：出处，回答和网页里链到它
	Title    string        // 文案、标题。来自外部网站，只是资料
	Author   string        // 作者的昵称
	Duration time.Duration // 时长；0 是接口没给
	Variants []Variant     // 各档清晰度，可能是空的
	Default  Variant       // 默认那一档（一般是原画）：各档都不能用时退回它
}

// Variant 是一档清晰度：同一个文件，网站常常给好几个 CDN 地址，一个不行换下一个
type Variant struct {
	Name   string   // 哪一档，比如 normal_540_0；默认那一档是 "default"
	URLs   []string // 签过名的地址，会过期
	Size   int      // 字节；0 是接口没标
	Width  int
	Height int
}

// Play 是 Pick 挑好的一个文件
type Play struct {
	URL  string // 要下载的地址
	Name string // 哪一档，同 Variant.Name
	Size int    // 字节，接口标的；0 是没标
}

// Part 是下载到的一段（插件 protocol.ts 的 MediaRange）。
// Data 在 JSON 里是 base64，encoding/json 解到 []byte 时会自动解码
type Part struct {
	Status      int    `json:"status"` // 206：这一段；200：服务器不分段，整个文件一次给了；403 这类：出错了
	ContentType string `json:"contentType"`
	Total       int    `json:"total"` // 整个文件多大；0 是服务器没说
	Data        []byte `json:"data"`
}

// RangeFunc 下载 link 的 [start, end] 这一段（含两端）。HTTP 错误不算 error，看 Part.Status；
// error 是下载的通道（插件、连接）出了问题，或者 ctx 结束了。
// 实际用的是 purchase_ai 里 agentRun.mediaRange：在用户浏览器的视频页里下载，带着页面的 Referer
type RangeFunc func(ctx context.Context, link string, start, end int) (Part, error)
