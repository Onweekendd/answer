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

// agentcase 让 Agent 在你自己的浏览器里跑 case，判定通过没有，最后输出每个 case 的通过率。
//
// 先启动 Hub（answer browser），插件连上以后：
//
//	BROWSER_HUB_TOKEN=<token> go run ./script/agentcase                      # 跑 cases.yaml 里的全部 case，每个 1 次
//	BROWSER_HUB_TOKEN=<token> go run ./script/agentcase -case 京东 -n 3      # 名字里带"京东"的，每个 3 次
//	BROWSER_HUB_TOKEN=<token> go run ./script/agentcase -task "在淘宝搜……"   # 临时跑一个任务，不判定
//
// 配了 CUBE_TEMPLATE_ID（和 script/dev.sh --cube 同一组环境变量）时 Agent 有工作台，每次一台临时沙箱；-sandbox=false 关掉。
//
// 真实网站只能低频地跑：默认每个 case 1 次，两次运行之间至少等 -gap（默认 30 秒）。
//
// 模型从 script/agentcase/llm.local.json 读（不提交，格式见 llm.example.json）。key 不会被打印。
// 每次运行的完整记录（每次调用模型的请求和回复）写到 -out 目录（不提交），写之前替换屏蔽词；
// 里面有页面内容，只在本机看。终端里只打印工具、参数、网址、耗时和回答，不打印快照。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/apache/answer/internal/service/browser"
	"github.com/apache/answer/internal/service/llm"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
	pacmanerrors "github.com/segmentfault/pacman/errors"
)

type options struct {
	hub, llmConfig, cases, only, task, out, redact string
	sandbox                                        bool
	n                                              int
	gap                                            time.Duration
}

func main() {
	os.Exit(runMain())
}

func runMain() int {
	var o options
	flag.StringVar(&o.hub, "hub", "http://127.0.0.1:8765", "Hub address")
	flag.StringVar(&o.llmConfig, "llm", "script/agentcase/llm.local.json", "model config (not committed)")
	flag.StringVar(&o.cases, "cases", "script/agentcase/cases.yaml", "case file")
	flag.StringVar(&o.only, "case", "", "only run cases whose name contains this")
	flag.StringVar(&o.task, "task", "", "run this one task instead of the case file, without judging")
	flag.IntVar(&o.n, "n", 1, "runs per case")
	flag.DurationVar(&o.gap, "gap", 30*time.Second, "pause between two runs (real sites: keep it low-frequency)")
	flag.StringVar(&o.out, "out", "script/agentcase/out", "where full records go (not committed)")
	flag.BoolVar(&o.sandbox, "sandbox", true, "give the agent a temporary sandbox when CUBE_TEMPLATE_ID is set")
	flag.StringVar(&o.redact, "redact", "script/pagebench/redact.txt", "words to hide in records and answers, one per line (not committed)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, o); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "agentcase:", err)
		return 1
	}
	return 0
}

// outcome 是一次运行的结果。跑不了的（插件没连上、模型服务报错、插件中途断开）不算失败：
// 那是环境的问题，不是 Agent 做错了，算进去会把两件事混在一起（概念 2、4）
type outcome string

const (
	outcomePass    outcome = "通过"
	outcomeFail    outcome = "失败"
	outcomeBlocked outcome = "跑不了"
	outcomeNone    outcome = "未判定" // -task 临时任务
)

type attempt struct {
	c        Case
	i        int
	outcome  outcome
	reason   []string // 失败时没满足的条件；跑不了时的原因
	res      *purchaseai.TaskResult
	usage    llm.Usage
	elapsed  time.Duration
	ran      bool // 真的让 Agent 跑了（跑不了里有一种是根本没开始）
	selfPass bool // 模型自己说做成了
}

