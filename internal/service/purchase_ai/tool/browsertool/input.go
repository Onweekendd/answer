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

package browsertool

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

type indexArgs struct {
	Index int `json:"index" description:"元素编号，就是最新一份快照里每行开头方括号中的数字"`
}

type typeArgs struct {
	Index  int    `json:"index" description:"输入框的编号，就是最新一份快照里每行开头方括号中的数字"`
	Text   string `json:"text" description:"要填入的文字，会替换输入框里原来的内容"`
	Submit bool   `json:"submit,omitempty" description:"填完以后是否按回车，比如在搜索框里搜索时填 true"`
}

type pressArgs struct {
	Key string `json:"key" enum:"Escape,ArrowRight,ArrowLeft" description:"要按的键：Escape 关浮层；ArrowRight、ArrowLeft 翻轮播图的下一张、上一张"`
}

type scrollArgs struct {
	Direction string `json:"direction" enum:"up,down" description:"滚动方向：down 往下，up 往上"`
	Pages     int    `json:"pages,omitempty" description:"滚动几屏，默认 1"`
}

func (s *TaskState) Click(ctx context.Context, arguments string) (tool.Result, error) {
	var args indexArgs
	if res := tool.DecodeArgs(arguments, &args, `{"index": 3}`); res != nil {
		return *res, nil
	}
	if res := s.RequireTab(); res != nil {
		return *res, nil
	}
	var act actResult
	if res, err := s.Call(ctx, s.TabID, "click", args, &act); res != nil {
		return *res, err
	}
	return s.afterAct(fmt.Sprintf("已点击 [%d]。", args.Index), act), nil
}

func (s *TaskState) Hover(ctx context.Context, arguments string) (tool.Result, error) {
	var args indexArgs
	if res := tool.DecodeArgs(arguments, &args, `{"index": 3}`); res != nil {
		return *res, nil
	}
	if res := s.RequireTab(); res != nil {
		return *res, nil
	}
	var act actResult
	if res, err := s.Call(ctx, s.TabID, "hover", args, &act); res != nil {
		return *res, err
	}
	return s.afterAct(fmt.Sprintf("已悬停在 [%d]，没有点击。", args.Index), act), nil
}

func (s *TaskState) Type(ctx context.Context, arguments string) (tool.Result, error) {
	var args typeArgs
	if res := tool.DecodeArgs(arguments, &args, `{"index": 0, "text": "...", "submit": true}`); res != nil {
		return *res, nil
	}
	if strings.TrimSpace(args.Text) == "" {
		return tool.Result{Failed: true, Text: "参数无效：text 不能为空。"}, nil
	}
	if res := s.RequireTab(); res != nil {
		return *res, nil
	}
	var act actResult
	if res, err := s.Call(ctx, s.TabID, "type", args, &act); res != nil {
		return *res, err
	}
	if !args.Submit && act.NewTab == nil {
		return tool.Result{Text: fmt.Sprintf("已在 [%d] 里填入「%s」。", args.Index, args.Text)}, nil
	}
	// 按了回车，页面多半会跳转
	return s.afterAct(fmt.Sprintf("已在 [%d] 里填入「%s」并按了回车。", args.Index, args.Text), act), nil
}

// PressKeys 是能按的键，和插件的 PRESS_KEYS 一致；值是给模型、给用户看的说法
var PressKeys = map[string]string{
	"Escape":     "按 Esc 关闭浮层",
	"ArrowRight": "翻到下一张图",
	"ArrowLeft":  "翻到上一张图",
}

// press 按一个键。Escape 以后页面可能变了（浮层关了、网址变回去了），和点击一样要求先 snapshot；
// 方向键只是翻轮播图，页面别的地方不变，编号照样能用，同一轮后面的截图也不用跳过
func (s *TaskState) Press(ctx context.Context, arguments string) (tool.Result, error) {
	var args pressArgs
	if res := tool.DecodeArgs(arguments, &args, `{"key": "Escape"}`); res != nil {
		return *res, nil
	}
	if _, ok := PressKeys[args.Key]; !ok {
		return tool.Result{Failed: true, Text: "只能按 Escape、ArrowRight、ArrowLeft。"}, nil
	}
	if res := s.RequireTab(); res != nil {
		return *res, nil
	}
	var act actResult
	if res, err := s.Call(ctx, s.TabID, "press", args, &act); res != nil {
		return *res, err
	}
	if args.Key == "Escape" {
		return s.afterAct("已按 Escape。", act), nil
	}
	return tool.Result{
		Text: fmt.Sprintf("已按 %s（%s）。快照里的编号还能用；要看这一张，就 screenshot 图片区的编号。", args.Key, PressKeys[args.Key]),
		URL:  act.URL,
	}, nil
}

