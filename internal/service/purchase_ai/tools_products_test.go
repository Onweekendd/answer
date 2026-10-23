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

package purchaseai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeImageStore 记下存过的图，返回一个假网址
type fakeImageStore struct {
	saved [][]byte
	err   error
}

func (f *fakeImageStore) SaveAgentImage(_ context.Context, data []byte) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.saved = append(f.saved, data)
	return "https://buy.example.com/uploads/agent/1.jpg", nil
}

func Test_pickProduct(t *testing.T) {
	t.Run("在搜索结果里填编号：链接和图来自页面，追踪参数被洗掉，事件推给前端", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		store := &fakeImageStore{}
		r.images = store
		events := make(chan Event, 4)
		r.events = events

		// Act
		res, err := r.pickProduct(context.Background(), `{"index":3,"title":"Apple iPhone 17 256G","shop":"京东自营","price":"¥5999","note":"自营，发货快"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Contains(t, res.Text, "已记下「Apple iPhone 17 256G」")
		assert.Equal(t, [][]byte{[]byte("jpeg")}, store.saved)
		require.Len(t, r.products, 1)
		card := r.products[0]
		assert.Equal(t, "https://item.jd.com/103.html", card.URL)
		assert.Equal(t, "https://buy.example.com/uploads/agent/1.jpg", card.Image)
		assert.Equal(t, 1, card.ID)
		ev := <-events
		assert.Equal(t, EventProduct, ev.Type)
		assert.Equal(t, card, *ev.Product)
	})

	t.Run("同一个商品再做一次，更新原来那张，不重复", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		_, _ = r.pickProduct(context.Background(), `{"index":3,"title":"旧标题","price":"¥5999"}`)

		// Act
		res, _ := r.pickProduct(context.Background(), `{"index":3,"title":"新标题","price":"¥5899","priceNote":"券后"}`)

		// Assert
		require.False(t, res.Failed)
		require.Len(t, r.products, 1)
		assert.Equal(t, "新标题", r.products[0].Title)
		assert.Equal(t, 1, r.products[0].ID)
	})

	t.Run("存图失败也照样做成卡片，只是没有图", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		r.images = &fakeImageStore{err: errors.New("disk full")}

		// Act
		res, _ := r.pickProduct(context.Background(), `{"index":3,"title":"商品","price":"¥1"}`)

		// Assert
		require.False(t, res.Failed)
		assert.Empty(t, r.products[0].Image)
		assert.Contains(t, res.Text, "没有截到商品图")
	})

	t.Run("最多 6 张", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		for i := range maxProducts {
			_, _ = r.pickProduct(context.Background(), `{"index":`+string(rune('0'+i))+`,"title":"商品","price":"¥1"}`)
		}

		// Act
		res, _ := r.pickProduct(context.Background(), `{"index":9,"title":"第七个","price":"¥1"}`)

		// Assert
		assert.True(t, res.Failed)
		assert.Len(t, r.products, maxProducts)
	})

	t.Run("在京东搜索页上不填 index：记下的是搜索页，不要", func(t *testing.T) {
		// Arrange：openedRun 停在京东首页，和停在搜索页一样不是商品页
		r := openedRun(t, newFakeBrowser())

		// Act
		res, _ := r.pickProduct(context.Background(), `{"title":"商品","price":"¥1"}`)

		// Assert
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "不是商品页")
		assert.Empty(t, r.products)
	})

	t.Run("没写标题不调插件", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		before := len(b.calls)

		// Act
		res, _ := r.pickProduct(context.Background(), `{"title":"  ","price":"¥1"}`)

		// Assert
		assert.True(t, res.Failed)
		assert.Len(t, b.calls, before)
	})

	t.Run("模型写的字段过打码，链接不过（商品 id 像手机号）", func(t *testing.T) {
		// Arrange：11 位、1 开头第二位 3～9 的商品 id
		b := newFakeBrowser()
		b.pages["https://item.jd.com/13800138000.html"] = fakePage{title: "商品"}
		r := newToolRun(b)
		_, err := r.Open(context.Background(), `{"url":"https://item.jd.com/13800138000.html"}`)
		require.NoError(t, err)

		// Act
		_, _ = r.pickProduct(context.Background(), `{"title":"客服 13800138000","price":"¥1"}`)

		// Assert
		assert.Equal(t, "客服 [手机号]", r.products[0].Title)
		assert.Equal(t, "https://item.jd.com/13800138000.html", r.products[0].URL)
	})
}

func Test_productsMarkdown(t *testing.T) {
	t.Run("没有卡片就什么都不加", func(t *testing.T) {
		assert.Empty(t, productsMarkdown(nil))
	})

	t.Run("每张卡片一行，标题里的方括号被转义", func(t *testing.T) {
		// Arrange
		products := []ProductCard{
			{ID: 1, Title: "[官方] 羽绒被", Shop: "无印良品MUJI官方旗舰店", Spec: "200x230cm", Price: "¥588", PriceNote: "平台加补后 ¥4??", URL: "https://detail.tmall.com/item.htm?id=1"},
			{ID: 2, Title: "宜家 羽绒被", URL: "https://www.ikea.cn/cn/zh/p/x"},
		}

		// Act
		got := productsMarkdown(products)

		// Assert
		assert.Equal(t, "\n\n**提到的商品**\n"+
			"\n1. [\\[官方\\] 羽绒被](https://detail.tmall.com/item.htm?id=1) · 无印良品MUJI官方旗舰店 · 200x230cm · ¥588（平台加补后 ¥4??）"+
			"\n2. [宜家 羽绒被](https://www.ikea.cn/cn/zh/p/x)", got)
	})
}
