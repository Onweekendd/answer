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

// pagebench 在一组固定页面上量一遍快照，和基线比较，看压缩规则改了以后变好还是变坏。
//
// 先启动 Hub（answer browser），插件连上以后：
//
//	BROWSER_HUB_TOKEN=<token> go run ./script/pagebench
//	BROWSER_HUB_TOKEN=<token> go run ./script/pagebench -update-baseline   # 把这次的结果存成新基线
//
// 只读不点：每个页面只 navigate 一次、snapshot 一次，页面之间等几秒。
// 跳到登录页或验证码页就跳过这个网站剩下的页面，交给人处理。
// 完整的快照文本只写到 out/ 目录（不提交），写之前把 redact.txt 和 PAGEBENCH_REDACT 里的词（比如账号昵称）替换掉。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
)

type page struct {
	Site string `json:"site"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Result 是一个页面的结果。baseline.json 就是一组 Result，只有数字
type Result struct {
	Name    string             `json:"name"`
	Status  string             `json:"status"` // ok / blocked: … / error: …
	Metrics Metrics            `json:"metrics"`
	Stats   map[string]float64 `json:"stats,omitempty"` // 插件自己算的统计（候选、隐藏、合并、耗时等）
}

type snapshotData struct {
	Elements  string             `json:"elements"`
	Truncated bool               `json:"truncated"`
	Stats     map[string]float64 `json:"stats"`
}

type pageInfo struct {
	TabID int    `json:"tabId"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

func main() {
	hub := flag.String("hub", "http://127.0.0.1:8765", "Hub address")
	pagesFile := flag.String("pages", "script/pagebench/pages.json", "pages to measure")
	baselineFile := flag.String("baseline", "script/pagebench/baseline.json", "baseline to compare with")
	outDir := flag.String("out", "script/pagebench/out", "where to write full snapshots (not committed)")
	redactFile := flag.String("redact", "script/pagebench/redact.txt", "words to hide in saved snapshots, one per line (not committed)")
	delay := flag.Duration("delay", 4*time.Second, "pause between pages, so we do not hit a site too fast")
	maxChars := flag.Int("max-chars", 8000, "snapshot budget")
	updateBaseline := flag.Bool("update-baseline", false, "save this run as the new baseline")
	flag.Parse()

	token := os.Getenv("BROWSER_HUB_TOKEN")
	if token == "" {
		fail(errors.New("set BROWSER_HUB_TOKEN to the token in the extension popup"))
	}
	pages, err := readJSON[[]page](*pagesFile)
	if err != nil {
		fail(err)
	}
	c := &client{hub: *hub, token: token, http: &http.Client{Timeout: 2 * time.Minute}}
	if err := c.ready(); err != nil {
		fail(err)
	}
	redact := strings.Split(os.Getenv("PAGEBENCH_REDACT"), ",")
	// 也可以写在本地文件里，一行一个词，这样跑脚本的人（包括 Claude）不用知道这些词
	if b, err := os.ReadFile(*redactFile); err == nil {
		redact = append(redact, strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n")...)
	}

	runDir := filepath.Join(*outDir, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		fail(err)
	}

	results := run(c, pages, *delay, *maxChars, func(name, elements string) {
		_ = os.WriteFile(filepath.Join(runDir, name+".txt"), []byte(Redact(elements, redact)), 0o600)
	})

	printResults(results)
	if old, err := readJSON[[]Result](*baselineFile); err == nil {
		printComparison(old, results)
	} else {
		fmt.Println("\nno baseline yet; run with -update-baseline to save this run as one")
	}
	writeJSON(filepath.Join(runDir, "summary.json"), results)
	if *updateBaseline {
		writeJSON(*baselineFile, results)
		fmt.Println("baseline updated:", *baselineFile)
	}
	fmt.Println("full snapshots:", runDir)
}

// run 开一个后台标签页，按顺序量每个页面，最后关掉标签页
func run(c *client, pages []page, delay time.Duration, maxChars int, save func(name, elements string)) []Result {
	var results []Result
	tabID := 0
	blockedSite := map[string]string{}
	defer func() {
		if tabID != 0 {
			_, _ = c.call("close", tabID, struct{}{}, 0)
		}
	}()

	for i, p := range pages {
		if reason, ok := blockedSite[p.Site]; ok {
			results = append(results, Result{Name: p.Name, Status: "skipped: " + reason})
			continue
		}
		if i > 0 {
			time.Sleep(delay)
		}
		fmt.Printf("%s ...\n", p.Name)

		info, err := c.open(&tabID, p.URL)
		if errors.Is(err, errHubGone) {
			fmt.Println("the extension went away, stopping:", err)
			return results
		}
		if err != nil {
			results = append(results, Result{Name: p.Name, Status: "error: " + err.Error()})
			continue
		}
		if reason := BlockedReason(info.URL, info.Title); reason != "" {
			blockedSite[p.Site] = reason
			results = append(results, Result{Name: p.Name, Status: "blocked: " + reason})
			continue
		}

		raw, err := c.call("snapshot", tabID, map[string]int{"maxChars": maxChars}, 30000)
		if err != nil {
			results = append(results, Result{Name: p.Name, Status: "error: " + err.Error()})
			continue
		}
		var snap snapshotData
		if err := json.Unmarshal(raw, &snap); err != nil {
			results = append(results, Result{Name: p.Name, Status: "error: bad snapshot: " + err.Error()})
			continue
		}
		save(p.Name, snap.Elements)
		status := "ok"
		if snap.Truncated {
			status = "ok (truncated)"
		}
		results = append(results, Result{Name: p.Name, Status: status, Metrics: Analyze(snap.Elements), Stats: snap.Stats})
	}
	return results
}

func printResults(results []Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "\npage\tlines\tchars\tdup\tempty\tclipped\thidden\tmerged\tms\tstatus\t")
	for _, r := range results {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%.0f\t%.0f\t%.0f\t%s\t\n", r.Name,
			r.Metrics.Lines, r.Metrics.Chars, r.Metrics.Duplicates, r.Metrics.Empty, r.Metrics.Clipped,
			r.Stats["hidden"], r.Stats["merged"], r.Stats["ms"], r.Status)
	}
	_ = w.Flush()
}

