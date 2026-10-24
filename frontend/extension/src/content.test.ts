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
const runtimeMessage = vi.fn();

beforeEach(async () => {
  vi.resetModules();
  scrollIntoView.mockClear();
  runtimeMessage.mockReset();
  vi.stubGlobal("chrome", {
    runtime: {
      onMessage: { addListener: (fn: typeof listener) => (listener = fn) },
      sendMessage: runtimeMessage
    }
  });
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
  vi.useRealTimers();
});

it("harvest 在每屏滚动前申请额度，被限流后不再滚动", async () => {
  vi.useFakeTimers();
  document.body.innerHTML = Array.from(
    { length: 4 },
    (_, i) =>
      `<article><a href="https://example.com/${i}"><img src="https://img.example.com/${i}.jpg">商品${i}</a></article>`
  ).join("");
  const scrollBy = vi.fn();
  document.documentElement.scrollBy = scrollBy;
  runtimeMessage.mockResolvedValue({ ok: false, error: { code: "RATE_LIMITED", message: "请等待 30 秒" } });
  const done = request({ type: "harvestList", limit: 100, maxScrolls: 2 });
  await vi.runAllTimersAsync();
  expect(await done).toMatchObject({ ok: false, error: { code: "RATE_LIMITED" } });
  expect(runtimeMessage).toHaveBeenCalledExactlyOnceWith({ type: "rateLimit", action: "harvestScroll" });
  expect(scrollBy).not.toHaveBeenCalled();
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

function square(left: number, top: number, size: number) {
  return () => ({
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
}

it("商品卡片：填了 index 取卡片的链接和卡片里最大的图，不留边", async () => {
  // Arrange：卡片是一个 <a>，里面有商品图和店铺 logo
  vi.stubGlobal("scrollX", 0);
  vi.stubGlobal("scrollY", 0);
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  vi.stubGlobal("devicePixelRatio", 1);
  document.body.innerHTML =
    '<a href="https://item.example.com/item.htm?id=42&spm=x"><img id="main"><img id="logo">羽绒被 ¥588</a>';
  document.getElementById("main")!.getBoundingClientRect = square(20, 30, 240);
  document.getElementById("logo")!.getBoundingClientRect = square(20, 280, 24);
  await snapshot();

  // Act
  const res = await request({ type: "productArea", index: 0 });

  // Assert：链接原样带回，查询参数由 Go 去清洗
  expect(res).toEqual({
    ok: true,
    data: {
      url: "https://item.example.com/item.htm?id=42&spm=x",
      image: { x: 20, y: 30, width: 240, height: 240, dpr: 1 }
    }
  });
});

it("商品卡片：不填 index 用当前网址和屏幕上最大的图", async () => {
  // Arrange：屏幕外还有一张更大的详情图，不选它
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  document.body.innerHTML = '<img id="main"><img id="detail"><img id="thumb">';
  document.getElementById("main")!.getBoundingClientRect = square(100, 100, 400);
  document.getElementById("detail")!.getBoundingClientRect = square(100, 2000, 750);
  document.getElementById("thumb")!.getBoundingClientRect = square(20, 100, 60);

  // Act
  const res = await request({ type: "productArea" });

  // Assert
  expect(res).toMatchObject({
    ok: true,
    data: { url: location.href, image: { x: 100, y: 100, width: 400, height: 400 } }
  });
});

it("商品卡片：图有地址时一起带回，插件先下载原图", async () => {
  // Arrange
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  document.body.innerHTML = '<a href="https://item.example.com/1"><img id="main" src="/img/beef_800.jpg">牛排</a>';
  document.getElementById("main")!.getBoundingClientRect = square(20, 30, 240);
  await snapshot();

  // Act
  const res = await request({ type: "productArea", index: 0 });

  // Assert
  expect(res).toMatchObject({
    ok: true,
    data: { url: "https://item.example.com/1", src: new URL("/img/beef_800.jpg", location.href).href }
  });
});

it("商品卡片：没有够大的图时 image 为 null", async () => {
  // Arrange
  document.body.innerHTML = '<a href="https://item.example.com/1">无图商品</a>';
  await snapshot();

  // Act：每个元素默认是 100×30，不够大
  const res = await request({ type: "productArea", index: 0 });

  // Assert
  expect(res).toEqual({ ok: true, data: { url: "https://item.example.com/1", image: null } });
});

it("商品卡片：元素里没有链接时报错", async () => {
  // Arrange
  document.body.innerHTML = "<button>加入购物车</button>";
  await snapshot();

  // Act
  const res = await request({ type: "productArea", index: 0 });

  // Assert
  expect(res).toMatchObject({ ok: false, error: { code: "EXEC_FAILED" } });
});

it("商品卡片：图只露出一条时先滚到中间再截，滚过以后还露不出一半就不要图", async () => {
  // Arrange：图在视口上面，只露出底下 100 像素（jsdom 里滚动不会改变位置）
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  document.body.innerHTML = '<a href="https://www.ikea.cn/cn/zh/p/x"><img id="main">被子</a>';
  document.getElementById("main")!.getBoundingClientRect = square(20, -300, 400);
  await snapshot();

  // Act
  const res = await request({ type: "productArea", index: 0 });

  // Assert
  expect(scrollIntoView).toHaveBeenCalled();
  expect(res).toEqual({ ok: true, data: { url: "https://www.ikea.cn/cn/zh/p/x", image: null } });
});

it("原图：取元素里最大那张图 srcset 里最大的地址", async () => {
  // Arrange
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  document.body.innerHTML =
    '<a href="https://item.example.com/1"><img id="main" src="/img/a_220.jpg" ' +
    'srcset="/img/a_220.jpg 220w, /img/a_800.jpg 800w, /img/a_400.jpg 400w">商品</a>';
  document.getElementById("main")!.getBoundingClientRect = square(20, 30, 240);
  await snapshot();

  // Act
  const res = await request({ type: "imageSource", index: 0 });

  // Assert
  expect(res).toMatchObject({ ok: true, data: { src: new URL("/img/a_800.jpg", location.href).href } });
});

it("原图：内嵌的 data: 图不能下载，报错让模型改用截图", async () => {
  // Arrange
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  document.body.innerHTML =
    '<a href="https://item.example.com/1"><img id="main" src="data:image/png;base64,AAAA">商品</a>';
  document.getElementById("main")!.getBoundingClientRect = square(20, 30, 240);
  await snapshot();

  // Act
  const res = await request({ type: "imageSource", index: 0 });

  // Assert
  expect(res).toMatchObject({ ok: false, error: { code: "EXEC_FAILED" } });
});

it("原图：没有够大的图时报错", async () => {
  // Arrange
  document.body.innerHTML = '<a href="https://item.example.com/1">无图商品</a>';
  await snapshot();

  // Act
  const res = await request({ type: "imageSource", index: 0 });

  // Assert
  expect(res).toMatchObject({ ok: false, error: { code: "EXEC_FAILED" } });
});

it("页面被浮层盖住时，浮层的关闭按钮以「关闭弹窗」编进快照", async () => {
  // Arrange：全屏的 fixed 遮罩里有一个营销弹窗，关闭按钮是没有标签的 div
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 720);
  document.body.innerHTML = `<button>搜索</button>
    <div id="mask" style="position: fixed"><div id="promo">秋冬上新95折</div><div class="dialog-close"></div></div>`;
  const mask = document.getElementById("mask")!;
  mask.getBoundingClientRect = square(0, 0, 1280);
  document.querySelector(".dialog-close")!.getBoundingClientRect = square(900, 100, 24);
  const promo = document.getElementById("promo")!;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => promo });

  // Act
  const res = await request({ type: "snapshot", maxChars: 8000 });

  // Assert
  expect(res.ok).toBe(true);
  const data = res.ok ? (res.data as { elements: string; stats: { overlay?: boolean } }) : undefined;
  expect(data?.elements).toMatch(/\[\d+\]<div[^>]*>关闭弹窗/);
  expect(data?.stats.overlay).toBe(true);
});

it("fetchRange 带上 Range 分段下载，从 Content-Range 读出总大小", async () => {
  // Arrange：服务器回 206 和这一段的 3 个字节
  const fetch = vi.fn().mockResolvedValue(
    new Response(new Uint8Array([1, 2, 3]), {
      status: 206,
      headers: { "content-type": "video/mp4", "content-range": "bytes 0-2/128" }
    })
  );
  vi.stubGlobal("fetch", fetch);

  // Act
  const res = await request({ type: "fetchRange", url: "https://v5.zjcdn.com/v.mp4", start: 0, end: 2 });

  // Assert：不带 cookie，数据是 base64
  expect(fetch).toHaveBeenCalledWith("https://v5.zjcdn.com/v.mp4", {
    headers: { Range: "bytes=0-2" },
    credentials: "omit"
  });
  expect(res).toEqual({ ok: true, data: { status: 206, contentType: "video/mp4", total: 128, data: "AQID" } });
});

it("fetchRange 遇到 403 不抛错，把状态码交给 Go 判断是不是网址过期了", async () => {
  // Arrange
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("expired", { status: 403 })));

  // Act
  const res = await request({ type: "fetchRange", url: "https://v5.zjcdn.com/v.mp4", start: 0, end: 2 });

  // Assert
  expect(res).toMatchObject({ ok: true, data: { status: 403, data: "" } });
});

it("fetchRange 只下 https，一段不超过 2MB", async () => {
  // Arrange
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);

  // Act
  const plain = await request({ type: "fetchRange", url: "http://v5.zjcdn.com/v.mp4", start: 0, end: 2 });
  const big = await request({ type: "fetchRange", url: "https://v5.zjcdn.com/v.mp4", start: 0, end: 3 << 20 });

  // Assert
  expect(plain).toMatchObject({ ok: false, error: { code: "BAD_MESSAGE" } });
  expect(big).toMatchObject({ ok: false, error: { code: "BAD_MESSAGE" } });
  expect(fetch).not.toHaveBeenCalled();
});
