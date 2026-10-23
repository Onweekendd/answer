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
	"crypto/sha256"
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/tool/sandboxtool"
	"github.com/apache/answer/internal/service/uploader"
	"github.com/segmentfault/pacman/log"
)

// import_file：把资源放进工作台。工作台是一台隔开的电脑，用户传的图、之前存下来的图、交付过的文件
// 它都够不着（我们的存储在服务器这一边，工作台自己下载不了，也不该让它去下载），要用就得送过去。
// 来源只有两种，都在我们自己手里：
//   - 图N：这次任务里截过、看过的图，就在内存里，不用再取一次，也不经过模型
//   - 我们自己存的文件的网址：用户传的（uploads/post）、Agent 存的图（uploads/agent）、
//     交付的文件（uploads/agent-files），本地磁盘或者对象存储（S3）上的，由服务端读了送进去，见 uploader.ReadUpload
//
// 不收别的网址：这个工具不能变成「替模型抓任意网址」，网址是模型写的，模型的输入来自外部网页

// ResourceReader 读我们自己存的文件。uploader.UploaderService 实现了它
type ResourceReader interface {
	ReadUpload(ctx context.Context, fileURL string) (name string, data []byte, err error)
}

// importer 是 import_file
type importer struct {
	pics  *pictures
	store ResourceReader // nil 就只能送图N
}

// plugin 装 import_file。只在有沙箱时装
func (i *importer) plugin(bench *sandboxtool.Bench) agent.Plugin {
	return func(r *agent.Registry) {
		r.Tool(tool.Spec{Name: toolImport, Description: "把一个资源放进工作台（/home/user 下），工作台里的脚本和网页才用得上它。source 填两种之一：" +
			"「图N」：你这次截过、看过的图（screenshot、view_image 得到的），比如商品实拍图、价格区、买家秀，放进 images/；" +
			"或者我们自己服务器上存的文件的网址：用户上传的图、之前存下来的图、交付过的文件（形如 https://站点/uploads/post/…、/uploads/agent/…、/uploads/agent-files/…），放进 resources/。" +
			"别的网站的网址不行：京东、天猫这些外站的图，先用 screenshot 或 view_image 看到它、得到图N 再放。" +
			"放好以后网页里用 <img src=\"images/文件名\"> 引用，交付时自动内嵌。收割（harvest）收下的图已经在 harvest/ 下，不用再放。",
			Args: importArgs{}, Run: bench.With(i.run)})
	}
}

type importArgs struct {
	Source string `json:"source" description:"「图N」（比如 图3），或者我们服务器上存的文件的网址"`
	Path   string `json:"path,omitempty" description:"放成什么文件名，相对 /home/user，比如 images/jd-mini-se.jpg；不填图N 按内容起名放 images/，网址的放 resources/ 保留原名"`
}

// pictureRef 认「图3」「[图3]」「图 3」
var pictureRef = regexp.MustCompile(`^[\[【(（]?\s*图\s*(\d+)\s*[\]】)）]?$`)

