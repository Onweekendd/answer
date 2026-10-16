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

import { clip } from "./text";

describe("clip", () => {
  it("把卡片里的换行拼成一行", () => {
    // Arrange
    const innerText = "Apple/苹果\niPhone\n17\n\n5499\n国补后";

    // Act
    const got = clip(innerText, 100);

    // Assert
    expect(got).toBe("Apple/苹果 iPhone 17 5499 国补后");
  });

  it("超长的截断并加省略号", () => {
    // Arrange
    const long = "一二三四五六七八九十";

    // Act
    const got = clip(long, 4);

    // Assert
    expect(got).toBe("一二三四…");
  });

  it("去掉图标字体的字符", () => {
    // Arrange：淘宝首页实测，"购物车"前面是 iconfont 的图标 U+E63D
    const innerText = " 购物车";

    // Act
    const got = clip(innerText, 100);

    // Assert
    expect(got).toBe("购物车");
  });

  it("价格的整数和小数拼回一起", () => {
    // Arrange：淘宝搜索页实测，整数和小数在两个元素里
    const innerText = "¥ 7679\n.04 300+人付款";

    // Act
    const got = clip(innerText, 100);

    // Assert
    expect(got).toBe("¥ 7679.04 300+人付款");
  });

  it("不超长的原样返回", () => {
    expect(clip("  搜索  ", 10)).toBe("搜索");
  });
});
