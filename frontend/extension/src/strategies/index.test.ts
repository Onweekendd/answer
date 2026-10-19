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
    expect(collectStrategyElements(new URL(url), document)).toEqual({ elements: [], stats: {} });
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
