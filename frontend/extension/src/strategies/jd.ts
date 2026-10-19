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

import type { SiteStrategy } from "./types";

// 京东搜索结果（2026-10-19 在 DevTools 里看的）：
//   - 一张商品卡片是外层带 data-sku="100378972868" 的 div，标题、价格、店铺全是 div 和 span，
//     没有一个指向商品页的 <a>：点进详情页靠京东自己的 JS
//   - 卡片里唯一的 <a> 是店铺名后面的笑脸图标，href 是 //chat.jd.com/…，打开的是和店家的聊天
// 不加策略的话，快照里每张卡片只剩那个没有文字的 <a>：模型想进商品就点了客服，或者干脆不进详情页
export const jd: SiteStrategy = {
  name: "jd-search",
  matches: (url) => url.hostname === "search.jd.com",
  interactive: (doc) => [
    // 只要最外层的 data-sku：卡片里面的元素也可能带着它
    ...Array.from(doc.querySelectorAll("[data-sku]")).filter((el) => !el.parentElement?.closest("[data-sku]")),
    ...Array.from(doc.querySelectorAll('a[href*="chat.jd.com"]'))
  ],
  // 卡片照常用它的文字（标题、价格、店铺）；客服图标没有文字，起个名字让模型知道别点
  label: (el) => (el.matches('a[href*="chat.jd.com"]') ? "客服（打开的是和店家的聊天，不要点）" : undefined),
  // 卡片的商品页由 data-sku 拼出来，和点进去打开的是同一个页面
  link: (el) => {
    const sku = el.getAttribute("data-sku") ?? "";
    return /^\d+$/.test(sku) ? `https://item.jd.com/${sku}.html` : undefined;
  }
};
