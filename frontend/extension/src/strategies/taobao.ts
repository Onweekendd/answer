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

export const taobao: SiteStrategy = {
  name: "taobao-search",
  matches: (url) => url.hostname === "s.taobao.com" && url.pathname === "/search",
  interactive: (doc) =>
    // 实测这些都是没有 role 的 div，只按类名前缀匹配，不依赖构建 hash：
    //   价格菜单：从低到高 / 从高到低
    //   价格区间面板（hover"区间"后挂在 body 末尾）：预设区间，和"重置""确定"。
    //   回车不会提交区间，填完最低价、最高价必须点"确定"（2026-10-18 实测）
    Array.from(
      doc.querySelectorAll(
        [
          '[class*="priceTagWrap--"] > [class*="priceTag--"]',
          '[class*="sectionPriceSelecter--"] [class*="priceSelectItemWrapper--"] > *',
          '[class*="sectionPriceSelecter--"] [class*="buttonWrapper--"] > [class*="resetButton--"]',
          '[class*="sectionPriceSelecter--"] [class*="buttonWrapper--"] > [class*="confirmButton--"]'
        ].join(", ")
      )
    )
};

// 天猫、淘宝的商品详情（2026-10-19 在 DevTools 里看的，天猫无印良品旗舰店）：
//   规格在 #skuOptionsArea 里，每组是一个 skuItem--（组名在 labelWrap-- 里，"颜色分类"），
//   每个选项是一个 valueItem--（选中的类名带 isSelected--，无货的 data-disabled="true"），名字在 valueItemText-- 的 title 里。
//   都是 div，快照里一个都没有：模型选不了规格，只能看默认规格的价
export const taobaoItem: SiteStrategy = {
  name: "taobao-item",
  matches: (url) =>
    url.hostname === "item.taobao.com" ||
    url.hostname === "detail.tmall.com" ||
    url.hostname === "chaoshi.detail.tmall.com",
  interactive: (doc) =>
    Array.from(doc.querySelectorAll('#skuOptionsArea [class*="valueItem--"]')).filter((el) =>
      Array.from(el.classList).some((c) => c.startsWith("valueItem--"))
    ),
  // "颜色分类：芥末黄色（已选）"。角标（"近期热销"）不要，它和规格无关
  label: (el) => {
    const textEl = el.querySelector('[class*="valueItemText--"]');
    const text = (textEl?.getAttribute("title") || textEl?.textContent || "").trim();
    // 组名那一栏右边还有个"切换大图模式"的开关，文字粘在组名后面（2026-10-19 实测），去掉
    const group = el
      .closest('[class*="skuItem--"]')
      ?.querySelector('[class*="labelWrap--"]')
      ?.textContent?.replace("切换大图模式", "")
      .trim();
    const classes = el.className.toString();
    const state = classes.includes("isSelected--")
      ? "（已选）"
      : el.getAttribute("data-disabled") === "true"
        ? "（无货）"
        : "";
    return `${group ? `${group}：` : "规格："}${text}${state}`;
  }
};
