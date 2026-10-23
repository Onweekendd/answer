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

// 调试用：看一段文字所在的元素在 DOM 里的位置，写站点策略时用，不用每次请人打开 DevTools 截图。
// 只有 Go 的 HTTP API（/api/browser/call）会发这个命令，Agent 的工具里没有它。
// 输出标签、id、class、role 和 data-* 属性，文字最多 TEXT_MAX 个字：页面上的昵称、地址不该整段流出来

const TEXT_MAX = 20;
const ATTR_MAX = 40;
const CHAIN_MAX = 14; // 往上最多几层
const OUTLINE_DEPTH = 3; // 展开子树时最多几层
const OUTLINE_MAX = 80; // 展开子树时最多几行

export interface InspectResult {
  found: number; // 一共有几处文字对得上
  chains: string[][]; // 前几处，每处从元素本身往上到 body
  outline: string[]; // up 不为空时：第一处往上 up 层那个元素的子树
}

export function inspect(doc: Document, text: string, up?: number, maxMatches = 3): InspectResult {
  const owners: Element[] = [];
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const parent = node.parentElement;
    if (parent && node.textContent?.includes(text) && !owners.includes(parent)) {
      owners.push(parent);
    }
  }
  // 文字也可能在属性里（title、alt、placeholder）
  for (const el of Array.from(doc.querySelectorAll("[title], [alt], [placeholder], [aria-label]"))) {
    const attrs = ["title", "alt", "placeholder", "aria-label"].map((a) => el.getAttribute(a) ?? "");
    if (attrs.some((v) => v.includes(text)) && !owners.includes(el)) {
      owners.push(el);
    }
  }

  const chains = owners.slice(0, maxMatches).map((el) => {
    const chain: string[] = [];
    for (let cur: Element | null = el; cur && cur !== doc.body && chain.length < CHAIN_MAX; cur = cur.parentElement) {
      chain.push(describeNode(cur));
    }
    return chain;
  });

  const outline: string[] = [];
  if (up !== undefined && owners[0]) {
    let root: Element = owners[0];
    for (let i = 0; i < up && root.parentElement && root.parentElement !== doc.body; i++) {
      root = root.parentElement;
    }
    walk(root, 0, outline);
  }
  return { found: owners.length, chains, outline };
}

function walk(el: Element, depth: number, out: string[]) {
  if (out.length >= OUTLINE_MAX) {
    return;
  }
  const own = Array.from(el.childNodes)
    .filter((n) => n.nodeType === Node.TEXT_NODE)
    .map((n) => n.textContent ?? "")
    .join("")
    .replace(/\s+/g, " ")
    .trim();
  out.push(`${"  ".repeat(depth)}${describeNode(el)}${own ? ` "${own.slice(0, TEXT_MAX)}"` : ""}`);
  if (depth >= OUTLINE_DEPTH) {
    if (el.children.length > 0) {
      out.push(`${"  ".repeat(depth + 1)}…${el.children.length} 个子元素`);
    }
    return;
  }
  for (const child of Array.from(el.children)) {
    if (["SCRIPT", "STYLE", "SVG", "PATH", "svg", "path"].includes(child.tagName)) {
      continue;
    }
    walk(child, depth + 1, out);
  }
}

// describeNode 是一个元素的样子：div#id.class1.class2[role=x][data-sku=1]
function describeNode(el: Element): string {
  const tag = el.tagName.toLowerCase();
  const id = el.id ? `#${el.id.slice(0, ATTR_MAX)}` : "";
  const classes = Array.from(el.classList)
    .slice(0, 5)
    .map((c) => `.${c.slice(0, ATTR_MAX)}`)
    .join("");
  const attrs = Array.from(el.attributes)
    .filter((a) => a.name === "role" || a.name === "href" || a.name.startsWith("data-") || a.name.startsWith("aria-"))
    .slice(0, 6)
    .map((a) => `[${a.name}=${a.value.slice(0, ATTR_MAX)}]`)
    .join("");
  return `${tag}${id}${classes}${attrs}`;
}
