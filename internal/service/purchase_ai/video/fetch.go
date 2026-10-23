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
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Fetch 把 play 分段下载下来，拼成一整个文件。
//
// 用 get 一段段地要，每段 Chunk 字节：第一段是 [0, Chunk-1]，第二段 [Chunk, 2*Chunk-1]……
//
// 每一段看 Status：
//   - 206：正常，接到后面
//   - 200：服务器不分段，Data 就是整个文件，直接结束
//   - 403、404、410：签名网址过期了，返回 ErrLinkExpired，调用方重新抓一次接口再来
//   - 别的：普通的错误
//
// 下完的条件：拼起来的长度到了 Total；Total 是 0（服务器没说多大）时，比 Chunk 短的那一段就是最后一段。
// get 的 error 原样返回：ctx 结束时就是它，调用方用 ctx.Err() 区分
func Fetch(ctx context.Context, get RangeFunc, play Play) ([]byte, error) {
	// 接口标的大小先看一眼：太大的一段都不下
	if play.Size > MaxBytes {
		return nil, tooBig(play.Size)
	}
	file := make([]byte, 0, play.Size) // 标了大小就一次分配好，append 不用反复扩容

	for {
		start := len(file)
		part, err := get(ctx, play.URL, start, start+Chunk-1)
		if err != nil {
			return nil, err
		}

		switch part.Status {
		case http.StatusPartialContent:
		case http.StatusOK:
			// 服务器不认 Range，一次给了整个文件
			if len(part.Data) > MaxBytes {
				return nil, tooBig(len(part.Data))
			}
			return part.Data, nil
		case http.StatusForbidden, http.StatusNotFound, http.StatusGone:
			return nil, fmt.Errorf("CDN 回了 HTTP %d：%w", part.Status, ErrLinkExpired)
		default:
			return nil, fmt.Errorf("CDN 回了 HTTP %d", part.Status)
		}

		// 第一段才知道整个文件多大：太大就马上停，不要下到一半才发现
		if part.Total > MaxBytes {
			return nil, tooBig(part.Total)
		}
		if len(part.Data) == 0 {
			// 说是这一段，却什么都没给：再要下去是死循环
			return nil, errors.New("CDN 回的这一段是空的，文件没下完")
		}
		file = append(file, part.Data...)

		switch {
		case part.Total > 0 && len(file) >= part.Total:
			return file, nil
		case part.Total == 0 && len(part.Data) < Chunk:
			// 服务器没说多大：不满一段，就是最后一段
			return file, nil
		case len(file) > MaxBytes:
			return nil, tooBig(len(file))
		}
	}
}

func tooBig(n int) error {
	return fmt.Errorf("视频有 %.0f MB，超过了 %d MB 的上限", float64(n)/(1<<20), MaxBytes>>20)
}
