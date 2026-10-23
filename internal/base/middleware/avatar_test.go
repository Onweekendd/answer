/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements. See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership. The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License. You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAvatarMiddlewarePreservesStreamedUploadTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, item := range []struct{ path, contentType, body string }{
		{"/uploads/agent-files/page01.html", "text/html; charset=utf-8", "<!doctype html><h1>Preview</h1>"},
		{"/uploads/agent-files/table01.csv", "text/csv; charset=utf-8", "a,b"},
		{"/uploads/agent-files/data01.json", "application/json", "{}"},
		{"/uploads/agent/photo01.jpg", "image/jpeg", "jpeg"},
	} {
		t.Run(item.contentType, func(t *testing.T) {
			r := gin.New()
			r.Use((&AvatarMiddleware{}).AvatarThumb())
			r.GET(item.path, func(c *gin.Context) {
				// Gin's reader renderer preserves any header pre-set by middleware,
				// exactly like the production object-store response.
				c.DataFromReader(http.StatusOK, int64(len(item.body)), item.contentType, strings.NewReader(item.body), nil)
			})
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, item.path, nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, item.contentType, response.Header().Get("Content-Type"))
			require.Equal(t, item.body, response.Body.String())
		})
	}
}
