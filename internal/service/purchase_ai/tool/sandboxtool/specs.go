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
	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// 工作台工具的名字
const (
	NameBash      = "bash"
	NameReadFile  = "read_file"
	NameWriteFile = "write_file"
	NameEditFile  = "edit_file"
)

// Specs 是工作台工具，按固定的顺序。每个都先拿到工作台再执行（With）
func (b *Bench) Specs() []tool.Spec {
	list := []tool.Spec{
		{Name: NameBash, Description: "在工作台（一台独立的 Linux 电脑，装了 python3、node、Chromium）里执行一条 bash 命令，在 /home/user 下执行，返回退出码和输出。" +
			"跑脚本、截图（shot 页面.html 输出.png）、装包都用它；脚本先用 write_file 写进文件再执行，不要把长代码塞进一条命令。",
			Args: bashArgs{}, Run: b.With(b.Bash)},
		{Name: NameReadFile, Description: "读工作台里的一个文件：文本文件带行号返回，可以用 offset、limit 翻页；图片会让你看到，并编成「图N」，能在回答里引用给用户看。" +
			"生成了图片（截好的网页、拼图）就用它打开看一眼，满意了再给用户。",
			Args: readFileArgs{}, Run: b.With(b.ReadFile)},
		{Name: NameWriteFile, Description: "在工作台里新建或整个重写一个文件，比如写一个网页或脚本。只改几行用 edit_file。",
			Args: writeFileArgs{}, Run: b.With(b.WriteFile)},
		{Name: NameEditFile, Description: "修改工作台里的一个文件：把一段原文换成新文字。原文要和文件里一字不差、只出现一次，拿不准就先 read_file。",
			Args: editFileArgs{}, Run: b.With(b.EditFile)},
	}
	return list
}
