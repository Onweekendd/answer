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

import { collectStrategyElements, profileFor, strategyMain, strategyReady } from "./index";

afterEach(() => {
  document.body.innerHTML = "";
});

// page 照 2026-10-21 用 inspect 看到的结构写（类名是构建产物，留几个意思一下）
const page = `
  <div class="flex"><button>全网</button><button>文库</button><button>学术</button></div>
  <div id="search-content-container-2112836423424745472">
    <div class="thinking-container"><button>思考了3.39s</button><div aria-label="来源5个">5</div></div>
    <div class="relative"><div class="MuiBox-root"><div class="markdown-body MuiBox-root">
      <p>国家层面并无统一的"官方瑜伽教练证书"。</p>
    </div></div></div>
    <div class="flex" data-testid="SearchResultGenerateButtonGroup.div.生成幻灯"><button>生成幻灯片</button><button>展示海报</button></div>
  </div>
  <div class="search-origin-list-box">
    <div class="search-origin-box"><div class="contentContainer"><div>
      <img src="https://favicon.test/sport.gov.cn.png"><div>关于举办健身瑜伽高级教练员培训班的函_国家体育总局</div>
    </div></div></div>
    <div class="search-origin-box"><div class="contentContainer"><div>
      <img src="https://favicon.test/zhihu.png"><div>如何判断瑜伽教练证书的真伪？</div>
    </div></div></div>
  </div>
  <div><button>深度研究</button><button>互动网页</button></div>`;

describe("metaso", () => {
  const url = new URL("https://metaso.cn/chat/2112835623076831233");

  it("认得秘塔，不认得别的", () => {
    expect(profileFor(url)?.name).toBe("metaso");
    expect(profileFor(new URL("https://metaso.cn.evil.test/chat/1"))).toBeUndefined();
  });

  it("read 只读回答正文和来源列表，按钮的字不混进来", () => {
    // Arrange
    document.body.innerHTML = page;

    // Act
    const roots = strategyMain(url, document);

    // Assert
    const text = roots.map((r) => r.textContent ?? "").join("\n");
    expect(roots).toHaveLength(2);
    expect(text).toContain("国家层面并无统一");
    expect(text).toContain("如何判断瑜伽教练证书的真伪？");
    expect(text).not.toContain("生成幻灯片");
    expect(text).not.toContain("全网");
  });

  it("回答写完了（下面出现了生成幻灯片那排按钮）才算好，还在写的时候 read 要等", () => {
    // Arrange：同一个页面，去掉那排按钮就是还在写
    document.body.innerHTML = page;
    const done = strategyReady(url, document);
    document.querySelector('[data-testid^="SearchResultGenerateButtonGroup"]')?.remove();

    // Act
    const writing = strategyReady(url, document);

    // Assert
    expect(done).toBe(true);
    expect(writing).toBe(false);
  });

  it("还没提问的页面没有回答，不用等；别的网站也不用等", () => {
    document.body.innerHTML = "<textarea placeholder='请输入您的问题'></textarea>";
    expect(strategyReady(new URL("https://metaso.cn/"), document)).toBe(true);
    expect(strategyReady(new URL("https://www.jd.com/"), document)).toBe(true);
  });

  it("来源编进快照，名字是「来源：标题」，模型能点开读原文", () => {
    // Arrange
    document.body.innerHTML = page;

    // Act
    const { elements, labels } = collectStrategyElements(url, document);

    // Assert
    expect(elements).toHaveLength(2);
    expect(labels.get(elements[0])).toBe("来源：关于举办健身瑜伽高级教练员培训班的函_国家体育总局");
    expect(labels.get(elements[1])).toBe("来源：如何判断瑜伽教练证书的真伪？");
  });
});
