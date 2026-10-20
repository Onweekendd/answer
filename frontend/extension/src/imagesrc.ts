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

// largestSource 是 srcset 里最大的那张，没有 srcset 就是正在显示的那张。只要 http(s) 的地址
export function largestSource(img: HTMLImageElement): string | null {
  let best = img.currentSrc || img.src;
  let bestW = 0;
  for (const candidate of (img.getAttribute("srcset") ?? "").split(",")) {
    const [url, size] = candidate.trim().split(/\s+/);
    const w = size?.endsWith("w") ? parseFloat(size) : size?.endsWith("x") ? parseFloat(size) * 1000 : 0;
    if (url && w > bestW) {
      best = new URL(url, location.href).href;
      bestW = w;
    }
  }
  return httpUrl(best);
}

// displayedSource 是这张图现在显示的那张（瀑布流里就是缩略图）。只要 http(s) 的地址
export function displayedSource(img: HTMLImageElement): string | null {
  return httpUrl(img.currentSrc || img.src);
}

function httpUrl(raw: string): string | null {
  if (!raw) {
    return null; // 空地址会被解析成页面自己的网址
  }
  try {
    const url = new URL(raw, location.href);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : null;
  } catch {
    return null;
  }
}
