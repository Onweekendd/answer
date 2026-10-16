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

import { clip } from "./text";

// 通用的网址显示，不认任何具体网站。
//
// 快照里的元素不放 href（首页实测 href 占了 81.6%，browser-use 也不放）。
// 这里只用来显示"当前页面是哪个网址"，让它对模型可读：
//   解码：%E6%96%B0 变回"新"（中文编码后长 9 倍）
//   截断：总长超过 max 就截掉。跟踪参数一般在最后，截掉的通常就是它们
//
// 绝对不要用正则去删 %XX：browser-use 就是这样把 URL 毁掉的（第 7 站坑 5）。

export function displayUrl(href: string, max = 100): string {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return clip(href, max); // 不是合法 URL，原样截断
  }

  // 参考：路径解码。decodeURIComponent 遇到不完整的 %XX 会抛 URIError，失败就保留原文
  let path = url.pathname;
  try {
    path = decodeURIComponent(path);
  } catch {
    // 保留原文
  }

  // 查询参数：URLSearchParams 迭代出来的 k、v 已经解码好了，遇到不完整的 %XX 也不会抛异常
  const params = [...url.searchParams].map(([k, v]) => `${k}=${v}`);
  const query = params.length > 0 ? `?${params.join("&")}` : "";
  return clip(`${url.host}${path}${query}`, max);
}