// printComparison 只列出和基线不一样的数字
func printComparison(old, now []Result) {
	byName := make(map[string]Result, len(old))
	for _, r := range old {
		byName[r.Name] = r
	}
	fmt.Println("\ncompared with baseline:")
	changed := false
	for _, r := range now {
		b, ok := byName[r.Name]
		if !ok || !strings.HasPrefix(r.Status, "ok") || !strings.HasPrefix(b.Status, "ok") {
			continue
		}
		for _, d := range []struct {
			name     string
			old, now int
		}{
			{"lines", b.Metrics.Lines, r.Metrics.Lines},
			{"chars", b.Metrics.Chars, r.Metrics.Chars},
			{"duplicates", b.Metrics.Duplicates, r.Metrics.Duplicates},
			{"empty", b.Metrics.Empty, r.Metrics.Empty},
			{"clipped", b.Metrics.Clipped, r.Metrics.Clipped},
		} {
			if d.old != d.now {
				changed = true
				fmt.Printf("  %-14s %-10s %6d -> %-6d (%+d)\n", r.Name, d.name, d.old, d.now, d.now-d.old)
			}
		}
	}
	if !changed {
		fmt.Println("  no change")
	}
}

type client struct {
	hub, token string
	http       *http.Client
}

// errHubGone 表示 Hub 连不上或者插件不在线，后面的页面都不用试了
var errHubGone = errors.New("hub or extension is not available")

// ready 在开始之前确认 Hub 在跑、token 对、插件连着
func (c *client) ready() error {
	req, err := http.NewRequest(http.MethodGet, c.hub+"/api/browser/tabs", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: start it with `answer browser` (%v)", errHubGone, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: /api/browser/tabs returned HTTP %d, check BROWSER_HUB_TOKEN", errHubGone, resp.StatusCode)
	}
	return nil
}

// open 第一次调用时新开一个后台标签页，之后在同一个标签页里跳转
func (c *client) open(tabID *int, url string) (pageInfo, error) {
	var raw json.RawMessage
	var err error
	if *tabID == 0 {
		raw, err = c.call("open", 0, map[string]string{"url": url}, 60000)
	} else {
		raw, err = c.call("navigate", *tabID, map[string]string{"url": url}, 60000)
	}
	if err != nil {
		return pageInfo{}, err
	}
	var info pageInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return pageInfo{}, err
	}
	if info.TabID != 0 {
		*tabID = info.TabID
	}
	return info, nil
}

// call 调一次 Hub 的 /api/browser/call
func (c *client) call(cmd string, tabID int, args any, timeoutMs int) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"tabId": tabID, "cmd": cmd, "args": args, "timeoutMs": timeoutMs})
	req, err := http.NewRequest(http.MethodPost, c.hub+"/api/browser/call", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errHubGone, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway {
		return nil, fmt.Errorf("%w: %s got HTTP %d", errHubGone, cmd, resp.StatusCode)
	}

	var out struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: HTTP %d, body is not JSON", cmd, resp.StatusCode)
	}
	if !out.OK {
		if out.Error == nil {
			return nil, fmt.Errorf("%s: HTTP %d", cmd, resp.StatusCode)
		}
		return nil, fmt.Errorf("%s: %s", cmd, out.Error.Code)
	}
	return out.Data, nil
}

func readJSON[T any](path string) (T, error) {
	var v T
	b, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal(b, &v)
}

func writeJSON(path string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write", path+":", err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "pagebench:", err)
	os.Exit(1)
}
