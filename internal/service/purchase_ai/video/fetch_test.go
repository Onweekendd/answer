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

package video

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeServer 代替 CDN：从 file 里切一段给出去，记下要了几次。
// 它的 get 方法就是一个 RangeFunc：方法值（f.get）可以直接当函数传
type fakeServer struct {
	file      []byte
	status    int  // 不为 0 就一直回这个状态，没有数据（403 这类）
	whole     bool // 不分段：回 200 和整个文件
	hideTotal bool // 不说文件多大
	total     int  // 不为 0 就谎报这个大小
	calls     int
}

func (f *fakeServer) get(ctx context.Context, _ string, start, end int) (Part, error) {
	if err := ctx.Err(); err != nil {
		return Part{}, err
	}
	f.calls++
	switch {
	case f.status != 0:
		return Part{Status: f.status, ContentType: "text/plain"}, nil
	case f.whole:
		return Part{Status: 200, ContentType: "video/mp4", Total: len(f.file), Data: f.file}, nil
	}
	part := f.file[min(start, len(f.file)):min(end+1, len(f.file))]
	total := len(f.file)
	if f.hideTotal {
		total = 0
	}
	if f.total != 0 {
		total = f.total
	}
	return Part{Status: 206, ContentType: "video/mp4", Total: total, Data: part}, nil
}

// videoBytes 是一个 n 字节的假视频，每个字节都不一样（按位置），拼错了一眼看得出来
func videoBytes(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i * 7)
	}
	return data
}

func TestFetch(t *testing.T) {
	file := videoBytes(2*Chunk + 123) // 两段整的，加一段零头
	play := Play{URL: cdn("540"), Name: "normal_540_0", Size: len(file)}

	t.Run("一段段下完，拼起来一个字节不差", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file}

		// Act
		got, err := Fetch(context.Background(), server.get, play)

		// Assert
		require.NoError(t, err)
		assert.True(t, bytes.Equal(file, got), "拼起来的和原文件不一样")
		assert.Equal(t, 3, server.calls)
	})

	t.Run("服务器不说多大：短的那一段就是最后一段", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file, hideTotal: true}

		// Act
		got, err := Fetch(context.Background(), server.get, Play{URL: play.URL})

		// Assert
		require.NoError(t, err)
		assert.True(t, bytes.Equal(file, got))
		assert.Equal(t, 3, server.calls)
	})

	t.Run("服务器不分段、一次给了整个文件：不再要下一段", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file, whole: true}

		// Act
		got, err := Fetch(context.Background(), server.get, play)

		// Assert
		require.NoError(t, err)
		assert.True(t, bytes.Equal(file, got))
		assert.Equal(t, 1, server.calls)
	})

	t.Run("403：签名网址过期了，返回 ErrLinkExpired", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file, status: 403}

		// Act
		_, err := Fetch(context.Background(), server.get, play)

		// Assert
		require.ErrorIs(t, err, ErrLinkExpired)
		assert.Contains(t, err.Error(), "403")
	})

	t.Run("500：普通的错误，不是过期", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file, status: 500}

		// Act
		_, err := Fetch(context.Background(), server.get, play)

		// Assert
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrLinkExpired)
	})

	t.Run("接口标的大小超了上限：一段都不下", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file}

		// Act
		_, err := Fetch(context.Background(), server.get, Play{URL: play.URL, Size: MaxBytes + 1})

		// Assert
		require.Error(t, err)
		assert.Zero(t, server.calls)
	})

	t.Run("第一段才知道太大：马上停，不往下下", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file, total: MaxBytes + 1}

		// Act
		_, err := Fetch(context.Background(), server.get, Play{URL: play.URL})

		// Assert
		require.Error(t, err)
		assert.Equal(t, 1, server.calls)
	})

	t.Run("206 却是空的：报错，不要死循环", func(t *testing.T) {
		// Arrange：服务器谎报文件比实际大，下完真实的部分以后每一段都是空的
		server := &fakeServer{file: file, total: len(file) + 3*Chunk}

		// Act
		_, err := Fetch(context.Background(), server.get, Play{URL: play.URL})

		// Assert
		require.Error(t, err)
		assert.LessOrEqual(t, server.calls, 4)
	})

	t.Run("用户关了页面：返回 ctx 的错误", func(t *testing.T) {
		// Arrange
		server := &fakeServer{file: file}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Act
		_, err := Fetch(ctx, server.get, play)

		// Assert
		require.ErrorIs(t, err, context.Canceled)
	})
}
