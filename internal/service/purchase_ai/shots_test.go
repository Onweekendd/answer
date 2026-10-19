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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_citeShot(t *testing.T) {
	t.Run("截下来存好，告诉模型是图几，图也给它看", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		store := &fakeImageStore{}
		r.images = store

		// Act
		res, err := r.citeShot(context.Background(), `{"index":3,"caption":"京东自营 1849 元"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Contains(t, res.Text, "图1")
		assert.Contains(t, res.Text, "[图1]")
		assert.True(t, strings.HasPrefix(res.Image, "data:image/jpeg;base64,"))
		assert.Contains(t, b.calls, "screenshot 3")
		require.Len(t, r.shots, 1)
		assert.Equal(t, Shot{ID: 1, Caption: "京东自营 1849 元", Image: "https://buy.example.com/uploads/agent/1.jpg", Source: jdHome}, r.shots[0])
	})

	t.Run("cite_image 下载原图，和截图共用编号", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		store := &fakeImageStore{}
		r.images = store
		_, _ = r.citeShot(context.Background(), `{"caption":"价格区"}`)

		// Act
		res, err := r.citeImage(context.Background(), `{"index":5,"caption":"详情页参数图"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Contains(t, res.Text, "图2")
		assert.Contains(t, b.calls, "image 5")
		assert.Equal(t, []byte("original"), store.saved[1])
		require.Len(t, r.shots, 2)
		assert.Equal(t, 2, r.shots[1].ID)
		assert.Equal(t, "保存原图：详情页参数图", res.Display)
	})

	t.Run("存不了图就算失败，不记下", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		r.images = &fakeImageStore{err: errors.New("disk full")}

		// Act
		res, _ := r.citeShot(context.Background(), `{"caption":"价格"}`)

		// Assert
		assert.True(t, res.Failed)
		assert.Empty(t, r.shots)
	})

	t.Run("最多 4 张", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		r.images = &fakeImageStore{}
		for range maxShots {
			_, _ = r.citeShot(context.Background(), `{"caption":"价格"}`)
		}

		// Act
		res, _ := r.citeShot(context.Background(), `{"caption":"第五张"}`)

		// Assert
		assert.True(t, res.Failed)
		assert.Len(t, r.shots, maxShots)
	})

	t.Run("说明不能为空", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		r.images = &fakeImageStore{}

		// Act
		res, _ := r.citeShot(context.Background(), `{"caption":"  "}`)

		// Assert
		assert.True(t, res.Failed)
	})
}

// jpegOf 画一张 w×h 的图：底色 bg，左上角 1/4 画成 fg
func jpegOf(t *testing.T, w, h int, bg, fg color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := bg
			if x < w/2 && y < h/2 {
				c = fg
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func Test_looksBlank(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}

	t.Run("一片白算空白", func(t *testing.T) {
		// Act
		blank, err := looksBlank(jpegOf(t, 400, 200, white, white))

		// Assert
		require.NoError(t, err)
		assert.True(t, blank)
	})

	t.Run("四分之一有内容就不算", func(t *testing.T) {
		// Act
		blank, err := looksBlank(jpegOf(t, 400, 200, white, color.RGBA{230, 80, 30, 255}))

		// Assert
		require.NoError(t, err)
		assert.False(t, blank)
	})

	t.Run("不是 jpeg 时报错，不当成空白", func(t *testing.T) {
		// Act
		_, err := looksBlank(base64.StdEncoding.EncodeToString([]byte("shot1")))

		// Assert
		assert.Error(t, err)
	})
}

func Test_withShots(t *testing.T) {
	shots := []Shot{
		{ID: 1, Caption: "自营价 [1849]", Image: "https://buy.example.com/uploads/agent/a.jpg", Source: "https://item.jd.com/100.html"},
		{ID: 2, Caption: "差评", Image: "https://buy.example.com/uploads/agent/b.jpg"},
	}

	t.Run("[图N] 换成图片和说明，说明里的方括号不会变成链接", func(t *testing.T) {
		// Act
		got := withShots("京东最便宜。\n［图1］\n建议买。", shots[:1])

		// Assert
		assert.Equal(t, "京东最便宜。\n\n![图1](https://buy.example.com/uploads/agent/a.jpg)\n"+
			"*图1：自营价 「1849」（[来源](https://item.jd.com/100.html)）*\n\n建议买。", got)
	})

	t.Run("没引用的图放在最后，不存在的编号去掉，同一张图第二次只写文字", func(t *testing.T) {
		// Act
		got := withShots("看【图 1】，再看[图1]和[图9]。", shots)

		// Assert
		assert.Equal(t, 1, strings.Count(got, "a.jpg"))
		assert.Contains(t, got, "再看图1和。")
		assert.NotContains(t, got, "图9")
		assert.True(t, strings.HasSuffix(got, "**截图**\n\n![图2](https://buy.example.com/uploads/agent/b.jpg)\n*图2：差评*"))
	})

	t.Run("没有截图时原样返回", func(t *testing.T) {
		// Act
		got := withShots("就这样。", nil)

		// Assert
		assert.Equal(t, "就这样。", got)
	})
}
