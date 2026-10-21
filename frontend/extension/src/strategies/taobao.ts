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

// 搜索结果的卡片（2026-10-21 用 inspect 看的 s.taobao.com/search?q=电热水器60升）：
//   - 每个商品是 a[id=item_id_<商品 id>]，里面是 div.doubleCard--：主图 img.mainImg--、标题 div.title--、
//     规格摘要 descBox-- 里的一个个 text--（"60L""650x310x560mm"）、价格区 priceWrapper--、店铺 shopInfo--
//   - 价格区是几个兄弟元素：unit--"¥"、priceInt--"7??"、priceFloat--（小数，常常是空的）、priceDesc--"补贴后"、
//     realSales--"8000+人付款"，后面是发货地。很多商品的价被藏成"7??"，网页上只能看到区间
//   - 店铺区：shopTagWrapper--"品牌旗舰"，shopNameText--"小米官方旗舰店"
//   - 广告（直通车）的链接是 click.simba.taobao.com 的跳转，不是商品页；它的店铺链接也是
// 通用实现取整张卡片的文字当标题，"榜·第5名""直接下单享优惠"都会混进来。比价要的是规格、价、销量、店铺，
// 所以标题拼成一行：标题 ｜ 规格 ｜ 价 ｜ 销量 ｜ 店铺。模型在工作台读 harvest/items.json 就能直接比
const CARD = 'a[id^="item_id_"]';

function text(el: Element | null | undefined): string {
  return el?.textContent?.replace(/\s+/g, " ").trim() ?? "";
}

function itemID(card: Element): string | undefined {
  return card.id.replace("item_id_", "") || undefined;
}

function cardTitle(card: Element): string | undefined {
  const title = text(card.querySelector('[class*="descWrapper--"] [class*="title--"]'));
  if (!title) {
    return undefined;
  }
  const specs = Array.from(card.querySelectorAll('[class*="descBox--"] [class*="text--"]'))
    .map(text)
    .filter(Boolean)
    .join(" · ");
  const price = [
    text(card.querySelector('[class*="unit--"]')) +
      text(card.querySelector('[class*="priceInt--"]')) +
      text(card.querySelector('[class*="priceFloat--"]')),
    text(card.querySelector('[class*="priceDesc--"]'))
  ]
    .filter(Boolean)
    .join(" ");
  const sales = text(card.querySelector('[class*="realSales--"]'));
  const shop = [text(card.querySelector('[class*="shopTagWrapper--"]')), text(card.querySelector('[class*="shopNameText--"]'))]
    .filter(Boolean)
    .join(" ");
  return [title, specs, price, sales, shop].filter(Boolean).join(" ｜ ");
}

export const taobao: PageProfile = {
  name: "taobao-search",
  matches: (url) => url.hostname === "s.taobao.com" && url.pathname === "/search",
  snapshot: {
    candidates: (doc) =>
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
  },
  list: {
    cards: (doc) => Array.from(doc.querySelectorAll(CARD)),
    sponsored: (card) => (card.getAttribute("href") ?? "").includes("click.simba.taobao.com"),
    // 卡片的 href 带着一长串跟踪参数，只留商品 id。天猫的商品在 detail.tmall.com，别的在 item.taobao.com
    link: (card) => {
      const id = itemID(card);
      if (!id) {
        return undefined;
      }
      const host = (card.getAttribute("href") ?? "").includes("tmall.com") ? "detail.tmall.com" : "item.taobao.com";
      return `https://${host}/item.htm?id=${id}`;
    },
    title: cardTitle,
    image: (card) => card.querySelector<HTMLImageElement>('img[class*="mainImg--"]') ?? undefined,
    key: (card) => {
      const id = itemID(card);
      return id ? `tb:${id}` : undefined;
    }
  }
};

// 天猫、淘宝的商品详情（2026-10-19 在 DevTools 里看的，天猫无印良品旗舰店）：
//   规格在 #skuOptionsArea 里，每组是一个 skuItem--（组名在 labelWrap-- 里，"颜色分类"），
//   每个选项是一个 valueItem--（选中的类名带 isSelected--，无货的 data-disabled="true"），名字在 valueItemText-- 的 title 里。
//   都是 div，快照里一个都没有：模型选不了规格，只能看默认规格的价
export const taobaoItem: PageProfile = {
  name: "taobao-item",
  matches: (url) =>
    url.hostname === "item.taobao.com" ||
    url.hostname === "detail.tmall.com" ||
    url.hostname === "chaoshi.detail.tmall.com",
  snapshot: {
    candidates: (doc) =>
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
  }
};
