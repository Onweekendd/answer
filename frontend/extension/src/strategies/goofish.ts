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

// 闲鱼搜索结果（2026-10-19 实测 www.goofish.com/search?q=…）：
//   每件商品是一个 a.feeds-item-wrap--…，通用规则能编号，但文字是卖家写的一大段描述，价格排在最后，
//   快照每行只留 120 个字，多数商品的价格被截掉了。名字改成价格在前：
//   价格（price-wrap）、降价和想要人数（price-desc）、成色和容量（cpv-wrap）、标题（main-title）、地区和信用（row4）
const CARD = 'a[class*="feeds-item-wrap--"]';

const textOf = (el: Element | null | undefined) => (el?.textContent ?? "").replace(/\s+/g, " ").trim();

export const goofishSearch: SiteStrategy = {
  name: "goofish-search",
  matches: (url) => url.hostname === "www.goofish.com" && url.pathname === "/search",
  interactive: (doc) => Array.from(doc.querySelectorAll(CARD)),
  label: (el) => {
    const part = (cls: string) => textOf(el.querySelector(`[class*="${cls}"]`));
    const cpv = Array.from(el.querySelectorAll('[class*="cpv-wrap--"]'))
      .map((c) => textOf(c))
      .filter(Boolean)
      .join(" ");
    const parts = [part("price-wrap--"), part("price-desc--"), cpv, part("main-title--"), part("row4-wrap-seller--")];
    const label = parts.filter(Boolean).join(" | ");
    return label || undefined;
  }
};

// 闲鱼商品详情（2026-10-19 实测 www.goofish.com/item?id=…，在新标签页里打开）：
//   read 整页时，后半段是"为你推荐"，是按这个浏览器账号的兴趣推的别的商品，和问题无关。
//   只读卖家信息（item-user-container：地区、来闲鱼几年、卖出几件、好评率）和商品信息（item-main-container：价格、想要、描述、成色）
export const goofishItem: SiteStrategy = {
  name: "goofish-item",
  matches: (url) => url.hostname === "www.goofish.com" && url.pathname === "/item",
  main: (doc) => [
    doc.querySelector('[class*="item-user-container--"]'),
    doc.querySelector('[class*="item-main-container--"]')
  ]
};
