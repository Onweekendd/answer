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

import { genericList, harvestList } from "./harvest/list";
import { largestSource } from "./imagesrc";
import { inspect } from "./inspect";
import {
  CommandError,
  type HarvestResult,
  type ImageSource,
  type ProductArea,
  type ShotArea,
  type TabRequest,
  type TabResponse,
  toErrorBody
} from "./protocol";
import { findClosers } from "./snapshot/closers";
import { displayUrl } from "./snapshot/links";
import { maskPrivate } from "./snapshot/privacy";
import { waitForStable } from "./snapshot/stable";
import { clip, paragraphs } from "./snapshot/text";
import { collectStrategyElements, profileFor, StepFailures, strategyMain, withFallback } from "./strategies";

// content script：Go 第一次要操作某个页面时，由 background 注入进去（commands.ts 的 inject），
// 和网页跑在同一个渲染进程，能读写 DOM，但看不到网页自己的 JS 变量（isolated world）。
// 页面一跳转它就被销毁，下一条命令会再注入一份，所以这里的变量只在当前页面有效。

// 上一次 snapshot 编过号的元素。click(12) 就是点 elements[12]，
// 所以编号只在"同一个页面、同一次快照之后"有意义。
let elements: Element[] = [];
// 策略给元素起的名字，describe 用它代替元素的文字
let labels = new Map<Element, string>();
// 策略给商品卡片找的商品页链接，productArea 优先用它
let links = new Map<Element, string>();

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
    case "read":
      // 和 snapshot 一样先等页面安静：浮层刚打开时正文常常还在加载
      await waitForStable(document.body);
      return readText(req.maxChars, req.index);
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
    case "screenshotArea":
      return screenshotArea(req.index);
    case "productArea":
      return productArea(req.index);
    case "imageSource":
      return imageSource(req.index);
    case "inspect":
      return inspect(document, req.text, req.up);
    case "viewport":
      return { width: window.innerWidth, height: window.innerHeight };
    case "scrollPosition":
      return scrollPages();
    case "harvestList":
      return harvestOnPage(req.limit, req.maxScrolls);
  }
}

