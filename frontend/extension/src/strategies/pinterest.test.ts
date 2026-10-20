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

import { genericList } from "../harvest/list";
import { profileFor, StepFailures, withFallback } from "./index";
import { pinterestSearch } from "./pinterest";

afterEach(() => {
  document.body.innerHTML = "";
});

// pin 照 2026-10-20 用 inspect 看到的结构写（类名是构建产物，留几个意思一下）
function pin(id: string, title: string, sponsor?: string): string {
  return `<div class="Masonry__Item_P" data-grid-item="true" role="listitem"><div data-grid-item-idx="0">
    <div data-test-id="pin" data-test-pin-id="${id}"><div role="group" aria-label="Pin 图卡片">
      <a href="/pin/${id}/" aria-label="${title}"><div class="PinCard__imageWrapper" data-test-image-signature="sig${id}">
        <img class="img_G" src="https://i.pinimg.com/236x/aa/${id}.jpg"
             srcset="https://i.pinimg.com/236x/aa/${id}.jpg 1x, https://i.pinimg.com/originals/aa/${id}.jpg 4x"></div></a>
      ${sponsor ? `<div><a href="/${sponsor}/">${sponsor}</a><div>赞助的 Pin 图</div></div>` : ""}
    </div></div>
  </div></div>`;
}

describe("pinterest-search", () => {
  it("只认搜索页", () => {
    expect(profileFor(new URL("https://www.pinterest.com/search/pins/?q=wedding"))?.name).toBe("pinterest-search");
    expect(profileFor(new URL("https://pinterest.com/search/pins/?q=wedding"))?.name).toBe("pinterest-search");
    expect(profileFor(new URL("https://www.pinterest.com/pin/1/"))).toBeUndefined();
    expect(profileFor(new URL("https://www.pinterest.com.evil.test/search/pins/"))).toBeUndefined();
  });

  it("瀑布流的每一格是一张卡片：链接补全、标题用 aria-label、去重用 pin id、广告认得出来", () => {
    // Arrange
    document.body.innerHTML = `<div role="list">${pin("814166438925594107", "Outdoor Reception")}${pin("42", "Lights", "Walmart")}</div>`;
    const steps = withFallback("list", pinterestSearch.list, genericList, new StepFailures());

    // Act
    const [first, ad] = steps.cards(document);

    // Assert
    expect(steps.cards(document)).toHaveLength(2);
    expect(steps.link(first)).toBe("https://www.pinterest.com/pin/814166438925594107/");
    expect(steps.title(first)).toBe("Outdoor Reception");
    expect(steps.key(first)).toBe("pin:814166438925594107");
    expect(steps.sponsored(first)).toBe(false);
    expect(steps.sponsored(ad)).toBe(true);
    // 图片用通用实现：卡片里唯一的 img
    expect(steps.image(first)?.getAttribute("src")).toBe("https://i.pinimg.com/236x/aa/814166438925594107.jpg");
  });
});