func (i *importer) run(ctx context.Context, ws Workspace, arguments string) (tool.Result, error) {
	var args importArgs
	if res := tool.DecodeArgs(arguments, &args, `{"source": "图2"} 或 {"source": "https://站点/uploads/post/xxx.jpg", "path": "resources/me.jpg"}`); res != nil {
		return *res, nil
	}
	source := strings.TrimSpace(args.Source)
	var (
		name, defaultPath string
		data              []byte
		size              string
		failed            *tool.Result
	)
	if m := pictureRef.FindStringSubmatch(source); m != nil {
		n, _ := strconv.Atoi(m[1])
		name, data, size, failed = i.picture(n)
		defaultPath = "images/" + name
	} else {
		var err error
		name, data, failed, err = i.resource(ctx, source)
		if err != nil {
			return tool.Result{}, err
		}
		defaultPath = "resources/" + name
	}
	if failed != nil {
		return *failed, nil
	}
	target, bad := workspacePath(args.Path, defaultPath)
	if bad != "" {
		return tool.Result{Failed: true, Text: bad}, nil
	}
	if err := ws.Upload(ctx, "/home/user/"+target, data); err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		return tool.Result{Failed: true, Text: fmt.Sprintf("没能放进工作台：%v。可以再试一次。", err)}, nil
	}

	text := fmt.Sprintf("放好了 %s（%s%d KB）。", target, size, (len(data)+1023)/1024)
	if isImageName(target) {
		text += fmt.Sprintf("网页里用 <img src=\"%s\"> 引用，交付时会自动内嵌。", target)
	} else {
		text += "在工作台里用 bash、read_file 读它。"
	}
	return tool.Result{Text: text, Display: "把「" + path.Base(target) + "」放进了工作台"}, nil
}

// picture 取图N 的字节。名字按内容起：同一张图放两次是同一个文件，不同的图不会互相覆盖。size 是「宽×高，」，写进给模型的话里
func (i *importer) picture(n int) (name string, data []byte, size string, failed *tool.Result) {
	if n < 1 || n > len(i.pics.list) {
		return "", nil, "", &tool.Result{Failed: true, Text: fmt.Sprintf("没有「图%d」：现在编了 %d 张图。先 screenshot 或 view_image 看到它。", n, len(i.pics.list))}
	}
	shot := i.pics.list[n-1].shot
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil || len(data) == 0 {
		return "", nil, "", &tool.Result{Failed: true, Text: fmt.Sprintf("「图%d」的图片数据不能用，重新 screenshot 一次。", n)}
	}
	ext := ".jpg"
	switch shot.Mime {
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x%s", sum[:4], ext), data, fmt.Sprintf("%d×%d，", shot.Width, shot.Height), nil
}

// resource 读我们自己存的文件。读不了的原因要让模型分得清：不是我们存的（换办法）、已经不在了（别重试）、临时出错（可以再试）
func (i *importer) resource(ctx context.Context, source string) (name string, data []byte, failed *tool.Result, err error) {
	if i.store == nil {
		return "", nil, &tool.Result{Failed: true, Text: "现在只能放「图N」：这里没有接文件存储。"}, nil
	}
	name, data, err = i.store.ReadUpload(ctx, source)
	switch {
	case err == nil:
		return name, data, nil, nil
	case ctx.Err() != nil:
		return "", nil, nil, ctx.Err()
	case stderrors.Is(err, uploader.ErrNotUpload):
		return "", nil, &tool.Result{Failed: true, Text: "这不是我们服务器上存的文件：source 填「图N」，或者 uploads/post、uploads/agent、uploads/agent-files 下的网址。" +
			"外站的图（京东、天猫）先 screenshot 或 view_image 看到它、得到图N 再放。"}, nil
	case stderrors.Is(err, uploader.ErrUploadNotFound):
		return "", nil, &tool.Result{Failed: true, Text: "这个文件已经不在了，不要再试。"}, nil
	}
	log.Warnf("purchase ai: cannot read %s into the workspace: %v", source, err)
	return "", nil, &tool.Result{Failed: true, Text: fmt.Sprintf("读不出这个文件：%v。可以再试一次，还不行就不要它。", err)}, nil
}

// workspacePath 是放进工作台的文件名（相对 /home/user）。模型给了就用它，要合规矩；没给用 fallback。
// 不合规矩的返回要告诉模型的话
func workspacePath(given, fallback string) (target, failed string) {
	given = strings.TrimSpace(given)
	if given == "" {
		return fallback, ""
	}
	clean := path.Clean(strings.TrimPrefix(given, "/home/user/"))
	if path.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "path 要写成相对 /home/user 的文件名，比如 images/jd.jpg。"
	}
	return clean, ""
}

func isImageName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	}
	return false
}
