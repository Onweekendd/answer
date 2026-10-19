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

// 京东商品详情（2026-10-19 在 DevTools 里看的，item.jd.com 和海外自营的 npcitem.jd.hk）：
//   - 右边一栏 .page-right-content 是标题、价格、优惠、规格、服务；页面最上面是顶栏和这个浏览器账号的
//     "最近感兴趣""最近收藏"。read 整页时前 3000 字全是后者，价格一个字都读不到，还把账号看过的东西给了模型
//   - 规格在 .page-right-spec 里，有两种写法，都是 div，快照里一个都没有，模型没法换规格看价格：
//       第一组：.layout-label（"系列品"）加一排 .specification-series-item
//       下面几组：.specification-group，组名在 .specification-group-label（"内存频率"），
//       选项是 .specification-item-sku，没有这个组合的类名带 --unavailable（title 里写着"无此商品"）
//     选中的那个类名都带 --selected
const SPEC_OPTIONS = ".page-right-spec .specification-series-item, .page-right-spec .specification-item-sku";
const SPEC_GROUP_LABEL = ":scope > .layout-label, :scope > .specification-group-label";

export const jdItem: SiteStrategy = {
  name: "jd-item",
  matches: (url) => url.hostname === "item.jd.com" || url.hostname === "npcitem.jd.hk",
  interactive: (doc) => Array.from(doc.querySelectorAll(SPEC_OPTIONS)),
  // "内存频率：[D5 6000]银爵国产颗粒 C36（已选）"：组名从同一组的标签来，选中、没有这个组合从类名来
  label: (el) => {
    const text = (el.querySelector('[class$="-text"]')?.textContent ?? el.textContent ?? "").trim();
    const name = groupLabel(el);
    const classes = el.className.toString();
    const state = classes.includes("--selected") ? "（已选）" : classes.includes("--unavailable") ? "（无此商品）" : "";
    return `${name ? `${name}：` : "规格："}${text}${state}`;
  },
  main: (doc) => doc.querySelector(".page-right-content")
};

// groupLabel 往上找到同一组的组名（"系列品""内存频率"）。不用 :has()，测试用的 jsdom 不认
function groupLabel(el: Element): string | undefined {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const label = p.querySelector(SPEC_GROUP_LABEL);
    if (label) {
      return label.textContent?.trim() || undefined;
    }
    if (p.classList.contains("page-right-spec")) {
      return undefined;
    }
  }
  return undefined;
}
