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
import { xiaohongshu } from "./xiaohongshu";

afterEach(() => {
  document.body.innerHTML = "";
});

// note 照 2026-10-21 用 inspect 看到的搜索页结构写；标题、昵称、id 都是编的
function note(id: string, title: string, query = "?xsec_token=ABtok%3D&xsec_source=pc_search"): string {
  return `<section class="note-item" data-width="1440" data-height="1920" data-index="0" data-note-id="${id}"><div>
    <a href="/explore/${id}" style="display: none"></a>
    <a class="cover mask" href="/search_result/${id}${query}"><img data-xhs-img src="https://sns-webpic.test/${id}!nc_n_webp_mw_1"></a>
    <div class="footer">
      <a class="title" href="/search_result/${id}${query}"><span>${title}</span></a>
      <div class="card-bottom-wrapper">
        <a class="author" href="/user/profile/u1"><img class="author-avatar" src="https://avatar.test/u1.jpg"><span class="name">某位作者</span></a>
        <span class="like-wrapper"><span class="count">682</span></span>
      </div>
    </div>
  </div></section>`;
}

// 「大家都在搜」：也是 section.note-item，但没有封面，id 不是笔记 id
const querySuggestions = `<section class="note-item" data-index="6" data-note-id="5091a9d4-30d9#179"><div>
  <div class="query-note-wrapper"><div>大家都在搜</div><div>奶油原木风</div><div>现代奶油风客厅</div></div>
</div></section>`;

describe("xiaohongshu 列表收割", () => {
  const steps = () => withFallback("list", xiaohongshu.list, genericList, new StepFailures());

  it("搜索页和发现流都认得", () => {
    expect(profileFor(new URL("https://www.xiaohongshu.com/search_result?keyword=奶油风"))?.name).toBe("xiaohongshu");
    expect(profileFor(new URL("https://www.xiaohongshu.com/explore"))?.name).toBe("xiaohongshu");
  });

  it("每篇笔记是一张卡片，「大家都在搜」不算", () => {
    // Arrange
    document.body.innerHTML = `<div class="feeds-container">${note("6a51b2720000000011011469", "奶油中古家")}${querySuggestions}${note("69b2cc6f0000000022023cd0", "别装成出租屋")}</div>`;

    // Act
    const cards = steps().cards(document);

    // Assert
    expect(cards.map((c) => c.getAttribute("data-note-id"))).toEqual([
      "6a51b2720000000011011469",
      "69b2cc6f0000000022023cd0"
    ]);
  });

  it("链接换成带 xsec_token 的 /explore/<id>，标题不带作者昵称，图是封面，去重用笔记 id", () => {
    // Arrange
    document.body.innerHTML = note("6a51b2720000000011011469", " 被问爆的奶油中古家 ");
    const s = steps();
    const [card] = s.cards(document);

    // Act + Assert
    expect(s.link(card)).toBe(
      "https://www.xiaohongshu.com/explore/6a51b2720000000011011469?xsec_token=ABtok%3D&xsec_source=pc_search"
    );
    expect(s.title(card)).toBe("被问爆的奶油中古家");
    expect(s.image(card)?.getAttribute("src")).toBe("https://sns-webpic.test/6a51b2720000000011011469!nc_n_webp_mw_1");
    expect(s.key(card)).toBe("xhs:6a51b2720000000011011469");
  });

  it("封面链接里没有 token 时也给出笔记页", () => {
    // Arrange
    document.body.innerHTML = note("69b2cc6f0000000022023cd0", "别装成出租屋", "");
    const s = steps();

    // Act
    const link = s.link(s.cards(document)[0]);

    // Assert
    expect(link).toBe("https://www.xiaohongshu.com/explore/69b2cc6f0000000022023cd0");
  });
});
