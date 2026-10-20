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
	File     string `json:"file,omitempty"` // 相对 HarvestDir 的文件名，比如 012.jpg；没有缩略图就是空的
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
// TODO(精华，你来写)，按这个顺序：
//  1. 决定没有缩略图（Thumb 为空）的卡片怎么办：跳过不编号，还是照样占一个号、File 留空？
//     想想总览图上的数字要和什么对齐、模型拿着编号回来问时，这张卡片的出处还要不要找得到。写一句注释说明选择
//  2. 编号从 1 开始连续；File 是三位数补零的 "%03d.jpg"
//  3. 其余字段从 Item 抄过来
func Number(items []Item) []Entry {
	return nil
}

// Seed 把编好号的素材放进沙箱：打成一个 tar 传一次，再用 bash 解开（概念 3 第 3 点：一个 bundle，
// 不是一百个小请求；解包要么全成功要么全失败）。
//
// TODO(精华，你来写)，按这个顺序：
//  1. 拼出要打包的文件：每个有 File 的 Entry 一个 tarFile{Name: e.File, Data: e.Thumb}，
//     再加一个 items.json（json.Marshal(entries)，Thumb 带着 json:"-" 不会进去）
//  2. tarFiles 打包
//  3. t.Upload 传到一个临时路径，比如 /tmp/harvest.tar
//  4. t.Run 解开：先删掉旧的 HarvestDir（同一个对话收第二次时，旧的 100 张不能混进来），
//     再建目录、tar -xf 解进去、删掉临时的 tar。一条命令用 && 串起来，期限 seedRunTimeout
//  5. 检查结果：error 不为 nil、TimedOut、ExitCode 不是 0 都算失败，返回的 error 里带上 Stderr，排查用
func Seed(ctx context.Context, t Target, entries []Entry) error {
	return errTODO
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
