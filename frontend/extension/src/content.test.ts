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

// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import type { TabRequest, TabResponse } from "./protocol";

vi.mock("./snapshot/stable", () => ({ waitForStable: async () => ({ waitedMs: 0, stable: true }) }));

let listener: (req: TabRequest, sender: unknown, respond: (res: TabResponse) => void) => void;
const scrollIntoView = vi.fn();

beforeEach(async () => {
  vi.resetModules();
  scrollIntoView.mockClear();
  vi.stubGlobal("chrome", { runtime: { onMessage: { addListener: (fn: typeof listener) => (listener = fn) } } });
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
    x: 10,
    y: 10,
    left: 10,
    top: 10,
    right: 110,
    bottom: 40,
    width: 100,
    height: 30,
    toJSON: () => ({})
  });
  Object.defineProperty(HTMLElement.prototype, "innerText", {
    configurable: true,
    get() {
      return this.textContent;
    }
  });
  Element.prototype.scrollIntoView = scrollIntoView;
  await import("./content");
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function request(req: TabRequest): Promise<TabResponse> {
  return new Promise((resolve) => listener(req, {}, resolve));
}

async function snapshot(): Promise<string> {
  const res = await request({ type: "snapshot", maxChars: 8000 });
  expect(res.ok).toBe(true);
  return res.ok ? (res.data as { elements: string }).elements : "";
}

it("采集 div 文字的 tab 和选中状态，hover 后的菜单项独立编号", async () => {
  document.body.innerHTML = `<ul role="tablist"><li role="tab" tabindex="-1" aria-selected="false"><div>价格</div></li>
    <li role="tab" aria-selected="true"><div>销量</div></li></ul><div>普通文案</div>
    <div role="button">菜单<div role="menuitem" style="visibility:hidden">从低到高</div></div>`;
  const before = await snapshot();
  expect(before).toContain("<li role=tab aria-selected=false>价格");
  expect(before).toContain("<li role=tab aria-selected=true>销量");
  expect(before).not.toContain("<div>普通文案");
  expect(before).not.toContain("<div role=menuitem>");
  const item = document.querySelector<HTMLElement>('[role="menuitem"]')!;
  item.style.visibility = "visible";
  expect(await snapshot()).toContain("<div role=menuitem>从低到高");
});

it("已经点得到的菜单项不滚动（滚动会让 hover 菜单收起），并拒绝隐藏或失效的旧编号", async () => {
  document.body.innerHTML = '<div role="menuitem">从低到高</div>';
  const item = document.body.firstElementChild as HTMLElement;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => item });
  await snapshot();
  expect(await request({ type: "prepareInput", index: 0, editable: false })).toEqual({
    ok: true,
    data: { x: 60, y: 25 }
  });
  expect(scrollIntoView).not.toHaveBeenCalled();
  item.style.visibility = "hidden";
  expect(await request({ type: "prepareInput", index: 0, editable: false })).toMatchObject({
    ok: false,
    error: { code: "ELEMENT_NOT_FOUND" }
  });
  item.remove();
  expect(await request({ type: "prepareInput", index: 0, editable: false })).toMatchObject({
    ok: false,
    error: { code: "ELEMENT_NOT_FOUND" }
  });
});

it("被吸顶栏这类东西盖住时滚到屏幕中间再定位，还盖着就报错", async () => {
  document.body.innerHTML = '<div id="bar">吸顶栏</div><button>销量</button>';
  const button = document.querySelector("button")!;
  const bar = document.getElementById("bar")!;
  // 吸顶栏横跨整个页面，比按钮大得多：不是叠在按钮身上的装饰
  bar.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 1000,
    bottom: 60,
    width: 1000,
    height: 60,
    toJSON: () => ({})
  });
  let covered = true;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => (covered ? bar : button) });
  scrollIntoView.mockImplementation(() => {
    covered = false;
  });
  await snapshot();
  expect(await request({ type: "prepareInput", index: 0, editable: false })).toMatchObject({ ok: true });
  expect(scrollIntoView).toHaveBeenCalledWith({ block: "center", inline: "center", behavior: "instant" });

  covered = true;
  scrollIntoView.mockImplementation(() => {});
  expect(await request({ type: "prepareInput", index: 0, editable: false })).toMatchObject({
    ok: false,
    error: { code: "EXEC_FAILED" }
  });
});

