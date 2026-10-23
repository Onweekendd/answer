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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDouyin(t *testing.T) {
	// Arrange：视频页接口的样子，外面包一层 aweme_detail；旁边还有一个搜索接口那样套在列表里的别的视频
	body := []byte(`{"status_code":0,
		"aweme_detail":{"aweme_id":"7696338249064795407","desc":"扫地机器人实测 #测评","author":{"nickname":"测评君"},
			"video":{"duration":644000,
				"play_addr":{"url_list":["https://v5.zjcdn.com/default.mp4"],"data_size":128261735,"width":1080,"height":1920},
				"bit_rate":[{"gear_name":"normal_540_0","bit_rate":600000,"play_addr":{"url_list":["https://v5.zjcdn.com/540.mp4"],"data_size":48000000,"width":540,"height":960}}]}},
		"data":[{"aweme_info":{"aweme_id":"2","video":{"play_addr":{"url_list":[]}}}}],
		"log_pb":{"impr_id":"x"}}`)

	// Act
	s, ok := ParseDouyin(body, "7696338249064795407")

	// Assert
	require.True(t, ok)
	assert.Equal(t, Source{
		Site: "douyin", ID: "7696338249064795407", Page: "https://www.douyin.com/video/7696338249064795407",
		Title: "扫地机器人实测 #测评", Author: "测评君", Duration: 644 * time.Second,
		Variants: []Variant{{Name: "normal_540_0", URLs: []string{"https://v5.zjcdn.com/540.mp4"}, Size: 48000000, Width: 540, Height: 960}},
		Default:  Variant{Name: "default", URLs: []string{"https://v5.zjcdn.com/default.mp4"}, Size: 128261735, Width: 1080, Height: 1920},
	}, s)
}

func TestParseDouyin_notThere(t *testing.T) {
	// 返回的是别的视频、是图文（没有 play_addr）、根本不是 JSON（验证码页）：都找不到
	for _, body := range []string{
		`{"aweme_detail":{"aweme_id":"2","video":{"play_addr":{"url_list":["https://v5.zjcdn.com/2.mp4"]}}}}`,
		`{"aweme_detail":{"aweme_id":"1","images":[{"url":"x"}],"video":{}}}`,
		`<html>验证码</html>`,
	} {
		_, ok := ParseDouyin([]byte(body), "1")
		assert.False(t, ok, body)
	}
}

func TestDouyinHostOK(t *testing.T) {
	for _, tt := range []struct {
		link string
		want bool
	}{
		{"https://v5-dy-ov-experiment.zjcdn.com/abc/video/tos/1.mp4", true},
		{"https://v3-web.douyinvod.com/abc/1.mp4", true},
		{"http://v5.zjcdn.com/1.mp4", false},            // 不是 https
		{"https://zjcdn.com.evil.example/1.mp4", false}, // 只是名字里带着
		{"https://www.douyin.com/aweme/v1/play/?video_id=1", false},
		{"javascript:alert(1)", false},
	} {
		assert.Equal(t, tt.want, DouyinHostOK(tt.link), tt.link)
	}
}

func TestDouyinPage(t *testing.T) {
	for _, tt := range []struct {
		link string
		id   string
	}{
		{"https://www.douyin.com/video/7696338249064795407", "7696338249064795407"},
		{"https://www.douyin.com/video/7696338249064795407?modeFrom=search", "7696338249064795407"},
		{"https://www.douyin.com/search/扫地机", ""},
		{"http://www.douyin.com/video/7696338249064795407", ""},
		{"https://www.douyin.com/video/7696338249064795407abc", ""},
	} {
		m := DouyinPage.FindStringSubmatch(tt.link)
		got := ""
		if m != nil {
			got = m[1]
		}
		assert.Equal(t, tt.id, got, tt.link)
	}
}
