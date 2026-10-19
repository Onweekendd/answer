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

// Unicode 私用区。图标字体（比如 iconfont）把每个图标画成这个区里的一个字符，
// innerText 会把它当文字读出来，对模型是乱码。淘宝首页"购物车"前面的就是 U+E63D
const PRIVATE_USE = /[-]/g;

// 去掉图标字符；连续的空白（换行、缩进、Tab）压成一个空格；超过 max 个字就截断并加 …
// 卡片的 innerText 里，标题碎片、价格、店铺之间都是换行，压成空格后就拼成了一行。
export function clip(s: string, max: number): string {
  const t = s
    .replace(PRIVATE_USE, "")
    .replace(/\s+/g, " ")
    // 价格的整数和小数常常放在两个元素里，innerText 中间会多出一个空格："7679 .04" → "7679.04"
    .replace(/(\d) \.(\d)/g, "$1.$2")
    .trim();
  return t.length > max ? `${t.slice(0, max)}…` : t;
}

// paragraphs 整理 read 读出来的一大段文字：和 clip 不同，换行要留着，分段才看得出哪句是哪段。
// 每行去掉图标字符、压掉多余空白；空行去掉；和上一行一样的也去掉（"点赞 点赞"这类按钮文字常常重复）
export function paragraphs(s: string): string {
  const out: string[] = [];
  for (const raw of s.split("\n")) {
    const line = raw.replace(PRIVATE_USE, "").replace(/\s+/g, " ").trim();
    if (line && line !== out[out.length - 1]) {
      out.push(line);
    }
  }
  return out.join("\n");
}
