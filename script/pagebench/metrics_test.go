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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnalyze(t *testing.T) {
	t.Run("统计行数、字数、重复、空行和截断", func(t *testing.T) {
		// Arrange：轮播图的同一张幻灯片出现两次，一个空链接，一行被截断
		elements := "[0]<a>首页\n" +
			"[1]<a>当产物就是催化剂\n" +
			"[2]<a>\n" +
			"[3]<a>当产物就是催化剂\n" +
			"[4]<a>很长的标题…"

		// Act
		m := Analyze(elements)

		// Assert
		assert.Equal(t, 5, m.Lines)
		assert.Equal(t, 1, m.Duplicates)
		assert.Equal(t, 1, m.Empty)
		assert.Equal(t, 1, m.Clipped)
		assert.Equal(t, len([]rune(elements)), m.Chars)
	})

	t.Run("属性不同的两行不算重复", func(t *testing.T) {
		m := Analyze("[0]<a id=a>搜索\n[1]<a id=b>搜索")

		assert.Equal(t, 0, m.Duplicates)
	})

	t.Run("有属性但没有文字的也算空行", func(t *testing.T) {
		m := Analyze("[0]<button aria-label=关闭>")

		assert.Equal(t, 1, m.Empty)
	})

	t.Run("空快照所有指标都是 0", func(t *testing.T) {
		assert.Equal(t, Metrics{}, Analyze(""))
	})
}

func TestBlockedReason(t *testing.T) {
	tests := []struct {
		name, url, title string
		blocked          bool
	}{
		{"淘宝登录页", "https://login.taobao.com/member/login.jhtml", "淘宝网 - 淘！我喜欢", true},
		{"京东登录页", "https://passport.jd.com/new/login.aspx", "京东-欢迎登录", true},
		{"路径里有 login", "https://www.1688.com/member/login.htm", "", true},
		{"标题里有验证", "https://s.taobao.com/search?q=x", "安全验证", true},
		{"正常的搜索页", "https://search.jd.com/Search?keyword=iphone", "iphone - 商品搜索 - 京东", false},
		{"正常的首页", "https://www.1688.com/", "阿里1688", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.blocked, BlockedReason(tt.url, tt.title) != "")
		})
	}
}

func TestRedact(t *testing.T) {
	t.Run("把昵称替换掉，空的词跳过", func(t *testing.T) {
		got := Redact("[3]<a>你好，小明同学 退出", []string{"小明同学", " ", ""})

		assert.Equal(t, "[3]<a>你好，[已屏蔽] 退出", got)
	})
}
