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

import { maskPrivate } from "./privacy";

// 测试里的手机号、身份证号、邮箱都是编的
describe("maskPrivate", () => {
  it("手机号打码", () => {
    expect(maskPrivate("收货人 张三 13812345678")).toBe("收货人 张三 [手机号]");
  });

  it("更长的数字里的片段不当成手机号", () => {
    // Arrange：13 位和 12 位的编号，都包含 1 开头的 11 位数字片段
    const line = "[5]<a id=item_id_1030811149465>编号 980696242744";

    // Act
    const got = maskPrivate(line);

    // Assert
    expect(got).toBe(line);
  });

  it("身份证号打码，包括末位是 X 的", () => {
    expect(maskPrivate("证件 11010519491231002X")).toBe("证件 [身份证号]");
    expect(maskPrivate("证件 110105194912310021")).toBe("证件 [身份证号]");
  });

  it("邮箱打码", () => {
    expect(maskPrivate("联系 someone@example.com 谢谢")).toBe("联系 [邮箱] 谢谢");
  });

  it("普通商品文字不变", () => {
    const text = "Apple/苹果 iPhone 17 5499 国补后 3000+人付款";
    expect(maskPrivate(text)).toBe(text);
  });
});
