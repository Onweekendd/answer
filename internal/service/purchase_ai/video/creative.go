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

package video

import "strings"

// PickCreative enforces the requested resolution, prefers a compatible H.264
// MP4 when available, then avoids unnecessarily large files. Pick's existing
// analysis-oriented policy remains unchanged.
func PickCreative(s Source, minShortSide int) (Play, Variant, bool) {
	var best Variant
	var play Play
	found := false
	for _, v := range append(append([]Variant{}, s.Variants...), s.Default) {
		if min(v.Width, v.Height) < minShortSide || v.Size > MaxBytes {
			continue
		}
		link, ok := firstOK(v.URLs, DouyinHostOK)
		if !ok {
			continue
		}
		h264, bestH264 := strings.HasSuffix(v.Name, "_0"), strings.HasSuffix(best.Name, "_0")
		better := !found
		if found {
			switch {
			case h264 != bestH264:
				better = h264
			case (v.Size == 0) != (best.Size == 0):
				better = v.Size > 0
			default:
				better = v.Size < best.Size
			}
		}
		if better {
			best, play, found = v, Play{URL: link, Name: v.Name, Size: v.Size}, true
		}
	}
	return play, best, found
}
