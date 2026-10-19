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

func Test_saveAgentImage_写进agent目录(t *testing.T) {
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

func Test_saveAgentImage_不是jpeg就不存(t *testing.T) {
	// Arrange：png 和一段 HTML，都不是插件截出来的 jpeg
	uploadPath := t.TempDir()
	pngData := encoded(t, func(b *bytes.Buffer, img image.Image) error { return png.Encode(b, img) })

	for _, data := range [][]byte{pngData, []byte("<script>alert(1)</script>"), nil} {
		// Act
		_, err := saveAgentImage(uploadPath, data)

		// Assert
		assert.Error(t, err)
	}
	entries, _ := os.ReadDir(uploadPath)
	assert.Empty(t, entries)
}
