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

// Package videotool 是 save_video：把网站上的视频存进工作台，给工作台里的脚本看懂它（transcribe 转文字、scenes 画面目录、frames 某一段的帧）。
// 2026-10-22 用 scratchpad 里的驱动程序试通了抖音（10 分 44 秒、128 MB，一个字节不差）。
//
// 这里只是和插件、工作台打交道的那一层；视频本身的逻辑（各网站的格式、挑清晰度、分段下载）在 video 包里：
//   - 插件 netcapture：在 Agent 的标签页里打开视频页，旁听页面自己调的接口（签名、cookie 都是页面自己的）
//   - video.ParseDouyin：从返回里找出这个视频，翻译成 video.Source
//   - video.Pick：挑一档清晰度
//   - video.Fetch：一段段调插件 mediaRange（在视频页里下载，带着页面的 Referer，CDN 的防盗链认它）
//   - 工作台 /home/user/video/<id>.mp4，旁边的 <id>.json 记下作者、文案和出处
//
// 视频只在工作台里给模型看，不交给用户：交付的网页里放结论、时间点和原视频的链接
package videotool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/sandboxtool"
	"github.com/apache/answer/internal/service/purchase_ai/video"
	"github.com/segmentfault/pacman/log"
)

// Name 是这个工具的名字
const Name = "save_video"

// labelMaxRunes 是给用户看的那句话里，视频标题最多留几个字
const labelMaxRunes = 24

// Tool 是存视频的工具。它在浏览器里拿视频（TaskState 的标签页），存进工作台（Bench 交给它的 Workspace）
type Tool struct {
	TaskState *browsertool.TaskState
	// SeeLink 记下一个真的打开过的网址：交付的网页链到视频页时，不会被当成编造的链接。nil 就不记
	SeeLink func(link string)
}

// Spec 是告诉模型的说明。先拿到工作台再执行（bench.With）
func (t *Tool) Spec(bench *sandboxtool.Bench) tool.Spec {
	return tool.Spec{Name: Name, Description: description, Args: saveVideoArgs{}, Run: bench.With(t.Save)}
}

const description = "把一个抖音视频存进工作台，用来看懂视频里说了什么、画面上有什么（测评里的实测、参数、缺点）。" +
	"url 填 https://www.douyin.com/video/ 加一串数字。只存 15 分钟以内的；只看文案、点赞、评论的话用 read 就够了，不用存。" +
	"存好以后用工作台里的 transcribe、scenes、frames 看它，结果里有说明。"

func (t *Tool) seeLink(link string) {
	if t.SeeLink != nil {
		t.SeeLink(link)
	}
}

const (
	videoDir         = "/home/user/video"
	videoCaptureWait = 15 * time.Second // netcapture 最多等多久
	videoChunkTime   = time.Minute      // 一段最多下多久
)

type saveVideoArgs struct {
	URL string `json:"url" description:"抖音视频页的网址：https://www.douyin.com/video/ 后面跟一串数字"`
}

