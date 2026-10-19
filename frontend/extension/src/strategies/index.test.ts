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

// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";

import { collectStrategyElements } from "./index";

afterEach(() => {
  document.body.innerHTML = "";
});

it("仅淘宝搜索页补充价格菜单，不匹配其他站点或商品卡片", () => {
  document.body.innerHTML =
    '<div class="priceTagWrap--abc"><div class="priceTag--def">从低到高</div><div class="priceTag--ghi">从高到低</div></div><div class="priceTag--other">商品价格</div>';
  const result = collectStrategyElements(new URL("https://s.taobao.com/search?q=phone"), document);
  expect(result.elements.map((el) => el.textContent)).toEqual(["从低到高", "从高到低"]);
  expect(result.stats).toEqual({ strategy: "taobao-search", strategyMatched: 2 });
  for (const url of [
    "https://example.com/search",
    "https://s.taobao.com.evil.test/search",
    "https://s.taobao.com/other"
  ]) {
    expect(collectStrategyElements(new URL(url), document)).toEqual({
      elements: [],
      labels: new Map(),
      links: new Map(),
      stats: {}
    });
  }
});

it("淘宝价格区间面板：预设区间、重置、确定都补充进来，按页面顺序；面板外同名类不算", () => {
  // Arrange：结构照 2026-10-18 在 DevTools 里看到的写
  document.body.innerHTML = `<div class="buttonWrapper--x"><div class="confirmButton--y">别处的确定</div></div>
    <div class="next-overlay-wrapper v2 opened"><div class="sectionPriceSelecter--BUniLbQt">
      <div class="priceGapWrapper--YivbFFbh"><input placeholder="¥ 最低价"><input placeholder="¥ 最高价"></div>
      <div class="priceSelectItemWrapper--Yo3CXc2Y"><div>0-5987 30%的选择</div><div>5987-5999 60%的选择</div></div>
      <div class="buttonWrapper--yU2hX3tC"><div class="resetButton--uWOgRiAr">重置</div><div class="confirmButton--gHXjLus4">确定</div></div>
    </div></div>`;

  // Act
  const result = collectStrategyElements(new URL("https://s.taobao.com/search?q=phone"), document);

  // Assert：输入框本来就能被通用规则认出来，策略只补这四个
  expect(result.elements.map((el) => el.textContent)).toEqual([
    "0-5987 30%的选择",
    "5987-5999 60%的选择",
    "重置",
    "确定"
  ]);
  expect(result.stats).toEqual({ strategy: "taobao-search", strategyMatched: 4 });
});

it("选择器零命中或策略抛异常仍允许通用快照继续", () => {
  expect(collectStrategyElements(new URL("https://s.taobao.com/search"), document).stats).toEqual({
    strategy: "taobao-search",
    strategyMatched: 0
  });
  const result = collectStrategyElements(new URL("https://example.com"), document, [
    {
      name: "broken",
      matches: () => true,
      interactive: () => {
        throw new Error("broken selector");
      }
    }
  ]);
  expect(result.elements).toEqual([]);
  expect(result.stats.strategyError).toContain("broken selector");
});

it("小红书：笔记浮层的图片区编进快照，好让截图只截图片；别处同名类不算", () => {
  // Arrange
  document.body.innerHTML = `<div class="media-container">首页里的</div>
    <div id="noteContainer"><div class="media-container"><div class="fraction">2/6</div></div></div>`;

  // Act
  const result = collectStrategyElements(new URL("https://www.xiaohongshu.com/explore/abc"), document);

  // Assert
  expect(result.elements.map((el) => el.textContent)).toEqual(["2/6"]);
  expect(result.labels.get(result.elements[0])).toBe("笔记图片 第2/6张");
  expect(result.stats).toEqual({ strategy: "xiaohongshu-note", strategyMatched: 1 });
});

it("小红书：单图笔记没有 2/6 也认得出是图片，视频笔记说是视频", () => {
  // Arrange
  const url = new URL("https://www.xiaohongshu.com/explore/abc");
  document.body.innerHTML = '<div id="noteContainer"><div class="media-container"><img src="a.jpg"></div></div>';

  // Act
  const single = collectStrategyElements(url, document);
  document.body.innerHTML = '<div id="noteContainer"><div class="media-container"><video></video></div></div>';
  const video = collectStrategyElements(url, document);

  // Assert
  expect(single.labels.get(single.elements[0])).toBe("笔记图片（1 张）");
  expect(video.labels.get(video.elements[0])).toBe("笔记视频");
});

it("京东搜索结果：卡片编进来并给出商品页链接，客服图标起个别点的名字", () => {
  // Arrange：结构照 2026-10-19 在 DevTools 里看到的写，卡片里唯一的 <a> 是客服
  document.body.innerHTML = `<div class="_wrapper_1y1ag_3 plugin_goodsCardWrapper" data-sku="100378972868">
      <div class="_card_1y1ag_92"><div class="_goods_title_container_1k2fi_1">Apple iPad Air M3</div>
      <span class="_name_3z0zc_15">京东数码海外自营旗舰店</span>
      <a target="_blank" href="//chat.jd.com/index.action?entry=jd_search&pid=100378972868" class="_customer_service_icon_3z0zc_60"></a>
      <div data-sku="100378972868">里面也带 data-sku 的元素</div></div>
    </div>`;

  // Act
  const result = collectStrategyElements(new URL("https://search.jd.com/Search?keyword=ipad"), document);

  // Assert：只要最外层的卡片，不要里面那个
  const [card, chat] = result.elements;
  expect(result.elements).toHaveLength(2);
  expect(card.getAttribute("class")).toContain("plugin_goodsCardWrapper");
  expect(result.links.get(card)).toBe("https://item.jd.com/100378972868.html");
  expect(result.labels.has(card)).toBe(false);
  expect(result.labels.get(chat)).toContain("客服");
  expect(result.links.has(chat)).toBe(false);
});
