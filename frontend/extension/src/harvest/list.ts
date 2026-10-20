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

import { displayedSource, largestSource } from "../imagesrc";
import type { HarvestItem, HarvestResult } from "../protocol";
import type { ListSteps } from "../strategies";
import { repeatedCards } from "./cards";

// 列表收割的骨架（模板方法）：问步骤要卡片 → 每张变成一条 → 去广告、去重 → 滚一屏 → 再来，
// 收够 limit 张、滚满 maxScrolls 屏、或者连着几屏没有新卡片就停。
// 骨架只管流程；"哪些是卡片、哪个是广告"是步骤的事（通用实现在下面，页面的增强在 strategies/）；
// 滚动、等图片加载这些动页面的事由调用方传进来（content.ts），测试里换成假的

// 连着几屏没有新卡片就当作到底了：瀑布流加载慢的时候，一屏没新的不代表没有了
const STALE_ROUNDS = 2;
const TITLE_MAX = 100;

export interface HarvestPage {
  // 往下滚一屏，等新内容出来。返回 false：滚不动了（到底了）
  scroll(): Promise<boolean>;
  // 等这些卡片里的图加载完（有上限，不会一直等）
  settle(cards: Element[]): Promise<void>;
}

export interface HarvestOptions {
  limit: number;
  maxScrolls: number;
}

// 通用实现：任何列表页都能用，只是不如页面增强准
export const genericList: ListSteps = {
  cards: repeatedCards,
  sponsored: () => false,
  link: (card) => {
    const a = card.closest("a[href]") ?? card.querySelector("a[href]");
    return httpHref(a?.getAttribute("href") ?? undefined, card.ownerDocument.baseURI);
  },
  title: (card) => {
    const a = card.closest("a[href]") ?? card.querySelector("a[href]");
    const img = card.querySelector("img");
    const text = a?.getAttribute("aria-label") || img?.getAttribute("alt") || card.textContent || "";
    return text.replace(/\s+/g, " ").trim().slice(0, TITLE_MAX) || undefined;
  },
  image: (card) => largestImg(card),
  key: () => undefined
};

export async function harvestList(
  doc: Document,
  steps: ListSteps,
  options: HarvestOptions,
  page: HarvestPage
): Promise<Omit<HarvestResult, "stats"> & { stats: Omit<HarvestResult["stats"], "ms"> }> {
  const items: HarvestItem[] = [];
  const seen = new Set<string>();
  let sponsored = 0;
  let noImage = 0;
  let scrolls = 0;
  let stale = 0;
  let ended = false;
  // 滚不动了以后再收最后一遍：最后一屏的新卡片是滚动以后才出来的
  let lastPass = false;

  for (;;) {
    const cards = steps.cards(doc);
    await page.settle(cards);
    let fresh = 0;
    for (const card of cards) {
      const item = toItem(card, steps);
      if (!item) {
        // 没有图的卡片先不记进 seen：可能只是图还没加载，滚回来还有机会。所以 noImage 是"出现过几次"
        noImage++;
        continue;
      }
      if (seen.has(item.key)) {
        continue;
      }
      seen.add(item.key);
      fresh++;
      if (steps.sponsored(card)) {
        sponsored++;
        continue;
      }
      items.push(item);
      if (items.length >= options.limit) {
        return done();
      }
    }
    stale = fresh === 0 ? stale + 1 : 0;
    if (lastPass || stale >= STALE_ROUNDS) {
      ended = true;
      return done();
    }
    if (scrolls >= options.maxScrolls) {
      return done();
    }
    scrolls++;
    lastPass = !(await page.scroll());
  }

  function done() {
    return { items, stats: { scrolls, seen: seen.size, sponsored, noImage, ended } };
  }
}

// toItem 把一张卡片变成一条；没有能用的图就是 undefined
function toItem(card: Element, steps: ListSteps): HarvestItem | undefined {
  const img = steps.image(card);
  const thumb = img ? displayedSource(img) : null;
  if (!img || !thumb) {
    return undefined;
  }
  const url = steps.link(card);
  return {
    key: steps.key(card) ?? url ?? thumb,
    url,
    title: steps.title(card) ?? "",
    thumb,
    original: largestSource(img) ?? thumb,
    width: img.naturalWidth || undefined,
    height: img.naturalHeight || undefined
  };
}

// largestImg 是卡片里显示得最大的那张图。没有布局的时候（测试里）面积都是 0，取第一张
function largestImg(card: Element): HTMLImageElement | undefined {
  const images = [...(card instanceof HTMLImageElement ? [card] : []), ...card.querySelectorAll("img")];
  let best = images[0];
  let bestArea = 0;
  for (const img of images) {
    const rect = img.getBoundingClientRect();
    if (rect.width * rect.height > bestArea) {
      best = img;
      bestArea = rect.width * rect.height;
    }
  }
  return best;
}

function httpHref(href: string | undefined, base: string): string | undefined {
  if (!href) {
    return undefined;
  }
  try {
    const url = new URL(href, base);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : undefined;
  } catch {
    return undefined;
  }
}
