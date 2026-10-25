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
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"testing"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/require"
)

type scriptedModel struct {
	calls []llm.ToolCall
	index int
}

func (m *scriptedModel) Chat(context.Context, *llm.ChatReq) (*llm.ChatResp, error) {
	if m.index >= len(m.calls) {
		return nil, errors.New("unexpected model call")
	}
	call := m.calls[m.index]
	m.index++
	return &llm.ChatResp{ToolCalls: []llm.ToolCall{call}}, nil
}

type imageBrowser struct {
	data   []byte
	closed bool
}

func (b *imageBrowser) Connected() bool { return true }
func (b *imageBrowser) Call(_ context.Context, _ int, cmd string, _ any) (json.RawMessage, error) {
	switch cmd {
	case "open":
		return json.RawMessage(`{"tabId":7,"url":"https://example.test/photo","loaded":true}`), nil
	case "image":
		v, _ := json.Marshal(map[string]any{"data": base64.StdEncoding.EncodeToString(b.data), "mime": "image/jpeg", "url": "https://example.test/photo", "src": "https://example.test/photo.jpg"})
		return v, nil
	case "close":
		b.closed = true
		return json.RawMessage(`{}`), nil
	}
	return nil, errors.New("unexpected browser command " + cmd)
}

func TestImageRunnerUploadsOnlyValidatedBytes(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 24, 32)), nil))
	for _, tc := range []struct {
		name       string
		data       []byte
		failUpload bool
		want       int
	}{{"valid", buf.Bytes(), false, 1}, {"invalid", []byte("not an image"), false, 0}, {"upload_failure", buf.Bytes(), true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			model := &scriptedModel{calls: []llm.ToolCall{{ID: "1", Name: "open", Arguments: `{"url":"https://example.test/photo"}`}, {ID: "2", Name: "collect_image", Arguments: `{"title":"mountain"}`}, {ID: "3", Name: "done", Arguments: `{"text":"complete","success":true}`}}}
			store := &memoryStore{fail: tc.failUpload}
			browser := &imageBrowser{data: tc.data}
			runner := &Runner{Model: model, Browser: browser, Store: store}
			var emitted []Asset
			_, err := runner.Run(context.Background(), Job{ID: "task", Request: Request{ProjectID: "p", Kind: "image", Count: 1, MinShortSide: 20}}, func(a Asset) error { emitted = append(emitted, a); return nil }, func(string) {})
			require.NoError(t, err)
			require.Len(t, emitted, tc.want)
			require.True(t, browser.closed)
			if tc.want == 1 {
				require.Equal(t, 24, emitted[0].Width)
				require.Equal(t, 32, emitted[0].Height)
				require.Equal(t, "https://example.test/photo", emitted[0].SourceURL)
			}
		})
	}
}

func TestDisconnectedBrowserRequiresInput(t *testing.T) {
	r := &Runner{}
	out, err := r.Run(context.Background(), Job{}, nil, nil)
	require.NoError(t, err)
	require.True(t, out.NeedsInput)
}

func TestMediaValidationRejectsCorruption(t *testing.T) {
	_, err := ValidateMedia(context.Background(), Asset{Kind: "image"}, []byte("bad"))
	require.Error(t, err)
}

type videoBrowser struct{ closed bool }

func (b *videoBrowser) Connected() bool { return true }
func (b *videoBrowser) Call(_ context.Context, _ int, cmd string, _ any) (json.RawMessage, error) {
	switch cmd {
	case "open":
		return json.RawMessage(`{"tabId":8,"loaded":true}`), nil
	case "netcapture":
		body := `{"aweme_detail":{"aweme_id":"123456789","desc":"mountains","video":{"duration":13000,"play_addr":{"url_list":["https://v5.zjcdn.com/source"],"width":1080,"height":1920,"data_size":5},"bit_rate":[{"format":"mp4","gear_name":"normal_1080_0","play_addr":{"url_list":["https://v5.zjcdn.com/source"],"width":1080,"height":1920,"data_size":5}}]}}}`
		raw, _ := json.Marshal(map[string]any{"captures": []map[string]string{{"body": body}}})
		return raw, nil
	case "mediaRange":
		return json.RawMessage(`{"status":206,"total":5,"contentType":"video/mp4","data":"dmlkZW8="}`), nil
	case "close":
		b.closed = true
		return json.RawMessage(`{}`), nil
	}
	return nil, errors.New("unexpected browser command " + cmd)
}

func TestVideoRunnerRequiresObservedSourceAndAcceptsCanonicalURL(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "caller_source_with_query", false: "invented_source"}[known], func(t *testing.T) {
			model := &scriptedModel{calls: []llm.ToolCall{{ID: "1", Name: "collect_video", Arguments: `{"url":"https://www.douyin.com/video/123456789"}`}, {ID: "2", Name: "done", Arguments: `{"text":"complete","success":true}`}}}
			browser := &videoBrowser{}
			store := &memoryStore{}
			runner := &Runner{Model: model, Browser: browser, Store: store, Validate: func(_ context.Context, a Asset, data []byte) (Asset, error) {
				require.Equal(t, []byte("video"), data)
				a.Width, a.Height, a.DurationMS = 1080, 1920, 13000
				return a, nil
			}}
			req := Request{ProjectID: "p", Kind: "video", Count: 1, MinShortSide: 1080, MaxDurationSeconds: 30}
			if known {
				req.SourceURLs = []string{"https://www.douyin.com/video/123456789?from=search"}
			}
			var emitted []Asset
			_, err := runner.Run(context.Background(), Job{ID: "task", Request: req}, func(a Asset) error { emitted = append(emitted, a); return nil }, func(string) {})
			require.NoError(t, err)
			if known {
				require.Len(t, emitted, 1)
				require.True(t, browser.closed)
				require.Equal(t, "https://www.douyin.com/video/123456789", emitted[0].SourceURL)
			} else {
				require.Empty(t, emitted)
				require.Empty(t, store.files)
			}
		})
	}
}
