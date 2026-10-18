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

import { CommandError, type TabRequest, type TabResponse, toErrorBody } from "./protocol";
import { displayUrl } from "./snapshot/links";
import { maskPrivate } from "./snapshot/privacy";
import { waitForStable } from "./snapshot/stable";
import { clip } from "./snapshot/text";

// content script：Go 第一次要操作某个页面时，由 background 注入进去（commands.ts 的 inject），
// 和网页跑在同一个渲染进程，能读写 DOM，但看不到网页自己的 JS 变量（isolated world）。
// 页面一跳转它就被销毁，下一条命令会再注入一份，所以这里的变量只在当前页面有效。

// 上一次 snapshot 编过号的元素。click(12) 就是点 elements[12]，
// 所以编号只在"同一个页面、同一次快照之后"有意义。
let elements: Element[] = [];

chrome.runtime.onMessage.addListener((req: TabRequest, _sender, sendResponse) => {
  handle(req).then(
    (data) => sendResponse({ ok: true, data } satisfies TabResponse),
    (err) => sendResponse({ ok: false, error: toErrorBody(err) } satisfies TabResponse)
  );
  return true; // 告诉 Chrome：我会异步回复，先别关掉这条消息通道
});

async function handle(req: TabRequest): Promise<unknown> {
  switch (req.type) {
    case "snapshot": {
      // 先等页面安静下来再看：有的网站 complete 以后才用 JS 把内容渲染出来
      const settle = await waitForStable(document.body);
      const result = snapshot(req.maxChars);
      result.stats.waitedMs = Math.round(settle.waitedMs);
      result.stats.stable = settle.stable;
      return result;
    }
    case "prepareInput":
      return prepareInput(req.index, req.editable);
    case "selectInput": {
      const el = editableAt(req.index);
      // 在 shadow DOM 里的输入框，document.activeElement 是外面的宿主元素，要问它自己所在的那棵树
      if ((el.getRootNode() as Document | ShadowRoot).activeElement !== el) {
        throw new CommandError("EXEC_FAILED", "输入框没有获得焦点，请重新 snapshot 后重试");
      }
      el.select();
      return { ok: true };
    }
    case "visibility":
      return document.visibilityState;
    case "viewport":
      return { width: window.innerWidth, height: window.innerHeight };
    case "scrollPosition":
      return scrollPages();
  }
}

// 压缩前后的数字：调规则时看效果，也是面试时的硬数据。只给 Go 记录，不一定发给模型
interface SnapshotStats {
  candidates: number; // 选择器选出来的元素
  hidden: number; // 其中看不见、被过滤掉的
  merged: number; // 看得见，但被外层合并掉的
  visible: number; // 看得见、并且没被父元素合并掉的
  numbered: number; // 实际输出了几行
  links: number; // 其中几行是链接
  pagesAbove: number; // 当前位置上面还有几屏
  pagesBelow: number; // 下面还有几屏
  rawHtmlChars: number; // 原始 HTML 有多长
  outputChars: number; // 压缩后有多长
  ms: number; // 快照花了多少毫秒（不含算 rawHtmlChars）
  waitedMs?: number; // 快照前等页面安静等了多久
  stable?: boolean; // 等到安静了没有；false 说明页面一直在变（轮播图）或者还在加载
}

interface Snapshot {
  url: string;
  title: string;
  elements: string; // "[0]<input name=q placeholder=搜索>\n[1]<button>搜索"
  truncated: boolean;
  stats: SnapshotStats;
}

const INTERACTIVE = "a, button, input, select, textarea, [role=button]";
const MAX_TEXT = 120; // 每个元素的文字最多几个字：商品卡片的文字比普通按钮长
const MAX_ATTR = 60; // 每个属性值最多几个字

