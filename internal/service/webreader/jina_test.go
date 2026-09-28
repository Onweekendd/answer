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

package webreader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apache/answer/internal/service/service_config"
	"github.com/stretchr/testify/require"
)

// newFakeJina starts a local server that plays Jina and points a reader at it.
// The returned request is what the client actually sent.
func newFakeJina(t *testing.T, apiKey string, status int, body string) (*jinaReader, *http.Request) {
	t.Helper()

	var seen http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	reader := newJina(apiKey)
	reader.endpoint = server.URL + "/"
	return reader, &seen
}

func Test_jinaReader_Read(t *testing.T) {
	t.Run("按纯文本要页面，并去掉空行和多余空格", func(t *testing.T) {
		// Arrange
		body := `{"code":200,"data":{"title":" 潘兴坦克 报价 ","httpStatus":200,
			"text":" \n   \n  太平洋电脑网   \n参考价： \n ￥1580  （行货价格）  平稳 \n\n"}}`
		reader, req := newFakeJina(t, "", http.StatusOK, body)

		// Act
		page, err := reader.Read(context.Background(), "https://product.pconline.com.cn/886741_price.html")

		// Assert
		require.NoError(t, err)
		require.Equal(t, "潘兴坦克 报价", page.Title)
		require.Equal(t, "太平洋电脑网\n参考价：\n￥1580 （行货价格） 平稳", page.Text)

		require.Equal(t, "/https://product.pconline.com.cn/886741_price.html", req.URL.Path)
		require.Equal(t, "application/json", req.Header.Get("Accept"))
		require.Equal(t, "text", req.Header.Get("X-Return-Format"))
		require.Equal(t, "none", req.Header.Get("X-Retain-Images"))
		require.Empty(t, req.Header.Get("Authorization"), "no key means no auth header")
	})

	t.Run("有 key 时带上 Bearer", func(t *testing.T) {
		// Arrange
		reader, req := newFakeJina(t, "jina-test-key", http.StatusOK, `{"code":200,"data":{"httpStatus":200}}`)

		// Act
		_, err := reader.Read(context.Background(), "https://a.example/")

		// Assert
		require.NoError(t, err)
		require.Equal(t, "Bearer jina-test-key", req.Header.Get("Authorization"))
	})

	t.Run("Jina 拒绝时把原因带进错误，但不带 key", func(t *testing.T) {
		// Arrange: the body Jina really sends when it refuses an address
		reader, _ := newFakeJina(t, "jina-test-key", http.StatusForbidden,
			`{"data":null,"code":403,"name":"AbuseAlleviationError","message":"Request to localhost or non-public IP"}`)

		// Act
		page, err := reader.Read(context.Background(), "http://localhost/x")

		// Assert
		require.Nil(t, page)
		require.ErrorContains(t, err, "status 403")
		require.ErrorContains(t, err, "non-public IP")
		require.NotContains(t, err.Error(), "jina-test-key")
	})

	t.Run("原网页是 404 时算失败", func(t *testing.T) {
		// Arrange: Jina itself answers 200 and reports the page's status inside
		reader, _ := newFakeJina(t, "", http.StatusOK, `{"code":200,"data":{"title":"Not Found","text":"404","httpStatus":404}}`)

		// Act
		page, err := reader.Read(context.Background(), "https://a.example/gone")

		// Assert
		require.Nil(t, page)
		require.ErrorContains(t, err, "page answered 404")
	})
}

func Test_NewReader(t *testing.T) {
	t.Run("没有配置时用不带 key 的 Jina", func(t *testing.T) {
		require.IsType(t, &jinaReader{}, NewReader(nil))
	})

	t.Run("provider 为 off 时关闭", func(t *testing.T) {
		// Arrange
		conf := &service_config.ServiceConfig{Reader: &service_config.ReaderConfig{Provider: "off"}}

		// Act
		page, err := NewReader(conf).Read(context.Background(), "https://a.example/")

		// Assert
		require.Nil(t, page)
		require.ErrorIs(t, err, ErrDisabled)
	})
}
