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

// 快照之前等页面"安静"下来。京东首页 complete 以后商品才用 JS 渲染出来，
// 立刻快照只能拿到框架（pagebench 实测：同一个页面 37 行和 233 行）。

export interface StableOptions {
  quietMs: number; // 连续这么久没有 DOM 变化，就认为页面稳定了
  maxMs: number; // 最多等这么久。有轮播图的页面永远不会完全安静
}

export interface StableResult {
  stable: boolean; // true：等到了安静；false：等满 maxMs 也没安静下来
  waitedMs: number; // 实际等了多久
}

export const DEFAULT_STABLE: StableOptions = { quietMs: 500, maxMs: 5000 };

// 等 root 下面的 DOM 连续 quietMs 没有变化，最多等 maxMs。
export function waitForStable(root: Node, options: StableOptions = DEFAULT_STABLE): Promise<StableResult> {
  const { quietMs, maxMs } = options;
  const start = performance.now();

  return new Promise((resolve) => {
    // 两条路（等到安静、等满上限）都走这里：先收尾，再 resolve
    const finish = (stable: boolean) => {
      observer.disconnect();
      clearTimeout(quietTimer);
      clearTimeout(maxTimer);
      resolve({ stable, waitedMs: performance.now() - start });
    };

    // 安静计时器：每次 DOM 变化都要换一个新的，所以用 let，新的那个要存回来
    let quietTimer = setTimeout(() => finish(true), quietMs);
    const maxTimer = setTimeout(() => finish(false), maxMs);

    // 像防抖：有变化就把安静计时器重新计时
    const observer = new MutationObserver(() => {
      clearTimeout(quietTimer);
      quietTimer = setTimeout(() => finish(true), quietMs);
    });
    // 不观察 attributes：轮播图、悬停样式、懒加载换 src 都只改属性，对文字快照没影响，却会让页面永远"不安静"
    observer.observe(root, { childList: true, subtree: true, characterData: true });
  });
}
