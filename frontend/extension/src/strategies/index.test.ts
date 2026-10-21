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

import { collectStrategyElements, strategyMain } from "./index";

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
      snapshot: {
        candidates: () => {
          throw new Error("broken selector");
        }
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
  expect(result.stats).toEqual({ strategy: "xiaohongshu", strategyMatched: 1 });
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

it("京东详情页：规格选项编进来，名字带组名和选中状态；read 读右边一栏", () => {
  // Arrange：结构照 2026-10-19 在 DevTools 里看到的写
  document.body.innerHTML = `<div id="history">最近感兴趣 别的商品</div>
    <div class="page-right-content"><div class="page-right-skuname">金百达 32GB 套装</div>
      <div class="page-right-spec"><div class="horizontal-layout specification-series-layout">
        <div class="layout-label">系列品</div>
        <div class="layout-content">
          <div class="specification-series-item"><span class="specification-series-item-text">DDR4 马甲内存</span></div>
          <div class="specification-series-item specification-series-item--selected"><span class="specification-series-item-text">DDR5 国产颗粒</span></div>
        </div>
      </div>
      <div class="specifications-panel-content"><div class="specification-group specification-group-0">
        <div class="specification-group-label">内存频率</div>
        <div class="specification-group-content">
          <div class="specification-item-sku has-image specification-item-sku--selected"><img class="specification-item-sku-image" alt="x"><span class="specification-item-sku-text">[D5 6000]银爵国产颗粒 C36</span></div>
          <div class="specification-item-sku has-image specification-item-sku--unavailable" title="无此商品"><span class="specification-item-sku-text">[6000]星刃白灯条国产颗粒 C36</span></div>
        </div>
      </div></div></div>
    </div>`;
  const url = new URL("https://item.jd.com/100133261333.html");

  // Act
  const result = collectStrategyElements(url, document);

  // Assert：两种写法的组都认得，只要选项那一层，不要里面的 -text
  expect(result.elements.map((el) => result.labels.get(el))).toEqual([
    "系列品：DDR4 马甲内存",
    "系列品：DDR5 国产颗粒（已选）",
    "内存频率：[D5 6000]银爵国产颗粒 C36（已选）",
    "内存频率：[6000]星刃白灯条国产颗粒 C36（无此商品）"
  ]);
  expect(strategyMain(url, document).map((el) => el.className)).toEqual(["page-right-content"]);
  expect(strategyMain(new URL("https://search.jd.com/Search"), document)).toEqual([]);
});

it("天猫详情页：规格选项编进来，名字带组名、选中和无货，角标不要", () => {
  // Arrange：结构照 2026-10-19 在 DevTools 里看到的写
  document.body.innerHTML = `<div class="root--eXasfhBG" id="skuOptionsArea"><div class="skuItem--Z2AJB9Ew">
      <div class="labelWrap--ffBEejeJ"><span>颜色分类</span><span>切换大图模式</span></div>
      <div class="skuValueWrap--aEfxuhNr"><div class="content--DIGuLqdf">
        <div class="valueItem--smR4pNt4 isSelected--_a9zOp7C hasImg--K82HLg1O" data-vid="1" data-disabled="false">
          <div class="valueItemImgWrap--ZvA2Cmim"></div><span title="沙发本体（不含沙发套）" class="valueItemText--T7YrR8tO">沙发本体（不含沙发套）</span></div>
        <div class="valueItem--smR4pNt4 hasImg--K82HLg1O" data-vid="2" data-disabled="false">
          <span title="芥末黄色" class="valueItemText--T7YrR8tO">芥末黄色</span><span class="cornerText--VbKlraC5">近期热销</span></div>
        <div class="valueItem--smR4pNt4" data-vid="3" data-disabled="true"><span title="藏青色" class="valueItemText--T7YrR8tO">藏青色</span></div>
      </div></div>
    </div></div>`;

  // Act
  const result = collectStrategyElements(new URL("https://detail.tmall.com/item.htm?id=842192589383"), document);

  // Assert
  expect(result.elements.map((el) => result.labels.get(el))).toEqual([
    "颜色分类：沙发本体（不含沙发套）（已选）",
    "颜色分类：芥末黄色",
    "颜色分类：藏青色（无货）"
  ]);
});

it("1688 搜索结果：广告卡片标上（广告），正常的商品写上商品编号，链接是商品页", () => {
  // Arrange
  document.body.innerHTML = `<a href="https://dj.1688.com/ci_bb?a=7255&e=x">羽绒被 ¥ 180</a>
    <a href="http://detail.1688.com/offer/828190408511.html">芬兰原装羽绒被 ¥ 688</a>`;

  // Act
  const result = collectStrategyElements(new URL("https://s.1688.com/selloffer/offer_search.htm?keywords=x"), document);

  // Assert
  expect(result.elements.map((el) => result.labels.get(el))).toEqual([
    "（广告）羽绒被 ¥ 180",
    "商品 828190408511：芬兰原装羽绒被 ¥ 688"
  ]);
  expect([...result.links.values()]).toEqual(["https://detail.1688.com/offer/828190408511.html"]);
});

it("1688 详情页：read 按顺序读下单栏和商品属性，找不到的那块不要", () => {
  // Arrange
  document.body.innerHTML = '<div id="productAttributes">填充物 白鹅绒</div><div id="cart">¥ 376</div>';
  const url = new URL("https://detail.1688.com/offer/828190408511.html");

  // Act & Assert
  expect(strategyMain(url, document).map((el) => el.id)).toEqual(["cart", "productAttributes"]);
  document.getElementById("productAttributes")!.remove();
  expect(strategyMain(url, document).map((el) => el.id)).toEqual(["cart"]);
});

it("闲鱼搜索结果：卡片名字价格在前，长描述放在后面，不会把价格挤掉", () => {
  // Arrange：结构照 2026-10-19 用 inspect 看到的写
  document.body.innerHTML = `<a class="feeds-item-wrap--rGdH_KoF" href="https://www.goofish.com/item?id=1083647455304">
      <div class="feeds-image-container--biS2HR4Z"><img></div>
      <div class="feeds-content--blv39SOi">
        <div class="row1-wrap-title--qIlOySTh"><span class="main-title--sMrtWSJa">iPad Air 11英寸 M3 128G 国行 电池健康97%</span></div>
        <div class="row2-wrap-cpv--_dKW4c6D"><div class="cpv-wrap--g0kR4uhH">几乎全新</div><div class="cpv-wrap--g0kR4uhH">128GB</div></div>
        <div class="row3-wrap-price--IZmX7M0K"><div class="price-wrap--YzmU5cUl"><span>¥</span><span class="number--NKh1vXWM">3456</span><span>.78</span></div>
          <div class="price-desc--hxYyq3i3">累计降价98.77元</div></div>
        <div class="row4-wrap-seller--E0pIfXoF"><div class="seller-text-wrap--yZYFxBkK">广东</div><div class="credit-container--w3dcSvoi">百分百好评</div></div>
      </div></a>`;

  // Act
  const result = collectStrategyElements(new URL("https://www.goofish.com/search?q=ipad"), document);

  // Assert
  expect(result.elements.map((el) => result.labels.get(el))).toEqual([
    "¥3456.78 | 累计降价98.77元 | 几乎全新 128GB | iPad Air 11英寸 M3 128G 国行 电池健康97% | 广东百分百好评"
  ]);
});

it("闲鱼详情页：read 只读卖家和商品信息，不读按账号兴趣推的「为你推荐」", () => {
  // Arrange
  document.body.innerHTML = `<div class="item-user-container--fbTUeNre">卖出31件宝贝</div>
    <div class="item-main-container--jhpFKlaS">¥ 3456.78</div><div class="item-feeds-container--cYdVMAnU">为你推荐</div>`;

  // Act
  const roots = strategyMain(new URL("https://www.goofish.com/item?id=1"), document);

  // Assert
  expect(roots.map((el) => el.textContent)).toEqual(["卖出31件宝贝", "¥ 3456.78"]);
});