it("站点候选按 DOM 顺序编号，隐藏菜单仍过滤，文字仍经过隐私处理", async () => {
  vi.stubGlobal("location", new URL("https://s.taobao.com/search"));
  document.body.innerHTML = `<li role="tab">价格</li><div class="priceTagWrap--abc">
    <div class="priceTag--a">从低到高</div><div class="priceTag--b" style="visibility:hidden">从高到低</div>
    <div class="priceTag--c">联系 13812345678</div></div><button>搜索</button>`;
  const result = await snapshot();
  expect(result).toContain("[0]<li role=tab>价格");
  expect(result).toContain("[1]<div>从低到高");
  expect(result).not.toContain("从高到低");
  expect(result).toContain("[2]<div>联系 [手机号]");
  expect(result).toContain("[3]<button>搜索");
});

it("视口中的弹出菜单优先于屏外商品，不会被长度预算截掉", async () => {
  document.body.innerHTML = '<button>屏外商品详情</button><div role="menuitem">从低到高</div>';
  Object.defineProperty(document.querySelector("button")!, "getBoundingClientRect", {
    value: () => ({
      x: 10,
      y: 2000,
      left: 10,
      top: 2000,
      right: 110,
      bottom: 2030,
      width: 100,
      height: 30,
      toJSON: () => ({})
    })
  });
  const res = await request({ type: "snapshot", maxChars: 35 });
  expect(res).toMatchObject({ ok: true, data: { elements: "[0]<div role=menuitem>从低到高", truncated: true } });
});

it("截图区域：不填 index 是当前这一屏，坐标相对整个文档", async () => {
  // Arrange
  vi.stubGlobal("scrollX", 0);
  vi.stubGlobal("scrollY", 500);
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  vi.stubGlobal("devicePixelRatio", 1.25);

  // Act
  const res = await request({ type: "screenshotArea" });

  // Assert
  expect(res).toEqual({ ok: true, data: { x: 0, y: 500, width: 1280, height: 720, dpr: 1.25 } });
});

it("截图区域：填了 index 是那个元素加一圈边，在屏幕上就不滚动", async () => {
  // Arrange：元素在视口里的 (10,10)-(110,40)
  vi.stubGlobal("scrollX", 0);
  vi.stubGlobal("scrollY", 500);
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  vi.stubGlobal("devicePixelRatio", 1);
  document.body.innerHTML = "<button>销量</button>";
  await snapshot();

  // Act
  const res = await request({ type: "screenshotArea", index: 0 });

  // Assert：四周各留 8 像素，左边和上边被视口截到 2
  expect(res).toEqual({ ok: true, data: { x: 2, y: 502, width: 116, height: 46, dpr: 1 } });
  expect(scrollIntoView).not.toHaveBeenCalled();
});

it("截图区域：元素已经失效时报 ELEMENT_NOT_FOUND", async () => {
  // Arrange
  document.body.innerHTML = "<button>销量</button>";
  await snapshot();
  document.querySelector("button")!.remove();

  // Act
  const res = await request({ type: "screenshotArea", index: 0 });

  // Assert
  expect(res).toMatchObject({ ok: false, error: { code: "ELEMENT_NOT_FOUND" } });
});

it("被 iframe 盖住时说出它的来源和路径，不带查询参数", async () => {
  // Arrange：风控提示是别的域名的 iframe，盖在整个页面上
  document.body.innerHTML =
    '<button>区间</button><iframe src="https://h5api.example.com/h5/x/punish?userid=123&token=abc"></iframe>';
  const frame = document.querySelector("iframe")!;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => frame });
  await snapshot();

  // Act
  const res = await request({ type: "prepareInput", index: 0, editable: false });

  // Assert
  expect(res).toMatchObject({ ok: false, error: { code: "EXEC_FAILED" } });
  const message = res.ok ? "" : res.error.message;
  expect(message).toContain("<iframe>，来自 h5api.example.com/h5/x/punish");
  expect(message).not.toContain("userid");
  expect(message).toContain("不要再试");
});

it("被弹窗盖住时描述弹窗本身，带一小段文字", async () => {
  // Arrange
  document.body.innerHTML = '<button>区间</button><div role="dialog" aria-label="登录"><p>请先登录后再操作</p></div>';
  const p = document.querySelector("p")!;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => p });
  await snapshot();

  // Act
  const res = await request({ type: "prepareInput", index: 0, editable: false });

  // Assert
  const message = res.ok ? "" : res.error.message;
  expect(message).toContain("<div role=dialog aria-label=登录>请先登录后再操作");
});

async function read(index?: number, maxChars = 3000) {
  const res = await request({ type: "read", index, maxChars });
  expect(res.ok).toBe(true);
  return res.ok ? (res.data as { scope: string; text: string; truncated: boolean; totalChars: number }) : undefined;
}