// actResult 是插件 click / type 的结果
// readTimeout 比别的命令长：边生成边显示的页面（秘塔的 AI 回答），插件要等它写完才读，最多等 45 秒（插件的 READY_WAIT_MS）
const readTimeout = 60 * time.Second

type actResult struct {
	URL    string    `json:"url"`    // 动作之后这个标签页的网址
	NewTab *pageInfo `json:"newTab"` // 动作在新标签页里打开了页面
	// NewTabClosed：动作开了一个新标签页，还没加载完就自己关掉了。多半是文件下载（Word、PDF），
	// 网页里看不了（2026-10-21 秘塔的「管理办法」来源，模型连点三次都是插件报错）
	NewTabClosed bool `json:"newTabClosed"`
}

// afterAct 写出点击、回车之后的结果。在新标签页里打开了的，切换过去并记下来，结束时一起关掉：
// 淘宝的搜索就是这样，不跟过去的话，Agent 会一直盯着没有变化的首页，以为没搜成功
func (s *TaskState) afterAct(done string, act actResult) tool.Result {
	if tab := act.NewTab; tab != nil {
		s.TabID = tab.TabID
		s.OpenedTabs = append(s.OpenedTabs, tab.TabID)
		return tool.Result{
			Text:        done + fmt.Sprintf("结果在新标签页里打开了：「%s」（%s），已经切换到这个标签页。先 snapshot 再继续。", tool.OneLine(tab.Title), tab.URL),
			Display:     "在新标签页里打开了「" + tool.OneLine(tab.Title) + "」",
			PageChanged: true,
			URL:         tab.URL,
		}
	}
	if act.NewTabClosed {
		return tool.Result{
			Text: done + "点开的是一个新标签页，但它还没加载完就自己关掉了：多半是文件下载（Word、PDF），在网页里看不了。" +
				"不要再点它，换一个是网页的来源。页面没有变，快照里的编号还能用。",
			Display: "点开的是文件下载，换一个来源",
			URL:     act.URL,
		}
	}
	return tool.Result{
		Text:        done + fmt.Sprintf("现在的网址是 %s。页面可能已经变了，先 snapshot 再继续。", act.URL),
		PageChanged: true,
		URL:         act.URL,
	}
}

// closeTabs 关掉 Agent 开过的标签页，不在用户的浏览器里留下一堆。
// 整次任务的 ctx 这时可能已经结束了，所以用一个新的、很短的期限；关不掉的就算了
func (s *TaskState) CloseTabs() {
	for _, id := range s.OpenedTabs {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = s.Browser.Call(ctx, id, "close", struct{}{})
		cancel()
	}
	s.OpenedTabs = nil
}

type scrollData struct {
	PagesAbove float64 `json:"pagesAbove"`
	PagesBelow float64 `json:"pagesBelow"`
}

func (s *TaskState) Scroll(ctx context.Context, arguments string) (tool.Result, error) {
	var args scrollArgs
	if res := tool.DecodeArgs(arguments, &args, `{"direction": "down", "pages": 1}`); res != nil {
		return *res, nil
	}
	if args.Direction != "up" && args.Direction != "down" {
		return tool.Result{Failed: true, Text: `参数无效：direction 只能是 "up" 或 "down"。`}, nil
	}
	if args.Pages <= 0 {
		args.Pages = 1
	}
	if res := s.RequireTab(); res != nil {
		return *res, nil
	}
	var pos scrollData
	if res, err := s.Call(ctx, s.TabID, "scroll", args, &pos); res != nil {
		return *res, err
	}
	// 滚动不拍快照，但页面状态变了：位置变了才算有进展，到底了还在滚，位置就不会再变
	s.PageHash = PageState(s.SnapshotHash, pos.PagesAbove)

	direction := "下"
	if args.Direction == "up" {
		direction = "上"
	}
	return tool.Result{
		Text: fmt.Sprintf("已向%s滚动 %d 屏。现在上面还有 %.1f 屏，下面还有 %.1f 屏。先 snapshot 查看新出现的内容。",
			direction, args.Pages, pos.PagesAbove, pos.PagesBelow),
		Display:     fmt.Sprintf("上面还有 %.1f 屏，下面还有 %.1f 屏", pos.PagesAbove, pos.PagesBelow),
		PageChanged: true,
	}, nil
}
