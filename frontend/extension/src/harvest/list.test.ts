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
import { afterEach, describe, expect, it } from "vitest";

import { StepFailures, withFallback } from "../strategies";
import { genericList, harvestList, type HarvestPage } from "./list";

afterEach(() => {
  document.body.innerHTML = "";
});

// card 是一张测试卡片：链接、标题、缩略图和 srcset 里的原图。ad 为 true 时文字里带"广告"
function card(id: number, opts: { ad?: boolean; noImage?: boolean } = {}): string {
  const img = opts.noImage
    ? ""
    : `<img src="https://img.test/236x/${id}.jpg" srcset="https://img.test/236x/${id}.jpg 1x, https://img.test/originals/${id}.jpg 4x">`;
  return `<div class="card"><a href="/pin/${id}/" aria-label="卡片 ${id}">${img}</a>${opts.ad ? "<span>广告</span>" : ""}</div>`;
}

// 测试用的步骤：卡片是 .card，文字里有"广告"的是广告；其余用通用实现
const steps = withFallback(
  "list",
  {
    cards: (doc: Document) => Array.from(doc.querySelectorAll(".card")),
    sponsored: (el: Element) => (el.textContent ?? "").includes("广告")
  },
  genericList,
  new StepFailures()
);

// fakeFeed 模仿瀑布流：每滚一屏，上一屏的卡片被删掉、换成下一批；批次用完就滚不动了
function fakeFeed(batches: string[][]): HarvestPage & { scrolled: number } {
  let at = 0;
  document.body.innerHTML = batches[0].join("");
  return {
    scrolled: 0,
    async scroll() {
      if (at + 1 >= batches.length) {
        return false;
      }
      at++;
      this.scrolled++;
      document.body.innerHTML = batches[at].join("");
      return true;
    },
    async settle() {}
  };
}

describe("harvestList", () => {
  it("边滚边收：滚上滚下重复出现的只收一次，广告不收，链接、标题、原图都带上", async () => {
    // Arrange：第二屏里还留着第一屏的最后一张
    const page = fakeFeed([
      [card(1), card(2, { ad: true }), card(3)],
      [card(3), card(4), card(5)]
    ]);

    // Act
    const { items, stats } = await harvestList(document, steps, { limit: 10, maxScrolls: 5 }, page);

    // Assert
    expect(items.map((i) => i.title)).toEqual(["卡片 1", "卡片 3", "卡片 4", "卡片 5"]);
    expect(items[0]).toMatchObject({
      key: `${location.origin}/pin/1/`,
      url: `${location.origin}/pin/1/`,
      thumb: "https://img.test/236x/1.jpg",
      original: "https://img.test/originals/1.jpg"
    });
    expect(stats).toMatchObject({ seen: 5, sponsored: 1, noImage: 0, ended: true });
  });

  it("收够 limit 张就停，不再往下滚", async () => {
    // Arrange
    const page = fakeFeed([[card(1), card(2), card(3)], [card(4)]]);

    // Act
    const { items, stats } = await harvestList(document, steps, { limit: 2, maxScrolls: 5 }, page);

    // Assert
    expect(items).toHaveLength(2);
    expect(page.scrolled).toBe(0);
    expect(stats.ended).toBe(false);
  });

  it("滚满 maxScrolls 屏就停，这时不算到底", async () => {
    // Arrange
    const page = fakeFeed([[card(1)], [card(2)], [card(3)], [card(4)]]);

    // Act
    const { items, stats } = await harvestList(document, steps, { limit: 10, maxScrolls: 2 }, page);

    // Assert
    expect(items.map((i) => i.title)).toEqual(["卡片 1", "卡片 2", "卡片 3"]);
    expect(stats).toMatchObject({ scrolls: 2, ended: false });
  });

  it("连着几屏都没有新卡片，当作到底了", async () => {
    // Arrange：滚得动，但内容一直不变（加载卡住了）
    const page = fakeFeed([[card(1)], [card(1)], [card(1)], [card(1)], [card(1)]]);

    // Act
    const { items, stats } = await harvestList(document, steps, { limit: 10, maxScrolls: 10 }, page);

    // Assert
    expect(items).toHaveLength(1);
    expect(stats.ended).toBe(true);
    expect(stats.scrolls).toBeLessThan(10);
  });

  it("没有图的卡片不收（图还没加载、或者是视频），记在 noImage 里", async () => {
    // Arrange
    const page = fakeFeed([[card(1), card(2, { noImage: true })]]);

    // Act
    const { items, stats } = await harvestList(document, steps, { limit: 10, maxScrolls: 0 }, page);

    // Assert
    expect(items.map((i) => i.title)).toEqual(["卡片 1"]);
    expect(stats.noImage).toBe(1);
  });

  it("一张卡片都认不出来时（通用 cards 还没写）照常结束，什么都不收", async () => {
    // Arrange
    const page = fakeFeed([[card(1)]]);

    // Act
    const { items, stats } = await harvestList(document, genericList, { limit: 10, maxScrolls: 3 }, page);

    // Assert
    expect(items).toEqual([]);
    expect(stats.seen).toBe(0);
  });
});

describe("genericList", () => {
  it("链接用卡片里的 <a>，相对地址补全，javascript: 这类不要", () => {
    // Arrange
    document.body.innerHTML = `<div id="a"><a href="/p/1">x</a></div><div id="b"><a href="javascript:alert(1)">y</a></div>`;

    // Act & Assert
    expect(genericList.link(document.getElementById("a")!)).toBe(`${location.origin}/p/1`);
    expect(genericList.link(document.getElementById("b")!)).toBeUndefined();
  });

  it("标题依次用链接的 aria-label、图片的 alt、卡片的文字", () => {
    // Arrange
    document.body.innerHTML = `<div id="a"><a href="/1" aria-label="标签">文字</a></div>
      <div id="b"><img alt="图说明"><span>文字</span></div>
      <div id="c"><span>  卡片   文字 </span></div>`;

    // Act & Assert
    expect(genericList.title(document.getElementById("a")!)).toBe("标签");
    expect(genericList.title(document.getElementById("b")!)).toBe("图说明");
    expect(genericList.title(document.getElementById("c")!)).toBe("卡片 文字");
  });
});
