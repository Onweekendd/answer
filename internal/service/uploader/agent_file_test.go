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

package uploader

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckAgentFile(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000IHDR")
	cases := []struct {
		name string
		ext  string
		data []byte
		ok   bool
	}{
		{"网页", ".html", []byte("<!doctype html><h1>清单</h1>"), true},
		{"扩展名大写也认", ".HTML", []byte("<h1>清单</h1>"), true},
		{"图片要真的是图片", ".png", png, true},
		{"网页起了图片的名字：不收，不然 .png 里藏着网页", ".png", []byte("<html><script>alert(1)</script>"), false},
		{"svg 不在白名单：它是能跑脚本的文档", ".svg", []byte("<svg onload=alert(1)>"), false},
		{"网页里是二进制：不收", ".html", png, false},
		{"空文件不收", ".html", nil, false},
		{"太大不收", ".html", bytes.Repeat([]byte("a"), AgentFileMaxBytes+1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			err := CheckAgentFile(tc.ext, tc.data)

			// Assert
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestAgentFileName(t *testing.T) {
	assert.True(t, AgentFileName.MatchString("8kTq2xYz1AbC.html"))
	assert.True(t, AgentFileName.MatchString("8kTq2xYz1AbC.png"))
	for _, name := range []string{"../conf.yaml", "a.svg", "a-b.html", "a.html.exe", ".html"} {
		assert.False(t, AgentFileName.MatchString(name), name)
	}
}
