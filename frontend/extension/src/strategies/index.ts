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

import { taobao } from "./taobao";
import type { SiteStrategy } from "./types";

const strategies: readonly SiteStrategy[] = [taobao];

export function collectStrategyElements(
  url: URL,
  doc: Document,
  registry = strategies
): {
  elements: Element[];
  stats: { strategy?: string; strategyMatched?: number; strategyError?: string };
} {
  for (const strategy of registry) {
    try {
      if (!strategy.matches(url)) {
        continue;
      }
      const elements = (strategy.interactive?.(doc) ?? []).filter((el) => el.ownerDocument === doc && el.isConnected);
      return { elements, stats: { strategy: strategy.name, strategyMatched: elements.length } };
    } catch (err) {
      // 选择器失效或策略异常不应影响通用快照。
      return { elements: [], stats: { strategy: strategy.name, strategyMatched: 0, strategyError: String(err) } };
    }
  }
  return { elements: [], stats: {} };
}
