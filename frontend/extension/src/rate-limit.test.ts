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

import { RATE_LIMIT, siteKey, SiteRateLimiter } from "./rate-limit";

let limiter: SiteRateLimiter;
let storage: Record<string, unknown>;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(100_000);
  storage = {};
  vi.stubGlobal("chrome", {
    storage: {
      session: {
        get: vi.fn(async (key: string) => ({ [key]: storage[key] })),
        set: vi.fn(async (patch: Record<string, unknown>) => {
          Object.assign(storage, patch);
        })
      }
    }
  });
  limiter = new SiteRateLimiter();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("站点操作限速", () => {
  it("按注册域名合并子域名，保留公共后缀和私有租户边界", () => {
    expect(siteKey("https://www.example.com/search?q=private")).toBe("example.com");
    expect(siteKey("https://shop.example.co.uk")).toBe("example.co.uk");
    expect(siteKey("https://one.github.io")).not.toBe(siteKey("https://two.github.io"));
    expect(siteKey("http://127.0.0.1:8080")).toBe("127.0.0.1");
    expect(siteKey("about:blank")).toBeUndefined();
  });

  it("不同标签页/子域名的操作共用最短间隔，其他站点可立即操作", async () => {
    const starts: number[] = [];
    const act = async () => {
      starts.push(Date.now());
    };
    const deadline = Date.now() + RATE_LIMIT.maxQueueWaitMs;
    await limiter.run("https://www.example.com", act, deadline);
    const next = limiter.run("https://shop.example.com", act, deadline);
    await limiter.run("https://example.org", act, deadline);
    expect(starts).toEqual([100_000, 100_000]);
    await vi.advanceTimersByTimeAsync(RATE_LIMIT.minIntervalMs - 1);
    expect(starts).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    await next;
    expect(starts).toEqual([100_000, 100_000, 103_000]);
  });

  it("同站点操作完成前不开始下一条，内部滚动仍能申请额度", async () => {
    let finish!: () => void;
    const first = limiter.run(
      "https://example.com",
      async () => {
        await limiter.acquire("https://example.com");
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
      },
      Date.now() + RATE_LIMIT.maxQueueWaitMs
    );
    await vi.advanceTimersByTimeAsync(RATE_LIMIT.minIntervalMs);
    const action = vi.fn(async () => {});
    const next = limiter.run("https://example.com", action, Date.now() + RATE_LIMIT.maxQueueWaitMs);
    await vi.advanceTimersByTimeAsync(1000);
    expect(action).not.toHaveBeenCalled();
    finish();
    await first;
    await vi.advanceTimersByTimeAsync(2000);
    await next;
    expect(action).toHaveBeenCalledOnce();
  });

  it("滚动窗口用尽立即拒绝；worker 重建不能清空额度，到期后才恢复", async () => {
    for (let i = 0; i < RATE_LIMIT.maxActions; i++) {
      await limiter.acquire("https://example.com");
      await vi.advanceTimersByTimeAsync(RATE_LIMIT.minIntervalMs);
    }
    const restarted = new SiteRateLimiter();
    const action = vi.fn(async () => {});
    await expect(restarted.run("https://example.com", action, Date.now() + 5000)).rejects.toMatchObject({
      code: "RATE_LIMITED"
    });
    expect(action).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
    await vi.advanceTimersByTimeAsync(30_000);
    await restarted.run("https://example.com", action, Date.now() + 5000);
    expect(action).toHaveBeenCalledOnce();
  });

  it("并发积压超过排队预算的操作被拒绝，以后也不会偷偷执行", async () => {
    const action = vi.fn(async () => {});
    const deadline = Date.now() + RATE_LIMIT.maxQueueWaitMs;
    const first = limiter.run("https://example.com", action, deadline);
    const second = limiter.run("https://example.com", action, deadline);
    const third = limiter.run("https://example.com", action, deadline);
    const rejected = expect(third).rejects.toMatchObject({ code: "RATE_LIMITED" });
    await vi.advanceTimersByTimeAsync(6000);
    await Promise.all([first, second, rejected]);
    expect(action).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(action).toHaveBeenCalledTimes(2);
  });

  it("执行失败仍计入访问次数，并且不堵塞后续命令", async () => {
    await expect(
      limiter.run(
        "https://example.com",
        async () => {
          throw new Error("failed");
        },
        Date.now() + 5000
      )
    ).rejects.toThrow("failed");
    const action = vi.fn(async () => {});
    const next = limiter.run("https://example.com", action, Date.now() + 5000);
    await vi.advanceTimersByTimeAsync(3000);
    await next;
    expect(action).toHaveBeenCalledOnce();
  });
});
