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
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 引用截图：Agent 觉得对用户有用、能给结论作证的画面（价格和优惠的条件、参数表、差评、价格走势、官方说明），
// 存到我们的服务器，回答里写 [图N]，服务端把它换成图片。图有两种来法：
//   - cite_screenshot：截屏，什么都能截（价格区、表格），但受屏幕缩放限制，小字会糊
//   - cite_image：页面上本来就是图片的（商品图、详情长图、买家秀），插件下载原图，清楚得多
// 和商品卡片一样，图片网址由系统给，模型只写占位：让模型抄网址，抄错一个字就是一张裂图

// Shot 是存下来的一张截图
type Shot struct {
	ID      int    `json:"id"`      // 图几，从 1 开始
	Caption string `json:"caption"` // 模型写的说明，过过打码
	Image   string `json:"image"`   // 存下来的图片网址
	Source  string `json:"source"`  // 截图所在页面的网址，清洗过；广告跳转之类的没有
}

type citeArgs struct {
	Index   *int   `json:"index,omitempty" description:"只截这个编号的元素（推荐）：价格区、参数表、一条评价、价格走势图；不填就截当前这一屏"`
	Caption string `json:"caption" description:"一句话说明这张图证明了什么，比如「京东自营 1849 元，含 200 元券后价」"`
}

type citeImageArgs struct {
	Index   *int   `json:"index,omitempty" description:"图片所在元素的编号，取这个元素里最大的那张图；不填取屏幕上最大的图"`
	Caption string `json:"caption" description:"一句话说明这张图证明了什么，比如「详情页的参数图：屏幕 11 英寸，重 460 克」"`
}

// imageData 是插件 image 命令的结果：下载下来、转成 jpeg 的原图
type imageData struct {
	screenshotData
	Src string `json:"src"` // 图片原来的地址，只记日志用
}

const (
	maxShots        = 4    // 一次回答最多几张图：再多就喧宾夺主了
	shotMaxWidth    = 1200 // 给用户看的图，比给模型看的窄一点，手机上也够清楚
	shotQuality     = 80
	imageMaxWidth   = 1600 // 下载的原图：详情长图里的字要看清，宽一点
	imageQuality    = 85
	shotCaptionRune = 60
)

// citeShot 截一张图存下来，告诉模型它是「图几」
func (r *agentRun) citeShot(ctx context.Context, arguments string) (toolResult, error) {
	var args citeArgs
	if res := decodeArgs(arguments, &args, `{"caption": "..."} 或 {"index": 3, "caption": "..."}`); res != nil {
		return *res, nil
	}
	return r.keepPicture(ctx, "screenshot", args.Index, args.Caption, shotMaxWidth, shotQuality, "截图存证：")
}

// citeImage 下载页面上的原图存下来，告诉模型它是「图几」
func (r *agentRun) citeImage(ctx context.Context, arguments string) (toolResult, error) {
	var args citeImageArgs
	if res := decodeArgs(arguments, &args, `{"caption": "..."} 或 {"index": 3, "caption": "..."}`); res != nil {
		return *res, nil
	}
	return r.keepPicture(ctx, "image", args.Index, args.Caption, imageMaxWidth, imageQuality, "保存原图：")
}