function snapshot(maxChars = 8000): Snapshot {
  const started = performance.now();

  // 先过滤、再编号：编号就是 elements 的下标，click(i) 靠它找回元素
  const candidates = Array.from(document.querySelectorAll(INTERACTIVE));
  const shown = candidates.filter(isVisible);
  const visible = shown.filter((el) => !insideNumberedParent(el));

  // 每个元素转成一行，总长度超过 maxChars 就停
  const lines: string[] = [];
  let length = 0;
  let truncated = false;
  for (const [i, el] of visible.entries()) {
    const line = describe(i, el);
    if (length + line.length + 1 > maxChars) {
      truncated = true;
      break;
    }
    lines.push(line);
    length += line.length + 1; // +1 是换行符
  }
  // 模型没看到的编号，也不应该能点
  elements = visible.slice(0, lines.length);

  const output = lines.join("\n");
  const ms = Math.round(performance.now() - started);
  return {
    url: displayUrl(location.href), // 给模型看的；Go 要原始地址可以问 background（chrome.tabs.get）
    title: document.title,
    elements: output,
    truncated,

    stats: {
      candidates: candidates.length,
      hidden: candidates.length - shown.length,
      merged: shown.length - visible.length,
      visible: visible.length,
      numbered: lines.length,
      links: elements.filter((el) => el instanceof HTMLAnchorElement).length,
      ...scrollPages(),
      rawHtmlChars: document.documentElement.outerHTML.length, // 要把整页序列化一遍，比较慢，所以放在计时之后
      outputChars: output.length,
      ms
    }
  };
}

// 宽高都是 0 的元素（display:none、折叠起来的菜单）用户看不见，也点不了
function isVisible(el: Element): boolean {
  const rect = el.getBoundingClientRect();
  return rect.width > 0 && rect.height > 0;
}

// ---------- 精华：一个可操作对象一行（你来写） ----------

// 会把里面的文字"并进自己"的元素。商品卡片的 <a> 里还套着店铺的 <a>，
// 外层的 innerText 已经包含了店铺名，里面那个再单独输出一行就重复了
const PROPAGATING = "a, button, [role=button]";

function insideNumberedParent(el: Element): boolean {
  // 被合并的元素不单独编号，它的文字借外层的 innerText 出现一次。
  // 只有外层自己会被编号（看得见）时才合并，否则两个都不出现，元素就彻底消失了。
  // 合并的代价：点不到子元素；外层文字超过 MAX_TEXT 时子元素的文字会被截掉；子元素的属性（aria-label 等）会丢。
  // 已知没处理的情况：a1（看得见）> a2（看不见）> el 时，el 会单独编号，文字和 a1 那一行重复。

  // 输入框永远单独编号，要能往里输入
  if (el.matches("input, select, textarea")) {
    return false;
  }

  const owner = el.parentElement?.closest(PROPAGATING);
  if (!owner) {
    return false; // 没有这样的祖先，单独编号
  }

  return isVisible(owner);
}

// ---------- 一个元素变成一行 ----------
//   [0]<input name=q placeholder=请输入搜索文字 value=iphone17>
//   [5]<a id=item_id_980696242744>【晒单返100元】Apple/苹果 iPhone 17 手机 A19 芯片… 5499 国补后 3000+人付款
// 链接不输出 href：模型通过编号操作元素，用不着地址（browser-use 也不放 href）

function describe(i: number, el: Element): string {
  const tag = el.tagName.toLowerCase();
  // innerText 包含所有子孙的文字；clip 把其中的换行压成空格，卡片就拼成了一行
  const text = clip((el as HTMLElement).innerText ?? "", MAX_TEXT) || imageAlt(el);
  const attrs = attributesOf(el, text);
  // 唯一出口：整行一起打码，属性和文字都覆盖到（第 7 站坑 6）
  return maskPrivate(`[${i}]<${[tag, ...attrs].join(" ")}>${text}`);
}