func run(ctx context.Context, o options) error {
	token := os.Getenv("BROWSER_HUB_TOKEN")
	if token == "" {
		return fmt.Errorf("BROWSER_HUB_TOKEN is not set")
	}
	provider, err := readProvider(o.llmConfig)
	if err != nil {
		return err
	}
	if o.n < 1 {
		return fmt.Errorf("-n must be at least 1")
	}

	adhoc := strings.TrimSpace(o.task) != ""
	var cases []Case
	if adhoc {
		cases = []Case{{Name: "task", Task: o.task}}
	} else if cases, err = loadCases(o.cases, o.only); err != nil {
		return err
	}

	b := browser.NewHubClient(o.hub, token)
	if !b.Connected() {
		return fmt.Errorf("the extension is not connected to the hub at %s (start answer browser and check the popup)", o.hub)
	}
	// 沙箱：没配 CUBE_TEMPLATE_ID 时 NewWorkspaces 返回 nil，Agent 不给沙箱工具。
	// 只用临时沙箱，不碰数据库，所以 store 给 nil
	var ws purchaseai.Workspaces
	if o.sandbox {
		ws = purchaseai.NewWorkspaces(nil)
	}
	redact := loadRedact(o.redact)
	runDir := filepath.Join(o.out, time.Now().Format("20060102-150405"))
	fmt.Printf("model %s，%d 个 case，每个 %d 次，沙箱 %v，记录写到 %s\n", provider.Model, len(cases), o.n, ws != nil, runDir)

	var all []attempt
	for _, c := range cases {
		for i := 1; i <= o.n; i++ {
			if len(all) > 0 {
				fmt.Printf("\n等 %s 再跑下一次（低频）…\n", o.gap)
				select {
				case <-time.After(o.gap):
				case <-ctx.Done():
				}
			}
			if ctx.Err() != nil {
				fmt.Println("\n中断了，下面是已经跑完的。")
				printSummary(all)
				return nil
			}

			fmt.Printf("\n==== %s（第 %d/%d 次）====\n任务：%s\n", c.Name, i, o.n, c.Task)
			path := filepath.Join(runDir, fmt.Sprintf("%s-%d.json", c.Name, i))
			files := &dirStore{dir: filepath.Join(runDir, fmt.Sprintf("%s-%d-files", c.Name, i))}
			a, err := runOnce(ctx, b, ws, files, *provider, c, i, adhoc, func(rec runRecord) {
				if err := save(path, rec, redact); err != nil {
					fmt.Fprintln(os.Stderr, "save record:", err)
				}
			})
			if err != nil {
				return err
			}
			all = append(all, a)
			printAttempt(a, redact)
		}
	}

	if !adhoc {
		printSummary(all)
	}
	fmt.Printf("\n完整记录在 %s（有页面内容，只在本机看，不要提交）\n", runDir)
	return nil
}

// runOnce 跑一次 case。返回的 error 只给"再跑也没用"的情况（模型配置错了），其余都折算成 outcome。
// 每次调用模型以后、以及结束时，都把当时的记录交给 save：中途被打断也有记录可看
func runOnce(ctx context.Context, b purchaseai.Browser, ws purchaseai.Workspaces, files purchaseai.ArtifactStore, p llm.StaticProvider, c Case, i int, adhoc bool, save func(runRecord)) (attempt, error) {
	a := attempt{c: c, i: i}
	rec := runRecord{Case: c.Name, Attempt: i, Task: c.Task, Outcome: "进行中"}
	defer func() { save(rec) }()

	if !b.Connected() {
		a.outcome, a.reason = outcomeBlocked, []string{"插件没连上 Hub"}
		rec.Outcome, rec.Problems = string(a.outcome), a.reason
		return a, nil
	}

	model := &recorder{inner: llm.NewStaticClient(p)}
	model.after = func(call recordedCall) {
		printLive(len(model.calls), call)
		rec.Usage, rec.Calls = model.usage, model.calls
		save(rec)
	}
	started := time.Now()
	res, err := purchaseai.RunTask(ctx, model, b, c.Task, purchaseai.WithWorkspaces(ws), purchaseai.WithArtifactStore(files))
	a.elapsed, a.usage, a.ran, a.res = time.Since(started), model.usage, true, res
	rec.Usage, rec.Calls = model.usage, model.calls

	switch {
	case err != nil:
		if configError(err) {
			return a, describe(err)
		}
		a.outcome, a.reason = outcomeBlocked, []string{describe(err).Error()}
		rec.Error = a.reason[0]
	case res.Finish == "browser_gone":
		a.outcome, a.reason = outcomeBlocked, []string{"插件中途断开了"}
	case adhoc:
		a.outcome = outcomeNone
	default:
		a.reason = judge(c, res, model.snapshots())
		a.outcome = outcomePass
		if len(a.reason) > 0 {
			a.outcome = outcomeFail
		}
	}
	if res != nil {
		a.selfPass = res.Success
		rec.Result = res
	}
	rec.Outcome, rec.Problems = string(a.outcome), a.reason
	return a, nil
}

// printLive 在每次调用模型以后打一行：模型要执行哪些工具，或者只回了文字。
// 不打印快照和回答内容，回答等结束时再打印
func printLive(n int, call recordedCall) {
	switch {
	case call.Error != "":
		fmt.Printf("  #%d 模型报错\n", n)
	case call.Response == nil:
	case len(call.Response.ToolCalls) == 0:
		fmt.Printf("  #%d 只回了文字（%d 字）\n", n, len([]rune(call.Response.Content)))
	default:
		for _, tc := range call.Response.ToolCalls {
			fmt.Printf("  #%d %s %s\n", n, tc.Name, clip(tc.Arguments, 80))
		}
	}
}

// configError 是模型配置的问题：换哪个 case 都一样，接着跑只是白等
func configError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "401") || strings.Contains(msg, "404")
}

