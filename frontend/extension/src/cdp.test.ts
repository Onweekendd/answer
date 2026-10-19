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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type * as CdpModuleNs from "./cdp";

type CdpModule = typeof CdpModuleNs;

const attach = vi.fn();
const detach = vi.fn();
const sendCommand = vi.fn();
let onDetach: ((source: chrome.debugger.Debuggee) => void) | undefined;
let cdp: CdpModule;

// 会话状态是模块级的，每个测试重新加载一份模块
beforeEach(async () => {
  vi.useFakeTimers();
  attach.mockReset().mockResolvedValue(undefined);
  detach.mockReset().mockResolvedValue(undefined);
  sendCommand.mockReset().mockResolvedValue(undefined);
  onDetach = undefined;
  vi.stubGlobal("chrome", {
    debugger: {
      attach,
      detach,
      sendCommand,
      onDetach: { addListener: (fn: typeof onDetach) => (onDetach = fn) }
    }
  });
  vi.resetModules();
  cdp = await import("./cdp");
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// 执行一个动作，同时把"挂上以后等页面高度定下来"的那段时间走掉
async function act<T>(tabId: number, run: () => Promise<T>): Promise<T> {
  const pending = cdp.withDebugger(tabId, run);
  pending.catch(() => {}); // 走时间的时候它可能已经失败了，由调用方去断言
  await vi.advanceTimersByTimeAsync(cdp.ATTACH_SETTLE_MS);
  return pending;
}

describe("withDebugger", () => {
  it("hover 只移动鼠标，随后 click 才按下和释放", async () => {
    const target = { tabId: 1 };
    await cdp.cdpHover(target, { x: 20, y: 30 });
    expect(sendCommand.mock.calls.map((call) => call[2].type)).toEqual(["mouseMoved"]);
    await cdp.cdpClick(target, { x: 20, y: 60 });
    expect(sendCommand.mock.calls.map((call) => call[2].type)).toEqual([
      "mouseMoved",
      "mouseMoved",
      "mousePressed",
      "mouseReleased"
    ]);
  });

  it("同一个标签页连续两个动作只挂上一次，中间不断开", async () => {
    // Act
    await act(1, async () => "a");
    await act(1, async () => "b");

    // Assert
    expect(attach).toHaveBeenCalledTimes(1);
    expect(detach).not.toHaveBeenCalled();
  });

  it("动作失败时原样抛出它的错误，会话保留", async () => {
    // Arrange
    const failure = new Error("input failed");

    // Act + Assert
    await expect(
      act(1, async () => {
        throw failure;
      })
    ).rejects.toBe(failure);
    expect(detach).not.toHaveBeenCalled();
  });

  it("挂不上（比如开着开发者工具）时不执行动作，也不去断开别人的会话", async () => {
    // Arrange
    attach.mockRejectedValue(new Error("Another debugger is already attached"));
    const run = vi.fn();

    // Act + Assert
    await expect(cdp.withDebugger(1, run)).rejects.toThrow("CDP attach failed");
    expect(run).not.toHaveBeenCalled();
    expect(detach).not.toHaveBeenCalled();
  });

  it("动作超时报 TIMEOUT 并断开会话，下一个动作重新挂上", async () => {
    // Arrange：第一个动作永远不完成，像后台标签页里等不到帧的滚动
    const hung = cdp.withDebugger(1, () => new Promise(() => {}));
    const assertion = expect(hung).rejects.toMatchObject({ code: "TIMEOUT" });

    // Act
    await vi.advanceTimersByTimeAsync(cdp.ATTACH_SETTLE_MS + cdp.ACTION_TIMEOUT_MS);

    // Assert
    await assertion;
    expect(detach).toHaveBeenCalledWith({ tabId: 1 });
    await act(1, async () => "next");
    expect(attach).toHaveBeenCalledTimes(2);
  });

  it("空闲满 IDLE_DETACH_MS 才断开；期间有新动作就重新计时", async () => {
    // Arrange
    await act(1, async () => "a");
    await vi.advanceTimersByTimeAsync(cdp.IDLE_DETACH_MS - 1);

    // Act：已经挂着，不用再等页面定下来
    await cdp.withDebugger(1, async () => "b");
    await vi.advanceTimersByTimeAsync(cdp.IDLE_DETACH_MS - 1);

    // Assert
    expect(detach).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(detach).toHaveBeenCalledWith({ tabId: 1 });
  });

  it("会话被浏览器断开以后（标签页关了、用户点了取消），下一个动作重新挂上", async () => {
    // Arrange
    await act(1, async () => "a");

    // Act
    onDetach?.({ tabId: 1 });
    await act(1, async () => "b");

    // Assert
    expect(attach).toHaveBeenCalledTimes(2);
  });
});

describe("cdpType", () => {
  it("submit 为 false 时只插入文字", async () => {
    // Act
    await cdp.cdpType({ tabId: 1 }, "搜索词", false);

    // Assert
    expect(sendCommand.mock.calls).toEqual([[{ tabId: 1 }, "Input.insertText", { text: "搜索词" }]]);
  });

  it("submit 为 true 时插入文字后只按一次回车，不再另外提交表单", async () => {
    // Act
    await cdp.cdpType({ tabId: 1 }, "搜索词", true);

    // Assert
    expect(sendCommand.mock.calls.map((call) => [call[1], call[2].type, call[2].key])).toEqual([
      ["Input.insertText", undefined, undefined],
      ["Input.dispatchKeyEvent", "keyDown", "Enter"],
      ["Input.dispatchKeyEvent", "keyUp", "Enter"]
    ]);
  });
});

describe("cdpScroll", () => {
  it("向下滚是负的 yDistance，一屏按 0.9 个视口高", async () => {
    // Act
    await cdp.cdpScroll({ tabId: 1 }, { width: 1000, height: 800 }, "down", 2);

    // Assert
    expect(sendCommand).toHaveBeenCalledWith(
      { tabId: 1 },
      "Input.synthesizeScrollGesture",
      expect.objectContaining({ x: 500, y: 400, yDistance: -1440 })
    );
  });

  it("屏数不合法时不发命令，报 BAD_MESSAGE", async () => {
    // Act + Assert
    await expect(cdp.cdpScroll({ tabId: 1 }, { width: 1000, height: 800 }, "down", 0)).rejects.toMatchObject({
      code: "BAD_MESSAGE"
    });
    expect(sendCommand).not.toHaveBeenCalled();
  });
});
