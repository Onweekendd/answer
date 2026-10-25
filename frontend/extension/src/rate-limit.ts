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

import { getDomain } from "tldts";

import { CommandError } from "./protocol";

// 统一限制站点操作；快照等纯读取不占额度。不是网站的安全阈值，可按实测继续调低。
export const RATE_LIMIT = {
  minIntervalMs: 3000,
  windowMs: 60_000,
  maxActions: 10,
  maxQueueWaitMs: 5000
} as const;

export function siteKey(rawUrl: string): string | undefined {
  const url = new URL(rawUrl);
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    return undefined;
  }
  // PSL 区分 example.co.uk 等后缀，也避免把不同 github.io 租户合并。
  return getDomain(url.hostname, { allowPrivateDomains: true }) ?? url.hostname;
}

function limited(waitMs: number): CommandError {
  return new CommandError(
    "RATE_LIMITED",
    `该站点操作过于频繁，请至少等待 ${Math.max(1, Math.ceil(waitMs / 1000))} 秒后再试；后续操作已停止`
  );
}

export class SiteRateLimiter {
  private commands = new Map<string, Promise<unknown>>();
  private reservations = new Map<string, Promise<unknown>>();

  // 保持同站点命令串行。额度锁单独管理，harvest 内部滚动也可以申请，不会死锁。
  async run<T>(url: string, action: () => Promise<T>, deadline: number): Promise<T> {
    const site = siteKey(url);
    if (!site) {
      return action();
    }
    return this.queued(this.commands, site, async () => {
      await this.acquire(url, deadline);
      return action();
    });
  }

  async acquire(url: string, deadline = Date.now() + RATE_LIMIT.maxQueueWaitMs): Promise<void> {
    const site = siteKey(url);
    if (!site) {
      return;
    }
    await this.queued(this.reservations, site, async () => {
      // 每个站点独立存储，避免不同站点并发写入时覆盖彼此；worker 重启后额度仍在。
      const key = `rateLimit:${site}`;
      const stored = (await chrome.storage.session.get(key))[key] as unknown;
      let now = Date.now();
      const history = (Array.isArray(stored) ? stored : [])
        .filter(
          (at): at is number =>
            typeof at === "number" && Number.isFinite(at) && at > now - RATE_LIMIT.windowMs && at <= now
        )
        .sort((a, b) => a - b);
      const wait = Math.max(0, (history.at(-1) ?? -Infinity) + RATE_LIMIT.minIntervalMs - now);
      if (history.length >= RATE_LIMIT.maxActions) {
        throw limited(Math.max(wait, history[0] + RATE_LIMIT.windowMs - now));
      }
      // 不挂起长时间冷却；排队预算从收到命令时开始计算，过期后不再开始操作。
      if (now + wait > deadline) {
        throw limited(Math.max(wait, RATE_LIMIT.minIntervalMs));
      }
      if (wait > 0) {
        await new Promise((resolve) => setTimeout(resolve, wait));
      }
      now = Date.now();
      if (now > deadline) {
        throw limited(RATE_LIMIT.minIntervalMs);
      }
      await chrome.storage.session.set({ [key]: [...history.filter((at) => at > now - RATE_LIMIT.windowMs), now] });
    });
  }

  private async queued<T>(queues: Map<string, Promise<unknown>>, site: string, action: () => Promise<T>): Promise<T> {
    const previous = queues.get(site) ?? Promise.resolve();
    const next = previous.catch(() => {}).then(action);
    queues.set(site, next);
    try {
      return await next;
    } finally {
      if (queues.get(site) === next) {
        queues.delete(site);
      }
    }
  }
}

export const siteRateLimiter = new SiteRateLimiter();
