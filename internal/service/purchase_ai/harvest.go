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
	"image/color"
	"image/draw"
	"image/jpeg"
	"strconv"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/sandbox"
	"github.com/disintegration/imaging"
	"github.com/segmentfault/pacman/log"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// 收割（概念 3）：插件在列表页上边滚边收卡片，缩略图由 Go 放进沙箱，模型只拿到一段摘要和一张编了号的总览图。
// 100 个 base64 塞进上下文就是上百万 token，所以图本身从来不经过模型

const (
	harvestDefaultLimit = 60
	// harvestMaxLimit 受总览图限制：100 张是 10×10，1600 像素见方，约 1700 个 token；再多一张图就看不清了
	harvestMaxLimit = 100
	// harvestMaxScrolls 和插件的默认值一样
	harvestMaxScrolls = 10
	// harvestTimeout：100 张带缩略图实测 18 秒（Pinterest，2026-10-20），留足余量
	harvestTimeout = 90 * time.Second
)

type harvestArgs struct {
	Limit int `json:"limit,omitempty" description:"最多收多少张，默认 60，最多 100"`
}

// harvestData 是插件 harvest 命令的结果（插件 protocol.ts 的 HarvestResult）
type harvestData struct {
	Items []struct {
		URL       string `json:"url"`
		Title     string `json:"title"`
		Original  string `json:"original"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		ThumbData string `json:"thumbData"` // base64 的 jpeg，插件没拿到就是空的
	} `json:"items"`
	Stats struct {
		Scrolls   int  `json:"scrolls"`
		Seen      int  `json:"seen"`
		Sponsored int  `json:"sponsored"`
		NoImage   int  `json:"noImage"`
		Ended     bool `json:"ended"`
		// ThumbWidth 是缩略图压到了多宽，页面的增强定的（插件的 ListSteps.thumbWidth）。
		// 比 harvestPickWidth 宽说明页面上拿不到更大的图，缩略图就是素材本身
		ThumbWidth int `json:"thumbWidth"`
	} `json:"stats"`
}

// harvestPickWidth 是只够挑选的缩略图有多宽（插件的默认值）
const harvestPickWidth = 200

// thumbsAreMaterial 说缩略图是不是就是素材：页面上拿不到更大的图（小红书列表只有 640 宽的封面预览，原图要点开笔记）
func (d harvestData) thumbsAreMaterial() bool {
	return d.Stats.ThumbWidth > harvestPickWidth
}

func (r *agentRun) harvest(ctx context.Context, ws Workspace, arguments string) (toolResult, error) {
	var args harvestArgs
	if res := decodeArgs(arguments, &args, `{} 或 {"limit": 60}`); res != nil {
		return *res, nil
	}
	if res := r.requireTab(); res != nil {
		return *res, nil
	}
	limit := harvestDefaultLimit
	if args.Limit > 0 {
		limit = min(args.Limit, harvestMaxLimit)
	}

	var data harvestData
	req := map[string]any{"limit": limit, "maxScrolls": harvestMaxScrolls, "thumbs": true}
	if res, err := r.callFor(ctx, harvestTimeout, r.tabID, "harvest", req, &data); res != nil {
		return *res, err
	}

	items := make([]sandbox.Item, 0, len(data.Items))
	for _, it := range data.Items {
		thumb, _ := base64.StdEncoding.DecodeString(it.ThumbData) // 解不开就当插件没拿到
		item := sandbox.Item{
			Title: oneLine(it.Title), URL: it.URL, Original: it.Original,
			Width: it.Width, Height: it.Height, Thumb: thumb,
		}
		if data.thumbsAreMaterial() {
			// 原图地址就是缩略图那张，清单里给了，模型会从工作台去这个网站下载（不是用户的浏览器，容易被拦）
			item.Original = ""
		}
		items = append(items, item)
	}
	entries := sandbox.Number(items)
	if len(entries) == 0 {
		return toolResult{Failed: true, Text: "这一页没收到卡片：可能不是搜索结果、瀑布流这类列表页，或者内容还没加载出来。先 snapshot 看看页面。"}, nil
	}

	if err := sandbox.Seed(ctx, ws, entries); err != nil {
		if ctx.Err() != nil {
			return toolResult{}, ctx.Err()
		}
		log.Warnf("purchase ai: cannot put the harvest into the sandbox: %v", err)
		return toolResult{Failed: true, Text: "收到了卡片，但没能放进工作台，可以再收一次。"}, nil
	}

	text := harvestText(entries, data)
	res := toolResult{Text: text, Display: fmt.Sprintf("收了 %d 张图", len(entries))}
	sheet, err := contactSheet(entries)
	if err != nil {
		log.Warnf("purchase ai: cannot draw the contact sheet: %v", err)
		res.Text += "\n\n总览图没画出来，要看图就用 read_file 打开 harvest/ 下的文件。"
		return res, nil
	}
	res.Text += "\n\n总览图在下一条消息里，格子左上角的数字就是编号。" + r.numberPicture(sheet)
	res.Image = "data:" + sheet.Mime + ";base64," + sheet.Data
	return res, nil
}

// harvestText 是收割的摘要：收了多少、放在哪、清单里有什么
func harvestText(entries []sandbox.Entry, data harvestData) string {
	noThumb := len(data.Items) - len(entries) // Number 跳过了没有缩略图的
	var b strings.Builder
	fmt.Fprintf(&b, "收了 %d 张（滚了 %d 屏，一共见过 %d 张，去掉广告 %d 张", len(entries), data.Stats.Scrolls, data.Stats.Seen, data.Stats.Sponsored)
	if data.Stats.NoImage > 0 {
		fmt.Fprintf(&b, "，还没加载出图的 %d 张", data.Stats.NoImage)
	}
	b.WriteString("）。\n")
	if data.thumbsAreMaterial() {
		fmt.Fprintf(&b, "素材放在工作台的 %s/：001.jpg 这样按编号命名的图（最宽 %d 像素）已经是这个网站列表页上能拿到的最大图，直接用它们做素材；", sandbox.HarvestDir, data.Stats.ThumbWidth)
		b.WriteString("items.json 是清单，每条有 n（编号）、file、title、url（出处）。编号 = 文件名 = 清单里的 n。")
		b.WriteString("\n不要用 bash 从这个网站另外下载图：工作台不是用户的浏览器，去这个网站下载容易被拦，还可能连累用户的账号。")
	} else {
		fmt.Fprintf(&b, "素材放在工作台的 %s/：001.jpg 这样按编号命名的是缩略图（%d 像素宽，只够挑选和预览）；", sandbox.HarvestDir, max(data.Stats.ThumbWidth, harvestPickWidth))
		b.WriteString("items.json 是清单，每条有 n（编号）、file、title、url（出处）、original（原图地址）。编号 = 文件名 = 清单里的 n。")
	}
	if noThumb > 0 {
		fmt.Fprintf(&b, "\n另有 %d 张插件没拿到缩略图，没有收。", noThumb)
	}
	if !data.Stats.Ended {
		b.WriteString("\n下面还有没收到的；要更多就滚动以后再收一次，会替换掉这次收的。")
	}
	return b.String()
}

// 总览图：每格一张缩略图，左上角是编号。10 列，宽不超过 imageMaxWidth
const (
	sheetCols = 10
	sheetCell = 155 // 10 格加 11 道缝是 1594 像素
	sheetGap  = 4
)

// contactSheet 把编了号的缩略图拼成一张总览图。没有缩略图的格子是灰的，编号照样写上
func contactSheet(entries []sandbox.Entry) (screenshotData, error) {
	cols := min(len(entries), sheetCols)
	rows := (len(entries) + cols - 1) / cols
	width := cols*sheetCell + (cols+1)*sheetGap
	height := rows*sheetCell + (rows+1)*sheetGap
	canvas := imaging.New(width, height, color.White)

	empty := imaging.New(sheetCell, sheetCell, color.Gray{Y: 0xdd})
	for i, e := range entries {
		x := sheetGap + (i%cols)*(sheetCell+sheetGap)
		y := sheetGap + (i/cols)*(sheetCell+sheetGap)
		cell := image.Image(empty)
		if len(e.Thumb) > 0 {
			if img, _, err := image.Decode(bytes.NewReader(e.Thumb)); err == nil {
				cell = imaging.Fit(img, sheetCell, sheetCell, imaging.Lanczos)
			}
		}
		// 不是正方形的图放在格子正中
		at := image.Pt(x+(sheetCell-cell.Bounds().Dx())/2, y+(sheetCell-cell.Bounds().Dy())/2)
		canvas = imaging.Paste(canvas, cell, at)
		canvas = imaging.Paste(canvas, numberLabel(e.N), image.Pt(x, y))
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: imageQuality}); err != nil {
		return screenshotData{}, err
	}
	return screenshotData{
		Data:   base64.StdEncoding.EncodeToString(buf.Bytes()),
		Mime:   "image/jpeg",
		Width:  width,
		Height: height,
	}, nil
}

// numberLabel 是黑底白字的编号。basicfont 是内置的 7×13 点阵字体，不用带字体文件；放大两倍才看得清
func numberLabel(n int) image.Image {
	text := strconv.Itoa(n)
	face := basicfont.Face7x13
	w, h := 7*len(text)+4, 15
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: face, Dot: fixed.P(2, 12)}
	d.DrawString(text)
	return imaging.Resize(img, w*2, h*2, imaging.NearestNeighbor)
}
