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
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif" // image.Decode 认得 gif、png
	"image/jpeg"
	_ "image/png"
	"net/http"
	"path"
	"strings"

	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // 网上的图很多是 webp
)

// ---------- read_file ----------

func (b *Bench) ReadFile(ctx context.Context, ws Workspace, arguments string) (tool.Result, error) {
	var args readFileArgs
	if res := tool.DecodeArgs(arguments, &args, `{"path": "harvest/items.json"}`); res != nil {
		return *res, nil
	}
	p := Path(args.Path)
	if p == "" {
		return tool.Result{Failed: true, Text: "参数无效：path 不能为空。"}, nil
	}
	data, err := ws.Download(ctx, p)
	if err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		return tool.Result{Failed: true, Text: fmt.Sprintf("读不到 %s：%v。文件可能不存在，用 bash 的 ls 看看。", p, err)}, nil
	}
	return b.fileResult(p, data, args.Offset, args.Limit), nil
}

// fileResult 按文件的真实类型决定怎么给模型看（概念 4）：看开头的魔数，不看扩展名，.jpg 结尾的不一定是 jpeg。
// 图片让模型看见并编号，文本带行号，其余的（zip、pdf）只说类型和大小，不把乱码塞给它
func (b *Bench) fileResult(p string, data []byte, offset, limit int) tool.Result {
	kind := http.DetectContentType(data)
	switch {
	case strings.HasPrefix(kind, "image/"):
		shot, err := imageShot(data)
		if err != nil {
			break // 认得是图但解不开（坏图、svg 这类 image.Decode 不认的），当二进制说
		}
		return tool.Result{
			Text:    fmt.Sprintf("%s 是一张图片（%d×%d），图片在下一条消息里。", p, shot.Width, shot.Height) + b.numberPicture(shot),
			Display: fmt.Sprintf("看了一张图（%d×%d）", shot.Width, shot.Height),
			Image:   "data:" + shot.Mime + ";base64," + shot.Data,
		}
	case strings.HasPrefix(kind, "text/") || kind == "application/json":
		return textFileResult(p, string(data), offset, limit)
	}
	return tool.Result{
		Text:    fmt.Sprintf("%s 不是文本也不是能看的图片（%s，%d 字节），内容不显示。要看里面有什么用 bash（unzip -l、file、pdftotext）。", p, kind, len(data)),
		Display: "不是文本文件",
	}
}

// readFileMaxRunes 是 read_file 一次给模型的文字最多多少个字：一行几十万字的压缩 JSON，行数再少也不能整个塞进去
const readFileMaxRunes = 20000

// textFileResult 给文本文件加上行号，按 offset、limit 取一段，并说清楚还有没有没读到的
func textFileResult(p, text string, offset, limit int) tool.Result {
	lines, from, to, total := numberedLines(text, offset, limit)
	if total == 0 {
		return tool.Result{Text: p + " 是空文件。", Display: "空文件"}
	}
	if from > to {
		return tool.Result{
			Failed: true,
			Text:   fmt.Sprintf("%s 一共只有 %d 行，offset=%d 超出了。", p, total, offset),
		}
	}

	var b strings.Builder
	if runes := []rune(lines); len(runes) > readFileMaxRunes {
		fmt.Fprintf(&b, "%s 一共 %d 行。第 %d 到 %d 行太长（%d 个字），只给前 %d 个字；行很长的文件用 bash 处理（head -c、python3 -m json.tool）。\n\n",
			p, total, from, to, len(runes), readFileMaxRunes)
		b.WriteString(string(runes[:readFileMaxRunes]))
	} else {
		fmt.Fprintf(&b, "%s 一共 %d 行，这是第 %d 到 %d 行", p, total, from, to)
		if to < total {
			fmt.Fprintf(&b, "，还有 %d 行没读，用 offset=%d 接着读", total-to, to+1)
		}
		b.WriteString("。\n\n")
		b.WriteString(lines)
	}
	return tool.Result{
		Text:    b.String(),
		Display: fmt.Sprintf("读了 %d 行", to-from+1),
	}
}

