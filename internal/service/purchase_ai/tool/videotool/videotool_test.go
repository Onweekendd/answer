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

package videotool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
	"github.com/apache/answer/internal/service/purchase_ai/video"
	"github.com/apache/answer/internal/service/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testVideoID = "7696338249064795407"

// cdnLink 是抖音 CDN 上的一个地址
func cdnLink(name string) string { return "https://v5-dy.zjcdn.com/sign/expire/video/" + name + ".mp4" }

// detailBody 是 aweme/detail 接口的返回：id 这个视频，时长 seconds 秒，540p 那一档的地址是 link、大小 size
func detailBody(t *testing.T, id string, seconds int, link string, size int) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"status_code": 0,
		"aweme_detail": map[string]any{
			"aweme_id": id, "desc": "扫地机器人实测 #测评", "author": map[string]any{"nickname": "测评君"},
			"video": map[string]any{
				"duration":  seconds * 1000,
				"play_addr": map[string]any{"url_list": []string{cdnLink("default")}, "data_size": 128 << 20, "width": 1080, "height": 1920},
				"bit_rate": []map[string]any{{"gear_name": "normal_540_0",
					"play_addr": map[string]any{"url_list": []string{link}, "data_size": size, "width": 540, "height": 960}}},
			},
		},
	})
	require.NoError(t, err)
	return string(body)
}

// fakeVideoBrowser 代替插件：netcapture 按顺序给 details 里的返回（用完了一直给最后一个），
// mediaRange 从 file 里切一段给出去，expired 里的地址回 403
type fakeVideoBrowser struct {
	details []string
	file    []byte
	expired map[string]bool
	calls   []string
}

func (f *fakeVideoBrowser) Connected() bool { return true }

func (f *fakeVideoBrowser) Call(ctx context.Context, _ int, cmd string, args any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(args)
	var a struct {
		URL   string `json:"url"`
		Match string `json:"match"`
		Start int    `json:"start"`
		End   int    `json:"end"`
	}
	_ = json.Unmarshal(raw, &a)
	switch cmd {
	case "netcapture":
		f.calls = append(f.calls, "netcapture "+a.URL+" "+a.Match)
		body := f.details[min(f.count("netcapture"), len(f.details))-1]
		return mustJSON(map[string]any{"captures": []map[string]any{
			{"url": "https://www.douyin.com/aweme/v1/web/social/count", "status": 200, "body": `{"aweme_id":"x"}`},
			{"url": "https://www.douyin.com" + video.DouyinDetailAPI, "status": 200, "body": body},
		}}), nil
	case "mediaRange":
		f.calls = append(f.calls, "mediaRange "+a.URL)
		if f.expired[a.URL] {
			return mustJSON(map[string]any{"status": 403, "contentType": "text/plain", "data": ""}), nil
		}
		part := f.file[min(a.Start, len(f.file)):min(a.End+1, len(f.file))]
		return mustJSON(map[string]any{"status": 206, "contentType": "video/mp4", "total": len(f.file), "data": part}), nil
	}
	return nil, errors.New("fakeVideoBrowser 不认识 " + cmd)
}

func (f *fakeVideoBrowser) count(cmd string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, cmd+" ") {
			n++
		}
	}
	return n
}

// videoRun 是一个开着标签页（7 号）的存视频工具，和它要存进去的工作台。seen 是它记下的见过的网址
func videoRun(b *fakeVideoBrowser) (vt *Tool, ws *fakeWorkspace, seen map[string]bool) {
	seen = map[string]bool{}
	vt = &Tool{TaskState: &browsertool.TaskState{Browser: b, TabID: 7}, SeeLink: func(link string) { seen[link] = true }}
	return vt, &fakeWorkspace{files: map[string][]byte{}}, seen
}

// fakeWorkspace 代替工作台：文件放在 map 里
type fakeWorkspace struct {
	files map[string][]byte
}

func (w *fakeWorkspace) Upload(_ context.Context, p string, data []byte) error {
	w.files[p] = append([]byte(nil), data...)
	return nil
}

func (w *fakeWorkspace) Download(_ context.Context, p string) ([]byte, error) {
	data, ok := w.files[p]
	if !ok {
		return nil, errors.New("no such file: " + p)
	}
	return data, nil
}

