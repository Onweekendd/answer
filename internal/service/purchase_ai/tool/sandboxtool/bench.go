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

package sandboxtool

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/sandbox"
	"github.com/segmentfault/pacman/log"
)

const (
	bashDefaultTimeout = 60 * time.Second
	// bashMaxTimeout 不能比整次任务的 runTimeout（8 分钟）长太多，不然一条命令就把任务耗完了
	bashMaxTimeout = 120 * time.Second
	// bashOutputMax 是 stdout、stderr 各自给模型看最多多少个字。怎么分给开头和结尾由你定（bashResult 第 4 步）
	bashOutputMax = 4000
	// readFileMaxLines 是 read_file 一次最多给多少行，再多就要用 offset 翻页
	readFileMaxLines = 400
	// workspaceTimeout 是要一个沙箱最多等多久：新建约 0.2 秒，暂停了的恢复更快，10 秒足够
	workspaceTimeout = 10 * time.Second
)

const (
	DownText  = "工作台（沙箱）暂时用不了，这次先不用它，直接回答用户。"
	ResetText = "注意：这个对话原来的工作台已经被回收了，这是新的一个：之前生成的文件都不在了，harvest/ 下的素材也没了，要用就重新收割。"
)

// 和 view_image 下载的原图一样：工作台里的图片给模型看之前缩到这么宽
const (
	imageMaxWidth = 1600
	imageQuality  = 85
)

type bashArgs struct {
	Command string `json:"command" description:"要执行的 bash 命令，在 /home/user 下执行，比如 python3 collage.py、ls harvest | head、node shot.js"`
	Timeout int    `json:"timeout,omitempty" description:"最多跑多少秒，默认 60，最多 120。跑得久的任务（下载很多图）拆成几次"`
}

type readFileArgs struct {
	Path   string `json:"path" description:"文件路径，相对路径从 /home/user 算起，比如 harvest/items.json、out/sheet.jpg"`
	Offset int    `json:"offset,omitempty" description:"文本文件从第几行开始读（从 1 开始），文件很长时用来翻页"`
	Limit  int    `json:"limit,omitempty" description:"文本文件最多读几行，默认也最多 400"`
}

type writeFileArgs struct {
	Path    string `json:"path" description:"文件路径，相对路径从 /home/user 算起，比如 collage.py"`
	Content string `json:"content" description:"文件的全部内容，已有的文件会被整个覆盖"`
}

type editFileArgs struct {
	Path       string `json:"path" description:"文件路径，相对路径从 /home/user 算起"`
	OldText    string `json:"old_text" description:"要替换的原文，必须和文件里的一字不差（空格、缩进都算），而且在文件里只出现一次；不唯一就多带几行上下文"`
	NewText    string `json:"new_text" description:"替换成的新文字"`
	ReplaceAll bool   `json:"replace_all,omitempty" description:"为 true 时替换所有出现的地方，比如改一个变量名"`
}

// Bench 是一次任务里工作台这一侧的状态。Agent 嵌入它，所以字段和方法在 Agent 上也能直接用
type Bench struct {
	// Workspaces 交出一个对话的工作台；nil 表示没有沙箱，没有这几个工具
	Workspaces Workspaces
	ChatID     int64  // 这次任务属于哪个对话，一个对话一个沙箱；0 表示没有对话 id，用临时沙箱
	UserID     string // 对话是谁的：Acquire 要核对，不能拿别人的对话 id 用别人的沙箱

	// NumberPicture 给一张图编号（图N），返回告诉模型怎么引用它的一句话。编号是整次任务共用的，由外面给
	NumberPicture func(img tool.Image) string

	ws   Workspace // 第一次用沙箱时才要（懒创建）；连不上了设回 nil，下次重新要
	note string    // 要告诉模型的一句话（原来的沙箱没了），加在下一个工作台工具结果的最前面
}

// With 包一层：先拿到这次任务的工作台再执行工具，原来的工作台没了的提醒加在第一个结果的最前面
func (b *Bench) With(run func(context.Context, Workspace, string) (tool.Result, error)) func(context.Context, string) (tool.Result, error) {
	return func(ctx context.Context, arguments string) (tool.Result, error) {
		ws, failed, err := b.Acquire(ctx)
		if failed != nil || err != nil {
			return *failed, err
		}
		res, err := run(ctx, ws, arguments)
		if err == nil {
			res.Text = b.NoteOnce(res.Text)
		}
		return res, err
	}
}

// Acquire 交出这次任务的工作台，第一次用时才去要（懒创建）。
// 要不到时返回给模型看的结果；err 只在整次任务结束了的时候不为 nil
func (b *Bench) Acquire(ctx context.Context) (Workspace, *tool.Result, error) {
	if b.ws != nil {
		return b.ws, nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, workspaceTimeout)
	defer cancel()
	ws, reset, err := b.Workspaces.Acquire(callCtx, b.ChatID, b.UserID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &tool.Result{}, ctx.Err()
		}
		log.Warnf("purchase ai: cannot get a sandbox for chat %d: %v", b.ChatID, err)
		return nil, &tool.Result{Failed: true, Text: DownText}, nil
	}
	b.ws = ws
	if reset {
		b.note = ResetText
	}
	return ws, nil, nil
}

// NoteOnce 把还没告诉模型的那句提醒加在 text 前面，只加一次
func (b *Bench) NoteOnce(text string) string {
	if b.note == "" {
		return text
	}
	text, b.note = b.note+"\n\n"+text, ""
	return text
}

func (b *Bench) numberPicture(img tool.Image) string {
	if b.NumberPicture == nil {
		return ""
	}
	return b.NumberPicture(img)
}

// Path 把模型写的路径变成沙箱里的绝对路径：相对路径从 sandbox.Home 算起
func Path(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !path.IsAbs(p) {
		p = path.Join(sandbox.Home, p)
	}
	return path.Clean(p)
}
