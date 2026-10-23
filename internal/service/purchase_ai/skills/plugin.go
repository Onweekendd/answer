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

package skills

import (
	"context"
	"fmt"
	"strings"

	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// Name 是读 skill 的工具的名字
const Name = "skill_read"

type readArgs struct {
	Path string `json:"path" description:"skill 里的文件路径，比如 site-guide/SKILL.md、site-guide/references/taobao.md"`
}

// Plugin 把 skill_read 装进 Agent。它不碰浏览器，还没打开网页时也能用；读到的内容一直留在对话里，不压缩：它不会过期
func Plugin() agent.Plugin {
	return func(r *agent.Registry) {
		r.Tool(tool.Spec{
			Name: Name,
			Description: "读取 skill 里的文件。系统提示词里列出了可用的 skill：先读它的 SKILL.md，再按里面说的读需要的 reference。" +
				"去一个网站操作之前，先读这个网站的 reference。",
			Args: readArgs{},
			Run:  skillRead,
		})
	}
}

func skillRead(_ context.Context, arguments string) (tool.Result, error) {
	var args readArgs
	if res := tool.DecodeArgs(arguments, &args, `{"path": "site-guide/SKILL.md"}`); res != nil {
		return *res, nil
	}
	content, err := Read(args.Path)
	if err != nil {
		skill, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(args.Path), "/"), "/")
		if files := FilesOf(skill); len(files) > 0 {
			return tool.Result{Failed: true, Text: fmt.Sprintf("没有 %s。这个 skill 里有：%s。", args.Path, strings.Join(files, "、"))}, nil
		}
		return tool.Result{Failed: true, Text: fmt.Sprintf("没有 %s。路径的格式是 skill名/文件，比如 site-guide/SKILL.md。", args.Path)}, nil
	}
	return tool.Result{Text: content}, nil
}
