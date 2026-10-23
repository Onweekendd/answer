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
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPurchaseAIRoutesWithoutLegacyPostAnalysis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := &AnswerAPIRouter{}
	// Only inspect registration; the retained controllers are not invoked.
	api.RegisterAnswerAPIRouter(r.Group("/answer/api/v1"))

	request := httptest.NewRequest(http.MethodPost, "/answer/api/v1/purchase/ai/analyze", strings.NewReader(`{"question_id":"42"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusNotFound, response.Code, "the retired retry endpoint must not enqueue another analysis")

	posts := make(map[string]bool)
	for _, route := range r.Routes() {
		if route.Method == http.MethodPost {
			posts[route.Path] = true
		}
	}
	for _, path := range []string{"/question", "/purchase/vote", "/purchase/ai/chat", "/purchase/ai/identify", "/purchase/ai/draft/result"} {
		require.True(t, posts["/answer/api/v1"+path], "retained endpoint %s", path)
	}
}
