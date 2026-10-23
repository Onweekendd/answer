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

package video

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// cdn 是 CDN 上的一个地址
func cdn(name string) string { return "https://v5-dy.zjcdn.com/sign/expire/video/" + name + ".mp4" }

// gear 是一档清晰度：竖屏视频，短边 short，接口标的大小 size，地址是 links
func gear(name string, short, size int, links ...string) Variant {
	return Variant{Name: name, URLs: links, Size: size, Width: short, Height: short * 16 / 9}
}

func TestPick(t *testing.T) {
	original := gear("default", 1080, 128<<20, cdn("default"))

	for _, tt := range []struct {
		name string
		s    Source
		want Play
		ok   bool
	}{
		{
			name: "≥540 的里面挑最小的：540 比 1080 小，360 不够清楚",
			s: Source{Default: original, Variants: []Variant{
				gear("normal_1080_0", 1080, 120<<20, cdn("1080")),
				gear("normal_540_0", 540, 45<<20, cdn("540")),
				gear("normal_360_0", 360, 20<<20, cdn("360")),
			}},
			want: Play{URL: cdn("540"), Name: "normal_540_0", Size: 45 << 20}, ok: true,
		},
		{
			name: "横屏的也按短边算",
			s: Source{Default: original, Variants: []Variant{
				{Name: "wide_540", URLs: []string{cdn("w540")}, Size: 40 << 20, Width: 960, Height: 540},
				{Name: "wide_360", URLs: []string{cdn("w360")}, Size: 15 << 20, Width: 640, Height: 360},
			}},
			want: Play{URL: cdn("w540"), Name: "wide_540", Size: 40 << 20}, ok: true,
		},
		{
			name: "没标大小的排在标了的后面",
			s: Source{Default: original, Variants: []Variant{
				gear("unknown", 720, 0, cdn("720")),
				gear("normal_1080_0", 1080, 120<<20, cdn("1080")),
			}},
			want: Play{URL: cdn("1080"), Name: "normal_1080_0", Size: 120 << 20}, ok: true,
		},
		{
			name: "一档 ≥540 的都没有：挑短边最大的",
			s: Source{Default: original, Variants: []Variant{
				gear("normal_240_0", 240, 5<<20, cdn("240")),
				gear("normal_360_0", 360, 20<<20, cdn("360")),
			}},
			want: Play{URL: cdn("360"), Name: "normal_360_0", Size: 20 << 20}, ok: true,
		},
		{
			name: "一档里跳过不合格的地址，用后面合格的",
			s: Source{Default: original, Variants: []Variant{
				gear("normal_540_0", 540, 45<<20, "http://v5.zjcdn.com/540.mp4", "https://evil.example/540.mp4", cdn("540b")),
			}},
			want: Play{URL: cdn("540b"), Name: "normal_540_0", Size: 45 << 20}, ok: true,
		},
		{
			name: "最小的那一档地址都不合格：这一档不要，挑下一个",
			s: Source{Default: original, Variants: []Variant{
				gear("normal_540_0", 540, 45<<20, "https://evil.example/540.mp4"),
				gear("normal_720_0", 720, 70<<20, cdn("720")),
			}},
			want: Play{URL: cdn("720"), Name: "normal_720_0", Size: 70 << 20}, ok: true,
		},
		{
			name: "没有各档清晰度：退回默认那一档",
			s:    Source{Default: original},
			want: Play{URL: cdn("default"), Name: "default", Size: 128 << 20}, ok: true,
		},
		{
			name: "哪一档都没有能用的地址",
			s:    Source{Default: Variant{Name: "default", URLs: []string{"https://www.douyin.com/aweme/v1/play/?video_id=1"}}},
			ok:   false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got, ok := Pick(tt.s, DouyinHostOK)

			// Assert
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
