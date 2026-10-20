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
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif" // image.Decode 认得 gif、png（jpeg 在 shots.go 里已经引入了）
	"image/jpeg"
	_ "image/png"
	"path"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/sandbox"
	"github.com/disintegration/imaging"
	"github.com/segmentfault/pacman/log"
	_ "golang.org/x/image/webp" // 网上的图很多是 webp
)

// 沙箱里的四个工具（概念 4）。都是"参数 → Workspace → 给模型的文字或图"，不知道 Cube 的存在。
// 路径不用设防：沙箱里的东西本来就随模型读写，隔离靠 MicroVM（选"文件留在沙箱里"的好处）

const (
	bashDefaultTimeout = 60 * time.Second
	// bashMaxTimeout 不能比整次任务的 runTimeout（5 分钟）长太多，不然一条命令就把任务耗完了
	bashMaxTimeout = 120 * time.Second
	// bashOutputMax 是 stdout、stderr 各自给模型看最多多少个字。怎么分给开头和结尾由你定（bashResult 第 4 步）
	bashOutputMax = 4000
	// readFileMaxLines 是 read_file 一次最多给多少行，再多就要用 offset 翻页
	readFileMaxLines = 400
	// workspaceTimeout 是要一个沙箱最多等多久：新建约 0.2 秒，暂停了的恢复更快，10 秒足够
	workspaceTimeout = 10 * time.Second
)

const (
	workspaceDownText  = "工作台（沙箱）暂时用不了，这次先不用它，直接回答用户。"
	workspaceResetText = "注意：这个对话原来的工作台已经被回收了，这是新的一个：之前生成的文件都不在了，harvest/ 下的素材也没了，要用就重新收割。"
)

type bashArgs struct {
	Command string `json:"command" description:"要执行的 bash 命令，在 /home/user 下执行，比如 python3 collage.py、ls harvest | head、pip install pillow"`
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

// withWorkspace 包一层：先拿到这次任务的沙箱再执行工具，原来的沙箱没了的提醒加在第一个结果的最前面
func (r *agentRun) withWorkspace(run func(context.Context, Workspace, string) (toolResult, error)) func(context.Context, string) (toolResult, error) {
	return func(ctx context.Context, arguments string) (toolResult, error) {
		ws, failed, err := r.workspace(ctx)
		if failed != nil || err != nil {
			return *failed, err
		}
		res, err := run(ctx, ws, arguments)
		if err == nil && r.wsNote != "" {
			res.Text = r.wsNote + "\n\n" + res.Text
			r.wsNote = ""
		}
		return res, err
	}
}

// workspace 交出这次任务的沙箱，第一次用时才去要（概念 2 第 1 点：懒创建）。
// 要不到时返回给模型看的结果；err 只在整次任务结束了的时候不为 nil
func (r *agentRun) workspace(ctx context.Context) (Workspace, *toolResult, error) {
	if r.ws != nil {
		return r.ws, nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, workspaceTimeout)
	defer cancel()
	ws, reset, err := r.workspaces.Acquire(callCtx, r.chatID, r.userID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &toolResult{}, ctx.Err()
		}
		log.Warnf("purchase ai: cannot get a sandbox for chat %d: %v", r.chatID, err)
		return nil, &toolResult{Failed: true, Text: workspaceDownText}, nil
	}
	r.ws = ws
	if reset {
		r.wsNote = workspaceResetText
	}
	return ws, nil, nil
}

// sandboxPath 把模型写的路径变成沙箱里的绝对路径：相对路径从 sandbox.Home 算起
func sandboxPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !path.IsAbs(p) {
		p = path.Join(sandbox.Home, p)
	}
	return path.Clean(p)
}

// ---------- bash ----------

func (r *agentRun) bash(ctx context.Context, ws Workspace, arguments string) (toolResult, error) {
	var args bashArgs
	if res := decodeArgs(arguments, &args, `{"command": "ls harvest | head"}`); res != nil {
		return *res, nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return toolResult{Failed: true, Text: "参数无效：command 不能为空。"}, nil
	}
	timeout := bashDefaultTimeout
	if args.Timeout > 0 {
		timeout = min(time.Duration(args.Timeout)*time.Second, bashMaxTimeout)
	}
	res, err := ws.Run(ctx, args.Command, timeout)
	return r.bashResult(ctx, res, err, timeout)
}

