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

import type { PageProfile } from "./profile";

// Pinterest 搜索结果（2026-10-20 用 inspect 看的 www.pinterest.com/search/pins/?q=…，中文界面）：
//   - 瀑布流的每一格是 div[data-grid-item=true][role=listitem]，滚出屏幕的格子会被删掉
//   - 格子里 div[data-test-id=pin] 带着 data-test-pin-id="814166438925594107"
//   - 链接是 a[href="/pin/814166438925594107/"]，aria-label 就是标题
//   - 广告格子没有特别的属性，只是文字里写着"Etsy 赞助的 Pin 图"。英文界面怎么写还没看过，先不猜
//   - 原图不用改写地址：img 的 srcset 里最大的那张就是 /originals/…（通用实现取的就是它）
const CARD = '[data-grid-item="true"]';
const PIN_LINK = 'a[href^="/pin/"]';

export const pinterestSearch: PageProfile = {
  name: "pinterest-search",
  matches: (url) => /(^|\.)pinterest\.com$/.test(url.hostname) && url.pathname.startsWith("/search/"),
  list: {
    cards: (doc) => Array.from(doc.querySelectorAll(CARD)),
    sponsored: (card) => (card.textContent ?? "").includes("赞助的 Pin 图"),
    link: (card) => {
      const href = card.querySelector(PIN_LINK)?.getAttribute("href");
      return href ? new URL(href, "https://www.pinterest.com").href : undefined;
    },
    title: (card) => card.querySelector(PIN_LINK)?.getAttribute("aria-label")?.trim() || undefined,
    key: (card) => {
      const id = card.querySelector("[data-test-pin-id]")?.getAttribute("data-test-pin-id");
      return id ? `pin:${id}` : undefined;
    }
  }
};
