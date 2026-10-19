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
