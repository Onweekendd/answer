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
	"encoding/base64"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/links"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/sandboxtool"
	"github.com/segmentfault/pacman/log"
)

// 产物（artifact）：Agent 在工作台里做好、交给用户的东西。第一版只交付一种：一个自包含的网页。
// 交付时顺便用 shot 截一张图：卡片的缩略图用它，用户要发微信群也用它（长辈不会点开一个网址）。
//
// 链路：模型调用 deliver → 从沙箱取出网页、内嵌本地图片、截图 → 存到我们这边（uploader.SaveAgentFile）
// → 记进 Details.Artifact、推一个 artifact 事件 → 页面在回答下面画一张卡片，点开是隔离的 iframe
// （网页由 /uploads/agent-files/ 提供，带 sandbox 的 CSP，见 router/static_router.go 的 agentFile）。
// 以后要交付一个工程（vite 构建出来的一个目录），Artifact 再加 Kind、Source，这条链路不用推翻

// Artifact 是交给用户的一个产物。字段和 frontend/web/src/core/chat/mapper.ts 的 Artifact 一一对应
type Artifact struct {
	ID    int    `json:"id"`              // 这一轮里的第几个，从 1 数：回答里用「文件1」指它
	Title string `json:"title"`           // 模型起的名字，比如「成都 4 天行程」
	URL   string `json:"url"`             // 网页，/uploads/agent-files/xxx.html
	Image string `json:"image,omitempty"` // 交付时截的图；没截成就没有，卡片上显示一个网页图标
	Size  int    `json:"size"`            // 网页有多少字节（内嵌了图片以后的）
}

// ArtifactStore 存交付的文件，返回给用户看的网址。uploader.UploaderService 实现了它；
// case 运行器没有网站，自己给一个存在本地目录的
type ArtifactStore interface {
	SaveAgentFile(ctx context.Context, ext string, data []byte) (string, error)
}

const (
	maxArtifacts = 5 // 一轮最多交付几个：再多用户就不看了
	// artifactShotTimeout：shot 截一页实测 6 秒左右（2026-10-21），长页面、第一次启动 Chromium 慢一些
	artifactShotTimeout = 90 * time.Second
	// inlineMaxImages、inlineMaxBytes 是一个网页最多内嵌多少张、多少字节的图：
	// 存下来的网页不能超过 uploader.AgentFileMaxBytes（20 MB），base64 又要多三分之一
	inlineMaxImages = 80
	inlineMaxBytes  = 12 << 20
)

// deliverer 是交付网页的工具，和这一轮交付了哪些
type deliverer struct {
	bench    *sandboxtool.Bench
	store    ArtifactStore // nil 就交付不了，deliver 会告诉模型存不了
	pres     *presenter
	links    *linkBook    // 网页里的链接核对
	products *productBook // 网页里要链上商品卡片
	list     []Artifact
	at       map[string]int // 工作台里的路径 → list 的下标：再交付同一个文件就是更新它
}

// plugin 装 deliver
func (d *deliverer) plugin() agent.Plugin {
	return func(r *agent.Registry) {
		r.Tool(tool.Spec{Name: toolDeliver, Description: "把工作台里做好的网页交给用户：用户会在回答下方看到一张卡片，点开是网页预览，还能保存成图片发给家人朋友。" +
			"用户要的对比表、清单、行程卡这类东西，做成网页、用 shot 截图看过没问题以后，一定要用它交付；中间稿、脚本、素材不要交付。" +
			"只能交付 .html。网页里引用的工作台里的图会自动内嵌；商品、结论要带上你打开过的商品页和原文链接（<a href>），会自动在新标签页打开；改了以后再交付同一个 path 就是更新它。",
			Args: deliverArgs{}, Run: d.bench.With(d.deliver)})
	}
}

// reminder 在还剩 at 步时提醒一次：用户要的图、表、网页留够步数做。只在有工作台时装
func (d *deliverer) reminder(at int) agent.Plugin {
	return func(r *agent.Registry) {
		r.EndOfTurn(func(t agent.Turn) agent.Verdict {
			if t.Left != at {
				return agent.Verdict{}
			}
			return agent.Verdict{Messages: []llm.Message{llm.Text(llm.RoleUser, deliverWarningText(t.Left))}}
		})
	}
}

// deliverWarningText 在有工作台、剩 deliverAt 步时追加一次：用户要的图、表、网页留够步数做
func deliverWarningText(remaining int) string {
	return fmt.Sprintf("还剩 %d 步。用户要你做图、表格、网页这类东西的话，现在就开始在工作台里做（写网页、shot 截图、看一眼），"+
		"做完还有步数再补查；用户没要就不用管这条。", remaining)
}

