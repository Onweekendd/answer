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

package imgprep

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeImage draws a w x h image and saves it under name, as PNG or JPEG
// whatever the name's extension says.
func writeImage(t *testing.T, name string, w, h int, asJPEG bool) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if asJPEG {
		require.NoError(t, jpeg.Encode(&buf, img, nil))
	} else {
		require.NoError(t, png.Encode(&buf, img))
	}
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
	return p
}

// decodeURI checks the URI's shape and returns the image inside it.
func decodeURI(t *testing.T, uri string) image.Image {
	t.Helper()
	const prefix = "data:image/jpeg;base64,"
	require.True(t, strings.HasPrefix(uri, prefix), "got %.40q", uri)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	require.NoError(t, err)
	img, err := jpeg.Decode(bytes.NewReader(raw))
	require.NoError(t, err, "the payload must really be a JPEG")
	return img
}

func TestDataURI(t *testing.T) {
	t.Run("大图等比缩小到最长边", func(t *testing.T) {
		// Arrange: a wide 3000x1500 screenshot
		p := writeImage(t, "wide.png", 3000, 1500, false)

		// Act
		uri, err := DataURI(p, 2000)

		// Assert
		require.NoError(t, err)
		b := decodeURI(t, uri).Bounds()
		require.Equal(t, 2000, b.Dx())
		require.Equal(t, 1000, b.Dy())
	})

	t.Run("竖长的手机截图按高度缩小", func(t *testing.T) {
		// Arrange: 1080x2400, the shape of most phone screenshots
		p := writeImage(t, "phone.png", 1080, 2400, false)

		// Act
		uri, err := DataURI(p, 2000)

		// Assert
		require.NoError(t, err)
		b := decodeURI(t, uri).Bounds()
		require.Equal(t, 2000, b.Dy())
		require.Equal(t, 900, b.Dx())
	})

	t.Run("用的是传进来的上限，不是默认值", func(t *testing.T) {
		// Arrange
		p := writeImage(t, "wide.png", 3000, 1500, false)

		// Act
		uri, err := DataURI(p, 1000)

		// Assert
		require.NoError(t, err)
		b := decodeURI(t, uri).Bounds()
		require.Equal(t, 1000, b.Dx())
		require.Equal(t, 500, b.Dy())
	})

	t.Run("小图不放大", func(t *testing.T) {
		// Arrange
		p := writeImage(t, "small.png", 265, 232, false)

		// Act
		uri, err := DataURI(p, 2000)

		// Assert
		require.NoError(t, err)
		b := decodeURI(t, uri).Bounds()
		require.Equal(t, 265, b.Dx())
		require.Equal(t, 232, b.Dy())
	})

	t.Run("扩展名是 png 但内容是 JPEG 也能读", func(t *testing.T) {
		// Arrange: what a user renaming a file, or some apps, produce
		p := writeImage(t, "actually-jpeg.png", 400, 300, true)

		// Act
		uri, err := DataURI(p, 2000)

		// Assert
		require.NoError(t, err)
		require.Equal(t, 400, decodeURI(t, uri).Bounds().Dx())
	})

	t.Run("不是图片时报错，并带上文件路径", func(t *testing.T) {
		// Arrange
		p := filepath.Join(t.TempDir(), "notes.png")
		require.NoError(t, os.WriteFile(p, []byte("not an image"), 0o600))

		// Act
		uri, err := DataURI(p, 2000)

		// Assert
		require.Empty(t, uri)
		require.ErrorContains(t, err, "notes.png")
	})

	t.Run("文件不存在时报错", func(t *testing.T) {
		// Act
		uri, err := DataURI(filepath.Join(t.TempDir(), "missing.png"), 2000)

		// Assert
		require.Empty(t, uri)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestFitWidth(t *testing.T) {
	encode := func(t *testing.T, w, h int) []byte {
		t.Helper()
		var buf bytes.Buffer
		require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), &jpeg.Options{Quality: 90}))
		return buf.Bytes()
	}

	t.Run("比上限宽的等比缩到上限", func(t *testing.T) {
		// Arrange: a full-resolution screenshot of a 2880-pixel-wide screen
		data := encode(t, 2880, 1000)

		// Act
		out, w, h, resized, err := FitWidth(data, 1600, 80)

		// Assert
		require.NoError(t, err)
		require.True(t, resized)
		require.Equal(t, 1600, w)
		require.Equal(t, 556, h)
		img, err := jpeg.Decode(bytes.NewReader(out))
		require.NoError(t, err)
		require.Equal(t, 1600, img.Bounds().Dx())
	})

	t.Run("本来就够窄的原样返回，不再压一遍", func(t *testing.T) {
		// Arrange: a shot of one product card
		data := encode(t, 496, 504)

		// Act
		out, w, h, resized, err := FitWidth(data, 1600, 80)

		// Assert
		require.NoError(t, err)
		require.False(t, resized)
		require.Equal(t, data, out)
		require.Equal(t, 496, w)
		require.Equal(t, 504, h)
	})

	t.Run("不是图片就报错", func(t *testing.T) {
		// Act
		_, _, _, _, err := FitWidth([]byte("shot1"), 1600, 80)

		// Assert
		require.Error(t, err)
	})
}