// keepPicture 让插件截图（cmd 是 screenshot）或者下载原图（image），存到服务器，记成下一张图。
// 图片也给模型看一眼，确认是对的
func (r *agentRun) keepPicture(ctx context.Context, cmd string, index *int, caption string,
	maxWidth, quality int, display string) (toolResult, error) {
	if oneLine(caption) == "" {
		return toolResult{Failed: true, Text: "参数无效：caption 不能为空，写一句这张图证明了什么。"}, nil
	}
	if len(r.shots) >= maxShots {
		return toolResult{Failed: true, Text: fmt.Sprintf("已经存了 %d 张图，不能再加。回答里用已有的就行。", maxShots)}, nil
	}
	if res := r.requireTab(); res != nil {
		return *res, nil
	}
	req := map[string]any{"maxWidth": maxWidth, "quality": quality}
	if index != nil {
		req["index"] = *index
	}
	var shot imageData
	if res, err := r.call(ctx, r.tabID, cmd, req, &shot); res != nil {
		return *res, err
	}
	if shot.Data == "" || shot.Mime == "" {
		return toolResult{Failed: true, Text: "插件没有返回图片。"}, nil
	}
	image := r.saveImage(ctx, &shot.screenshotData)
	if image == "" {
		return toolResult{Failed: true, Text: "图片没存成，这张图用不了。回答里照常用文字说明。"}, nil
	}

	caption = oneLine(caption)
	if clipped := clipRunes(caption, shotCaptionRune); clipped != caption {
		caption = clipped + "…"
	}
	source, _ := cleanProductURL(shot.URL)
	s := Shot{ID: len(r.shots) + 1, Caption: r.view(caption), Image: image, Source: source}
	r.shots = append(r.shots, s)

	return toolResult{
		Text: fmt.Sprintf("已存为「图%d」（%d×%d），图片在下一条消息里，确认截对了。"+
			"回答里在用到它的地方单独写一行 [图%d]，用户会在那里看到这张图；截错了就不要引用。", s.ID, shot.Width, shot.Height, s.ID),
		Display: display + s.Caption,
		Image:   "data:" + shot.Mime + ";base64," + shot.Data,
		URL:     shot.URL,
	}, nil
}

// shotRef 是回答里的 [图N]，全角的括号、【】、中间有空格也算
var shotRef = regexp.MustCompile(`[\[［【]\s*图\s*(\d+)\s*[\]］】]`)

var blankLines = regexp.MustCompile(`\n{3,}`)

// withShots 把回答里的 [图N] 换成图片和说明。没引用的图放在最后，存了就给用户看；
// 不存在的编号去掉。text 要先过打码再传进来：图片网址里的数字不能被当成手机号
func withShots(text string, shots []Shot) string {
	if len(shots) == 0 && !shotRef.MatchString(text) {
		return text
	}
	used := make(map[int]bool, len(shots))
	text = shotRef.ReplaceAllStringFunc(text, func(ref string) string {
		n, _ := strconv.Atoi(shotRef.FindStringSubmatch(ref)[1])
		if n < 1 || n > len(shots) {
			return ""
		}
		if used[n] {
			return "图" + strconv.Itoa(n) // 第二次提到同一张图就只写文字
		}
		used[n] = true
		return "\n\n" + shotMarkdown(shots[n-1]) + "\n\n"
	})
	var rest []string
	for _, s := range shots {
		if !used[s.ID] {
			rest = append(rest, shotMarkdown(s))
		}
	}
	if len(rest) > 0 {
		text = strings.TrimRight(text, "\n") + "\n\n**截图**\n\n" + strings.Join(rest, "\n\n")
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(text, "\n\n"))
}

// shotMarkdown 是一张图：图片，下面一行说明和来源
func shotMarkdown(s Shot) string {
	caption := markdownText(s.Caption)
	line := fmt.Sprintf("*图%d：%s*", s.ID, caption)
	if s.Source != "" {
		line = fmt.Sprintf("*图%d：%s（[来源](%s)）*", s.ID, caption, markdownURL(s.Source))
	}
	return fmt.Sprintf("![图%d](%s)\n%s", s.ID, markdownURL(s.Image), line)
}

// markdownText 去掉会改变 Markdown 结构的字符：说明是模型写的
func markdownText(s string) string {
	return strings.NewReplacer("[", "「", "]", "」", "*", "", "_", "", "`", "", "<", "‹", ">", "›").Replace(s)
}

// markdownURL 让网址在 Markdown 的括号里不断开
func markdownURL(u string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(u)
}
