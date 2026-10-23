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

// clearShort 是够看清画面上的字的短边：540p
const clearShort = 540

// Pick 挑出要下载的那个文件。
//
// 为什么要挑：默认那一档一般是原画，10 分钟就有 128 MB，要分 62 段传。
// 我们只是要看懂视频（转文字、看画面上的字），540p 足够，体积小好几倍。
//
// hostOK 说一个地址能不能下载：每个网站自己给（抖音是 DouyinHostOK），只认 https 和它自己的 CDN。
// 插件会去下载挑出来的地址，所以不合格的地址一个都不能挑。
//
// 规则：
//  1. 每一档从它的 URLs 里取第一个 hostOK 的地址；一个都没有，这一档就不要。
//  2. 在 s.Variants 里挑（谁更合适见 better）：
//     - 短边 ≥ 540 的档里，挑 Size 最小的；Size 是 0（没标）的排在有大小的后面
//     - 一档 ≥ 540 的都没有，挑短边最大的
//     - s.Variants 里一档能用的都没有（空的、地址都不合格），退回 s.Default
//  3. s.Default 也没有能用的地址，返回 false。
func Pick(s Source, hostOK func(link string) bool) (Play, bool) {
	var best Play
	var bestV Variant
	found := false
	// 边走边记住目前最好的一档：不用先排序，也不用把能用的都收集起来
	for _, v := range s.Variants {
		link, ok := firstOK(v.URLs, hostOK)
		if !ok {
			continue
		}
		if !found || better(v, bestV) {
			best, bestV, found = Play{URL: link, Name: v.Name, Size: v.Size}, v, true
		}
	}
	if found {
		return best, true
	}
	if link, ok := firstOK(s.Default.URLs, hostOK); ok {
		return Play{URL: link, Name: s.Default.Name, Size: s.Default.Size}, true
	}
	return Play{}, false
}

// firstOK 返回 urls 里第一个能下载的地址
func firstOK(urls []string, hostOK func(string) bool) (string, bool) {
	for _, u := range urls {
		if hostOK(u) {
			return u, true
		}
	}
	return "", false
}

// better 说 a 是不是比 b 更合适。只比清晰度和大小，地址能不能用在外面就判断好了
func better(a, b Variant) bool {
	aShort, bShort := min(a.Width, a.Height), min(b.Width, b.Height)
	aClear, bClear := aShort >= clearShort, bShort >= clearShort
	switch {
	case aClear != bClear:
		// 一个够清楚、一个不够：够清楚的赢
		return aClear
	case !aClear:
		// 都不够清楚：越清楚越好
		return aShort > bShort
	case (a.Size == 0) != (b.Size == 0):
		// 都够清楚，一个标了大小、一个没标：标了的赢，没标的可能是原画那么大
		return a.Size != 0
	default:
		// 都够清楚，大小都标了（或者都没标）：小的赢，下得快
		return a.Size < b.Size
	}
}
