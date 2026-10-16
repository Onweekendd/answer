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

import { describe, expect, it } from "vitest";

import { displayUrl } from "./links";

describe("displayUrl", () => {
  it("查询参数里的中文解码回来", () => {
    // Arrange：q=新型屋顶彩钢瓦 编码后的样子
    const href =
      "https://shop.example.com/search?q=%E6%96%B0%E5%9E%8B%E5%B1%8B%E9%A1%B6%E5%BD%A9%E9%92%A2%E7%93%A6&page=2";

    // Act
    const got = displayUrl(href);

    // Assert
    expect(got).toBe("shop.example.com/search?q=新型屋顶彩钢瓦&page=2");
  });

  it("路径里的中文解码回来", () => {
    expect(displayUrl("https://example.com/%E5%95%86%E5%93%81/42")).toBe("example.com/商品/42");
  });

  it("没有查询参数时不加问号", () => {
    expect(displayUrl("https://example.com/about")).toBe("example.com/about");
  });

  it("太长的截断", () => {
    // Arrange：一串很长的跟踪参数
    const href = `https://example.com/item?id=1&track=${"x".repeat(200)}`;

    // Act
    const got = displayUrl(href, 40);

    // Assert
    expect(got).toBe(`example.com/item?id=1&track=${"x".repeat(12)}…`);
  });

  it("百分号编码不完整时不抛异常", () => {
    // Arrange：%E6%96 只有一个汉字的前两个字节
    const href = "https://example.com/%E6%96?q=%E6%96";

    // Act & Assert
    expect(() => displayUrl(href)).not.toThrow();
  });

  it("不是合法 URL 时原样返回", () => {
    expect(displayUrl("not a url")).toBe("not a url");
  });
});
