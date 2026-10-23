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

package agent

import (
	"context"
	"strings"

	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// DoneName 是结束任务的工具的名字
const DoneName = "done"

// DoneArgs 是 done 的参数。后续建议不在这里：模型愿意写就写在 text 末尾的 <followups> 块里，忘了就算了
type DoneArgs struct {
	Text    string `json:"text" description:"给用户的完整回答，用 Markdown"`
	Success bool   `json:"success" description:"用户要求的事有没有做成。没查到、被登录页或验证码挡住时填 false"`
}

// Done 是 done 工具的插件。放在所有插件的最后，工具定义里它也就在最后。
// 装了它，模型回了文字就会被提醒调用 done；没装，直接回答就是答案
func Done() Plugin {
	return func(r *Registry) {
		r.Tool(tool.Spec{
			Name:        DoneName,
			Description: "任务结束时调用：text 是给用户的完整回答，success 表示用户要求的事有没有做成。调用以后不能再操作浏览器。",
			Args:        DoneArgs{},
			Run:         runDone,
		})
	}
}

func runDone(_ context.Context, arguments string) (tool.Result, error) {
	var args DoneArgs
	if res := tool.DecodeArgs(arguments, &args, `{"text": "...", "success": true}`); res != nil {
		return *res, nil
	}
	if strings.TrimSpace(args.Text) == "" {
		return tool.Result{Failed: true, Text: "参数无效：text 不能为空，要写上给用户的完整回答。"}, nil
	}
	return tool.Result{Text: "已结束。", Terminate: true, Answer: args.Text, Success: args.Success}, nil
}
