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

// Package sandboxtool 是工作台（沙箱）工具：bash、read_file、write_file、edit_file。
// 工作台是一台独立的 Linux 电脑（Cube 的 MicroVM），一个对话一台，第一次用时才去要。
// 状态都在 Bench 里：这次任务的工作台、要不要提醒模型「原来的工作台没了」
package sandboxtool

import (
	"context"
	"time"

	"github.com/apache/answer/internal/service/sandbox"
	"github.com/segmentfault/pacman/log"
)

// Workspace 是 Agent 用到的沙箱操作。和 Browser 一样，接口定义在用它的这一边，只写用到的三个方法：
//   - sandbox.Box：产品里用，Cube 的一个 MicroVM
//   - fakeWorkspace：测试用，文件放在 map 里
//
// Go 的接口和 TS 的类型一样是结构化的：sandbox.Box 不用声明 implements，方法对上了就满足
type Workspace interface {
	Upload(ctx context.Context, path string, data []byte) error
	Download(ctx context.Context, path string) ([]byte, error)
	Run(ctx context.Context, cmd string, timeout time.Duration) (sandbox.RunResult, error)
}

// Workspaces 交出一个对话的工作台。reset 为 true 表示原来的沙箱没了，这是新建的，之前生成的文件都不在了
type Workspaces interface {
	Acquire(ctx context.Context, chatID int64, userID string) (ws Workspace, reset bool, err error)
}

// NewWorkspaces 给依赖注入用。没配 CUBE_TEMPLATE_ID 就是 nil：没有沙箱，Agent 不给沙箱工具，其余照常
func NewWorkspaces(store sandbox.Store) Workspaces {
	if !sandbox.Configured() {
		log.Info("purchase ai: CUBE_TEMPLATE_ID is not set, the agent has no sandbox")
		return nil
	}
	return managerWorkspaces{m: sandbox.NewManager(sandbox.NewCube(), store)}
}

// managerWorkspaces 把 *sandbox.Manager 接到 Workspaces 上。
// 不能直接拿 Manager 当 Workspaces：它的 Acquire 返回 sandbox.Box，不是 Workspace。
// Go 的接口要求方法签名一字不差，Box 满足 Workspace 也不行（返回值没有协变），所以转一手
type managerWorkspaces struct {
	m *sandbox.Manager
}

func (w managerWorkspaces) Acquire(ctx context.Context, chatID int64, userID string) (Workspace, bool, error) {
	box, reset, err := w.m.Acquire(ctx, chatID, userID)
	if err != nil {
		// 出错时显式返回 nil：要是以后 Acquire 改成返回具体类型（*cubeBox）的 nil，
		// 装进接口就不是 nil 了，调用方的 ws != nil 会被骗过去
		return nil, false, err
	}
	return box, reset, nil
}
