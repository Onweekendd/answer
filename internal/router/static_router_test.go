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