it("读文字：页面中间盖着浮层时只读浮层，背后的列表不读", async () => {
  // Arrange：笔记浮层是 fixed 定位，占满整屏；屏幕正中是浮层里的正文
  vi.stubGlobal("innerWidth", 1000);
  vi.stubGlobal("innerHeight", 800);
  document.body.innerHTML = `<div id="list"><a>笔记一</a><a>笔记二</a></div>
    <div id="mask" style="position: fixed"><div id="note"><h1>用了半年的感受</h1><p>续航一般，拍照很好</p></div></div>`;
  const mask = document.getElementById("mask")!;
  mask.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 1000,
    bottom: 800,
    width: 1000,
    height: 800,
    toJSON: () => ({})
  });
  const body = document.querySelector("p")!;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => body });

  // Act
  const got = await read();

  // Assert
  expect(got).toMatchObject({ scope: "overlay", truncated: false });
  expect(got?.text).toContain("续航一般，拍照很好");
  expect(got?.text).not.toContain("笔记一");
});

it("读文字：没有浮层时读 <main>，太短的 <main> 改读整页", async () => {
  // Arrange
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => null });
  document.body.innerHTML = `<nav>首页 分类</nav><main><p>${"正文".repeat(150)}</p></main>`;

  // Act
  const withMain = await read();
  document.body.innerHTML = "<nav>首页 分类</nav><main><p>加载中</p></main><div>别处渲染的正文</div>";
  const shell = await read();

  // Assert
  expect(withMain?.scope).toBe("main");
  expect(withMain?.text).not.toContain("首页");
  expect(shell?.scope).toBe("page");
  expect(shell?.text).toContain("别处渲染的正文");
});

it("读文字：填 index 读那个元素的全部文字，快照里的 120 字上限不管用", async () => {
  // Arrange
  const title = "很长的商品标题".repeat(30);
  document.body.innerHTML = `<a>${title}</a>`;
  await snapshot();

  // Act
  const got = await read(0);

  // Assert
  expect(got).toMatchObject({ scope: "element", text: title, truncated: false });
});

it("读文字：先打码再截断，超出的部分不返回", async () => {
  // Arrange
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => null });
  document.body.innerHTML = "<p>联系 13812345678 了解更多</p>";

  // Act
  const got = await read(undefined, 8);

  // Assert
  expect(got).toMatchObject({ text: "联系 [手机号]…", truncated: true, totalChars: 13 });
  expect(got?.text).not.toContain("138");
});

it("读文字：数出露在屏幕上的大图，头像这种小图和屏幕外的不算", async () => {
  // Arrange：jsdom 没有排版，每个元素的位置都是假的，三张图各给一个
  vi.stubGlobal("innerWidth", 1000);
  vi.stubGlobal("innerHeight", 800);
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => null });
  document.body.innerHTML = '<p>正文</p><img id="big"><img id="avatar"><img id="offscreen">';
  const rect = (left: number, top: number, size: number) => () => ({
    x: left,
    y: top,
    left,
    top,
    right: left + size,
    bottom: top + size,
    width: size,
    height: size,
    toJSON: () => ({})
  });
  document.getElementById("big")!.getBoundingClientRect = rect(0, 0, 600);
  document.getElementById("avatar")!.getBoundingClientRect = rect(0, 0, 40);
  document.getElementById("offscreen")!.getBoundingClientRect = rect(1200, 0, 600);

  // Act
  const res = await request({ type: "read", maxChars: 3000 });

  // Assert
  expect(res).toMatchObject({ ok: true, data: { images: 1 } });
});

it("盖在元素身上的装饰照样点：宜家搜索框上叠着轮播提示词", async () => {
  // Arrange：提示词的 div 和输入框一样大（jsdom 里每个元素的位置都是同一个假值）
  document.body.innerHTML = '<input aria-label="search"><div id="hint">遮光窗帘</div>';
  const hint = document.getElementById("hint")!;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => hint });
  await snapshot();

  // Act
  const res = await request({ type: "prepareInput", index: 0, editable: false });

  // Assert：点在中心，和真人一样点在提示词上
  expect(res).toEqual({ ok: true, data: { x: 60, y: 25 } });
});

it("比元素大的东西盖着（cookie 横幅）照旧报错，不点下去", async () => {
  // Arrange
  document.body.innerHTML = '<button>搜索</button><div id="banner">我们使用 cookie</div>';
  const banner = document.getElementById("banner")!;
  banner.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 1000,
    bottom: 80,
    width: 1000,
    height: 80,
    toJSON: () => ({})
  });
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => banner });
  await snapshot();

  // Act
  const res = await request({ type: "prepareInput", index: 0, editable: false });

  // Assert
  expect(res).toMatchObject({ ok: false, error: { code: "EXEC_FAILED" } });
});
