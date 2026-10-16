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

package browser

import (
	"encoding/json"
	"errors"
	"fmt"
)

// WebSocket 上的消息格式，和 frontend/extension/src/protocol.ts 一一对应，改一边就要改另一边。

// Command 是 Go 发给插件的命令
type Command struct {
	ID    string `json:"id"`   // Go 生成，插件在结果里原样带回，用来配对
	Type  string `json:"type"` // 固定为 "command"
	Cmd   string `json:"cmd"`  // navigate / snapshot / click
	TabID int    `json:"tabId"`
	Args  any    `json:"args"`
}

// ErrorBody 是插件报告的错误
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Message 是插件发来的任何一条消息：
//   - 结果：Type 是 "result"，带 ID，配对到某一次 Call
//   - 事件：Type 是 "event"，没有 ID，比如 hello、ping、tab_closed
type Message struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"` // 先不解析，交给调用方按命令类型解析
	Error *ErrorBody      `json:"error,omitempty"`
	Event string          `json:"event,omitempty"`
	TabID int             `json:"tabId,omitempty"`
}

// TabInfo 是插件上报的一个网页标签页。不带网址：网址里可能有搜索词
type TabInfo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// hello 事件的 data
type helloData struct {
	Version string    `json:"version"`
	Token   string    `json:"token"`
	Tabs    []TabInfo `json:"tabs"`
}

var (
	// ErrNotConnected 表示现在没有插件连着
	ErrNotConnected = errors.New("browser extension is not connected")
	// ErrDisconnected 表示等结果的时候插件断开了
	ErrDisconnected = errors.New("browser extension disconnected")
)

// CommandError 是插件执行命令失败时返回的错误，比如元素不存在、标签页没有 content script
type CommandError struct {
	Code    string
	Message string
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("browser command failed: %s: %s", e.Code, e.Message)
}