// describe 把 Agent 的错误说成一句话。服务商的原话不打印：认证失败时它会带上 key 的最后几位
func describe(err error) error {
	msg := err.Error()
	// Answer 的错误类型 Error() 只返回 Message，模型服务商的报错在 Err 里：不取出来就是一行空字符串
	var pe *pacmanerrors.Error
	if errors.As(err, &pe) && pe.Err != nil {
		msg = pe.Err.Error()
	}
	switch {
	case strings.Contains(msg, "401"):
		return fmt.Errorf("模型服务商认证失败（401）：检查 llm.local.json 里的 api_key 和 api_host 是不是同一家的")
	case strings.Contains(msg, "404"):
		return fmt.Errorf("模型服务商返回 404：检查 api_host 和 model 名字")
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("被中断了")
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

func printAttempt(a attempt, redact []string) {
	if a.res != nil {
		printSteps(a.res.Steps)
	}
	fmt.Printf("\n结果：%s", a.outcome)
	if a.ran {
		fmt.Printf("（%s，%s token", a.elapsed.Round(time.Second), kilo(a.usage.TotalTokens))
		if a.res != nil {
			fmt.Printf("，%s", selfReport(a.res))
		}
		fmt.Print("）")
	}
	fmt.Println()
	for _, r := range a.reason {
		fmt.Println("  -", r)
	}
	if a.res != nil {
		fmt.Println("---- 回答 ----")
		fmt.Println(redactText(a.res.Text, redact))
		for _, p := range a.res.Products {
			fmt.Println(redactText(fmt.Sprintf("卡片 %d：%s · %s · %s %s %s", p.ID, p.Title, p.Shop, p.Price, p.PriceNote, p.URL), redact))
		}
		for _, f := range a.res.Artifacts {
			fmt.Printf("文件 %d：%s %s（截图 %s）\n", f.ID, f.Title, f.URL, f.Image)
		}
	}
}

// selfReport 是模型对自己的评价，单独一栏，不参与判定
func selfReport(res *purchaseai.TaskResult) string {
	switch {
	case res.Finish != "":
		return "被迫收尾：" + res.Finish
	case res.Success:
		return "模型说做成了"
	default:
		return "模型说没做成"
	}
}

func printSteps(steps []purchaseai.TaskStep) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "步\t工具\t参数\t结果\t网址\t耗时")
	for _, s := range steps {
		status := "ok"
		if s.Failed {
			status = "失败"
		}
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", s.Step+1, s.Tool, clip(s.Arguments, 60), status, clip(s.URL, 70), s.Duration.Round(100*time.Millisecond))
	}
	_ = w.Flush()
}

// printSummary 每个 case 一行。通过率的分母不含跑不了的；步数、token、耗时是真的跑了的那几次的平均
func printSummary(all []attempt) {
	if len(all) == 0 {
		return
	}
	fmt.Println("\n==== 汇总 ====")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "case\t通过率\t跑不了\t模型自评做成\t平均步数\t平均 token\t平均耗时")

	var order []string
	byCase := map[string][]attempt{}
	for _, a := range all {
		if _, ok := byCase[a.c.Name]; !ok {
			order = append(order, a.c.Name)
		}
		byCase[a.c.Name] = append(byCase[a.c.Name], a)
	}
	for _, name := range order {
		s := summarize(byCase[name])
		rate := "-"
		if s.judged > 0 {
			rate = fmt.Sprintf("%d/%d", s.passed, s.judged)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%d/%d\t%s\t%s\t%s\n", name, rate, s.blocked, s.selfPass, s.ran,
			avg(s.steps, s.ran), kilo(avgInt(s.tokens, s.ran)), avgDuration(s.elapsed, s.ran))
	}
	_ = w.Flush()
}

type caseSummary struct {
	passed, judged, blocked, selfPass, ran, steps, tokens int
	elapsed                                               time.Duration
}

func summarize(attempts []attempt) caseSummary {
	var s caseSummary
	for _, a := range attempts {
		switch a.outcome {
		case outcomePass:
			s.passed++
			s.judged++
		case outcomeFail:
			s.judged++
		case outcomeBlocked:
			s.blocked++
		}
		if !a.ran {
			continue
		}
		s.ran++
		s.tokens += a.usage.TotalTokens
		s.elapsed += a.elapsed
		if a.res != nil {
			s.steps += len(a.res.Steps)
			if a.selfPass {
				s.selfPass++
			}
		}
	}
	return s
}

func avg(total, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(total)/float64(n))
}

func avgInt(total, n int) int {
	if n == 0 {
		return 0
	}
	return total / n
}

func avgDuration(total time.Duration, n int) string {
	if n == 0 {
		return "-"
	}
	return (total / time.Duration(n)).Round(time.Second).String()
}

// kilo 把 token 数写成 35.2k 这样
func kilo(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