// bashResult 把一条命令的结果写给模型看。一条命令可能在三个不同的地方出问题（概念 4）：
// 沙箱连不上（传输层）、跑太久被杀（进程层）、程序自己失败（退出码不是 0）。
//
// TODO(精华，你来写)，按这个顺序：
//  1. err 不为 nil 时先看 ctx.Err()：整次任务结束了（用户关了页面、超时）就原样返回 ctx.Err()，循环会立刻停
//  2. err 不为 nil、ctx 还活着：传输层出错，沙箱连不上了。把 r.ws 设成 nil，下一次调用会重新 Acquire
//     （沙箱真没了的话会重建，并带上 workspaceResetText）；返回 Failed 的结果，告诉模型工作台暂时连不上，可以再试一次
//  3. res.TimedOut：进程跑过了 timeout 被杀掉了。告诉模型跑了多久被终止、可以怎么拆小。
//     算不算 Failed 你来定：Failed 会计入连续失败，连着 3 次就收尾（failureCounter）
//  4. 其余情况都是程序跑完了：退出码不管是不是 0 都**不算** Failed，它是信息不是故障，模型看了 traceback 自己修。
//     Text 写上退出码、stdout、stderr（分开标注，空的不写）；每一段超过 bashOutputMax 个字就截断：
//     结尾留得比开头多（报错和 traceback 在最后），中间写上省略了多少。按 rune 截，别把汉字切成两半
//  5. Display 是给用户看的一句话，只写退出码这类没有内容的东西（resultSummary 里说的白名单）
func (r *agentRun) bashResult(ctx context.Context, res sandbox.RunResult, err error, timeout time.Duration) (toolResult, error) {
	return toolResult{Failed: true, Text: "bash 还没写好。"}, nil
}

// ---------- read_file ----------

func (r *agentRun) readFile(ctx context.Context, ws Workspace, arguments string) (toolResult, error) {
	var args readFileArgs
	if res := decodeArgs(arguments, &args, `{"path": "harvest/items.json"}`); res != nil {
		return *res, nil
	}
	p := sandboxPath(args.Path)
	if p == "" {
		return toolResult{Failed: true, Text: "参数无效：path 不能为空。"}, nil
	}
	data, err := ws.Download(ctx, p)
	if err != nil {
		if ctx.Err() != nil {
			return toolResult{}, ctx.Err()
		}
		return toolResult{Failed: true, Text: fmt.Sprintf("读不到 %s：%v。文件可能不存在，用 bash 的 ls 看看。", p, err)}, nil
	}
	return r.fileResult(p, data, args.Offset, args.Limit), nil
}

// fileResult 按文件的真实类型决定怎么给模型看（概念 4）。
//
// TODO(精华，你来写)，按这个顺序：
//  1. 用 http.DetectContentType(data) 看开头的魔数判断类型，不看扩展名：.jpg 结尾的不一定是 jpeg
//  2. 图片（image/png、image/jpeg、image/gif、image/webp）：imageShot 转成 jpeg，再 r.numberPicture 编号。
//     返回的 toolResult 要带上 Image（"data:image/jpeg;base64," + shot.Data），模型才看得见；
//     Text 里写尺寸，接上 numberPicture 返回的那句话；Display 也写尺寸。转不了的当二进制处理
//  3. text/ 开头的（也包括 application/json）：numberedLines 加行号、按 offset 和 limit 截取，
//     Text 里写清一共多少行、这次是第几到第几行、还有没有没读到的
//  4. 其余的是二进制（zip、pdf）：只告诉模型类型和大小，不把乱码塞给它
func (r *agentRun) fileResult(p string, data []byte, offset, limit int) toolResult {
	return toolResult{Failed: true, Text: "read_file 还没写好。"}
}

// imageShot 把沙箱里的一张图（png、jpeg、gif、webp）转成和截图一样的 jpeg，宽不超过 imageMaxWidth，
// 这样就能和截图共用编号「图N」，回答引用了也一样存到服务器给用户看
func imageShot(data []byte) (screenshotData, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return screenshotData{}, err
	}
	if img.Bounds().Dx() > imageMaxWidth {
		img = imaging.Resize(img, imageMaxWidth, 0, imaging.Lanczos)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: imageQuality}); err != nil {
		return screenshotData{}, err
	}
	return screenshotData{
		Data:   base64.StdEncoding.EncodeToString(buf.Bytes()),
		Mime:   "image/jpeg",
		Width:  img.Bounds().Dx(),
		Height: img.Bounds().Dy(),
		// URL 留空：这张图出自沙箱，没有网页出处，存下来以后也不带「来源」链接
	}, nil
}