// 属性白名单，顺序就是输出顺序。没有 class、style、href、src。
// 和 browser-use 一样保留 id：很多网站会在 id 里写有意义的名字（比如商品编号）
const ATTRS = ["id", "name", "placeholder", "aria-label", "title"];

function attributesOf(el: Element, text: string): string[] {
  const out: string[] = [];

  if (el instanceof HTMLInputElement) {
    if (el.type !== "text") {
      out.push(`type=${el.type}`);
    }
    // 读 el.value（property）而不是 getAttribute('value')（attribute）：用户输入以后后者不会变。
    // 密码框的值永远不输出
    if (el.type !== "password" && el.value) {
      out.push(`value=${clip(el.value, MAX_ATTR)}`);
    }
  }

  for (const name of ATTRS) {
    const value = clip(el.getAttribute(name) ?? "", MAX_ATTR);
    // 空的不输出；和文字一样的也不输出（<button aria-label=搜索>搜索，"搜索"只说一遍）
    if (value && value !== text) {
      out.push(`${name}=${value}`);
    }
  }
  return out;
}

// 只有图片没有字的链接（商品图、图标），用图片的 alt 当文字。
// 不输出图片地址：browser-use 在淘宝上光图片文件名就占了一半预算（第 7 站 7.5）
function imageAlt(el: Element): string {
  return clip(el.querySelector("img[alt]")?.getAttribute("alt") ?? "", MAX_TEXT);
}

// ---------- 滚动位置 ----------

// 一"屏" = 视口高度。告诉模型上下还有多少内容，它才知道要不要滚
function scrollPages(): { pagesAbove: number; pagesBelow: number } {
  const screen = window.innerHeight || 1;
  const below = document.documentElement.scrollHeight - window.scrollY - screen;
  const round = (x: number) => Math.round(x * 10) / 10;
  return {
    pagesAbove: round(window.scrollY / screen),
    pagesBelow: round(Math.max(0, below) / screen) // 缩放、滚动条可能让它算出一点负数
  };
}

function elementAt(index: number): Element {
  const el = elements[index];
  if (!el) {
    throw new CommandError("ELEMENT_NOT_FOUND", `编号 ${index} 不存在，先重新 snapshot`);
  }
  return el;
}

// content script 负责定位和选择文字；实际输入由后台通过 CDP 执行。
function editableAt(index: number): HTMLInputElement | HTMLTextAreaElement {
  const el = elementAt(index);
  if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) || el.disabled || el.readOnly) {
    throw new CommandError("NOT_EDITABLE", `编号 ${index} 不是可编辑的输入框`);
  }
  if (el instanceof HTMLInputElement && !["text", "search", "tel", "url", "email", "password"].includes(el.type)) {
    throw new CommandError("NOT_EDITABLE", `暂不支持 ${el.type} 类型的输入框`);
  }
  return el;
}

function prepareInput(index: number, editable: boolean): { x: number; y: number } {
  const el = editable ? editableAt(index) : elementAt(index);
  if (!el.isConnected || el.matches(":disabled")) {
    throw new CommandError("ELEMENT_NOT_FOUND", "元素已经失效或被禁用，请重新 snapshot");
  }
  el.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
  const rect = el.getBoundingClientRect();
  const left = Math.max(0, rect.left);
  const right = Math.min(window.innerWidth, rect.right);
  const top = Math.max(0, rect.top);
  const bottom = Math.min(window.innerHeight, rect.bottom);
  if (right <= left || bottom <= top) {
    throw new CommandError("EXEC_FAILED", "元素不在可点击区域，请重新 snapshot");
  }
  const x = (left + right) / 2;
  const y = (top + bottom) / 2;
  const hit = document.elementFromPoint(x, y);
  if (!hit || !el.contains(hit)) {
    throw new CommandError("EXEC_FAILED", "元素被遮挡，请先处理页面弹窗并重新 snapshot");
  }
  return { x, y };
}

Object.assign(globalThis, { __pagehand: { snapshot } });