// imageShot 把沙箱里的一张图（png、jpeg、gif、webp）转成和截图一样的 jpeg，宽不超过 imageMaxWidth，
// 这样就能和截图共用编号「图N」，回答引用了也一样存到服务器给用户看
func imageShot(data []byte) (tool.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return tool.Image{}, err
	}
	if img.Bounds().Dx() > imageMaxWidth {
		img = imaging.Resize(img, imageMaxWidth, 0, imaging.Lanczos)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: imageQuality}); err != nil {
		return tool.Image{}, err
	}
	return tool.Image{
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

func (b *Bench) WriteFile(ctx context.Context, ws Workspace, arguments string) (tool.Result, error) {
	var args writeFileArgs
	if res := tool.DecodeArgs(arguments, &args, `{"path": "collage.py", "content": "..."}`); res != nil {
		return *res, nil
	}
	p := Path(args.Path)
	if p == "" {
		return tool.Result{Failed: true, Text: "参数无效：path 不能为空。"}, nil
	}
	if err := ws.Upload(ctx, p, []byte(args.Content)); err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		return tool.Result{Failed: true, Text: fmt.Sprintf("写不进 %s：%v", p, err)}, nil
	}
	return tool.Result{
		Text:    fmt.Sprintf("已写入 %s（%d 字节）。", p, len(args.Content)),
		Display: "写入 " + path.Base(p),
	}, nil
}

// editFile 把文件里的一段原文换成新文字。替换在 Go 里做，沙箱只负责存字节：先取出来，换好再放回去。
// 原文要恰好出现一次，否则不知道改的是哪一处；报错写给模型看，要让它一次就能改对
func (b *Bench) EditFile(ctx context.Context, ws Workspace, arguments string) (tool.Result, error) {
	var args editFileArgs
	if res := tool.DecodeArgs(arguments, &args, `{"path": "collage.py", "old_text": "...", "new_text": "..."}`); res != nil {
		return *res, nil
	}
	p := Path(args.Path)
	if p == "" || args.OldText == "" {
		return tool.Result{Failed: true, Text: "参数无效：path 和 old_text 都不能为空。要新建或者整个重写文件用 write_file。"}, nil
	}
	if args.OldText == args.NewText {
		return tool.Result{Failed: true, Text: "old_text 和 new_text 一样，没有要改的。"}, nil
	}

	data, err := ws.Download(ctx, p)
	if err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		return tool.Result{Failed: true, Text: fmt.Sprintf("读不到 %s：%v。要新建文件用 write_file。", p, err)}, nil
	}
	content := string(data)
	count := strings.Count(content, args.OldText)
	switch {
	case count == 0:
		return tool.Result{Failed: true, Text: fmt.Sprintf("%s 里找不到 old_text。空格、缩进、换行都要一字不差，先用 read_file 看看现在的内容。", p)}, nil
	case count > 1 && !args.ReplaceAll:
		return tool.Result{Failed: true, Text: fmt.Sprintf("old_text 在 %s 里出现了 %d 次，不知道改哪一处。多带几行上下文让它只出现一次；要全部替换就把 replace_all 设成 true。", p, count)}, nil
	}

	n := 1
	if args.ReplaceAll {
		n = -1
	}
	if err := ws.Upload(ctx, p, []byte(strings.Replace(content, args.OldText, args.NewText, n))); err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		return tool.Result{Failed: true, Text: fmt.Sprintf("写不回 %s：%v", p, err)}, nil
	}
	replaced := 1
	if args.ReplaceAll {
		replaced = count
	}
	return tool.Result{
		Text:    fmt.Sprintf("已修改 %s，替换了 %d 处。", p, replaced),
		Display: "修改 " + path.Base(p),
	}, nil
}
