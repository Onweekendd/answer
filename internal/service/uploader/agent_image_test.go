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

package uploader

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/answer/internal/base/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encoded(t *testing.T, encode func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return buf.Bytes()
}

func Test_saveAgentImage_SavesToAgentDirectory(t *testing.T) {
	// Arrange
	uploadPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(uploadPath, constant.AgentSubPath), 0o755))
	data := encoded(t, func(b *bytes.Buffer, img image.Image) error { return jpeg.Encode(b, img, nil) })

	// Act
	name, err := saveAgentImage(uploadPath, data)

	// Assert
	require.NoError(t, err)
	assert.Regexp(t, `^\w+\.jpg$`, name)
	saved, err := os.ReadFile(filepath.Join(uploadPath, constant.AgentSubPath, name))
	require.NoError(t, err)
	assert.Equal(t, data, saved)
}

func Test_saveAgentImage_SkipsNonJPEG(t *testing.T) {
	// Arrange：png 和一段 HTML，都不是插件截出来的 jpeg
	uploadPath := t.TempDir()
	pngData := encoded(t, func(b *bytes.Buffer, img image.Image) error { return png.Encode(b, img) })

	for _, data := range [][]byte{pngData, []byte("<script>alert(1)</script>"), nil} {
		// Act
		_, err := saveAgentImage(uploadPath, data)

		// Assert
		require.Error(t, err)
	}
	entries, _ := os.ReadDir(uploadPath)
	assert.Empty(t, entries)
}

// fakePutter 记下放进对象存储的东西
type fakePutter struct {
	objects map[string][]byte
	err     error
}

func (f *fakePutter) Put(_ context.Context, key string, data []byte, contentType string) error {
	if f.err != nil {
		return f.err
	}
	if contentType != "image/jpeg" {
		return errors.New("wrong content type " + contentType)
	}
	f.objects[key] = data
	return nil
}

func Test_putAgentImage(t *testing.T) {
	jpegData := encoded(t, func(b *bytes.Buffer, img image.Image) error { return jpeg.Encode(b, img, nil) })

	t.Run("放进对象存储的 agent/ 下面", func(t *testing.T) {
		// Arrange
		store := &fakePutter{objects: map[string][]byte{}}

		// Act
		name, err := putAgentImage(context.Background(), store, jpegData)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, jpegData, store.objects["agent/"+name])
	})

	t.Run("不是 jpeg 就不放", func(t *testing.T) {
		// Arrange
		store := &fakePutter{objects: map[string][]byte{}}

		// Act
		_, err := putAgentImage(context.Background(), store, []byte("<script>alert(1)</script>"))

		// Assert
		require.ErrorIs(t, err, ErrNotJPEG)
		assert.Empty(t, store.objects)
	})

	t.Run("对象存储出错时把错误交回去", func(t *testing.T) {
		// Arrange
		store := &fakePutter{objects: map[string][]byte{}, err: errors.New("garage down")}

		// Act
		_, err := putAgentImage(context.Background(), store, jpegData)

		// Assert
		assert.EqualError(t, err, "garage down")
	})
}
