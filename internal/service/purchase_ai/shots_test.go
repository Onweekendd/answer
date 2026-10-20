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

func Test_pictures(t *testing.T) {
	t.Run("截的图编成图几，告诉模型怎么引用，这时还不存", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		store := &fakeImageStore{}
		r.images = store

		// Act
		res, err := r.screenshot(context.Background(), `{"index":3}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Contains(t, res.Text, "「图1」")
		assert.Contains(t, res.Text, "[图1：")
		assert.True(t, strings.HasPrefix(res.Image, "data:image/jpeg;base64,"))
		require.Len(t, r.pictures, 1)
		assert.Empty(t, store.saved)
	})

	t.Run("view_image 下载原图给模型看，和截图共用编号", func(t *testing.T) {
		// Arrange
		b := newFakeBrowser()
		r := openedRun(t, b)
		_, _ = r.screenshot(context.Background(), `{}`)

		// Act
		res, err := r.viewImage(context.Background(), `{"index":5}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Contains(t, res.Text, "「图2」")
		assert.Contains(t, b.calls, "image 5")
		assert.True(t, strings.HasPrefix(res.Image, "data:image/jpeg;base64,"))
		assert.Equal(t, "查看原图（1600×2400）", res.Display)
	})

	t.Run("几乎空白的图不编号", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser())
		white := color.RGBA{255, 255, 255, 255}

		// Act
		text := r.numberPicture(screenshotData{Data: jpegOf(t, 400, 200, white, white), Mime: "image/jpeg"})

		// Assert
		assert.Contains(t, text, "空白")
		assert.Empty(t, r.pictures)
	})
}

func Test_citedShots(t *testing.T) {
	// openedRun6 截好 6 张图
	openedRun6 := func(t *testing.T, store *fakeImageStore) *agentRun {
		r := openedRun(t, newFakeBrowser())
		r.images = store
		for range 6 {
			_, _ = r.screenshot(context.Background(), `{}`)
		}
		return r
	}

	t.Run("只存回答引用了的，按引用的顺序，重复和不存在的编号不算，最多 4 张", func(t *testing.T) {
		// Arrange
		store := &fakeImageStore{}
		r := openedRun6(t, store)

		// Act
		shots := r.citedShots(context.Background(), "[图3：价格] 再看 [图1]、[图3]、[图9]、[图2]、【图 5】、[图4]")

		// Assert
		ids := []int{}
		for _, s := range shots {
			ids = append(ids, s.ID)
		}
		assert.Equal(t, []int{3, 1, 2, 5}, ids)
		require.Len(t, store.saved, 4)
		assert.Equal(t, []byte("shot3"), store.saved[0])
		assert.Equal(t, Shot{ID: 3, Image: "https://buy.example.com/uploads/agent/1.jpg", Source: jdHome}, shots[0])
	})

	t.Run("回答没引用就什么都不存", func(t *testing.T) {
		// Arrange
		store := &fakeImageStore{}
		r := openedRun6(t, store)

		// Act
		shots := r.citedShots(context.Background(), "就这样。")

		// Assert
		assert.Empty(t, shots)
		assert.Empty(t, store.saved)
	})

	t.Run("存不成的图没有，回答照常", func(t *testing.T) {
		// Arrange
		r := openedRun6(t, &fakeImageStore{})
		r.images = &fakeImageStore{err: errors.New("disk full")}

		// Act
		shots := r.citedShots(context.Background(), "[图1]")

		// Assert
		assert.Empty(t, shots)
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
		{ID: 1, Image: "https://buy.example.com/uploads/agent/a.jpg", Source: "https://item.jd.com/100.html"},
		{ID: 3, Image: "https://buy.example.com/uploads/agent/b.jpg"},
	}

	t.Run("[图N：说明] 换成图片和说明，说明里的 Markdown 符号去掉", func(t *testing.T) {
		// Act
		got := withShots("京东最便宜。\n［图1：自营价 *1849* <券后>］\n建议买。", shots[:1])

		// Assert
		assert.Equal(t, "京东最便宜。\n\n![图1](https://buy.example.com/uploads/agent/a.jpg)\n"+
			"*图1：自营价 1849 ‹券后›（[来源](https://item.jd.com/100.html)）*\n\n建议买。", got)
	})

	t.Run("没写说明的只标图几", func(t *testing.T) {
		// Act
		got := withShots("差评：[图3]", shots)

		// Assert
		assert.Equal(t, "差评：\n\n![图3](https://buy.example.com/uploads/agent/b.jpg)\n*图3*", got)
	})

	t.Run("没存的编号去掉，同一张图第二次只写文字，没引用的不放出来", func(t *testing.T) {
		// Act
		got := withShots("看【图 1】，再看[图1：价格]和[图2：没存]。", shots)

		// Assert
		assert.Equal(t, 1, strings.Count(got, "a.jpg"))
		assert.Contains(t, got, "再看图1和。")
		assert.NotContains(t, got, "没存")
		assert.NotContains(t, got, "b.jpg")
	})

	t.Run("没有引用时原样返回", func(t *testing.T) {
		// Act
		got := withShots("就这样。", nil)

		// Assert
		assert.Equal(t, "就这样。", got)
	})
}
