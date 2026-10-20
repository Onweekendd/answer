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
	"errors"
)

// Manager 交出每个对话的沙箱：第一次用时才建，同一个对话复用，没了就重建（概念 2）
type Manager struct {
	cube  Cube
	store Store

	// TODO(精华，你来写)：同一个对话并发 Acquire 时只建一个沙箱要用到的东西，加在这里。
	// 可以是「每个对话一把锁」（map[int64]*sync.Mutex 加一把保护 map 的锁），
	// 也可以是 golang.org/x/sync/singleflight（缓存的是"正在建"这件事，就是前端缓存 Promise 那招）。
	// 选哪个、为什么，写一句注释
}

// NewManager 给依赖注入用
func NewManager(cube Cube, store Store) *Manager {
	return &Manager{cube: cube, store: store}
}

// errTODO 是精华代码还没写时的占位，写完就删掉
var errTODO = errors.New("sandbox: TODO")

// Acquire 交出 userID 的对话 chatID 的沙箱。reset 为 true 表示原来有沙箱、但已经没了，这是新建的：
// 之前生成的文件都不在了，调用方要告诉模型（概念 2 第 5 点）。
// chatID 为 0 是没有对话 id 的请求：建一个临时沙箱，不记下来，闲置 tempIdle 后 Cube 自己销毁。
//
// TODO(精华，你来写)，按这个顺序：
//  1. chatID 为 0：m.cube.Create 一个临时沙箱（ChatID 0、Idle tempIdle、Pause false），直接返回，reset 为 false。
//     临时沙箱不用加锁：每次都是新的，不会和谁抢
//  2. 加上这个对话的锁（或者进 singleflight），下面 3～6 步整个在锁里，函数返回时释放。
//     顺序很关键：先加锁再查表。先查表再加锁的话，两个请求都查到"没有"，就会各建一个（概念 2 第 4 点）
//  3. m.store.SandboxOf 查记着的沙箱 id。found 为 false 返回 ErrChatNotFound
//  4. id 不为空就 m.cube.Connect：成功了直接返回，reset 为 false；
//     错误是 ErrGone（用 errors.Is）就记下 reset = true，往下走去重建；别的错误原样返回
//  5. m.cube.Create 一个新的（ChatID chatID、Idle chatIdle、Pause true）
//  6. m.store.SwapSandbox(chatID, 第 3 步查到的 id, 新 id)：
//     swapped 为 false 说明别的后端实例抢先记了一个（锁只管得了本进程）：
//     把自己刚建的 Kill 掉，回到第 3 步重新查、去连人家那个
//
// 思考题：第 6 步 SwapSandbox 失败（数据库出错）时，第 5 步建好的沙箱怎么办？不处理的话它会变成什么？
func (m *Manager) Acquire(ctx context.Context, chatID int64, userID string) (box Box, reset bool, err error) {
	return nil, false, errTODO
}
