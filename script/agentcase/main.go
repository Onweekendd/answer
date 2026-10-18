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

// agentcase 让 Agent 在你自己的浏览器里做一个任务，打印每一步和最后的回答。
// 现在只能跑一个 -task；1c 会在这里加上 case 文件和"做成了没有"的判定。
//
// 先启动 Hub（answer browser --console=false），插件连上以后：
//
//	BROWSER_HUB_TOKEN=<token> go run ./script/agentcase -task "在淘宝搜 iPhone 17 256GB，看看现在卖多少钱"
//
// 模型从 script/agentcase/llm.local.json 读（不提交，格式见 llm.example.json）。key 不会被打印。
// 每一步只打印工具、参数、网址和耗时，不打印快照内容：快照里可能有你的昵称、地址。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/apache/answer/internal/service/browser"
	"github.com/apache/answer/internal/service/llm"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
)

func main() {
	hub := flag.String("hub", "http://127.0.0.1:8765", "Hub address")
	config := flag.String("llm", "script/agentcase/llm.local.json", "model config (not committed)")
	task := flag.String("task", "", "what the agent should do")
	flag.Parse()

	if err := run(*hub, *config, *task); err != nil {
		fmt.Fprintln(os.Stderr, "agentcase:", err)
		os.Exit(1)
	}
}

func run(hubURL, configPath, task string) error {
	if strings.TrimSpace(task) == "" {
		return fmt.Errorf("-task is required")
	}
	token := os.Getenv("BROWSER_HUB_TOKEN")
	if token == "" {
		return fmt.Errorf("BROWSER_HUB_TOKEN is not set")
	}
	provider, err := readProvider(configPath)
	if err != nil {
		return err
	}

	b := browser.NewDebugClient(hubURL, token)
	if !b.Connected() {
		return fmt.Errorf("the extension is not connected to the hub at %s (start answer browser and check the popup)", hubURL)
	}
	fmt.Printf("model %s, task: %s\n\n", provider.Model, task)

	started := time.Now()
	res, err := purchaseai.RunTask(context.Background(), llm.NewStaticClient(*provider), b, task)
	if err != nil {
		return describe(err)
	}

	printSteps(res.Steps)
	fmt.Printf("\n用时 %s，%d 次工具调用，", time.Since(started).Round(time.Second), len(res.Steps))
	switch {
	case res.Finish != "":
		fmt.Printf("被迫收尾：%s\n", res.Finish)
	case res.Success:
		fmt.Println("模型说做成了")
	default:
		fmt.Println("模型说没做成")
	}
	fmt.Println("\n---- 回答 ----")
	fmt.Println(res.Text)
	return nil
}

// describe 把 Agent 的错误说成一句话。服务商的原话不打印：认证失败时它会带上 key 的最后几位
func describe(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401"):
		return fmt.Errorf("模型服务商认证失败（401）：检查 llm.local.json 里的 api_key 和 api_host 是不是同一家的")
	case strings.Contains(msg, "404"):
		return fmt.Errorf("模型服务商返回 404：检查 api_host 和 model 名字")
	case strings.Contains(msg, "context deadline exceeded"):
		return fmt.Errorf("超时了：整次任务最多 5 分钟，每次调用模型最多 60 秒")
	}
	// 只留第一行：Answer 的错误类型会把调用栈也拼进去
	first, _, _ := strings.Cut(msg, "\n")
	return fmt.Errorf("%s", first)
}

// readProvider 读模型配置。只报告缺了哪个字段，不打印内容
func readProvider(path string) (*llm.StaticProvider, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s (copy llm.example.json and fill it in): %w", path, err)
	}
	var p llm.StaticProvider
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON", path)
	}
	for name, value := range map[string]string{"api_host": p.APIHost, "api_key": p.APIKey, "model": p.Model} {
		if value == "" {
			return nil, fmt.Errorf("%s: %s is empty", path, name)
		}
	}
	return &p, nil
}

func printSteps(steps []purchaseai.TaskStep) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "步\t工具\t参数\t结果\t网址\t耗时")
	for _, s := range steps {
		status := "ok"
		if s.Failed {
			status = "失败"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", s.Step+1, s.Tool, clip(s.Arguments, 60), status, clip(s.URL, 70), s.Duration.Round(100*time.Millisecond))
	}
	_ = w.Flush()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
