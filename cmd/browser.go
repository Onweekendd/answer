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
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/apache/answer/internal/service/browser"
	"github.com/spf13/cobra"
)

// answer browser 启动独立的浏览器 Hub，供插件连接、后端通过 HTTP 接口调用。
// 不读配置文件，不连数据库，和 answer run 互不影响。
var (
	browserAddr string

	browserCmd = &cobra.Command{
		Use:   "browser",
		Short: "Start the browser hub",
		Long: `Start the browser hub and serve its HTTP API until Ctrl+C.
The token is read from the BROWSER_HUB_TOKEN environment variable and must match the one set in the extension popup.
The same token protects the HTTP API:

  curl -H "Authorization: Bearer $BROWSER_HUB_TOKEN" http://127.0.0.1:8765/api/browser/tabs
  curl -H "Authorization: Bearer $BROWSER_HUB_TOKEN" -d '{"tabId":1,"cmd":"snapshot"}' http://127.0.0.1:8765/api/browser/call`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runBrowserHub(browserAddr, os.Getenv("BROWSER_HUB_TOKEN"))
		},
	}
)

func init() {
	browserCmd.Flags().StringVar(&browserAddr, "addr", "127.0.0.1:8765", "address to listen on; keep it on 127.0.0.1")
}

func runBrowserHub(addr, token string) error {
	if token == "" {
		return errors.New("set BROWSER_HUB_TOKEN to the token you entered in the extension popup")
	}
	hub := browser.NewHub(token)
	mux := http.NewServeMux()
	mux.Handle("/agent", hub)
	mux.Handle("/api/browser/", browser.NewAPIHandler(hub, token))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	defer func() { _ = srv.Close() }()

	fmt.Printf("browser hub listening on ws://%s/agent, waiting for the extension...\n", addr)
	fmt.Printf("browser API on http://%s/api/browser/ (GET status, GET tabs, POST call)\n", addr)
	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		return nil
	}
}
