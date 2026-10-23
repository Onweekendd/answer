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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/sandbox"
	"github.com/segmentfault/pacman/log"
)

func (b *Bench) Bash(ctx context.Context, ws Workspace, arguments string) (tool.Result, error) {
	var args bashArgs
	if res := tool.DecodeArgs(arguments, &args, `{"command": "ls harvest | head"}`); res != nil {
		return *res, nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return tool.Result{Failed: true, Text: "参数无效：command 不能为空。"}, nil
	}
	timeout := bashDefaultTimeout
	if args.Timeout > 0 {
		timeout = min(time.Duration(args.Timeout)*time.Second, bashMaxTimeout)
	}
	res, err := ws.Run(ctx, args.Command, timeout)
	return b.bashResult(ctx, res, err, timeout)
}

// bashResult 把一条命令的结果写给模型看。一条命令可能在三个不同的地方出问题（概念 4）：
// 沙箱连不上（传输层）、跑太久被杀（进程层）、程序自己失败（退出码不是 0）。
// 返回的 error 只在整次任务结束了（用户关了页面、超时）时不为 nil，循环看到它就停
func (b *Bench) bashResult(ctx context.Context, res sandbox.RunResult, err error, timeout time.Duration) (tool.Result, error) {
	if err != nil {
		if ctx.Err() != nil {
			return tool.Result{}, ctx.Err()
		}
		// 传输层：沙箱连不上了。丢掉 b.ws，下一次调用重新 Acquire（真没了会重建并带上 workspaceResetText）。
		// 不能把 err 往上返回：那会让整次任务停下来，而这只是一次调用失败，模型可以再试
		log.Warnf("purchase ai: sandbox command failed for chat %d: %v", b.ChatID, err)
		b.ws = nil
		return tool.Result{Failed: true, Text: "工作台暂时连不上，命令没有执行完。可以再试一次；连着失败就先不用工作台，直接回答用户。"}, nil
	}

	// 进程层：跑过期限被杀掉了。算 Failed：连着超时说明路子不对，failureCounter 会让它收尾
	if res.TimedOut {
		return tool.Result{
			Failed: true,
			Text: fmt.Sprintf("命令跑了 %d 秒还没结束，被终止了，输出也没有拿到。"+
				"把它拆小一点：一次只处理一部分文件、下载的加上超时，或者放到后台跑（nohup … > log.txt 2>&1 &），再用 cat log.txt 看进度。",
				int(timeout.Seconds())),
		}, nil
	}

	// 程序跑完了：退出码不是 0 也不算 Failed，模型看了 traceback 自己修
	var out strings.Builder
	fmt.Fprintf(&out, "退出码 %d", res.ExitCode)
	if res.Stdout == "" && res.Stderr == "" {
		out.WriteString("，没有输出。")
	}
	if res.Stdout != "" {
		out.WriteString("\n\nstdout：\n")
		out.WriteString(clipMiddle(res.Stdout, bashOutputMax))
	}
	if res.Stderr != "" {
		out.WriteString("\n\nstderr：\n")
		out.WriteString(clipMiddle(res.Stderr, bashOutputMax))
	}
	return tool.Result{
		Text:    out.String(),
		Display: fmt.Sprintf("退出码 %d", res.ExitCode), // 输出里可能有网页上的内容，用户这边只说退出码
	}, nil
}

// bashHeadShare 是截断时开头占几分之一：其余留给结尾，报错和 traceback 都在最后
const bashHeadShare = 4

// clipMiddle 把 s 截到最多 max 个字（rune，汉字不会被切成两半）：留开头一小段、结尾一大段，
// 中间写上省略了多少
func clipMiddle(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	head := max / bashHeadShare
	tail := max - head
	return fmt.Sprintf("%s\n…（中间省略了 %d 个字）…\n%s",
		string(runes[:head]), len(runes)-head-tail, string(runes[len(runes)-tail:]))
}