// 压缩前后的数字：调规则时看效果，也是面试时的硬数据。只给 Go 记录，不一定发给模型
interface SnapshotStats {
  strategy?: string;
  strategyMatched?: number;
  strategyError?: string;
  overlay?: boolean; // 页面中间开着一个浮层（弹窗、笔记浮层），后面的元素多半点不到
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

const ARIA_INTERACTIVE =
  "[role=button], [role=link], [role=tab], [role=menuitem], [role=menuitemradio], [role=menuitemcheckbox], [role=option], [role=combobox], [role=checkbox], [role=radio], [role=switch]";
const INTERACTIVE = `a, button, input, select, textarea, ${ARIA_INTERACTIVE}, [aria-haspopup]`;
const MAX_TEXT = 120; // 每个元素的文字最多几个字：商品卡片的文字比普通按钮长
const MAX_ATTR = 60; // 每个属性值最多几个字

function snapshot(maxChars = 8000): Snapshot {
  const started = performance.now();

  // 先过滤、再编号：编号就是 elements 的下标，click(i) 靠它找回元素
  const strategy = collectStrategyElements(new URL(location.href), document);
  const extras = new Set(strategy.elements);
  labels = strategy.labels;
  links = strategy.links;
  // 页面被浮层盖住时，把浮层的关闭按钮编进来，名字叫"关闭弹窗"
  const overlay = topOverlay();
  for (const closer of overlay ? findClosers(overlay, isVisible) : []) {
    extras.add(closer);
    labels.set(closer, "关闭弹窗");
  }
  const candidates = Array.from(new Set([...document.querySelectorAll(INTERACTIVE), ...extras]));
  candidates.sort((a, b) => {
    const position = a.compareDocumentPosition(b);
    return position & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : position & Node.DOCUMENT_POSITION_PRECEDING ? 1 : 0;
  });
  const shown = candidates.filter(isVisible);
  const visible = shown.filter((el) => extras.has(el) || !insideNumberedParent(el));
  // 弹出菜单常被挂到 body 最后，不能被前面的屏外商品耗尽预算。
  // 视口内优先；各组仍按 DOM 顺序排列，滚动后屏外内容自然进入优先组。
  const inViewport = new Set(visible.filter(intersectsViewport));
  visible.sort((a, b) => Number(inViewport.has(b)) - Number(inViewport.has(a)));

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
      ...strategy.stats,
      overlay: overlay !== undefined,
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
  const style = getComputedStyle(el);
  return rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.visibility !== "collapse";
}

function intersectsViewport(el: Element): boolean {
  const rect = el.getBoundingClientRect();
  return rect.bottom > 0 && rect.right > 0 && rect.top < window.innerHeight && rect.left < window.innerWidth;
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
  if (el.matches(`input, select, textarea, ${ARIA_INTERACTIVE}, [aria-haspopup]`)) {
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
  const text = clip(labels.get(el) ?? (el as HTMLElement).innerText ?? "", MAX_TEXT) || imageAlt(el);
  const attrs = attributesOf(el, text);
  // 唯一出口：整行一起打码，属性和文字都覆盖到（第 7 站坑 6）
  return maskPrivate(`[${i}]<${[tag, ...attrs].join(" ")}>${text}`);
}

// 属性白名单，顺序就是输出顺序。没有 class、style、href、src。
// 和 browser-use 一样保留 id：很多网站会在 id 里写有意义的名字（比如商品编号）
const ATTRS = [
  "id",
  "name",
  "placeholder",
  "aria-label",
  "title",
  "role",
  "aria-selected",
  "aria-expanded",
  "aria-haspopup",
  "aria-checked",
  "aria-disabled"
];

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
  if (!el.isConnected || el.matches(':disabled, [aria-disabled="true"]') || !isVisible(el)) {
    throw new CommandError("ELEMENT_NOT_FOUND", "元素已经失效或被禁用，请重新 snapshot");
  }
  // 已经能点到的不滚动：移动页面会让 hover 菜单收起。
  // 点不到（在屏幕外，或者被吸顶的搜索栏这类东西盖住）才滚到屏幕中间再试一次；
  // 滚到边上（block: nearest）正好会被吸顶栏盖住
  const point = clickablePoint(el);
  if (point) {
    return point;
  }
  el.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
  const centered = clickablePoint(el);
  if (!centered) {
    const center = visibleCenter(el);
    if (!center) {
      throw new CommandError("EXEC_FAILED", "元素不在可点击区域，请重新 snapshot");
    }
    // 盖在上面的东西整个落在元素自己身上，是叠在它上面的装饰（宜家搜索框上轮播提示词的 div，2026-10-19 实测）：
    // 真人也是点在它上面的，照样点。比元素大的（cookie 横幅、浮层）、弹窗、iframe 照旧报错
    if (isDecoration(el, center.x, center.y)) {
      return center;
    }
    // 说清楚被什么盖住：模型才分得清是普通弹窗（关掉就行），还是验证码、登录、风控提示（该停下来）
    throw new CommandError(
      "EXEC_FAILED",
      `元素被遮挡，盖在上面的是 ${describeCover(center.x, center.y)}。` +
        "如果是普通弹窗，先关掉它再重新 snapshot；如果是验证码、登录，或者“访问太频繁”这类提示，不要再试，告诉用户。"
    );
  }
  return centered;
}

// 元素在视口里那一部分的中心点；这个点上最上层的是它自己（或它里面的元素）才算点得到
function clickablePoint(el: Element): { x: number; y: number } | undefined {
  const center = visibleCenter(el);
  if (!center) {
    return undefined;
  }
  const hit = document.elementFromPoint(center.x, center.y);
  return hit && el.contains(hit) ? center : undefined;
}

// 元素在视口里那一部分的中心点，完全不在视口里时没有
function visibleCenter(el: Element): { x: number; y: number } | undefined {
  const rect = el.getBoundingClientRect();
  const left = Math.max(0, rect.left);
  const right = Math.min(window.innerWidth, rect.right);
  const top = Math.max(0, rect.top);
  const bottom = Math.min(window.innerHeight, rect.bottom);
  if (right <= left || bottom <= top) {
    return undefined;
  }
  return { x: (left + right) / 2, y: (top + bottom) / 2 };
}

// 装饰可以比元素大一点点（边框、阴影）
const DECORATION_SLACK_PX = 4;

function isDecoration(el: Element, x: number, y: number): boolean {
  const hit = document.elementFromPoint(x, y);
  if (
    !hit ||
    hit instanceof HTMLIFrameElement ||
    hit.closest('dialog, [role=dialog], [role=alertdialog], [aria-modal="true"]')
  ) {
    return false;
  }
  const cover = hit.getBoundingClientRect();
  const target = el.getBoundingClientRect();
  return (
    cover.left >= target.left - DECORATION_SLACK_PX &&
    cover.right <= target.right + DECORATION_SLACK_PX &&
    cover.top >= target.top - DECORATION_SLACK_PX &&
    cover.bottom <= target.bottom + DECORATION_SLACK_PX
  );
}

// describeCover 说明盖在 (x, y) 上的是什么。不认识任何网站，只描述元素本身：
// iframe 给来源和路径（风控、验证码、登录框常常是别的域名的 iframe，内容 content script 读不到）；
// 不带查询参数，那里常有用户 id 之类的东西。其他元素在弹窗里就描述弹窗，带一小段文字
function describeCover(x: number, y: number): string {
  const hit = document.elementFromPoint(x, y);
  if (!hit) {
    return "不明元素";
  }
  if (hit instanceof HTMLIFrameElement) {
    try {
      const src = new URL(hit.src, location.href);
      return `<iframe>，来自 ${src.host}${clip(src.pathname, 80)}`;
    } catch {
      return "<iframe>";
    }
  }
  const target = hit.closest('dialog, [role=dialog], [role=alertdialog], [aria-modal="true"]') ?? hit;
  const attrs = ["id", "role", "aria-label"]
    .map((name) => [name, target.getAttribute(name)] as const)
    .filter(([, value]) => value)
    .map(([name, value]) => `${name}=${clip(value ?? "", 40)}`);
  const text = clip(maskPrivate((target.textContent ?? "").replace(/\s+/g, " ").trim()), 40);
  return `<${[target.tagName.toLowerCase(), ...attrs].join(" ")}>${text}`;
}

// ---------- 读文字 ----------

// 读的是哪一块：element 是 index 指定的元素，overlay 是盖在页面上的浮层，main 是 <main>/<article>，page 是整页
type ReadScope = "element" | "overlay" | "main" | "page";

interface ReadResult {
  url: string;
  title: string;
  scope: ReadScope;
  text: string;
  truncated: boolean;
  totalChars: number; // 没截断时一共多少字，模型据此知道还剩多少没读
  images: number; // 这块区域里露在屏幕上的大图有几张：图里的字 innerText 读不到，Go 据此提醒模型截图
}

// 多大算"大图"：比头像、图标、表情大得多，小红书笔记的图、商品详情的长图都远超这个
const LARGE_IMAGE_PX = 200;

// <main>/<article> 里的字少于这么多，多半只是个空壳（内容是 JS 渲染在别处的），改读整页
const MIN_MAIN_CHARS = 200;

// readText 读出一块区域的文字。用 innerText：它按 CSS 排版取字，display:none 的不算，
// 块级元素之间有换行，滚动容器里没露出来的部分也算（笔记正文很长时，不用滚动就能读完）
function readText(maxChars: number, index?: number): ReadResult {
  const { roots, scope } = readRoot(index);
  // 先整理、再打码、最后截断：截断不会切开一个手机号，让一半漏过打码。几块正文区之间空一行
  const full = maskPrivate(roots.map((root) => paragraphs((root as HTMLElement).innerText ?? "")).join("\n\n"));
  const truncated = full.length > maxChars;
  return {
    url: displayUrl(location.href),
    title: document.title,
    scope,
    text: truncated ? `${full.slice(0, maxChars)}…` : full,
    truncated,
    totalChars: full.length,
    images: roots.reduce((n, root) => n + largeImages(root), 0)
  };
}

// largeImages 数 root 里露在屏幕上的大图。只数屏幕上的：轮播图别的几张在屏幕外，翻到了再说
function largeImages(root: Element): number {
  return Array.from(root.querySelectorAll("img")).filter((img) => {
    const rect = img.getBoundingClientRect();
    return rect.width >= LARGE_IMAGE_PX && rect.height >= LARGE_IMAGE_PX && intersectsViewport(img) && isVisible(img);
  }).length;
}

function readRoot(index?: number): { roots: Element[]; scope: ReadScope } {
  if (index !== undefined) {
    const el = elementAt(index);
    if (!el.isConnected) {
      throw new CommandError("ELEMENT_NOT_FOUND", "元素已经失效，请重新 snapshot");
    }
    return { roots: [el], scope: "element" };
  }
  const overlay = topOverlay();
  if (overlay) {
    return { roots: [overlay], scope: "overlay" };
  }
  const fromStrategy = strategyMain(new URL(location.href), document);
  if (fromStrategy.length > 0) {
    return { roots: fromStrategy, scope: "main" };
  }
  const main = document.querySelector("main, [role=main], article");
  if (main && ((main as HTMLElement).innerText ?? "").trim().length >= MIN_MAIN_CHARS) {
    return { roots: [main], scope: "main" };
  }
  return { roots: [document.body], scope: "page" };
}

// topOverlay 找盖在页面中间的浮层：屏幕正中那个点往外，最外层的弹窗或 fixed 定位、占了至少四分之一屏的元素。
// 不认识任何网站：小红书的笔记浮层、各种详情弹窗都是这个样子；背后被盖住的列表不读
function topOverlay(): Element | undefined {
  const viewportArea = window.innerWidth * window.innerHeight;
  let found: Element | undefined;
  for (
    let el = document.elementFromPoint?.(window.innerWidth / 2, window.innerHeight / 2);
    el && el !== document.body && el !== document.documentElement;
    el = el.parentElement
  ) {
    const layer =
      el.matches('dialog[open], [role=dialog], [role=alertdialog], [aria-modal="true"]') ||
      getComputedStyle(el).position === "fixed";
    const rect = el.getBoundingClientRect();
    if (layer && rect.width * rect.height >= viewportArea / 4) {
      found = el;
    }
  }
  return found;
}

// 截元素时四周留的边：卡片的边框、角标贴着边，留一点才看得出是一张完整的卡片
const SHOT_PADDING = 8;

// screenshotArea 是要截的区域。不填 index 是当前这一屏；填了是那个元素在视口里的部分加上一圈边。
// 元素完全不在屏幕上时才滚到中间：截图不应该无故移动页面（hover 出来的菜单会收起）
async function screenshotArea(index?: number): Promise<ShotArea> {
  const dpr = window.devicePixelRatio || 1;
  if (index === undefined) {
    return { x: window.scrollX, y: window.scrollY, width: window.innerWidth, height: window.innerHeight, dpr };
  }
  const el = elementAt(index);
  if (!el.isConnected || !isVisible(el)) {
    throw new CommandError("ELEMENT_NOT_FOUND", "元素已经失效，请重新 snapshot");
  }
  await settleForShot(el);
  return elementArea(el, SHOT_PADDING);
}

// 元素截图前的准备：只露出一部分时先滚到中间，再等里面的图加载完。
// 2026-10-20 淘宝首页的轮播图直接截，图还没画出来，截下来一片白
async function settleForShot(el: Element): Promise<void> {
  const rect = el.getBoundingClientRect();
  const fits = rect.width <= window.innerWidth && rect.height <= window.innerHeight;
  if (fits && !fullyInViewport(el)) {
    el.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
    await new Promise((resolve) => setTimeout(resolve, SCROLL_SETTLE_MS));
  }
  const images = [...(el.matches("img") ? [el] : []), ...el.querySelectorAll("img")].slice(0, SHOT_IMAGE_WAIT);
  if (images.some((img) => img instanceof HTMLImageElement && !img.complete)) {
    await Promise.all(images.map(imageLoaded));
    // 加载完到画出来还差一帧，淡入的图还要更久
    await new Promise((resolve) => setTimeout(resolve, SCROLL_SETTLE_MS));
  }
}

// 截图前最多等元素里几张图：一个列表里几十张图，等前几张就够了
const SHOT_IMAGE_WAIT = 6;

// elementArea 是元素在视口里的部分，四周加 padding。元素完全不在屏幕上时先滚到中间
function elementArea(el: Element, padding: number): ShotArea {
  if (!intersectsViewport(el)) {
    el.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
  }
  const rect = el.getBoundingClientRect();
  const left = Math.max(0, rect.left - padding);
  const right = Math.min(window.innerWidth, rect.right + padding);
  const top = Math.max(0, rect.top - padding);
  const bottom = Math.min(window.innerHeight, rect.bottom + padding);
  if (right <= left || bottom <= top) {
    throw new CommandError("EXEC_FAILED", "元素不在屏幕上，截不到");
  }
  const dpr = window.devicePixelRatio || 1;
  return { x: left + window.scrollX, y: top + window.scrollY, width: right - left, height: bottom - top, dpr };
}

// 多大的图才算商品图：比它小的是图标、店铺 logo、详情页主图旁边那一列缩略图
const PRODUCT_IMAGE_PX = 120;

// productArea 找商品卡片要的链接和商品图（阶段 3 的商品卡片）。
// 链接和图都从页面上取，不让模型写：模型写的链接常常是编的，商品 id 错一位就是别的商品。
// 只截商品图那一块，不截整屏：整屏上有登录账号的名字，卡片是要给用户看、要存下来的
async function productArea(index?: number): Promise<ProductArea> {
  const el = index === undefined ? undefined : elementAt(index);
  if (el && (!el.isConnected || !isVisible(el))) {
    throw new CommandError("ELEMENT_NOT_FOUND", "元素已经失效，请重新 snapshot");
  }
  const url = el ? (links.get(el) ?? linkOf(el)) : location.href;
  if (!url) {
    throw new CommandError("EXEC_FAILED", "这个元素里没有链接");
  }
  const image = largestImage(el ?? document.body);
  if (!image) {
    return { url, image: null };
  }
  const area = await wholeImageArea(image);
  const src = image instanceof HTMLImageElement ? largestSource(image) : null;
  return src ? { url, image: area, src } : { url, image: area };
}

// imageSource 找一张图的原始地址，插件拿去下载原图。不填 index 是屏幕上最大的图。
// 懒加载的图先滚到屏幕上、等它加载完，不然拿到的是占位图的地址
async function imageSource(index?: number): Promise<ImageSource> {
  const el = index === undefined ? undefined : elementAt(index);
  if (el && (!el.isConnected || !isVisible(el))) {
    throw new CommandError("ELEMENT_NOT_FOUND", "元素已经失效，请重新 snapshot");
  }
  const img = largestImage(el ?? document.body);
  if (!(img instanceof HTMLImageElement)) {
    throw new CommandError("EXEC_FAILED", "这里没有够大的图片，可以用截图");
  }
  await wholeImageArea(img);
  const src = largestSource(img);
  if (!src) {
    throw new CommandError("EXEC_FAILED", "这张图没有可以下载的地址（可能是画出来的或者内嵌的），可以用截图");
  }
  return { src, naturalWidth: img.naturalWidth, naturalHeight: img.naturalHeight };
}

// 列表收割每滚一屏滚多远：留一点重叠，免得两屏交界处的卡片两边都只露一半
const HARVEST_SCROLL_RATIO = 0.85;
// 滚动以后等新卡片出来：瀑布流是滚动以后才请求下一批、再渲染
const HARVEST_SCROLL_WAIT_MS = 1200;

// scrollerOf 是包着 el、真的能滚的那个容器；没有就是整个页面
function scrollerOf(el: Element | undefined): Element {
  for (let p = el?.parentElement; p && p !== document.body; p = p.parentElement) {
    if (/(auto|scroll)/.test(getComputedStyle(p).overflowY) && p.scrollHeight > p.clientHeight + 1) {
      return p;
    }
  }
  return document.scrollingElement ?? document.documentElement;
}

// harvestOnPage 是 harvest 命令在页面里的那一半：按网址找增强，合成步骤，交给骨架，滚动和等图在这里做
async function harvestOnPage(limit: number, maxScrolls: number): Promise<HarvestResult> {
  const started = performance.now();
  const profile = profileFor(new URL(location.href));
  const failures = new StepFailures();
  const steps = withFallback("list", profile?.list, genericList, failures);
  let lastCards: Element[] = [];
  const { items, stats } = await harvestList(
    document,
    steps,
    { limit, maxScrolls },
    {
      async scroll() {
        // 滚卡片所在的那个滚动容器：有的网站滚的不是整个页面，而是里面一个 overflow 的 div
        const scroller = scrollerOf(lastCards[0]);
        const before = scroller.scrollTop;
        const height = scroller.scrollHeight;
        scroller.scrollBy(0, Math.round(scroller.clientHeight * HARVEST_SCROLL_RATIO));
        await new Promise((resolve) => setTimeout(resolve, HARVEST_SCROLL_WAIT_MS));
        // 到底了：位置没变，内容也没变长
        return scroller.scrollTop > before || scroller.scrollHeight > height;
      },
      async settle(cards) {
        lastCards = cards;
        const onScreen = cards.filter(intersectsViewport);
        await Promise.all(onScreen.flatMap((card) => Array.from(card.querySelectorAll("img"), imageLoaded)));
      }
    }
  );
  return {
    items,
    stats: {
      strategy: profile?.name,
      ...(failures.size > 0 ? { strategyError: failures.toString() } : {}),
      ...stats,
      ms: Math.round(performance.now() - started)
    }
  };
}

// 滚动以后等多久再截：滚动是同步的，画面要等下一帧才更新
const SCROLL_SETTLE_MS = 150;
// 图片最多等多久加载完：懒加载的图滚到屏幕上才开始下载
const IMAGE_LOAD_MS = 1500;

// wholeImageArea 是整张商品图的区域。图只露出一部分时先滚到中间、等它加载完再截：
// 2026-10-19 宜家搜索结果里的图只露出顶上一条，截下来是一条白边，放进正方形的卡片里一片白。
// 滚过以后露出来的还不到一半（图比屏幕还大），就不要图了
async function wholeImageArea(img: Element): Promise<ShotArea | null> {
  if (!fullyInViewport(img)) {
    img.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
    await new Promise((resolve) => setTimeout(resolve, SCROLL_SETTLE_MS));
  }
  await imageLoaded(img);
  const rect = img.getBoundingClientRect();
  const area = elementArea(img, 0);
  return area.width * area.height * 2 >= rect.width * rect.height ? area : null;
}

function fullyInViewport(el: Element): boolean {
  const rect = el.getBoundingClientRect();
  return rect.top >= 0 && rect.left >= 0 && rect.bottom <= window.innerHeight && rect.right <= window.innerWidth;
}

// imageLoaded 等 <img> 加载完（或者失败），最多等 IMAGE_LOAD_MS。complete 了就不等：没有 src 的图也算 complete
function imageLoaded(img: Element): Promise<void> {
  if (!(img instanceof HTMLImageElement) || img.complete) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer);
      img.removeEventListener("load", done);
      img.removeEventListener("error", done);
      resolve();
    };
    const timer = setTimeout(done, IMAGE_LOAD_MS);
    img.addEventListener("load", done);
    img.addEventListener("error", done);
  });
}

