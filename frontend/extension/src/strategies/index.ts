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
import { StepFailures, withFallback } from "./fallback";
import { goofishItem, goofishSearch } from "./goofish";
import { jd, jdItem } from "./jd";
import { pinterestSearch } from "./pinterest";
import type { PageProfile, ReadSteps, SnapshotSteps } from "./profile";
import { taobao, taobaoItem } from "./taobao";
import { xiaohongshu } from "./xiaohongshu";

export { StepFailures, withFallback } from "./fallback";
export type { ListSteps, PageProfile } from "./profile";

// 按顺序匹配，第一个认得这个网址的生效
const profiles: readonly PageProfile[] = [
  taobao,
  taobaoItem,
  xiaohongshu,
  jd,
  jdItem,
  alibabaSearch,
  alibabaDetail,
  goofishSearch,
  goofishItem,
  pinterestSearch
];

// profileFor 是认得这个网址的那类页面。matches 抛错当作不认得：一个写坏的增强不能挡住后面的
export function profileFor(url: URL, registry: readonly PageProfile[] = profiles): PageProfile | undefined {
  return registry.find((p) => {
    try {
      return p.matches(url);
    } catch {
      return false;
    }
  });
}

// 快照的通用实现：只靠内核自己的规则（content.ts 的 INTERACTIVE），不补元素，名字和链接照常
const genericSnapshot: SnapshotSteps = {
  candidates: () => [],
  label: () => undefined,
  link: () => undefined
};

// 读正文的通用实现：不指定，内核自己找 <main>，找不到读整页
const genericRead: ReadSteps = {
  main: () => []
};

export interface StrategyStats {
  strategy?: string;
  strategyMatched?: number;
  strategyError?: string;
}

export function collectStrategyElements(
  url: URL,
  doc: Document,
  registry: readonly PageProfile[] = profiles
): {
  elements: Element[];
  labels: Map<Element, string>;
  links: Map<Element, string>;
  stats: StrategyStats;
} {
  const labels = new Map<Element, string>();
  const links = new Map<Element, string>();
  const profile = profileFor(url, registry);
  if (!profile) {
    return { elements: [], labels, links, stats: {} };
  }
  const failures = new StepFailures();
  const steps = withFallback("snapshot", profile.snapshot, genericSnapshot, failures);
  const elements = steps.candidates(doc).filter((el) => el.ownerDocument === doc && el.isConnected);
  for (const el of elements) {
    const label = steps.label(el);
    if (label) {
      labels.set(el, label);
    }
    const link = steps.link(el);
    if (link) {
      links.set(el, link);
    }
  }
  return { elements, labels, links, stats: statsOf(profile, elements.length, failures) };
}

// strategyMain 是匹配到的那类页面给的正文区，页面上找不到的那几块去掉；没有匹配、没给、或者出错，都返回空数组
export function strategyMain(url: URL, doc: Document, registry: readonly PageProfile[] = profiles): Element[] {
  const profile = profileFor(url, registry);
  const steps = withFallback("read", profile?.read, genericRead, new StepFailures());
  return steps.main(doc).filter((el): el is Element => el !== null);
}

export function statsOf(profile: PageProfile, matched: number, failures: StepFailures): StrategyStats {
  return {
    strategy: profile.name,
    strategyMatched: matched,
    ...(failures.size > 0 ? { strategyError: failures.toString() } : {})
  };
}
