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

import { alibabaDetail, alibabaSearch } from "./alibaba1688";
import { goofishItem, goofishSearch } from "./goofish";
import { jd, jdItem } from "./jd";
import { taobao, taobaoItem } from "./taobao";
import type { SiteStrategy } from "./types";
import { xiaohongshu } from "./xiaohongshu";

const strategies: readonly SiteStrategy[] = [
  taobao,
  taobaoItem,
  xiaohongshu,
  jd,
  jdItem,
  alibabaSearch,
  alibabaDetail,
  goofishSearch,
  goofishItem
];

export function collectStrategyElements(
  url: URL,
  doc: Document,
  registry = strategies
): {
  elements: Element[];
  labels: Map<Element, string>;
  links: Map<Element, string>;
  stats: { strategy?: string; strategyMatched?: number; strategyError?: string };
} {
  for (const strategy of registry) {
    try {
      if (!strategy.matches(url)) {
        continue;
      }
      const elements = (strategy.interactive?.(doc) ?? []).filter((el) => el.ownerDocument === doc && el.isConnected);
      const labels = new Map<Element, string>();
      const links = new Map<Element, string>();
      for (const el of elements) {
        const label = strategy.label?.(el);
        if (label) {
          labels.set(el, label);
        }
        const link = strategy.link?.(el);
        if (link) {
          links.set(el, link);
        }
      }
      return { elements, labels, links, stats: { strategy: strategy.name, strategyMatched: elements.length } };
    } catch (err) {
      // 选择器失效或策略异常不应影响通用快照。
      return {
        elements: [],
        labels: new Map(),
        links: new Map(),
        stats: { strategy: strategy.name, strategyMatched: 0, strategyError: String(err) }
      };
    }
  }
  return { elements: [], labels: new Map(), links: new Map(), stats: {} };
}

// strategyMain 是匹配到的策略给的正文区，页面上找不到的那几块去掉；没有匹配的策略、策略没给、或者策略出错，都返回空数组
export function strategyMain(url: URL, doc: Document, registry = strategies): Element[] {
  const strategy = registry.find((s) => s.matches(url));
  try {
    return (strategy?.main?.(doc) ?? []).filter((el): el is Element => el !== null);
  } catch {
    return [];
  }
}
