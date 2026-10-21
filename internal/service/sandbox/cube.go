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
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/segmentfault/pacman/log"
	cubesandbox "github.com/tencentcloud/CubeSandbox/sdk/go"
)

// Configured 说的是有没有配沙箱：CUBE_TEMPLATE_ID 没设就当没有，Agent 不给沙箱工具。
// 其余的配置都是 SDK 自己从环境变量读的：
//   - CUBE_API_URL：CubeAPI，默认 http://127.0.0.1:3000
//   - CUBE_TEMPLATE_ID：用 deploy/sandbox/Dockerfile 建的模板（python、node、Chromium、中文字体）
//   - CUBE_PROXY_NODE_IP：CubeProxy 的 IP。设了以后传文件、跑命令都直接连这个 IP，不解析 *.cube.app，
//     Clash 的 fake-ip 就劫持不到（SDK 的 transport.go，DialContext 不管域名）
//   - CUBE_PROXY_PORT_HTTP：CubeProxy 的端口。本地 WSL 里是 443（TLS）
//   - CUBE_CA_FILE：我们自己加的。CubeProxy 的证书是 Cube 安装时用 mkcert 签的，只有 WSL 里认；
//     指向那个 CA 的证书（公钥，不是私钥），比如 answer-data/cube/ca.crt
func Configured() bool {
	return strings.TrimSpace(os.Getenv("CUBE_TEMPLATE_ID")) != ""
}

// cubeClient 用官方 Go SDK 实现 Cube
type cubeClient struct {
	client *cubesandbox.Client
}

// NewCube 按环境变量连上 Cube。没配的时候不要调它，先看 Configured
func NewCube() Cube {
	cfg := cubesandbox.NewConfigFromEnv()
	var opts []cubesandbox.ClientOption
	if caFile := strings.TrimSpace(os.Getenv("CUBE_CA_FILE")); caFile != "" {
		hc, err := httpClientWithCA(cfg, caFile)
		if err != nil {
			log.Errorf("sandbox: cannot use CUBE_CA_FILE, sandbox requests will fail TLS: %v", err)
		} else {
			opts = append(opts, cubesandbox.WithHTTPClient(hc))
		}
	}
	return &cubeClient{client: cubesandbox.NewClient(cfg, opts...)}
}

// httpClientWithCA 是认得 Cube 自己那个 CA 的 HTTP 客户端。SDK 的 WithHTTPClient 会同时换掉控制面
// （CubeAPI）和数据面（CubeProxy）的客户端，所以这里要自己把 SDK 原来做的事也做了：
// 只有 *.cube.app 直连 CubeProxy 的 IP，CubeAPI 照常按地址连；都不走系统代理（Clash）
func httpClientWithCA(cfg cubesandbox.Config, caFile string) (*http.Client, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificate in %s", caFile)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	if cfg.ProxyNodeIP != "" {
		target := net.JoinHostPort(cfg.ProxyNodeIP, strconv.Itoa(cfg.ProxyPortHTTP))
		suffix := "." + cfg.SandboxDomain
		dialer := &net.Dialer{Timeout: cfg.RequestTimeout, KeepAlive: 30 * time.Second}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// TLS 握手还是按原来的域名（49983-<id>.cube.app）验证证书，只是 TCP 连到 CubeProxy 的 IP
			if host, _, err := net.SplitHostPort(addr); err == nil && strings.HasSuffix(host, suffix) {
				addr = target
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}
	// 不设 Timeout：命令的输出是流式的，期限由 ctx 管
	return &http.Client{Transport: transport}, nil
}

func (c *cubeClient) Create(ctx context.Context, opts CreateOpts) (Box, error) {
	onTimeout := "kill"
	if opts.Pause {
		onTimeout = "pause"
	}
	idle := opts.Idle
	sb, err := c.client.Create(ctx, cubesandbox.CreateOptions{
		Timeout:  &idle,
		Metadata: map[string]string{"app": "maibumai", "chat_id": strconv.FormatInt(opts.ChatID, 10)},
		// SDK 还没有 lifecycle 字段，按 CubeAPI 的线上格式直接放（docs/guide/lifecycle.md）：
		// 闲置到点以后暂停或销毁；暂停了的，下一个请求进来自动恢复
		Extra: map[string]any{"lifecycle": map[string]any{"onTimeout": onTimeout, "autoResume": true}},
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox: create: %w", err)
	}
	return &cubeBox{sb: sb}, nil
}

func (c *cubeClient) Connect(ctx context.Context, sandboxID string) (Box, error) {
	sb, err := c.client.Connect(ctx, sandboxID)
	if isGone(err) {
		return nil, fmt.Errorf("%w: %v", ErrGone, err)
	}
	if err != nil {
		return nil, fmt.Errorf("sandbox: connect %s: %w", sandboxID, err)
	}
	return &cubeBox{sb: sb}, nil
}

// isGone 认出"沙箱已经没了"：404 是找不到这个沙箱；409 是它的容器已经退出了，
// 文档说 connect 这时回 "... is not connectable in state 'stopped'"（docs/guide/lifecycle.md）
func isGone(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, cubesandbox.ErrSandboxNotFound) {
		return true
	}
	var apiErr *cubesandbox.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// cubeBox 是一个真的沙箱
type cubeBox struct {
	sb *cubesandbox.Sandbox
}

func (b *cubeBox) ID() string { return b.sb.SandboxID }

func (b *cubeBox) Upload(ctx context.Context, path string, data []byte) error {
	return b.sb.Files().Write(ctx, path, data)
}

func (b *cubeBox) Download(ctx context.Context, path string) ([]byte, error) {
	// SDK 用 string 装文件内容：Go 的 string 就是一串字节，图片这类二进制原样在里面
	s, err := b.sb.Files().Read(ctx, path)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// runGrace 是命令期限之外多等的一会儿：期限到了 envd 杀掉进程、把结果流回来也要时间
const runGrace = 5 * time.Second

func (b *cubeBox) Run(ctx context.Context, cmd string, timeout time.Duration) (RunResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout+runGrace)
	defer cancel()
	res, err := b.sb.Commands().Run(runCtx, cmd, cubesandbox.CommandOptions{Timeout: timeout, Cwd: Home})
	if err != nil {
		// 整次任务的 ctx 还活着，是这条命令自己的期限到了：这是"进程被杀"，不是沙箱连不上
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return RunResult{TimedOut: true}, nil
		}
		return RunResult{}, err
	}
	return RunResult{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, nil
}

func (b *cubeBox) Kill(ctx context.Context) error {
	return b.sb.Kill(ctx)
}
