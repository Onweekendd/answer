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

import { describe, expect, it, vi } from "vitest";

import { waitForStable } from "./stable";

// 用真的计时器，时间都设得很短：MutationObserver 的回调是微任务，和假计时器混用容易出错
const OPTIONS = { quietMs: 50, maxMs: 400 };

// 每隔 everyMs 往 root 里加一个元素，加 times 次
function keepChanging(root: Element, everyMs: number, times: number): void {
  let n = 0;
  const timer = setInterval(() => {
    root.appendChild(document.createElement("div"));
    n += 1;
    if (n >= times) {
      clearInterval(timer);
    }
  }, everyMs);
}

describe("waitForStable", () => {
  it("页面一直没变化，等 quietMs 就返回 stable", async () => {
    // Arrange
    document.body.innerHTML = "<main></main>";

    // Act
    const result = await waitForStable(document.body, OPTIONS);

    // Assert
    expect(result.stable).toBe(true);
    expect(result.waitedMs).toBeGreaterThanOrEqual(40);
    expect(result.waitedMs).toBeLessThan(200);
  });

  it("页面一直在变，每次变化都重新计时，变完以后再等 quietMs", async () => {
    // Arrange：前 150ms 每 20ms 变一次，之后不再变
    document.body.innerHTML = "<main></main>";
    keepChanging(document.body, 20, 7);

    // Act
    const result = await waitForStable(document.body, OPTIONS);

    // Assert：至少要等到最后一次变化（约 140ms）之后再加 quietMs
    expect(result.stable).toBe(true);
    expect(result.waitedMs).toBeGreaterThanOrEqual(170);
  });

  it("页面永远在变（比如轮播图），等满 maxMs 返回 stable=false，不报错", async () => {
    // Arrange：比 quietMs 更频繁地变化，一直变到超过 maxMs
    document.body.innerHTML = "<main></main>";
    keepChanging(document.body, 20, 40);

    // Act
    const result = await waitForStable(document.body, OPTIONS);

    // Assert
    expect(result.stable).toBe(false);
    expect(result.waitedMs).toBeGreaterThanOrEqual(380);
    expect(result.waitedMs).toBeLessThan(700);
  });

  it("返回以前会停止观察（disconnect），不留下观察器", async () => {
    // Arrange
    document.body.innerHTML = "<main></main>";
    const disconnect = vi.spyOn(MutationObserver.prototype, "disconnect");

    // Act
    await waitForStable(document.body, OPTIONS);

    // Assert
    expect(disconnect).toHaveBeenCalled();
    disconnect.mockRestore();
  });
});
