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

package answercmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/browser"
	"github.com/spf13/cobra"
)

// answer browser：开发调试用。起一个 Hub 等插件连上来，然后在终端里敲命令控制浏览器，
// 或者用 --console=false 只开 HTTP 调试接口，给 curl、脚本和自动化测试用。
// 不读配置文件，不连数据库，和 answer run 互不影响。

const browserCallTimeout = 30 * time.Second

var (
	browserAddr    string
	browserConsole bool

	browserCmd = &cobra.Command{
		Use:   "browser",
		Short: "Debug the browser extension from the terminal",
		Long: `Start the browser hub and drive the extension by typing commands.
The token is read from the BROWSER_HUB_TOKEN environment variable and must match the one set in the extension popup.
The same token protects the HTTP debug API:

  curl -H "Authorization: Bearer $BROWSER_HUB_TOKEN" http://127.0.0.1:8765/debug/tabs
  curl -H "Authorization: Bearer $BROWSER_HUB_TOKEN" -d '{"tabId":1,"cmd":"snapshot"}' http://127.0.0.1:8765/debug/call

Use --console=false to run without the interactive console, for scripts and background jobs.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runBrowserConsole(browserAddr, os.Getenv("BROWSER_HUB_TOKEN"), browserConsole)
		},
	}
)

func init() {
	browserCmd.Flags().StringVar(&browserAddr, "addr", "127.0.0.1:8765", "address to listen on; keep it on 127.0.0.1")
	browserCmd.Flags().BoolVar(&browserConsole, "console", true, "read commands from the terminal; false serves only the HTTP debug API until Ctrl+C")
}

func runBrowserConsole(addr, token string, console bool) error {
	if token == "" {
		return errors.New("set BROWSER_HUB_TOKEN to the token you entered in the extension popup")
	}
	hub := browser.NewHub(token)
	mux := http.NewServeMux()
	mux.Handle("/agent", hub)
	mux.Handle("/debug/", browser.NewDebugHandler(hub, token))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	defer func() { _ = srv.Close() }()

	fmt.Printf("browser hub listening on ws://%s/agent, waiting for the extension...\n", addr)
	fmt.Printf("debug API on http://%s/debug/ (GET tabs, POST call)\n", addr)
	if !console {
		return waitForInterrupt(serveErr)
	}
	printBrowserHelp()

	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			fmt.Println("read input:", err)
		}
		close(lines)
	}()

	for {
		fmt.Print("> ")
		select {
		case err := <-serveErr:
			return err // 端口被占用之类
		case line, ok := <-lines:
			if !ok {
				return nil // 输入结束（Ctrl+Z / Ctrl+D）
			}
			if quit := runBrowserLine(hub, strings.Fields(line)); quit {
				return nil
			}
		}
	}
}

// waitForInterrupt 在没有交互终端时一直运行，直到 Ctrl+C 或者服务出错
func waitForInterrupt(serveErr <-chan error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		return nil
	}
}

// runBrowserLine 执行一行命令，返回 true 表示退出
func runBrowserLine(hub *browser.Hub, args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "quit", "exit":
		return true
	case "help":
		printBrowserHelp()
	case "tabs":
		tabs := hub.Tabs()
		if len(tabs) == 0 {
			fmt.Println("no web page tabs (chrome:// pages and new tabs are not listed)")
		}
		for _, t := range tabs {
			fmt.Printf("%d\t%s\n", t.ID, t.Title)
		}
	case "navigate":
		if len(args) != 3 {
			fmt.Println("usage: navigate <tabId> <url>")
			return false
		}
		callAndPrint(hub, args[1], "navigate", map[string]string{"url": args[2]})
	case "snapshot":
		maxChars := 8000
		if len(args) == 3 {
			maxChars, _ = strconv.Atoi(args[2])
		}
		if len(args) < 2 {
			fmt.Println("usage: snapshot <tabId> [maxChars]")
			return false
		}
		callAndPrint(hub, args[1], "snapshot", map[string]int{"maxChars": maxChars})
	case "click":
		if len(args) != 3 {
			fmt.Println("usage: click <tabId> <index>")
			return false
		}
		index, err := strconv.Atoi(args[2])
		if err != nil {
			fmt.Println("index must be a number")
			return false
		}
		callAndPrint(hub, args[1], "click", map[string]int{"index": index})
	default:
		fmt.Printf("unknown command %q\n", args[0])
		printBrowserHelp()
	}
	return false
}

func callAndPrint(hub *browser.Hub, tab, cmd string, args any) {
	tabID, err := strconv.Atoi(tab)
	if err != nil {
		fmt.Println("tabId must be a number; run `tabs` to list them")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), browserCallTimeout)
	defer cancel()

	start := time.Now()
	data, err := hub.Call(ctx, tabID, cmd, args)
	if err != nil {
		fmt.Printf("error after %s: %v\n", time.Since(start).Round(time.Millisecond), err)
		return
	}

	// snapshot 的 elements 是多行文本，单独打印出来，比挤在 JSON 字符串里好读
	var fields map[string]any
	elements, isText := "", false
	if json.Unmarshal(data, &fields) == nil {
		elements, isText = fields["elements"].(string)
	}
	if cmd == "snapshot" && isText {
		delete(fields, "elements")
		meta, _ := json.MarshalIndent(fields, "", "  ")
		fmt.Printf("%s\n%s\n", meta, elements)
	} else {
		fmt.Println(string(data))
	}
	fmt.Printf("(%s)\n", time.Since(start).Round(time.Millisecond))
}

func printBrowserHelp() {
	fmt.Println(`commands:
  tabs                          list the web page tabs the extension reported
  navigate <tabId> <url>        open url in that tab and wait for it to load
  snapshot <tabId> [maxChars]   print the numbered elements of the page
  click <tabId> <index>         click an element from the latest snapshot
  quit`)
}