// linkOf 是卡片的链接：卡片本身是 <a>，或者套在 <a> 里，或者里面有 <a>（取第一个，一般是标题）
function linkOf(el: Element): string | undefined {
  const a = el.closest("a[href]") ?? el.querySelector("a[href]");
  return a instanceof HTMLAnchorElement && /^https?:/.test(a.href) ? a.href : undefined;
}

// largestImage 是 root 里最大的那张图。屏幕上有的就只在屏幕上的里面挑：
// 详情页往下滚过以后，屏幕上的那张是模型正在看的
function largestImage(root: Element): Element | undefined {
  const images = [...(root.matches("img") ? [root] : []), ...root.querySelectorAll("img")].filter((img) => {
    const rect = img.getBoundingClientRect();
    return rect.width >= PRODUCT_IMAGE_PX && rect.height >= PRODUCT_IMAGE_PX && isVisible(img);
  });
  const onScreen = images.filter(intersectsViewport);
  const pool = onScreen.length > 0 ? onScreen : images;
  let best: Element | undefined;
  let bestArea = 0;
  for (const img of pool) {
    const rect = img.getBoundingClientRect();
    if (rect.width * rect.height > bestArea) {
      best = img;
      bestArea = rect.width * rect.height;
    }
  }
  return best;
}

Object.assign(globalThis, { __pagehand: { snapshot } });
