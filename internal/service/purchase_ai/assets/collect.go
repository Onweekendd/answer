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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/apache/answer/internal/service/purchase_ai/video"
)

type captureResult struct {
	Captures []struct {
		Body string `json:"body"`
	} `json:"captures"`
}

func (c *collection) searchVideos(ctx context.Context, arguments string) (tool.Result, error) {
	var args struct {
		Query string `json:"query"`
	}
	if res := tool.DecodeArgs(arguments, &args, `{"query":"旅行风景"}`); res != nil {
		return *res, nil
	}
	if strings.TrimSpace(args.Query) == "" || len([]rune(args.Query)) > 200 {
		return failure(errors.New("query must contain 1-200 characters")), nil
	}
	page := searchURL(args.Query)
	c.progress("searching")
	if err := c.ensureTab(ctx, "https://www.douyin.com/"); err != nil {
		return failure(err), nil
	}
	var out captureResult
	if err := c.call(ctx, "netcapture", map[string]any{"url": page, "match": "/search/", "waitMs": 20000}, &out); err != nil {
		return failure(err), nil
	}
	type candidate struct {
		URL        string `json:"url"`
		Title      string `json:"title"`
		Author     string `json:"author"`
		DurationMS int64  `json:"duration_ms"`
	}
	var candidates []candidate
	for _, capture := range out.Captures {
		for _, src := range video.ListDouyin([]byte(capture.Body)) {
			if c.seen[src.Page] {
				continue
			}
			c.seen[src.Page] = true
			candidates = append(candidates, candidate{src.Page, src.Title, src.Author, src.Duration.Milliseconds()})
			if len(candidates) >= 30 {
				break
			}
		}
	}
	res := resultJSON(map[string]any{"candidates": candidates, "hint": "If empty, use read_candidates to read the rendered search page, then resolve_video with a real title."})
	res.PageChanged, res.URL = true, page
	return res, nil
}

func (c *collection) readCandidates(ctx context.Context, _ string) (tool.Result, error) {
	if res := c.state.RequireTab(); res != nil {
		return *res, nil
	}
	var out struct {
		Text string `json:"text"`
		URL  string `json:"url"`
	}
	if err := c.call(ctx, "read", map[string]any{"maxChars": 12000}, &out); err != nil {
		return failure(err), nil
	}
	res := resultJSON(out)
	return res, nil
}

var cardID = regexp.MustCompile(`(?:waterfall_item_|/video/)(\d{6,25})`)

func (c *collection) resolveVideo(ctx context.Context, arguments string) (tool.Result, error) {
	var args struct {
		Title string `json:"title"`
	}
	if res := tool.DecodeArgs(arguments, &args, `{"title":"实际读到的视频标题"}`); res != nil {
		return *res, nil
	}
	if args.Title == "" || len([]rune(args.Title)) > 500 {
		return failure(errors.New("invalid title")), nil
	}
	if res := c.state.RequireTab(); res != nil {
		return *res, nil
	}
	var outline json.RawMessage
	if err := c.call(ctx, "inspect", map[string]any{"text": args.Title, "up": 6}, &outline); err != nil {
		return failure(err), nil
	}
	ids := map[string]bool{}
	for _, match := range cardID.FindAllStringSubmatch(string(outline), -1) {
		ids[match[1]] = true
	}
	if len(ids) != 1 {
		return failure(errors.New("could not resolve one unique video; use a more specific title from read_candidates")), nil
	}
	for id := range ids {
		page := video.DouyinPageOf(id)
		c.seen[page] = true
		return resultJSON(map[string]string{"url": page, "title": args.Title}), nil
	}
	return failure(errors.New("video not found")), nil
}

func (c *collection) captureVideo(ctx context.Context, page, id string) (video.Source, error) {
	var out captureResult
	if err := c.call(ctx, "netcapture", map[string]any{"url": page, "match": video.DouyinDetailAPI, "waitMs": 15000}, &out); err != nil {
		return video.Source{}, err
	}
	for _, capture := range out.Captures {
		if src, ok := video.ParseDouyin([]byte(capture.Body), id); ok {
			return src, nil
		}
	}
	return video.Source{}, errors.New("video details unavailable; check snapshot/read for login or verification before continuing")
}

