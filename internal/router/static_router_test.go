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

package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/service/service_config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_agentImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uploadPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(uploadPath, constant.AgentSubPath), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uploadPath, constant.AgentSubPath, "abc123.jpg"), []byte("jpeg"), 0o644))
	r := gin.New()
	NewStaticRouter(&service_config.ServiceConfig{UploadPath: uploadPath}, nil).RegisterStaticRouter(r.Group(""))

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	t.Run("没有对象存储时从磁盘读：以前存的图照样能看", func(t *testing.T) {
		// Act
		w := get("/uploads/agent/abc123.jpg")

		// Assert
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "jpeg", w.Body.String())
		assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
	})

	t.Run("不像 Agent 起的名字一律 404", func(t *testing.T) {
		for _, path := range []string{"/uploads/agent/..%2Fconf.yaml", "/uploads/agent/a.html", "/uploads/agent/missing.jpg"} {
			// Act
			w := get(path)

			// Assert
			assert.Equal(t, http.StatusNotFound, w.Code, path)
		}
	})
}

func Test_agentFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uploadPath := t.TempDir()
	dir := filepath.Join(uploadPath, constant.AgentFilesSubPath)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, body := range map[string]string{
		"card01.png":   "png",
		"page01.html":  "<h1>清单</h1>",
		"deck01.pptx":  "pptx",
		"table01.csv":  "a,b",
		"notes01.json": "{}",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	r := gin.New()
	NewStaticRouter(&service_config.ServiceConfig{UploadPath: uploadPath}, nil).RegisterStaticRouter(r.Group(""))

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	t.Run("图片：类型按白名单给，不许浏览器猜，永久缓存", func(t *testing.T) {
		// Act
		w := get("/uploads/agent-files/card01.png")

		// Assert
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "png", w.Body.String())
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
		assert.Equal(t, "public, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
		assert.Empty(t, w.Header().Get("Content-Disposition"))
	})

	t.Run("网页：带上隔离用的 CSP，脚本读不到我们的登录态，也发不出请求", func(t *testing.T) {
		// Act
		w := get("/uploads/agent-files/page01.html")

		// Assert
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
		csp := w.Header().Get("Content-Security-Policy")
		assert.Equal(t, htmlSandboxCSP, csp)
		assert.Contains(t, csp, "sandbox allow-scripts")
		assert.Contains(t, csp, "allow-popups-to-escape-sandbox", "出处链接要能在新标签页打开")
		assert.NotContains(t, csp, "allow-same-origin", "加了它，sandbox 就形同虚设")
		assert.Contains(t, csp, "default-src 'none'")
	})

	t.Run("别的类型不带 CSP：图片、表格不会执行脚本", func(t *testing.T) {
		for _, path := range []string{"/uploads/agent-files/card01.png", "/uploads/agent-files/table01.csv"} {
			// Act
			w := get(path)

			// Assert
			assert.Empty(t, w.Header().Get("Content-Security-Policy"), path)
		}
	})

	t.Run("Office 文件直接下载，不在浏览器里打开", func(t *testing.T) {
		// Act
		w := get("/uploads/agent-files/deck01.pptx")

		// Assert
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/vnd.openxmlformats-officedocument.presentationml.presentation", w.Header().Get("Content-Type"))
		assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	})

	t.Run("想跳出目录的连路由都匹配不上，404", func(t *testing.T) {
		// Act
		w := get("/uploads/agent-files/..%2Fconf.yaml")

		// Assert
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("名字不对、类型不在白名单、文件不存在：一律 404，而且不缓存", func(t *testing.T) {
		for _, path := range []string{
			"/uploads/agent-files/evil01.svg",
			"/uploads/agent-files/page-01.html",
			"/uploads/agent-files/missing01.png",
		} {
			// Act
			w := get(path)

			// Assert
			assert.Equal(t, http.StatusNotFound, w.Code, path)
			assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), path)
		}
	})
}
