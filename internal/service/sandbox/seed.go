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

package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 收割的素材放进沙箱（概念 3）：
//
//	/home/user/harvest/
//	  001.jpg … 100.jpg   缩略图
//	  items.json          清单，[]Entry
//
// 编号在三处对齐：总览图上的「12」= 012.jpg = items.json 里 n 为 12 的那条。
// 模型看着总览图说「留 3、12、47」，写脚本时就能按编号找到文件和出处

// HarvestDir 是素材所在的目录
const HarvestDir = Home + "/harvest"

// Item 是收割到的一张卡片
type Item struct {
	Title    string
	URL      string // 出处：卡片打开的那个页面
	Original string // 原图地址
	Width    int    // 缩略图本身的尺寸
	Height   int
	Thumb    []byte // 缩略图（jpeg），插件没拿到就是空的
}

// Entry 是编了号的一张，也是 items.json 里的一条
type Entry struct {
	N        int    `json:"n"`
	File     string `json:"file"` // 相对 HarvestDir 的文件名，比如 012.jpg
	Title    string `json:"title"`
	URL      string `json:"url,omitempty"`
	Original string `json:"original,omitempty"`
	Width    int    `json:"w,omitempty"`
	Height   int    `json:"h,omitempty"`
	Thumb    []byte `json:"-"` // 不进清单：图本身在文件里
}

// Target 是 Seed 用到的沙箱操作，定义在用它的这一边：Box 满足它，purchase_ai 的 Workspace 也满足它
type Target interface {
	Upload(ctx context.Context, path string, data []byte) error
	Run(ctx context.Context, cmd string, timeout time.Duration) (RunResult, error)
}

// seedRunTimeout 是解包那条命令的期限：几 MB 的 tar，一秒都用不了
const seedRunTimeout = 30 * time.Second

// Number 给收割到的卡片编号。纯函数，总览图和 Seed 用的是同一份结果，编号才对得上。
//
// 没有缩略图的卡片跳过，不编号：这样清单里的每一条都有文件，模型写的脚本
// （for it in items: Image.open("harvest/" + it["file"])）不用处理 file 为空的例外。
// 丢掉的很少（插件超了 3MB 预算、缓存里没有才会缺），而且模型看不到的图本来也挑不了。
// 编号用单独的计数器而不是下标，跳过一张以后编号照样连续：001、002、003，不会是 001、002、004
func Number(items []Item) []Entry {
	entries := make([]Entry, 0, len(items))
	n := 0
	for _, item := range items {
		if len(item.Thumb) == 0 {
			continue
		}
		n++
		entries = append(entries, Entry{
			N:        n,
			File:     fmt.Sprintf("%03d.jpg", n),
			Title:    item.Title,
			URL:      item.URL,
			Original: item.Original,
			Width:    item.Width,
			Height:   item.Height,
			Thumb:    item.Thumb,
		})
	}
	return entries
}

// seedUploadPath 是 tar 在沙箱里的中转位置：上传和解包用同一个常量，两边才对得上
const seedUploadPath = "/tmp/harvest.tar"

// Seed 把编好号的素材放进沙箱：打成一个 tar 传一次，再用 bash 解开（概念 3 第 3 点：一个 bundle，
// 不是一百个小请求；解包要么全成功要么全失败）。
// 每一步失败都包上是哪一步（%w 留着原来的错误，调用方还能 errors.Is）
func Seed(ctx context.Context, t Target, entries []Entry) error {
	var files []tarFile
	for _, e := range entries {
		if e.File == "" {
			continue
		}
		files = append(files, tarFile{Name: e.File, Data: e.Thumb})
	}

	manifest, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("sandbox: harvest manifest: %w", err)
	}
	files = append(files, tarFile{Name: "items.json", Data: manifest})

	archive, err := tarFiles(files)
	if err != nil {
		return fmt.Errorf("sandbox: pack harvest: %w", err)
	}

	if err := t.Upload(ctx, seedUploadPath, archive); err != nil {
		return fmt.Errorf("sandbox: upload harvest: %w", err)
	}

	// 先删旧目录：同一个对话收第二次时，上次的图不能混进来。&& 串起来，哪一段失败整条的退出码就不是 0
	cmd := "rm -rf " + HarvestDir + " && mkdir -p " + HarvestDir +
		" && tar -xf " + seedUploadPath + " -C " + HarvestDir + " && rm " + seedUploadPath
	res, err := t.Run(ctx, cmd, seedRunTimeout)
	// 三层失败（概念 4）：沙箱连不上在 err 里；超时和命令失败 err 都是 nil，要看 res
	if err != nil {
		return fmt.Errorf("sandbox: unpack harvest: %w", err)
	}
	if res.TimedOut {
		return fmt.Errorf("sandbox: unpack harvest: timed out after %v", seedRunTimeout)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("sandbox: unpack harvest: exit %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// tarFile 是要打进 tar 的一个文件
type tarFile struct {
	Name string
	Data []byte
}

// tarFiles 在内存里打一个 tar，不落盘
func tarFiles(files []tarFile) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		hdr := &tar.Header{Name: f.Name, Mode: 0o644, Size: int64(len(f.Data)), ModTime: time.Now()}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return nil, err
		}
	}
	// Close 写出 tar 结尾的两个空块，不 Close 的话 tar -x 会说文件被截断了
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
