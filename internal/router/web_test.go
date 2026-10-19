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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func webServer(t *testing.T) *gin.Engine {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "index-abc.js"), []byte("js"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("secret"), 0o644))
	t.Setenv(webDirEnv, dir)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.True(t, RegisterWeb(r))
	return r
}

func get(r *gin.Engine, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func Test_RegisterWeb(t *testing.T) {
	t.Run("文件存在就返回文件，打包出来的资源长期缓存", func(t *testing.T) {
		w := get(webServer(t), "/app/assets/index-abc.js")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "js", w.Body.String())
		assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
	})

	t.Run("前端路由返回 index.html，并且不缓存", func(t *testing.T) {
		for _, target := range []string{"/app/", "/app/login", "/app/community"} {
			w := get(webServer(t), target)

			assert.Equal(t, http.StatusOK, w.Code, target)
			assert.Equal(t, "<html>app</html>", w.Body.String(), target)
			assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"), target)
		}
	})

	t.Run("不存在的资源是 404，不给 HTML", func(t *testing.T) {
		w := get(webServer(t), "/app/assets/old-123.js")

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("出不了目录", func(t *testing.T) {
		w := get(webServer(t), "/app/../secret.txt")

		assert.NotContains(t, w.Body.String(), "secret")
	})

	t.Run("/app 跳到 /app/", func(t *testing.T) {
		w := get(webServer(t), "/app")

		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		assert.Equal(t, "/app/", w.Header().Get("Location"))
	})

	t.Run("没有打包结果就不注册", func(t *testing.T) {
		t.Setenv(webDirEnv, t.TempDir())

		assert.False(t, RegisterWeb(gin.New()))
	})
}
