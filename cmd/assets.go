// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information regarding
// copyright ownership. The ASF licenses this file to You under the
// Apache License, Version 2.0 (the "License"); you may not use this
// file except in compliance with the License. You may obtain a copy
// at http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

package answercmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"time"

	"github.com/apache/answer/internal/service/browser"
	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/assets"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var assetAddr, assetStateDir, assetEnvFile, assetModelFile string
var assetCreateBucket bool

var assetsCmd = &cobra.Command{
	Use:   "assets",
	Short: "Run the media collection agent with HTTP and MCP APIs",
	RunE:  func(_ *cobra.Command, _ []string) error { return runAssetService() },
}

func init() {
	assetsCmd.Flags().StringVar(&assetAddr, "addr", "127.0.0.1:8766", "HTTP/MCP listen address")
	assetsCmd.Flags().StringVar(&assetStateDir, "state-dir", "answer-data/asset-tasks", "persistent task manifests (one service process per directory)")
	assetsCmd.Flags().StringVar(&assetEnvFile, "env-file", "", "optional private environment file (takes precedence over .env defaults)")
	assetsCmd.Flags().StringVar(&assetModelFile, "llm-config", "", "optional JSON model config: api_host, api_key, model")
	assetsCmd.Flags().BoolVar(&assetCreateBucket, "create-bucket", false, "create the configured S3 bucket if absent")
}

func assetEnv(primary, fallback, defaultValue string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if v := os.Getenv(fallback); v != "" {
		return v
	}
	return defaultValue
}

func runAssetService() error {
	if assetEnvFile != "" {
		if err := godotenv.Overload(assetEnvFile); err != nil {
			return err
		}
	}
	if len(os.Getenv("ASSET_API_TOKEN")) < 16 {
		return errors.New("set ASSET_API_TOKEN (at least 16 characters)")
	}
	hubToken := os.Getenv("BROWSER_HUB_TOKEN")
	if hubToken == "" {
		return errors.New("set BROWSER_HUB_TOKEN to the connected extension's token")
	}
	for _, binary := range []string{"ffprobe", "ffmpeg"} {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("%s is required to validate media: %w", binary, err)
		}
	}
	provider := llm.StaticProvider{APIHost: os.Getenv("ASSET_LLM_API_HOST"), APIKey: os.Getenv("ASSET_LLM_API_KEY"), Model: os.Getenv("ASSET_LLM_MODEL")}
	if assetModelFile != "" {
		raw, err := os.ReadFile(assetModelFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &provider); err != nil {
			return errors.New("invalid LLM config JSON")
		}
	}
	if provider.APIHost == "" || provider.APIKey == "" || provider.Model == "" {
		return errors.New("configure ASSET_LLM_API_HOST, ASSET_LLM_API_KEY, ASSET_LLM_MODEL or --llm-config")
	}
	secure, err := strconv.ParseBool(assetEnv("ASSET_S3_USE_SSL", "S3_USE_SSL", "false"))
	if err != nil {
		return errors.New("invalid ASSET_S3_USE_SSL")
	}
	store, err := assets.NewS3Store(assets.S3Config{
		Endpoint:  assetEnv("ASSET_S3_ENDPOINT", "S3_ENDPOINT", "127.0.0.1:9002"),
		AccessKey: assetEnv("ASSET_S3_ACCESS_KEY_ID", "S3_ACCESS_KEY_ID", ""),
		SecretKey: assetEnv("ASSET_S3_SECRET_ACCESS_KEY", "S3_SECRET_ACCESS_KEY", ""),
		Bucket:    assetEnv("ASSET_S3_BUCKET", "S3_BUCKET", "creative-assets"),
		Region:    assetEnv("ASSET_S3_REGION", "S3_REGION", "us-east-1"),
		Prefix:    assetEnv("ASSET_S3_PREFIX", "", "assets"), Secure: secure,
	})
	if err != nil {
		return err
	}
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 15*time.Second)
	err = store.CheckBucket(checkCtx, assetCreateBucket)
	checkCancel()
	if err != nil {
		return fmt.Errorf("S3 bucket check failed: %w", err)
	}
	b := browser.NewHubClient(assetEnv("BROWSER_HUB_URL", "", "http://127.0.0.1:8765"), hubToken)
	runner := &assets.Runner{Model: llm.NewStaticClient(provider), Browser: b, Store: store}
	service, err := assets.NewService(assetStateDir, runner.Run, store)
	if err != nil {
		return err
	}
	defer service.Close()
	handler, err := assets.NewHandler(service, os.Getenv("ASSET_API_TOKEN"))
	if err != nil {
		return err
	}
	server := &http.Server{Addr: assetAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	fmt.Printf("media agent HTTP: http://%s/api/assets/tasks\nmedia agent MCP: http://%s/mcp\n", assetAddr, assetAddr)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