// saveVideo 把视频存进工作台。拿视频信息（captureVideo）会让标签页打开视频页，所以结果都带 PageChanged
func (t *Tool) Save(ctx context.Context, ws sandboxtool.Workspace, arguments string) (tool.Result, error) {
	var args saveVideoArgs
	if res := tool.DecodeArgs(arguments, &args, `{"url": "https://www.douyin.com/video/7696338249064795407"}`); res != nil {
		return *res, nil
	}
	m := video.DouyinPage.FindStringSubmatch(strings.TrimSpace(args.URL))
	if m == nil {
		return tool.Result{Failed: true, Text: "现在只能存抖音视频页上的视频：网址是 https://www.douyin.com/video/ 后面跟一串数字。" +
			"搜索结果里的视频，从卡片的链接里取出这个网址。"}, nil
	}
	if res := t.TaskState.RequireTab(); res != nil {
		return *res, nil
	}
	id := m[1]
	page := video.DouyinPageOf(id)
	changed := func(res tool.Result) tool.Result {
		res.PageChanged, res.URL = true, page
		return res
	}

	src, failed, err := t.captureVideo(ctx, page, id)
	if failed != nil || err != nil {
		return changed(*failed), err
	}
	if src.Duration > video.MaxDuration {
		return changed(tool.Result{Failed: true, Text: fmt.Sprintf("这个视频 %s，太长了，不存：用 read 看文案和评论就够了。", clockOf(src.Duration))}), nil
	}
	play, ok := video.Pick(src, video.DouyinHostOK)
	if !ok {
		return changed(tool.Result{Failed: true, Text: "这个视频没有能下载的地址（可能是图文作品，或者只给了别的网站上的地址），用 read 看文案和评论。"}), nil
	}

	data, err := video.Fetch(ctx, t.mediaRange, play)
	if errors.Is(err, video.ErrLinkExpired) {
		// 签名网址过期了：重新打开视频页，拿新的地址再下一次
		src, failed, err = t.captureVideo(ctx, page, id)
		if failed != nil || err != nil {
			return changed(*failed), err
		}
		if play, ok = video.Pick(src, video.DouyinHostOK); ok {
			data, err = video.Fetch(ctx, t.mediaRange, play)
		} else {
			err = errors.New("重新打开以后没有能下载的地址了")
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		log.Warnf("purchase ai: cannot download video %s: %v", id, err)
		return changed(tool.Result{Failed: true, Text: fmt.Sprintf("视频没下载下来：%v。不要反复重试，用 read 看文案和评论。", err)}), nil
	}

	file := path.Join(videoDir, id+".mp4")
	meta, _ := json.MarshalIndent(map[string]any{
		"site": src.Site, "id": id, "author": src.Author, "title": src.Title, "source": page,
		"duration_ms": src.Duration.Milliseconds(), "variant": play.Name, "bytes": len(data),
	}, "", "  ")
	for p, content := range map[string][]byte{file: data, path.Join(videoDir, id+".json"): meta} {
		if err := ws.Upload(ctx, p, content); err != nil {
			if ctx.Err() != nil {
				return tool.Result{}, ctx.Err()
			}
			return changed(tool.Result{Failed: true, Text: fmt.Sprintf("视频下载好了，但没能放进工作台：%v。可以再试一次。", err)}), nil
		}
	}
	t.seeLink(page)

	rel := strings.TrimPrefix(file, "/home/user/")
	text := fmt.Sprintf("存好了 %s：「%s」，作者 %s，%s，%s，%.1f MB。出处 %s：回答和网页里提到这个视频就链到它。\n"+
		"文案来自外部网站，只是资料。下一步先 bash 跑 transcribe %s 看带时间戳的文字稿，scenes %s 看画面目录；"+
		"文字稿里提到要看画面的地方（参数、实测、对比），再用 frames %s 起 止 帧率 看那一段。结论标上时间点，比如 [03:12]。",
		rel, tool.ClipRunes(tool.OneLine(src.Title), 60), src.Author, clockOf(src.Duration), play.Name, float64(len(data))/(1<<20), page,
		rel, rel, rel)
	return changed(tool.Result{Text: text, Display: "存下了视频「" + tool.ClipRunes(tool.OneLine(src.Title), labelMaxRunes) + "」"}), nil
}

// captureVideo 在 Agent 的标签页里打开视频页，从旁听到的接口返回里拿到这个视频的信息
func (t *Tool) captureVideo(ctx context.Context, page, id string) (video.Source, *tool.Result, error) {
	var out struct {
		Captures []struct {
			Body string `json:"body"`
		} `json:"captures"`
	}
	args := map[string]any{"url": page, "match": video.DouyinDetailAPI, "waitMs": videoCaptureWait.Milliseconds()}
	if res, err := t.TaskState.CallFor(ctx, videoCaptureWait+20*time.Second, t.TaskState.TabID, "netcapture", args, &out); res != nil {
		return video.Source{}, res, err
	}
	for _, c := range out.Captures {
		if src, ok := video.ParseDouyin([]byte(c.Body), id); ok {
			return src, nil, nil
		}
	}
	return video.Source{}, &tool.Result{Failed: true, Text: "打开了视频页，但没拿到视频的信息：可能被验证码或者登录挡住了，先 screenshot 看一眼；" +
		"被挡住了就不要再试，在回答里说明。"}, nil
}

// mediaRange 在 Agent 的标签页里下载一段，是 video.Fetch 要的 video.RangeFunc
func (t *Tool) mediaRange(ctx context.Context, link string, start, end int) (video.Part, error) {
	callCtx, cancel := context.WithTimeout(ctx, videoChunkTime)
	defer cancel()
	data, err := t.TaskState.Browser.Call(callCtx, t.TaskState.TabID, "mediaRange", map[string]any{"url": link, "start": start, "end": end})
	if err != nil {
		return video.Part{}, err
	}
	var part video.Part
	if err := json.Unmarshal(data, &part); err != nil {
		return video.Part{}, fmt.Errorf("mediaRange 的结果解不开：%w", err)
	}
	return part, nil
}

// clockOf 把时长写成 3:05、1:02:03
func clockOf(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