type deliverArgs struct {
	Path  string `json:"path" description:"工作台里那个网页的路径，比如 trip.html、out/compare.html。只能是 .html"`
	Title string `json:"title" description:"给用户看的名字，写短一点，比如「成都 4 天行程」「扫地机器人对比」"`
}

// deliver 把工作台里做好的网页交给用户。
// 一次调用做完全部的事：沙箱随时可能被回收，模型调用 deliver 的那一刻文件一定还在，拷出来就不怕了。
// 返回的 error 只在整次任务结束了（用户关了页面、超时）时不为 nil；别的问题都是 Failed，模型可以改了再交
func (d *deliverer) deliver(ctx context.Context, ws Workspace, arguments string) (tool.Result, error) {
	var args deliverArgs
	if res := tool.DecodeArgs(arguments, &args, `{"path": "trip.html", "title": "成都 4 天行程"}`); res != nil {
		return *res, nil
	}
	p := sandboxtool.Path(args.Path)
	if p == "" {
		return tool.Result{Failed: true, Text: "参数无效：path 不能为空。"}, nil
	}
	if strings.ToLower(path.Ext(p)) != ".html" {
		return tool.Result{Failed: true, Text: "只能交付网页（.html）：对比表、清单、行程卡都写成一个网页再交付。" +
			"图片不用单独交付，交付网页时会自动截一张图，用户可以保存下来发给别人。"}, nil
	}
	title := d.pres.cardText(args.Title)
	if title == "" {
		return tool.Result{Failed: true, Text: "参数无效：title 不能为空，写给用户看的名字，比如「成都 4 天行程」。"}, nil
	}
	if d.store == nil {
		return tool.Result{Failed: true, Text: "这里存不了交付的文件。在回答里告诉用户网页做好了、在工作台的哪个位置。"}, nil
	}
	at, again := d.at[p]
	if !again && len(d.list) >= maxArtifacts {
		return tool.Result{Failed: true, Text: fmt.Sprintf("这一轮已经交付了 %d 个文件，不能再交付了。"+
			"把要给用户的内容合进已经交付的网页里，改完再交付同一个 path。", maxArtifacts)}, nil
	}

	// ① 从沙箱取出网页
	page, err := ws.Download(ctx, p)
	if err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		return tool.Result{Failed: true, Text: fmt.Sprintf("读不到 %s：%v。文件可能不存在，用 bash 的 ls 看看。", p, err)}, nil
	}

	// ② 网页要自包含：引用的工作台里的图换成 data URI，不然存到我们这边以后全是裂图
	page, report, err := inlineImages(ctx, ws, path.Dir(p), page)
	if err != nil {
		return tool.Result{}, err
	}
	// 链接洗干净、在新标签页打开，核对这次见没见过（tools_deliver_links.go）
	page, linked := links.Fix(page, d.links.saw, d.links.full)
	report += d.links.text(linked)

	// ③ 存网页。存不了多半是太大了，或者根本不是文本
	pageURL, err := d.store.SaveAgentFile(ctx, ".html", page)
	if err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		log.Warnf("purchase ai: cannot save artifact %s: %v", p, err)
		return tool.Result{Failed: true, Text: fmt.Sprintf("这个网页存不下来：%v。网页连同内嵌的图不能超过 20 MB，图太多就先缩小再放进网页。", err)}, nil
	}

	// ④ 截一张图。截不成不算交付失败：卡片上没有缩略图，网页照样能看
	image, shotErr := d.shot(ctx, ws, p)
	if ctx.Err() != nil {
		return tool.Result{}, ctx.Err()
	}
	if shotErr != nil {
		log.Warnf("purchase ai: cannot screenshot artifact %s: %v", p, shotErr)
	}

	// ⑤ 记下来、推给页面。再交付同一个文件就是更新它，编号不变
	a := Artifact{Title: title, URL: pageURL, Image: image, Size: len(page)}
	if again {
		a.ID = d.list[at].ID
		d.list[at] = a
	} else {
		a.ID = len(d.list) + 1
		d.list = append(d.list, a)
		if d.at == nil {
			d.at = map[string]int{}
		}
		d.at[p] = len(d.list) - 1
	}
	d.pres.emit(ctx, Event{Type: EventArtifact, Artifact: &a})

	verb := "交付"
	if again {
		verb = "更新"
	}
	var text strings.Builder
	fmt.Fprintf(&text, "已%s 文件%d「%s」。用户会在回答下方看到它的卡片，点开是网页预览，也能保存成图片发给别人。"+
		"回答里提到它就写「文件%d」，不要写网址；要改就改了网页再交付同一个 path。", verb, a.ID, a.Title, a.ID)
	if report = strings.TrimSpace(report); report != "" {
		text.WriteString("\n" + report)
	}
	if shotErr != nil {
		fmt.Fprintf(&text, "\n截图没截成（%v），卡片上没有缩略图，网页照样能看。", shotErr)
	}
	return tool.Result{Text: text.String(), Display: verb + "了「" + a.Title + "」", Extras: map[string]any{extraArtifact: &a}}, nil
}

