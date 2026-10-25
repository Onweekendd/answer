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

package assets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"math"
	"os"
	"os/exec"
	"strconv"
)

// ValidateMedia checks the downloaded bytes rather than trusting metadata from
// the website. ffmpeg decodes every video frame before an upload is accepted.
func ValidateMedia(ctx context.Context, a Asset, data []byte) (Asset, error) {
	if a.Kind == "image" {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "jpeg" || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 80_000_000 {
			return Asset{}, errors.New("invalid JPEG image")
		}
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return Asset{}, fmt.Errorf("image decode failed: %w", err)
		}
		a.Width, a.Height = cfg.Width, cfg.Height
		return a, nil
	}
	f, err := os.CreateTemp("", "answer-asset-*.mp4")
	if err != nil {
		return Asset{}, err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return Asset{}, err
	}
	if err := f.Close(); err != nil {
		return Asset{}, err
	}
	output, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", f.Name()).Output()
	if err != nil {
		return Asset{}, fmt.Errorf("video probe failed: %w", err)
	}
	var probe struct {
		Streams []struct {
			Type   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return Asset{}, err
	}
	for _, stream := range probe.Streams {
		if stream.Type == "video" {
			a.Width, a.Height = stream.Width, stream.Height
			break
		}
	}
	seconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || a.Width <= 0 || a.Height <= 0 {
		return Asset{}, errors.New("video has invalid dimensions or duration")
	}
	a.DurationMS = int64(seconds * 1000)
	// -xerror makes corrupt frames a failure instead of a warning followed by 0.
	if err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-nostdin", "-i", f.Name(), "-f", "null", "-").Run(); err != nil {
		return Asset{}, fmt.Errorf("video decode failed: %w", err)
	}
	return a, nil
}
