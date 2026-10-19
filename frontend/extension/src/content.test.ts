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