// numberedLines 给文本加上行号（像 cat -n），只取从第 offset 行起（从 1 开始）的 limit 行。
// 返回加好行号的文字、实际取的是第 from 到第 to 行、一共几行。offset 超过总行数时 from > to，lines 为空
func numberedLines(text string, offset, limit int) (lines string, from, to, total int) {
	all := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if n := len(all); n > 0 && all[n-1] == "" {
		all = all[:n-1] // 文件最后的换行不算一行
	}
	total = len(all)
	from = max(offset, 1)
	if limit <= 0 || limit > readFileMaxLines {
		limit = readFileMaxLines
	}
	to = min(from+limit-1, total)

	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i, all[i-1])
	}
	return b.String(), from, to, total
}

// ---------- write_file / edit_file ----------

func (r *agentRun) writeFile(ctx context.Context, ws Workspace, arguments string) (toolResult, error) {
	var args writeFileArgs
	if res := decodeArgs(arguments, &args, `{"path": "collage.py", "content": "..."}`); res != nil {
		return *res, nil
	}
	p := sandboxPath(args.Path)
	if p == "" {
		return toolResult{Failed: true, Text: "参数无效：path 不能为空。"}, nil
	}
	if err := ws.Upload(ctx, p, []byte(args.Content)); err != nil {
		if ctx.Err() != nil {
			return toolResult{}, ctx.Err()
		}
		return toolResult{Failed: true, Text: fmt.Sprintf("写不进 %s：%v", p, err)}, nil
	}
	return toolResult{
		Text:    fmt.Sprintf("已写入 %s（%d 字节）。", p, len(args.Content)),
		Display: "写入 " + path.Base(p),
	}, nil
}

// editFile 把文件里的一段原文换成新文字。替换在 Go 里做，沙箱只负责存字节：先取出来，换好再放回去。
// 原文要恰好出现一次，否则不知道改的是哪一处；报错写给模型看，要让它一次就能改对
func (r *agentRun) editFile(ctx context.Context, ws Workspace, arguments string) (toolResult, error) {
	var args editFileArgs
	if res := decodeArgs(arguments, &args, `{"path": "collage.py", "old_text": "...", "new_text": "..."}`); res != nil {
		return *res, nil
	}
	p := sandboxPath(args.Path)
	if p == "" || args.OldText == "" {
		return toolResult{Failed: true, Text: "参数无效：path 和 old_text 都不能为空。要新建或者整个重写文件用 write_file。"}, nil
	}
	if args.OldText == args.NewText {
		return toolResult{Failed: true, Text: "old_text 和 new_text 一样，没有要改的。"}, nil
	}

	data, err := ws.Download(ctx, p)
	if err != nil {
		if ctx.Err() != nil {
			return toolResult{}, ctx.Err()
		}
		return toolResult{Failed: true, Text: fmt.Sprintf("读不到 %s：%v。要新建文件用 write_file。", p, err)}, nil
	}
	content := string(data)
	count := strings.Count(content, args.OldText)
	switch {
	case count == 0:
		return toolResult{Failed: true, Text: fmt.Sprintf("%s 里找不到 old_text。空格、缩进、换行都要一字不差，先用 read_file 看看现在的内容。", p)}, nil
	case count > 1 && !args.ReplaceAll:
		return toolResult{Failed: true, Text: fmt.Sprintf("old_text 在 %s 里出现了 %d 次，不知道改哪一处。多带几行上下文让它只出现一次；要全部替换就把 replace_all 设成 true。", p, count)}, nil
	}

	n := 1
	if args.ReplaceAll {
		n = -1
	}
	if err := ws.Upload(ctx, p, []byte(strings.Replace(content, args.OldText, args.NewText, n))); err != nil {
		if ctx.Err() != nil {
			return toolResult{}, ctx.Err()
		}
		return toolResult{Failed: true, Text: fmt.Sprintf("写不回 %s：%v", p, err)}, nil
	}
	replaced := 1
	if args.ReplaceAll {
		replaced = count
	}
	return toolResult{
		Text:    fmt.Sprintf("已修改 %s，替换了 %d 处。", p, replaced),
		Display: "修改 " + path.Base(p),
	}, nil
}
