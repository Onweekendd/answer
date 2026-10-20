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

// Package sandbox 是 Agent 的工作台：一个对话一个 CubeSandbox 沙箱（MicroVM），模型写的代码只在里面跑。
//
// 分层（概念 5）：
//   - Box：一个沙箱，能传文件、取文件、跑命令
//   - Cube：建沙箱、连上已有的沙箱。cubeClient 调官方 SDK；测试里是 fake
//   - Store：对话和沙箱的对应关系，存在 purchase_chat.sandbox_id
//   - Manager：把上面三样拼起来，Acquire 交出这个对话的沙箱（概念 2 的生命周期）
//
// purchase_ai 不直接用这些类型，它在自己那边定义只含三个方法的 Workspace 接口，Box 自动满足它。
package sandbox

import (
	"context"
	"errors"
	"time"
)

// Home 是沙箱里的工作目录：命令在这里跑，相对路径都以它为准
const Home = "/home/user"

// 闲置多久以后 Cube 自己回收（概念 2 第 3 点）。闲置是 Cube 按 CubeProxy 上的流量算的，不用我们续期
const (
	// chatIdle：对话的沙箱闲置这么久就暂停，内存和文件都留着，下次 connect 自动恢复（约 42ms）
	chatIdle = 10 * time.Minute
	// tempIdle：没有对话 id 的临时沙箱（chatID 为 0）闲置这么久就销毁，没人会再找它
	tempIdle = 5 * time.Minute
)

// RunResult 是一条命令的结果。命令没跑完就出了错（沙箱连不上）不在这里，是 Run 返回的 error
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int  // 进程的退出码：0 成功，127 找不到命令，Python 抛异常是 1
	TimedOut bool // 跑过了期限被杀掉了，这时 ExitCode 没有意义
}

// Box 是一个沙箱
type Box interface {
	ID() string
	// Upload 把 data 写到沙箱里的 path（绝对路径），文件已存在就覆盖
	Upload(ctx context.Context, path string, data []byte) error
	// Download 读出沙箱里 path 的全部内容
	Download(ctx context.Context, path string) ([]byte, error)
	// Run 在 Home 下用 bash 跑 cmd，最多跑 timeout。
	// 退出码不是 0 不算出错，它在 RunResult 里；error 只表示没能跑完（沙箱连不上、ctx 结束了）
	Run(ctx context.Context, cmd string, timeout time.Duration) (RunResult, error)
	// Kill 销毁沙箱，不能恢复
	Kill(ctx context.Context) error
}

// CreateOpts 是建沙箱的选项
type CreateOpts struct {
	ChatID int64         // 写进沙箱的 metadata，数据库和 Cube 对不上时能用 List 对回来；0 表示临时沙箱
	Idle   time.Duration // 闲置多久回收
	Pause  bool          // 回收时暂停（留着给这个对话）还是销毁
}

// Cube 是 Manager 用到的 Cube 操作，定义在用它的这一边（概念 5）
type Cube interface {
	Create(ctx context.Context, opts CreateOpts) (Box, error)
	// Connect 连上一个已有的沙箱，暂停了的会被恢复。沙箱已经没了（被删了、Cube 重启过）返回 ErrGone
	Connect(ctx context.Context, sandboxID string) (Box, error)
}

// Store 存对话和沙箱的对应关系（概念 2 第 2 点：放内存里，后端一重启就丢了）
type Store interface {
	// SandboxOf 返回 userID 的这个对话记着的沙箱 id，还没有就是 ""。
	// 对话不存在或者不是 userID 的，found 为 false：不能让别人用对话 id 摸到你的沙箱
	SandboxOf(ctx context.Context, chatID int64, userID string) (sandboxID string, found bool, err error)
	// SwapSandbox 只在记着的还是 old 时把它换成 newID（compare-and-set，需求 3 学过的那个），
	// swapped 为 false 表示有人先换了
	SwapSandbox(ctx context.Context, chatID int64, old, newID string) (swapped bool, err error)
}

var (
	// ErrGone 表示沙箱已经没了，只能重建
	ErrGone = errors.New("sandbox: the sandbox is gone")
	// ErrChatNotFound 表示对话不存在，或者不是这个用户的
	ErrChatNotFound = errors.New("sandbox: chat not found")
)
