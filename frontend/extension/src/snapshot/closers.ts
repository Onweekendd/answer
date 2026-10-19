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

// 浮层的关闭按钮。不认任何具体网站：营销弹窗的关闭按钮多半是一个没有标签、没有 role 的 div 或 img
// （2026-10-19 京东的"限时补贴"、1688 首页的"秋冬上新95折"都是这样），快照里没有它，
// 模型点哪里都报"被遮挡"，按 Escape 也关不掉。这里在浮层里找长得像关闭按钮的东西，编进快照

// 属性里带这些字的：close-btn、icon-close、aria-label=关闭、img src=…/close.png
const CLOSE_ATTR = /close|关闭|dismiss|cancel/i;
const CLOSE_ATTRS = ["class", "id", "aria-label", "title", "alt", "src"];
// 文字就是这些的
const CLOSE_TEXT = new Set(["×", "✕", "✖", "╳", "X", "x", "关闭", "我知道了", "知道了", "暂不", "以后再说", "跳过"]);
// 图标按钮不会太大；文字按钮（"我知道了"）可以宽一点
const ICON_MAX_PX = 80;
const TEXT_MAX_W = 240;
const TEXT_MAX_H = 64;
const MAX_CLOSERS = 3;

export function findClosers(overlay: Element, visible: (el: Element) => boolean): Element[] {
  const found: Element[] = [];
  for (const el of Array.from(overlay.querySelectorAll("*"))) {
    if (found.length >= MAX_CLOSERS) {
      break;
    }
    // 外层已经算上了的，里面的图标不再算一次
    if (found.some((f) => f.contains(el)) || !visible(el)) {
      continue;
    }
    const rect = el.getBoundingClientRect();
    const text = ownText(el);
    const byText = CLOSE_TEXT.has(text) && rect.width <= TEXT_MAX_W && rect.height <= TEXT_MAX_H;
    const byAttr =
      rect.width <= ICON_MAX_PX &&
      rect.height <= ICON_MAX_PX &&
      CLOSE_ATTRS.some((a) => CLOSE_ATTR.test(el.getAttribute(a) ?? ""));
    if (byText || byAttr) {
      found.push(el);
    }
  }
  return found;
}

// ownText 是元素自己的文字，不含子元素的："×"按钮里常常套着一个 span
function ownText(el: Element): string {
  const own = Array.from(el.childNodes)
    .filter((n) => n.nodeType === Node.TEXT_NODE)
    .map((n) => n.textContent ?? "")
    .join("")
    .trim();
  return own || (el.children.length === 0 ? (el.textContent ?? "").trim() : "");
}