func (w *fakeWorkspace) Run(context.Context, string, time.Duration) (sandbox.RunResult, error) {
	return sandbox.RunResult{}, nil
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func Test_mediaRange(t *testing.T) {
	// Arrange：插件回的 data 是 base64
	b := &fakeVideoBrowser{file: []byte("0123456789")}
	vt, _, _ := videoRun(b)

	// Act
	part, err := vt.mediaRange(context.Background(), cdnLink("540"), 2, 4)

	// Assert：解成了字节
	require.NoError(t, err)
	assert.Equal(t, video.Part{Status: 206, ContentType: "video/mp4", Total: 10, Data: []byte("234")}, part)
}

func Test_saveVideo(t *testing.T) {
	page := "https://www.douyin.com/video/" + testVideoID
	file := bytes.Repeat([]byte("mp4"), 1<<20) // 3 MB：两段

	t.Run("存进工作台：视频和出处都在，告诉模型下一步，打开过的视频页算见过", func(t *testing.T) {
		// Arrange
		b := &fakeVideoBrowser{details: []string{detailBody(t, testVideoID, 90, cdnLink("540"), len(file))}, file: file}
		vt, ws, seen := videoRun(b)

		// Act
		res, err := vt.Save(context.Background(), ws, `{"url":"`+page+`?modeFrom=search"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Equal(t, "netcapture "+page+" "+video.DouyinDetailAPI, b.calls[0], "只抄 aweme/detail，网址去掉了参数")
		assert.True(t, bytes.Equal(file, ws.files["/home/user/video/"+testVideoID+".mp4"]))
		var meta map[string]any
		require.NoError(t, json.Unmarshal(ws.files["/home/user/video/"+testVideoID+".json"], &meta))
		assert.Equal(t, page, meta["source"])
		assert.Equal(t, "测评君", meta["author"])
		assert.Contains(t, res.Text, "transcribe video/"+testVideoID+".mp4")
		assert.Contains(t, res.Text, "normal_540_0")
		assert.True(t, res.PageChanged)
		assert.True(t, seen[page])
	})

	t.Run("网址过期了：重新打开视频页，用新地址再下一次", func(t *testing.T) {
		// Arrange：第一次拿到的地址回 403，第二次换了一个
		b := &fakeVideoBrowser{
			details: []string{
				detailBody(t, testVideoID, 90, cdnLink("540"), len(file)),
				detailBody(t, testVideoID, 90, cdnLink("540-fresh"), len(file)),
			},
			file: file, expired: map[string]bool{cdnLink("540"): true},
		}
		vt, ws, _ := videoRun(b)

		// Act
		res, err := vt.Save(context.Background(), ws, `{"url":"`+page+`"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		assert.Equal(t, 2, b.count("netcapture"))
		assert.True(t, bytes.Equal(file, ws.files["/home/user/video/"+testVideoID+".mp4"]))
	})

	t.Run("不是抖音视频页：不碰浏览器", func(t *testing.T) {
		// Arrange
		b := &fakeVideoBrowser{}
		vt, ws, _ := videoRun(b)

		// Act
		for _, link := range []string{"https://www.douyin.com/search/扫地机", "https://evil.example/video/123456789", "http://www.douyin.com/video/7696338249064795407"} {
			res, err := vt.Save(context.Background(), ws, `{"url":"`+link+`"}`)

			// Assert
			require.NoError(t, err)
			assert.True(t, res.Failed, link)
		}
		assert.Empty(t, b.calls)
	})

	t.Run("还没开标签页：先 open", func(t *testing.T) {
		// Arrange
		b := &fakeVideoBrowser{}
		vt, ws, _ := videoRun(b)
		vt.TaskState.TabID = 0

		// Act
		res, err := vt.Save(context.Background(), ws, `{"url":"`+page+`"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Empty(t, b.calls)
	})

	t.Run("没拿到这个视频的信息（被验证码挡住了）：失败，说清楚别再试", func(t *testing.T) {
		// Arrange：抄到的返回里是别的视频
		b := &fakeVideoBrowser{details: []string{detailBody(t, "123456789", 90, cdnLink("540"), 100)}}
		vt, ws, _ := videoRun(b)

		// Act
		res, err := vt.Save(context.Background(), ws, `{"url":"`+page+`"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "不要再试")
		assert.True(t, res.PageChanged, "标签页已经打开了视频页")
		assert.Zero(t, b.count("mediaRange"))
	})

	t.Run("太长的不存", func(t *testing.T) {
		// Arrange：15 分 1 秒
		b := &fakeVideoBrowser{details: []string{detailBody(t, testVideoID, 15*60+1, cdnLink("540"), 100)}}
		vt, ws, _ := videoRun(b)

		// Act
		res, err := vt.Save(context.Background(), ws, `{"url":"`+page+`"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "15:01")
		assert.Zero(t, b.count("mediaRange"))
	})
}
