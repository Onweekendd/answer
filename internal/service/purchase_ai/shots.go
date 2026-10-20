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
	"image/jpeg"
	"regexp"
	"strconv"
	"strings"
)

// 图：Agent 截的图（screenshot）和下载的原图（view_image）都编成「图N」，模型自己看；
// 回答里写 [图N：说明] 引用了，才存到我们的服务器给用户看。看和存证是同一张图，不用为了存证再截一次。
// 没引用的只是 Agent 探路时看过，不存：里面可能有登录页、个人信息，也不占存储。
//   - screenshot：截屏，什么都能截（价格区、表格），但受屏幕缩放限制，小字会糊
//   - view_image：页面上本来就是图片的（商品图、详情长图、买家秀），插件下载原图，清楚得多
// 和商品卡片一样，图片网址由系统给，模型只写占位：让模型抄网址，抄错一个字就是一张裂图

// Shot 是回答引用了、存下来给用户看的一张图
type Shot struct {
	ID     int    `json:"id"`     // 图几，从 1 开始，就是 Agent 看到的编号
	Image  string `json:"image"`  // 存下来的图片网址
	Source string `json:"source"` // 图所在页面的网址，清洗过；广告跳转之类的没有
}

// picture 是这次任务里看过的一张图，还没存
type picture struct {
	shot   screenshotData
	source string
}

type viewImageArgs struct {
	Index *int `json:"index,omitempty" description:"图片所在元素的编号，取这个元素里最大的那张图；不填取屏幕上最大的图"`
}

// imageData 是插件 image 命令的结果：下载下来、转成 jpeg 的原图
type imageData struct {
	screenshotData
	Src string `json:"src"` // 图片原来的地址，只记日志用
}

const (
	maxShots        = 4    // 一次回答最多给用户看几张图：再多就喧宾夺主了
	maxPictures     = 40   // 一次任务最多编几张图的号；图在内存里，等回答引用
	imageMaxWidth   = 1600 // 下载的原图：详情长图里的字要看清，宽一点
	imageQuality    = 85
	shotCaptionRune = 60
)

// citeHint 告诉模型这张图的编号和怎么引用
func citeHint(id int) string {
	return fmt.Sprintf("这是「图%d」：能给结论作证、要给用户看的话，在回答里用到它的地方单独写一行 [图%d：一句话说明它证明了什么]；"+
		"没用的、截错了的不要引用。", id, id)
}

// numberPicture 给看过的图编号，返回告诉模型的那句话。空白的图和编满了的不编号
func (r *agentRun) numberPicture(shot screenshotData) string {
	if blank, err := looksBlank(shot.Data); err == nil && blank {
		return "这张几乎是一片空白：图可能还没加载出来，或者轮播已经切走了，没有编号。"
	}
	if len(r.pictures) >= maxPictures {
		return fmt.Sprintf("这次已经看了 %d 张图，这张不编号了。", maxPictures)
	}
	source, _ := cleanProductURL(shot.URL)
	r.pictures = append(r.pictures, picture{shot: shot, source: source})
	return citeHint(len(r.pictures))
}

// viewImage 下载页面上的原图给模型看，和截图一起编号
func (r *agentRun) viewImage(ctx context.Context, arguments string) (toolResult, error) {
	var args viewImageArgs
	if res := decodeArgs(arguments, &args, `{} 或 {"index": 3}`); res != nil {
		return *res, nil
	}
	if res := r.requireTab(); res != nil {
		return *res, nil
	}
	req := map[string]any{"maxWidth": imageMaxWidth, "quality": imageQuality}
	if args.Index != nil {
		req["index"] = *args.Index
	}
	var img imageData
	if res, err := r.call(ctx, r.tabID, "image", req, &img); res != nil {
		return *res, err
	}
	if img.Data == "" || img.Mime == "" {
		return toolResult{Failed: true, Text: "插件没有返回图片。"}, nil
	}
	return toolResult{
		Text:    fmt.Sprintf("已下载原图（%d×%d），图片在下一条消息里。", img.Width, img.Height) + r.numberPicture(img.screenshotData),
		Display: fmt.Sprintf("查看原图（%d×%d）", img.Width, img.Height),
		Image:   "data:" + img.Mime + ";base64," + img.Data,
		URL:     img.URL,
	}, nil
}

