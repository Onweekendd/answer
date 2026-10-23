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

// Package browsertool 是浏览器工具：在用户的浏览器里开标签页、看页面、点击输入、滚动。
// 浏览器是用户自己的（插件经 Hub 连过来），Agent 只操作它自己开的标签页，结束时关掉。
// 状态都在 TaskState 里：开着哪个标签页、最新一份快照、快照里每个编号叫什么
package browsertool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/browser"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/segmentfault/pacman/log"
)

// Package browsertool 是浏览器工具：在用户的浏览器里开标签页、看页面、点击输入、滚动。
// 浏览器是用户自己的（插件经 Hub 连过来），Agent 只操作它自己开的标签页，结束时关掉。
// 状态都在 TaskState 里：开着哪个标签页、最新一份快照、快照里每个编号叫什么
// Browser 是 Agent 眼里的浏览器：按标签页发一条命令，拿回结果。
// browser.Hub（同一个进程）和 browser.HubClient（case 运行器经 HTTP API 连 Hub）都实现了它
type Browser interface {
	Call(ctx context.Context, tabID int, cmd string, args any) (json.RawMessage, error)
	Connected() bool
}

// TaskState 是一次任务里浏览器这一侧的状态。Agent 嵌入它，所以字段和方法在 Agent 上也能直接用
type TaskState struct {
	Browser Browser
	Timeout time.Duration // 一条命令最多等多久（read、收割这类慢的用 CallFor 自己给）

	// NumberPicture 给一张图编号（图N），返回告诉模型怎么引用它的一句话。
	// 编号是整次任务共用的（截图、看原图、工作台里的图），所以由外面给；nil 就不编号
	NumberPicture func(img tool.Image) string
	// LabelsOf 从快照里取出每个编号的名字，给用户看「点击「…」」用。nil 就不取
	LabelsOf func(elements string) map[int]string

	TabID        int            // Agent 自己开的标签页；0 表示还没开
	OpenedTabs   []int          // Agent 开过的标签页，结束时关掉
	SnapshotHash string         // 最新一份快照的哈希
	PageHash     string         // 页面状态：快照哈希加上滚动位置，循环检测用，见 PageState
	Labels       map[int]string // 最新快照里每个编号的名字
}

// timeoutText 回复单次调用插件超时
const timeoutText = "页面太久没有响应，这个动作可能没有做成。可以先 snapshot 看看现在的页面，或者换一个网址。"

// TimeoutText 是单次调用插件超时时给模型的话（循环检测的测试要用）
const TimeoutText = timeoutText

func (s *TaskState) numberPicture(img tool.Image) string {
	if s.NumberPicture == nil {
		return ""
	}
	return s.NumberPicture(img)
}

func (s *TaskState) labelsOf(elements string) map[int]string {
	if s.LabelsOf == nil {
		return nil
	}
	return s.LabelsOf(elements)
}

// call 调用插件，期限是 toolTimeout，成功时把结果解析到 out（out 为 nil 就不解析）。
// 成功返回 (nil, nil)；失败时返回给模型看的结果，或者 err 不为 nil（这时结果的内容不重要）。
func (s *TaskState) Call(ctx context.Context, tabID int, cmd string, args, out any) (*tool.Result, error) {
	return s.CallFor(ctx, s.Timeout, tabID, cmd, args, out)
}

// callFor 和 call 一样，期限由调用方给：收割要边滚边等图加载，比别的命令慢得多
func (s *TaskState) CallFor(ctx context.Context, timeout time.Duration, tabID int, cmd string, args, out any) (*tool.Result, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	data, err := s.Browser.Call(callCtx, tabID, cmd, args)
	if err != nil {
		var cmdErr *browser.CommandError
		if errors.As(err, &cmdErr) && cmdErr.Code == "TAB_NOT_FOUND" {
			s.TabID = 0 // 标签页被关掉了，下一次 open 重新开
		}
		res, err := browserFailure(ctx, err)
		return &res, err
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			log.Warnf("purchase ai: bad %s result from the extension: %v", cmd, err)
			return &tool.Result{Failed: true, Text: "插件返回的结果无法解析，可以再试一次。"}, nil
		}
	}
	return nil, nil
}

// browserFailure 把插件的错误分成三种（⑤）。ctx 是整次任务的，不是单次调用的：
//   - ctx 结束了：原样返回，循环立刻停
//   - 插件没连上或者断开了：返回 tool.ErrBrowserGone，循环走收尾
//   - 其他的都是模型换个做法就可能解决的：返回 Failed 的结果
func browserFailure(ctx context.Context, err error) (tool.Result, error) {
	if ctx.Err() != nil {
		return tool.Result{}, ctx.Err()
	}

	if errors.Is(err, browser.ErrDisconnected) || errors.Is(err, browser.ErrNotConnected) {
		return tool.Result{}, tool.ErrBrowserGone
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return tool.Result{
			Text:   timeoutText,
			Failed: true,
		}, nil
	}

	var cmdErr *browser.CommandError
	if errors.As(err, &cmdErr) {
		return commandFailure(cmdErr), nil
	}

	return tool.Result{Failed: true, Text: "插件执行失败：" + err.Error()}, nil
}

// commandFailure 把插件的错误码翻译成给模型看的话，告诉它下一步可以怎么做
func commandFailure(cmdErr *browser.CommandError) tool.Result {
	var text string
	switch cmdErr.Code {
	case "ELEMENT_NOT_FOUND":
		text = "没有这个编号的元素，页面可能已经变了。先 snapshot，再用新的编号。"
	case "NOT_EDITABLE":
		text = "这个元素不是输入框，填不了文字。先 snapshot，找到输入框（<input> 或 <textarea>）的编号。"
	case "TAB_NOT_FOUND":
		text = "标签页已经被关掉了。用 open 重新打开网页。"
	case "PAGE_NOT_SCRIPTABLE":
		text = "浏览器不允许操作这个页面（比如浏览器自己的设置页）。换一个网址。"
	default:
		text = "插件执行失败：" + cmdErr.Message
	}
	return tool.Result{Failed: true, Text: text}
}

// requireTab 在还没开标签页时返回给模型的提示
func (s *TaskState) RequireTab() *tool.Result {
	if s.TabID != 0 {
		return nil
	}
	return &tool.Result{Failed: true, Text: "还没有打开任何网页，先用 open 打开一个网址。"}
}

// checkURL 只允许 http 和 https：javascript:、file:、chrome:// 都不能打开
func checkURL(raw string) *tool.Result {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return &tool.Result{Failed: true, Text: "网址格式不对，只能打开 http:// 或 https:// 开头的完整网址。"}
	}
	return nil
}

// pageState 是循环检测眼里的"页面"：快照的内容，加上滚到了哪里。
// 只看快照的话，连续往下滚、中间不看页面时，每次的页面都"一样"，正常加载更多也会被当成打转
func PageState(snapshotHash string, pagesAbove float64) string {
	return fmt.Sprintf("%s|%.1f", snapshotHash, pagesAbove)
}

// hashText 给快照算一个短哈希，只用来比较两份快照是不是一样
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