func (c *collection) collectVideo(ctx context.Context, arguments string) (tool.Result, error) {
	if c.job.Request.Kind != "video" {
		return failure(errors.New("this is an image task")), nil
	}
	var args struct {
		URL string `json:"url"`
	}
	if res := tool.DecodeArgs(arguments, &args, `{"url":"https://www.douyin.com/video/123456789"}`); res != nil {
		return *res, nil
	}
	match := video.DouyinPage.FindStringSubmatch(args.URL)
	if match == nil || !c.seen[args.URL] {
		return failure(errors.New("use a Douyin video URL provided by the caller, search_videos or resolve_video")), nil
	}
	page, id := video.DouyinPageOf(match[1]), match[1]
	if c.saved[page] {
		return failure(errors.New("this video has already been collected")), nil
	}
	if c.count >= c.job.Request.Count {
		return failure(errors.New("requested asset count has already been reached")), nil
	}
	if err := c.ensureTab(ctx, page); err != nil {
		return failure(err), nil
	}
	c.progress("reading_video_details")
	var src video.Source
	var play video.Play
	var data []byte
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		src, err = c.captureVideo(ctx, page, id)
		if err != nil {
			break
		}
		if src.Duration > time.Duration(c.job.Request.MaxDurationSeconds)*time.Second {
			err = errors.New("video exceeds the requested maximum duration")
			break
		}
		var ok bool
		play, _, ok = video.PickCreative(src, c.job.Request.MinShortSide)
		if !ok {
			err = errors.New("no self-contained MP4 meets the requested resolution and size limits")
			break
		}
		c.progress("downloading_video")
		data, err = video.Fetch(ctx, func(ctx context.Context, link string, start, end int) (video.Part, error) {
			var part video.Part
			err := c.call(ctx, "mediaRange", map[string]any{"url": link, "start": start, "end": end}, &part)
			return part, err
		}, play)
		if !errors.Is(err, video.ErrLinkExpired) {
			break
		}
	}
	if err != nil {
		return failure(err), nil
	}
	a := Asset{Kind: "video", SourceURL: page, Title: src.Title, Author: src.Author, ContentType: "video/mp4", Variant: play.Name}
	res, err := c.persist(ctx, a, data)
	res.PageChanged, res.URL = true, page
	if err == nil && !res.Failed {
		c.saved[page] = true
	}
	return res, err
}

type imageArgs struct {
	Index *int   `json:"index,omitempty" description:"最新快照中的元素编号，不填取当前屏幕最大图片"`
	Title string `json:"title" description:"图片说明"`
}

func (c *collection) collectImage(ctx context.Context, arguments string) (tool.Result, error) {
	if c.job.Request.Kind != "image" {
		return failure(errors.New("this is a video task")), nil
	}
	if c.count >= c.job.Request.Count {
		return failure(errors.New("requested asset count has already been reached")), nil
	}
	if res := c.state.RequireTab(); res != nil {
		return *res, nil
	}
	var args imageArgs
	if res := tool.DecodeArgs(arguments, &args, `{"title":"图片说明"}`); res != nil {
		return *res, nil
	}
	params := map[string]any{"maxWidth": 4096, "quality": 92}
	if args.Index != nil {
		params["index"] = *args.Index
	}
	var image struct {
		tool.Image
		Src string `json:"src"`
	}
	c.progress("downloading_image")
	if err := c.call(ctx, "image", params, &image); err != nil {
		return failure(err), nil
	}
	if len(image.Data) > 28<<20 {
		return failure(errors.New("image is too large")), nil
	}
	data, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		return failure(errors.New("invalid image data")), nil
	}
	if c.saved[image.Src] {
		return failure(errors.New("this image has already been collected")), nil
	}
	a := Asset{Kind: "image", SourceURL: image.URL, OriginalURL: image.Src, Title: tool.ClipRunes(args.Title, 500), ContentType: "image/jpeg"}
	res, err := c.persist(ctx, a, data)
	if err == nil && !res.Failed {
		c.saved[image.Src] = true
	}
	return res, err
}

func (c *collection) persist(ctx context.Context, a Asset, data []byte) (tool.Result, error) {
	c.progress("validating_media")
	validate := c.runner.Validate
	if validate == nil {
		validate = ValidateMedia
	}
	var err error
	a, err = validate(ctx, a, data)
	if err != nil {
		return failure(err), nil
	}
	if min(a.Width, a.Height) < c.job.Request.MinShortSide {
		return failure(errors.New("downloaded media does not meet the requested resolution")), nil
	}
	if a.Kind == "video" && a.DurationMS > int64(c.job.Request.MaxDurationSeconds)*1000 {
		return failure(errors.New("downloaded video exceeds the requested duration")), nil
	}
	c.progress("uploading")
	a, err = c.runner.Store.Put(ctx, c.job.Request.ProjectID+"/"+c.job.ID, a, data)
	if err != nil {
		return failure(fmt.Errorf("upload failed: %w", err)), nil
	}
	if c.saved[a.ObjectKey] {
		return failure(errors.New("duplicate asset content")), nil
	}
	if err := c.emit(a); err != nil {
		return tool.Result{}, fmt.Errorf("cannot record uploaded asset: %w", err)
	}
	c.saved[a.ObjectKey] = true
	c.count++
	// URLs are returned by the service, not sent back into model context.
	return resultJSON(map[string]any{"asset_id": a.ID, "source_url": a.SourceURL, "width": a.Width, "height": a.Height, "bytes": a.Bytes, "collected": c.count}), nil
}
