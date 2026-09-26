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

package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// a trimmed copy of a real Bocha response
const bochaOKBody = `{
  "code": 200,
  "msg": null,
  "data": {
    "webPages": {
      "value": [
        {
          "name": "任天堂 Switch OLED 港版 多少钱-聚超值",
          "url": "https://best.pconline.com.cn/youhui/15659719.html",
          "siteName": "太平洋聚超值",
          "snippet": "京东现价1849.0元,价格给力",
          "summary": "京东现价1849.0元,价格给力,喜欢的聚友可以入手。OLED版机型在硬件上进行了全方面的调整。",
          "datePublished": "2025-11-12T02:17:06+08:00"
        },
        {
          "name": "任天堂游戏机已解决相关问题-ZOL问答",
          "url": "https://ask.zol.com.cn/product_1158152/t_1.html",
          "siteName": "ZOL中关村在线问答",
          "snippet": "二手Switch OLED价格多在1100-1500元",
          "summary": "",
          "datePublished": "not a date"
        }
      ]
    }
  }
}`

// newFakeBocha starts a local server that plays Bocha and points a provider at
// it. seen receives what the client actually sent, so a test can check the
// request as well as the parsing.
func newFakeBocha(t *testing.T, status int, body string) (*bochaProvider, *http.Request, *bochaRequest) {
	t.Helper()

	var seenReq http.Request
	var seenBody bochaRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenReq = *r
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	provider := newBocha("test-key")
	provider.endpoint = server.URL
	return provider, &seenReq, &seenBody
}

func Test_bochaProvider_Search(t *testing.T) {
	t.Run("把博查的结果转成 Result", func(t *testing.T) {
		// Arrange
		provider, req, body := newFakeBocha(t, http.StatusOK, bochaOKBody)

		// Act
		results, err := provider.Search(context.Background(), "Switch OLED 值得买吗",
			Options{Count: 3, Freshness: FreshnessYear})

		// Assert
		require.NoError(t, err)
		require.Len(t, results, 2)

		require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		require.Equal(t, bochaRequest{
			Query:     "Switch OLED 值得买吗",
			Freshness: "oneYear",
			Summary:   true,
			Count:     3,
		}, *body)

		first := results[0]
		require.Equal(t, "任天堂 Switch OLED 港版 多少钱-聚超值", first.Title)
		require.Equal(t, "https://best.pconline.com.cn/youhui/15659719.html", first.URL)
		require.Equal(t, "太平洋聚超值", first.SiteName)
		require.Equal(t, "京东现价1849.0元,价格给力,喜欢的聚友可以入手。OLED版机型在硬件上进行了全方面的调整。", first.Summary)
		require.False(t, first.PublishedAt.IsZero())

		// an empty summary falls back to the snippet, and a bad date is left zero
		// instead of failing the search
		second := results[1]
		require.Equal(t, "二手Switch OLED价格多在1100-1500元", second.Summary)
		require.True(t, second.PublishedAt.IsZero())
	})

	t.Run("没传 Count 时用默认条数", func(t *testing.T) {
		// Arrange
		provider, _, body := newFakeBocha(t, http.StatusOK, bochaOKBody)

		// Act
		_, err := provider.Search(context.Background(), "Switch OLED", Options{})

		// Assert
		require.NoError(t, err)
		require.Equal(t, bochaDefaultCount, body.Count)
		require.Equal(t, "noLimit", body.Freshness)
	})

	t.Run("key 无效时把博查给的原因带进错误", func(t *testing.T) {
		// Arrange: the body Bocha really sends for a bad key; note code is a string here
		provider, _, _ := newFakeBocha(t, http.StatusUnauthorized,
			`{"code":"401","log_id":"ce287cf2d242ee30","message":"Invalid API KEY"}`)

		// Act
		results, err := provider.Search(context.Background(), "Switch OLED", Options{})

		// Assert
		require.Nil(t, results)
		require.ErrorContains(t, err, "status 401")
		require.ErrorContains(t, err, "Invalid API KEY")
		require.NotContains(t, err.Error(), "test-key")
	})

	t.Run("服务器 500 时返回错误", func(t *testing.T) {
		// Arrange
		provider, _, _ := newFakeBocha(t, http.StatusInternalServerError, "")

		// Act
		results, err := provider.Search(context.Background(), "Switch OLED", Options{})

		// Assert
		require.Nil(t, results)
		require.ErrorContains(t, err, "status 500")
	})

	t.Run("HTTP 200 但 body 里 code 报错时也返回错误", func(t *testing.T) {
		// Arrange
		provider, _, _ := newFakeBocha(t, http.StatusOK, `{"code":403,"msg":"余额不足"}`)

		// Act
		results, err := provider.Search(context.Background(), "Switch OLED", Options{})

		// Assert
		require.Nil(t, results)
		require.ErrorContains(t, err, "api error 403")
		require.ErrorContains(t, err, "余额不足")
	})
}

func Test_NewProvider_disabledWithoutKey(t *testing.T) {
	// Arrange: no search section at all
	provider := NewProvider(nil)

	// Act
	results, err := provider.Search(context.Background(), "anything", Options{})

	// Assert
	require.ErrorIs(t, err, ErrDisabled)
	require.Empty(t, results)
}
