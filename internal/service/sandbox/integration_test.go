//go:build integration

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

// 对着真的 Cube 跑（本地是 WSL 里那套），平时的 go test 不跑：
//
//	CUBE_TEMPLATE_ID=tpl-b25625065f3b426990ede91e CUBE_PROXY_NODE_IP=172.21.107.18 CUBE_PROXY_PORT_HTTP=443 \
//	  CUBE_CA_FILE="$(cygpath -m "$PWD/answer-data/cube/ca.crt")" \
//	  go test -tags integration -run Cube -v ./internal/service/sandbox/
//
// CUBE_CA_FILE 要写绝对路径：go test 在包的目录里跑，相对路径找不到

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCube_RoundTrip(t *testing.T) {
	if !Configured() {
		t.Skip("没设 CUBE_TEMPLATE_ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cube := NewCube()

	started := time.Now()
	box, err := cube.Create(ctx, CreateOpts{Idle: time.Minute})
	require.NoError(t, err)
	t.Logf("create: %v, id %s", time.Since(started), box.ID())
	defer func() { _ = box.Kill(context.Background()) }()

	t.Run("写进还不存在的目录", func(t *testing.T) {
		err := box.Upload(ctx, Home+"/deep/dir/a.txt", []byte("hello"))
		require.NoError(t, err, "envd 不会自己建父目录的话，write_file 要先 mkdir -p")
		got, err := box.Download(ctx, Home+"/deep/dir/a.txt")
		require.NoError(t, err)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("二进制原样来回", func(t *testing.T) {
		data := []byte{0x00, 0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, '\n', '\r', 0x80}
		require.NoError(t, box.Upload(ctx, Home+"/b.bin", data))
		got, err := box.Download(ctx, Home+"/b.bin")
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("在 Home 下执行，有 python3", func(t *testing.T) {
		res, err := box.Run(ctx, "pwd && python3 --version && (python3 -c 'import PIL; print(\"PIL\", PIL.__version__)' || echo 'no PIL')", 30*time.Second)
		require.NoError(t, err)
		t.Logf("stdout:\n%s\nstderr:\n%s", res.Stdout, res.Stderr)
		assert.Equal(t, 0, res.ExitCode)
		assert.Contains(t, res.Stdout, Home)
	})

	t.Run("退出码原样带回", func(t *testing.T) {
		res, err := box.Run(ctx, "echo oops >&2; exit 3", 10*time.Second)
		require.NoError(t, err)
		assert.Equal(t, 3, res.ExitCode)
		assert.Contains(t, res.Stderr, "oops")
	})

	t.Run("超时算进程被杀，不算出错", func(t *testing.T) {
		started := time.Now()
		res, err := box.Run(ctx, "sleep 30", 2*time.Second)
		require.NoError(t, err)
		assert.True(t, res.TimedOut)
		assert.Less(t, time.Since(started), 15*time.Second)
	})

	t.Run("连得上，销毁以后就是 ErrGone", func(t *testing.T) {
		again, err := cube.Connect(ctx, box.ID())
		require.NoError(t, err)
		assert.Equal(t, box.ID(), again.ID())

		require.NoError(t, box.Kill(ctx))
		_, err = cube.Connect(ctx, box.ID())
		assert.ErrorIs(t, err, ErrGone)
	})
}