// shot 在工作台里用 shot 把网页截成图，存起来返回网址。截的是沙箱里的原文件：相对路径的图都在
func (d *deliverer) shot(ctx context.Context, ws Workspace, page string) (string, error) {
	out := fmt.Sprintf("/tmp/deliver/%d.png", len(d.list)+1)
	res, err := ws.Run(ctx, "mkdir -p /tmp/deliver && shot "+shellQuote(page)+" "+out, artifactShotTimeout)
	switch {
	case err != nil:
		return "", err
	case res.TimedOut:
		return "", fmt.Errorf("截了 %d 秒没截完", int(artifactShotTimeout.Seconds()))
	case res.ExitCode != 0:
		return "", fmt.Errorf("shot 退出码 %d：%s", res.ExitCode, tool.ClipRunes(tool.OneLine(res.Stderr), 200))
	}
	png, err := ws.Download(ctx, out)
	if err != nil {
		return "", err
	}
	return d.store.SaveAgentFile(ctx, ".png", png)
}

// shellQuote 把一个参数包成 shell 里的单引号字符串：路径里可能有空格、中文、引号
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// 网页里引用图片的两种写法：<img src="…">（还有 <source src>），和样式里的 url(…)。
// 第 2 组是引用的地址，第 1、3 组是它前后的原文，换的时候原样留下
var (
	imgSrcRef = regexp.MustCompile(`(?i)(<(?:img|source)\b[^>]*?\bsrc\s*=\s*["'])([^"']+)(["'])`)
	cssURLRef = regexp.MustCompile(`(?i)(url\(\s*["']?)([^"')]+)(["']?\s*\))`)
)

// inlineImages 把网页里引用的工作台里的图换成 data URI，让网页自包含：
// 存到我们这边以后它是一个单独的文件，相对路径找不到任何东西；CSP 又不许它加载外面的东西。
// 返回换好的网页和一句给模型看的说明（哪些图找不到、哪些引用了外网），没什么要说的就是空的。
// error 只在整次任务结束了时不为 nil
func inlineImages(ctx context.Context, ws Workspace, dir string, page []byte) ([]byte, string, error) {
	cache := map[string]string{} // 工作台里的路径 → data URI；用不了的是空字符串
	var missing, external []string
	inlined, total := 0, 0
	var stop error

	resolve := func(ref string) (string, bool) {
		ref = strings.TrimSpace(ref)
		lower := strings.ToLower(ref)
		switch {
		case strings.HasPrefix(lower, "data:"), strings.HasPrefix(lower, "blob:"), strings.HasPrefix(lower, "#"):
			return "", false
		case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "//"):
			external = append(external, ref)
			return "", false
		}
		// 去掉 ?v=1、#frag 这类后缀，按网页所在的目录解析相对路径
		file := strings.SplitN(strings.SplitN(ref, "?", 2)[0], "#", 2)[0]
		p := path.Clean(path.Join(dir, file))
		if path.IsAbs(file) {
			p = path.Clean(file)
		}
		if uri, ok := cache[p]; ok {
			return uri, uri != ""
		}
		if stop != nil || inlined >= inlineMaxImages {
			return "", false
		}
		data, err := ws.Download(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				stop = ctx.Err()
				return "", false
			}
			cache[p] = ""
			missing = append(missing, ref)
			return "", false
		}
		kind := http.DetectContentType(data)
		if !strings.HasPrefix(kind, "image/") || total+len(data) > inlineMaxBytes {
			cache[p] = ""
			missing = append(missing, ref)
			return "", false
		}
		uri := "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(data)
		cache[p] = uri
		inlined++
		total += len(data)
		return uri, true
	}

	out := string(page)
	for _, re := range []*regexp.Regexp{imgSrcRef, cssURLRef} {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			parts := re.FindStringSubmatch(m)
			if uri, ok := resolve(parts[2]); ok {
				return parts[1] + uri + parts[3]
			}
			return m
		})
	}
	if stop != nil {
		return nil, "", stop
	}

	var report strings.Builder
	if len(missing) > 0 {
		fmt.Fprintf(&report, "这些图在工作台里找不到、不是图片或者太大，网页里会是空白：%s。", strings.Join(firstN(missing, 5), "、"))
	}
	if len(external) > 0 {
		fmt.Fprintf(&report, "网页引用了 %d 处外网的资源（比如 %s），预览里加载不出来：先下载到工作台再引用。", len(external), external[0])
	}
	return []byte(out), report.String(), nil
}

// firstN 是前 n 个，后面还有的话加一句一共几个
func firstN(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return append(append([]string{}, items[:n]...), fmt.Sprintf("一共 %d 个", len(items)))
}