// citedShots 把回答引用了的图存到服务器，按引用的顺序，最多 maxShots 张。
// 存不成的就没有：withShots 会把它的引用去掉，回答照常是文字
func (r *agentRun) citedShots(ctx context.Context, text string) []Shot {
	var shots []Shot
	seen := map[int]bool{}
	for _, m := range shotRef.FindAllStringSubmatch(text, -1) {
		id, _ := strconv.Atoi(m[1])
		if id < 1 || id > len(r.pictures) || seen[id] || len(shots) == maxShots {
			continue
		}
		seen[id] = true
		pic := r.pictures[id-1]
		if image := r.saveImage(ctx, &pic.shot); image != "" {
			shots = append(shots, Shot{ID: id, Image: image, Source: pic.source})
		}
	}
	return shots
}

// blankShare 是一张图里同一种颜色占多少才算空白。真的截图里，大片留白的价格区也就七八成
const blankShare = 0.94

// looksBlank 看一张 jpeg（base64）是不是几乎只有一种颜色：抽 48×48 个点，颜色粗分以后数最多的那种
func looksBlank(data string) (bool, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return false, err
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return false, err
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return true, nil
	}
	const grid = 48
	counts := map[[3]uint32]int{}
	most := 0
	for i := 0; i < grid; i++ {
		for j := 0; j < grid; j++ {
			x := b.Min.X + (2*i+1)*b.Dx()/(2*grid)
			y := b.Min.Y + (2*j+1)*b.Dy()/(2*grid)
			r, g, bl, _ := img.At(x, y).RGBA()
			key := [3]uint32{r >> 12, g >> 12, bl >> 12} // 16 级，jpeg 的噪点和渐变不算不同的颜色
			counts[key]++
			most = max(most, counts[key])
		}
	}
	return float64(most) >= blankShare*grid*grid, nil
}

// shotRef 是回答里的 [图N] 或 [图N：说明]：全角的括号、【】、中间有空格也算
var shotRef = regexp.MustCompile(`[\[［【]\s*图\s*(\d+)\s*(?:[：:]\s*([^\]］】\n]*?))?\s*[\]］】]`)

var blankLines = regexp.MustCompile(`\n{3,}`)

// withShots 把回答里的 [图N] 换成图片和说明，shots 是存下来的那几张（citedShots）。
// 没存下来的编号去掉：没引用的图本来就不给用户看。text 要先过打码再传进来：说明在 text 里，
// 图片网址里的数字也不能被当成手机号
func withShots(text string, shots []Shot) string {
	if !shotRef.MatchString(text) {
		return text
	}
	byID := make(map[int]Shot, len(shots))
	for _, s := range shots {
		byID[s.ID] = s
	}
	used := make(map[int]bool, len(shots))
	text = shotRef.ReplaceAllStringFunc(text, func(ref string) string {
		m := shotRef.FindStringSubmatch(ref)
		n, _ := strconv.Atoi(m[1])
		s, ok := byID[n]
		if !ok {
			return ""
		}
		if used[n] {
			return "图" + strconv.Itoa(n) // 第二次提到同一张图就只写文字
		}
		used[n] = true
		return "\n\n" + shotMarkdown(s, m[2]) + "\n\n"
	})
	return strings.TrimSpace(blankLines.ReplaceAllString(text, "\n\n"))
}

// shotMarkdown 是一张图：图片，下面一行说明和来源
func shotMarkdown(s Shot, caption string) string {
	caption = oneLine(caption)
	if clipped := clipRunes(caption, shotCaptionRune); clipped != caption {
		caption = clipped + "…"
	}
	label := fmt.Sprintf("图%d", s.ID)
	if caption != "" {
		label += "：" + markdownText(caption)
	}
	if s.Source != "" {
		label += fmt.Sprintf("（[来源](%s)）", markdownURL(s.Source))
	}
	return fmt.Sprintf("![图%d](%s)\n*%s*", s.ID, markdownURL(s.Image), label)
}

// markdownText 去掉会改变 Markdown 结构的字符：说明是模型写的
func markdownText(s string) string {
	return strings.NewReplacer("[", "「", "]", "」", "*", "", "_", "", "`", "", "<", "‹", ">", "›").Replace(s)
}

// markdownURL 让网址在 Markdown 的括号里不断开
func markdownURL(u string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(u)
}
