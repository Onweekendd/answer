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

// 1688 搜索结果（2026-10-19 实测 s.1688.com/selloffer/offer_search.htm）：
//   每个商品是一个 <a>，标题、价格、起批量、公司名都在文字里，通用规则就能编号。
//   但有一部分是广告：href 是 dj.1688.com/ci_bb?…，点了要经过广告跳转才到商品页，商家要按点击付费；
//   正常的商品 href 是 detail.1688.com/offer/数字.html。快照里不放 href，模型分不出来，所以在名字前面标上。
//   另外，click 卡片点在正中间，正好是商品图上悬浮的"找同款"，打开的是以图搜款页，不是商品页（2026-10-19 case 里两次都这样）。
//   所以正常的卡片在名字前面写上商品编号，模型直接 navigate 到 detail.1688.com/offer/编号.html。
//   只写编号不写网址：快照每行最多 120 个字，网址太长会把价格挤掉
const AD_CARD = 'a[href*="dj.1688.com/ci_bb"]';
const OFFER_CARD = 'a[href*="detail.1688.com/offer/"]';
const OFFER_ID = /detail\.1688\.com\/offer\/(\d+)\.html/;

const textOf = (el: Element) => ((el as HTMLElement).innerText ?? el.textContent ?? "").trim();

export const alibabaSearch: SiteStrategy = {
  name: "1688-search",
  matches: (url) => url.hostname === "s.1688.com",
  interactive: (doc) => Array.from(doc.querySelectorAll(`${AD_CARD}, ${OFFER_CARD}`)),
  label: (el) => {
    const id = OFFER_ID.exec(el.getAttribute("href") ?? "")?.[1];
    return id ? `商品 ${id}：${textOf(el)}` : `（广告）${textOf(el)}`;
  },
  link: (el) => {
    const id = OFFER_ID.exec(el.getAttribute("href") ?? "")?.[1];
    return id ? `https://detail.1688.com/offer/${id}.html` : undefined;
  }
};

// 1688 商品详情（2026-10-19 实测 detail.1688.com/offer/数字.html）：
//   read 整页时，前 3000 字是店铺、参数、评价和一大段价格说明，价格在 3000 字以后，被截掉了。
//   标题、价格、每个尺寸的价格和库存、运费都在右边的下单栏 #cart 里，填充物这类参数在 #productAttributes 里
export const alibabaDetail: SiteStrategy = {
  name: "1688-detail",
  matches: (url) => url.hostname === "detail.1688.com",
  main: (doc) => [doc.querySelector("#cart"), doc.querySelector("#productAttributes")]
};
