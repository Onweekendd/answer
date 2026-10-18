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

import { bringToFront, execute } from "./commands";
import type { Command } from "./protocol";

const remove = vi.fn();
const update = vi.fn();
const sendMessage = vi.fn();

beforeEach(() => {
  remove.mockReset();
  update.mockReset().mockResolvedValue({});
  sendMessage.mockReset();
  vi.stubGlobal("chrome", { tabs: { remove, update, sendMessage } });
});
afterEach(() => vi.unstubAllGlobals());

function close(tabId: number): Command {
  return { id: `close-${tabId}`, type: "command", cmd: "close", tabId, args: {} };
}

// 可以从外面决定什么时候完成的 promise
function deferred(): { promise: Promise<void>; resolve: () => void; reject: (err: Error) => void } {
  let resolve!: () => void;
  let reject!: (err: Error) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("execute 按标签页排队", () => {
  it("同一个标签页的第二条命令，等第一条做完才开始", async () => {
    // Arrange
    const first = deferred();
    remove.mockReturnValueOnce(first.promise).mockResolvedValueOnce(undefined);

    // Act
    const a = execute(close(1));
    const b = execute(close(1));
    await Promise.resolve();
    await Promise.resolve();

    // Assert
    expect(remove).toHaveBeenCalledTimes(1);
    first.resolve();
    await Promise.all([a, b]);
    expect(remove).toHaveBeenCalledTimes(2);
  });

  it("第一条失败了，第二条照样执行", async () => {
    // Arrange
    remove.mockRejectedValueOnce(new Error("No tab with id: 1")).mockResolvedValueOnce(undefined);

    // Act
    const a = execute(close(1));
    const b = execute(close(1));

    // Assert
    await expect(a).rejects.toMatchObject({ code: "TAB_NOT_FOUND" });
    await expect(b).resolves.toEqual({ closed: true });
  });

  it("不同标签页的命令互不等待", async () => {
    // Arrange
    const first = deferred();
    remove.mockReturnValueOnce(first.promise).mockResolvedValueOnce(undefined);

    // Act
    const a = execute(close(1));
    await expect(execute(close(2))).resolves.toEqual({ closed: true });

    // Assert
    first.resolve();
    await a;
  });
});

// content script 依次报告的可见状态，最后一个一直重复
function visibility(...states: string[]): void {
  for (const state of states) {
    sendMessage.mockResolvedValueOnce({ ok: true, data: state });
  }
  sendMessage.mockResolvedValue({ ok: true, data: states[states.length - 1] });
}

describe("bringToFront", () => {
  afterEach(() => vi.useRealTimers());

  it("切到前台，页面马上可见就返回", async () => {
    // Arrange
    visibility("visible");

    // Act
    await bringToFront(7);

    // Assert
    expect(update).toHaveBeenCalledWith(7, { active: true });
    expect(sendMessage).toHaveBeenCalledTimes(1);
  });

  it("切过去以后页面过一会儿才可见，等到可见再返回", async () => {
    // Arrange
    vi.useFakeTimers();
    visibility("hidden", "hidden", "visible");

    // Act
    const done = bringToFront(7);
    await vi.advanceTimersByTimeAsync(200);

    // Assert
    await expect(done).resolves.toBeUndefined();
    expect(sendMessage).toHaveBeenCalledTimes(3);
  });

  it("一直不可见（窗口最小化或被完全挡住）时报错，不假装成功", async () => {
    // Arrange
    vi.useFakeTimers();
    visibility("hidden");

    // Act
    const done = bringToFront(7);
    const assertion = expect(done).rejects.toMatchObject({ code: "EXEC_FAILED" });
    await vi.advanceTimersByTimeAsync(3000);

    // Assert
    await assertion;
  });

  it("标签页已经关了，报 TAB_NOT_FOUND", async () => {
    // Arrange
    update.mockRejectedValue(new Error("No tab with id: 7"));

    // Act + Assert
    await expect(bringToFront(7)).rejects.toMatchObject({ code: "TAB_NOT_FOUND" });
    expect(sendMessage).not.toHaveBeenCalled();
  });
});
