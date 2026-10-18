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

package purchaseai

import (
	"context"
	"encoding/json"

	"github.com/apache/answer/internal/service/browser"
)

// Browser 是 Agent 用到的浏览器。接口定义在用它的这一边，只写 Agent 用到的两个方法：
//   - *browser.Hub：产品里用，插件通过 WebSocket 连上来
//   - *browser.DebugClient：case 运行器用，通过调试接口调用另一个进程里的 Hub
//   - fakeBrowser：测试用
//
// 错误必须是 browser 包的那一套（*browser.CommandError、ErrNotConnected、ErrDisconnected），
// Agent 靠它们区分失败的种类。
type Browser interface {
	Call(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error)
	Connected() bool
}

// noBrowser 是还没接上 Hub 时的浏览器：永远没连上，Agent 降级成直接回答。
// Hub 挂进 answer run 之后（阶段 3）换成真的。
type noBrowser struct{}

func (noBrowser) Call(context.Context, int, string, any) (json.RawMessage, error) {
	return nil, browser.ErrNotConnected
}

func (noBrowser) Connected() bool { return false }

// NewNoBrowser 给依赖注入用
func NewNoBrowser() Browser { return noBrowser{} }
