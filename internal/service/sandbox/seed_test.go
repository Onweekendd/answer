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

package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// untar 把 tar 解成 文件名 → 内容
func untar(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		out[hdr.Name] = body
	}
}

func TestTarFiles(t *testing.T) {
	// Arrange
	files := []tarFile{{Name: "001.jpg", Data: []byte("jpeg-1")}, {Name: "items.json", Data: []byte(`[]`)}}

	// Act
	data, err := tarFiles(files)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{"001.jpg": []byte("jpeg-1"), "items.json": []byte(`[]`)}, untar(t, data))
}

// skipSeed 写完 Number 和 Seed 以后删掉
func skipSeed(t *testing.T) {
	t.Helper()
	t.Skip("TODO(精华)：写完 Number 和 Seed 删掉这一行")
}

func items(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{
			Title:    fmt.Sprintf("卡片 %d", i+1),
			URL:      fmt.Sprintf("https://www.pinterest.com/pin/%d/", i+1),
			Original: fmt.Sprintf("https://i.pinimg.com/originals/%d.jpg", i+1),
			Thumb:    fmt.Appendf(nil, "jpeg-%d", i+1),
		}
	}
	return out
}

func TestNumber_ConsecutiveFromOneWithPaddedNames(t *testing.T) {
	skipSeed(t)
	// Act
	entries := Number(items(12))

	// Assert
	require.Len(t, entries, 12)
	for i, e := range entries {
		assert.Equal(t, i+1, e.N)
		assert.Equal(t, fmt.Sprintf("%03d.jpg", i+1), e.File)
		assert.Equal(t, fmt.Sprintf("卡片 %d", i+1), e.Title)
	}
}

func TestNumber_CardWithoutThumbHasNoFile(t *testing.T) {
	skipSeed(t)
	// Arrange：第 2 张插件没拿到缩略图
	in := items(3)
	in[1].Thumb = nil

	// Act
	entries := Number(in)

	// Assert：跳过还是占号由你定，这里只管两件不管怎么选都成立的事
	for i, e := range entries {
		assert.Equal(t, i+1, e.N, "编号要连续")
		if e.File == "" {
			assert.Empty(t, e.Thumb)
		} else {
			assert.NotEmpty(t, e.Thumb, "%s 有文件名却没有图", e.File)
		}
	}
}

func TestSeed_UploadsOneTarAndUnpacksIt(t *testing.T) {
	skipSeed(t)
	// Arrange
	box := newFakeBox("sb-1")
	entries := Number(items(3))

	// Act
	err := Seed(context.Background(), box, entries)

	// Assert：只传了一个文件，就是那个 tar
	require.NoError(t, err)
	require.Len(t, box.files, 1)
	var tarball []byte
	for _, data := range box.files {
		tarball = data
	}
	files := untar(t, tarball)
	assert.Equal(t, []byte("jpeg-2"), files["002.jpg"])

	var manifest []map[string]any
	require.NoError(t, json.Unmarshal(files["items.json"], &manifest))
	require.Len(t, manifest, 3)
	assert.Equal(t, "002.jpg", manifest[1]["file"])
	assert.Equal(t, "https://www.pinterest.com/pin/2/", manifest[1]["url"])
	assert.NotContains(t, manifest[1], "Thumb", "图本身不进清单")

	require.Len(t, box.cmds, 1)
	assert.Contains(t, box.cmds[0], "tar")
	assert.Contains(t, box.cmds[0], HarvestDir)
}

func TestSeed_FailedUnpackIsAnError(t *testing.T) {
	skipSeed(t)
	// Arrange
	box := newFakeBox("sb-1")
	box.results = []RunResult{{ExitCode: 2, Stderr: "tar: Unexpected EOF in archive"}}

	// Act
	err := Seed(context.Background(), box, Number(items(2)))

	// Assert
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "Unexpected EOF"), "错误里要带上 stderr：%v", err)
}
