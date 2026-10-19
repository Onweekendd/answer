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

import { cdpClick, cdpHover, cdpPress, cdpScreenshot, cdpScroll, cdpType, withDebugger } from "./cdp";
import {
  type Command,
  CommandError,
  type InputAction,
  type InputTarget,
  type ProductArea,
  type ShotArea,
  type TabRequest,
  type TabResponse
} from "./protocol";

// 在 background 里执行 Go 发来的命令。
// 分工：跟"标签页"有关的（跳转、开关）在这里做；跟"页面内容"有关的转给 content script。

const DEFAULT_MAX_CHARS = 8000;
const DEFAULT_READ_CHARS = 3000;
// 截图的默认尺寸和质量，Go 一般会自己指定
const DEFAULT_SHOT_WIDTH = 1600;
const DEFAULT_SHOT_QUALITY = 80;
// 商品卡片的图只当缩略图用，不用看清小字
const DEFAULT_PRODUCT_WIDTH = 480;
// 按键以后等多久再读网址
const PRESS_SETTLE_MS = 500;

type ListenerOf<E> = E extends chrome.events.Event<infer F> ? F : never;

// 同一标签页串行执行，避免输入、快照和 debugger 会话相互干扰。
const queues = new Map<number, Promise<unknown>>();

export async function execute(cmd: Command): Promise<unknown> {
  const previous = queues.get(cmd.tabId) ?? Promise.resolve();
  const next = previous.catch(() => {}).then(() => executeCommand(cmd));
  queues.set(cmd.tabId, next);
  try {
    return await next;
  } finally {
    if (queues.get(cmd.tabId) === next) {
      queues.delete(cmd.tabId);
    }
  }
}

async function executeCommand(cmd: Command): Promise<unknown> {
  switch (cmd.cmd) {
    case "navigate":
      return navigate(cmd.tabId, cmd.args.url);
    case "snapshot":
      return sendToTab(cmd.tabId, {
        type: "snapshot",
        maxChars: cmd.args.maxChars ?? DEFAULT_MAX_CHARS
      });
    case "read":
      return sendToTab(cmd.tabId, {
        type: "read",
        index: cmd.args.index,
        maxChars: cmd.args.maxChars ?? DEFAULT_READ_CHARS
      });
    case "screenshot":
      return withDebugger(cmd.tabId, async (target) => {
        await bringToFront(cmd.tabId);
        const area = (await sendToTab(cmd.tabId, { type: "screenshotArea", index: cmd.args.index })) as ShotArea;
        const shot = await cdpScreenshot(
          target,
          area,
          cmd.args.maxWidth ?? DEFAULT_SHOT_WIDTH,
          cmd.args.quality ?? DEFAULT_SHOT_QUALITY
        );
        const tab = await chrome.tabs.get(cmd.tabId);
        return { ...shot, url: tab.url ?? "" };
      });
    case "product":
      return withDebugger(cmd.tabId, async (target) => {
        await bringToFront(cmd.tabId);
        const found = (await sendToTab(cmd.tabId, { type: "productArea", index: cmd.args.index })) as ProductArea;
        const image = found.image
          ? await cdpScreenshot(
              target,
              found.image,
              cmd.args.maxWidth ?? DEFAULT_PRODUCT_WIDTH,
              cmd.args.quality ?? DEFAULT_SHOT_QUALITY
            )
          : null;
        const tab = await chrome.tabs.get(cmd.tabId);
        return { url: found.url, title: tab.title ?? "", image };
      });
    case "click":
      return actIn(cmd.tabId, { type: "click", index: cmd.args.index });
    case "hover":
      return actIn(cmd.tabId, { type: "hover", index: cmd.args.index });
    case "press":
      return withDebugger(cmd.tabId, async (target) => {
        await bringToFront(cmd.tabId);
        await cdpPress(target, cmd.args.key);
        // 关浮层常常带一个动画、改一下网址（小红书的笔记浮层就是 history.back），等一下再报网址
        await new Promise((resolve) => setTimeout(resolve, PRESS_SETTLE_MS));
        const tab = await chrome.tabs.get(cmd.tabId);
        return { url: tab.pendingUrl ?? tab.url ?? "" };
      });
    case "type":
      return actIn(cmd.tabId, {
        type: "type",
        index: cmd.args.index,
        text: cmd.args.text,
        submit: cmd.args.submit ?? false
      });
    case "scroll":
      return withDebugger(cmd.tabId, async (target) => {
        await bringToFront(cmd.tabId);
        const viewport = (await sendToTab(cmd.tabId, { type: "viewport" })) as { width: number; height: number };
        await cdpScroll(target, viewport, cmd.args.direction, cmd.args.pages ?? 1);
        return sendToTab(cmd.tabId, { type: "scrollPosition" });
      });
    case "open":
      return openTab(cmd.args.url);
    case "close":
      return closeTab(cmd.tabId);
    case "reload":
      // 先把结果回给 Go，再重载：reload 一执行，background 和这条 WebSocket 就都没了，结果就发不回去了
      setTimeout(() => chrome.runtime.reload(), 200);
      return { reloading: true };
    default:
      // 类型上不会走到这里，但 Go 那边可以发任意字符串（比如调试接口里手打错了）
      throw new CommandError("UNKNOWN_COMMAND", `不认识的命令：${String((cmd as { cmd: unknown }).cmd)}`);
  }
}

// 跳转以后的页面。loaded 为 false：等到超时也没等到 complete，页面可能还没加载完
interface PageInfo {
  url: string;
  title: string;
  loaded: boolean;
}

// 在后台开一个新标签页，不抢用户正在看的页面。先开空白页再 navigate，复用 navigate 的等待和超时
async function openTab(url: string): Promise<{ tabId: number } & PageInfo> {
  const tab = await chrome.tabs.create({ url: "about:blank", active: false });
  if (tab.id === undefined) {
    throw new CommandError("EXEC_FAILED", "新开的标签页没有 id");
  }
  const page = await navigate(tab.id, url);
  return { tabId: tab.id, ...page };
}

async function closeTab(tabId: number): Promise<{ closed: true }> {
  try {
    await chrome.tabs.remove(tabId);
  } catch {
    throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页`);
  }
  return { closed: true };
}

const NAVIGATE_TIMEOUT_MS = 10_000;

// 跳转必须由 background 来做：页面一跳走，旧页面里的 content script 就被销毁了，
// 它没法替自己等"新页面加载完"。
function navigate(tabId: number, url: string): Promise<PageInfo> {
  return loadIn(tabId, () => chrome.tabs.update(tabId, { url }));
}

// 先监听，再调用 start 发起跳转，最后等这个标签页加载完。
// complete 最多等 10 秒，等不到也算成功（loaded: false）：有的页面一直有请求挂着，complete 永远不来，
// 内容却早就显示出来了。内容有没有出来，交给 snapshot 之前的 waitForStable
async function loadIn(tabId: number, start: () => Promise<unknown>): Promise<PageInfo> {
  let cleanup = () => {};
  const loaded = new Promise<boolean>((resolve) => {
    const onFinish: ListenerOf<typeof chrome.tabs.onUpdated> = (curTabId, changeInfo) => {
      if (curTabId !== tabId || changeInfo.status !== "complete") {
        return;
      }
      cleanup();
      resolve(true);
    };

    const timer = setTimeout(() => {
      cleanup();
      resolve(false);
    }, NAVIGATE_TIMEOUT_MS);

    cleanup = () => {
      clearTimeout(timer);
      chrome.tabs.onUpdated.removeListener(onFinish);
    };

    chrome.tabs.onUpdated.addListener(onFinish); // ① 先监听
  });

  try {
    await start(); // ② 再跳转
  } catch (err) {
    cleanup(); // 跳转都没开始，不用再等了
    const text = String(err);
    if (text.includes("No tab with id")) {
      throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页`);
    }
    throw new CommandError("EXEC_FAILED", `标签页 ${tabId} 跳转失败：${text}`);
  }

  const isLoaded = await loaded; // ③ 等加载完

  const tab = await chrome.tabs.get(tabId);
  return { url: tab.url ?? "", title: tab.title ?? "", loaded: isLoaded };
}

// 点击、回车以后的结果。newTab：动作在新标签页里打开了页面（比如淘宝的搜索），Agent 要切换过去
interface ActResult {
  url: string; // 动作之后这个标签页的网址；还在跳转的话是要去的网址
  newTab?: { tabId: number } & PageInfo;
}

// 新标签页一般在动作之后几百毫秒内出现，等这么久还没有就算没有
const NEW_TAB_WAIT_MS = 1000;

// 在页面里点击或输入。动作之前先留意"从这个标签页打开的新标签页"（Chrome 会记下 openerTabId），
// 动作之后等一小会儿：有的话等它加载完，一起告诉 Go。不认识任何网站，所有网站都一样
async function actIn(tabId: number, req: InputAction): Promise<ActResult> {
  const opened = watchOpened(tabId);
  try {
    await withDebugger(tabId, async (target) => {
      await bringToFront(tabId);
      const point = (await sendToTab(tabId, {
        type: "prepareInput",
        index: req.index,
        editable: req.type === "type"
      })) as InputTarget;
      if (req.type === "hover") {
        await cdpHover(target, point);
      } else {
        await cdpClick(target, point);
      }
      if (req.type === "type") {
        await sendToTab(tabId, { type: "selectInput", index: req.index });
        await cdpType(target, req.text, req.submit);
      }
    });
    const newTabId = await opened.next(NEW_TAB_WAIT_MS);
    const tab = await chrome.tabs.get(tabId);
    const result: ActResult = { url: tab.pendingUrl ?? tab.url ?? "" };
    if (newTabId !== undefined) {
      result.newTab = { tabId: newTabId, ...(await waitLoaded(newTabId)) };
    }
    return result;
  } finally {
    opened.stop();
  }
}

// 切到前台以后，最多等这么久页面变成可见
const VISIBLE_WAIT_MS = 2000;
const VISIBLE_POLL_MS = 50;

// CDP 的鼠标、键盘事件要经过渲染才送得进页面，后台标签页不渲染，事件会被悄悄丢掉（返回成功，页面没反应）。
// 所以输入之前先把标签页切到前台，再等页面真的可见。动作做完不切回去：滚动以后的懒加载也要页面在渲染。
// 切到前台了还不可见，是整个窗口被最小化或者完全挡住了，这时报错，不假装成功
export async function bringToFront(tabId: number): Promise<void> {
  try {
    await chrome.tabs.update(tabId, { active: true });
  } catch {
    throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页`);
  }
  const deadline = Date.now() + VISIBLE_WAIT_MS;
  for (;;) {
    if ((await sendToTab(tabId, { type: "visibility" })) === "visible") {
      return;
    }
    if (Date.now() >= deadline) {
      throw new CommandError(
        "EXEC_FAILED",
        "标签页已经切到前台，但页面不可见：浏览器窗口可能被最小化或完全挡住了，需要用户把它露出来"
      );
    }
    await new Promise((resolve) => setTimeout(resolve, VISIBLE_POLL_MS));
  }
}

// 留意从 openerId 打开的第一个新标签页
function watchOpened(openerId: number): { next(ms: number): Promise<number | undefined>; stop(): void } {
  let found: number | undefined;
  let notify: ((id: number) => void) | undefined;
  const onCreated = (tab: chrome.tabs.Tab) => {
    if (tab.openerTabId !== openerId || tab.id === undefined || found !== undefined) {
      return;
    }
    found = tab.id;
    notify?.(tab.id);
  };
  chrome.tabs.onCreated.addListener(onCreated);

  return {
    // 已经有了就马上给；还没有就最多等 ms 毫秒
    next(ms) {
      if (found !== undefined) {
        return Promise.resolve(found);
      }
      return new Promise((resolve) => {
        const timer = setTimeout(() => resolve(undefined), ms);
        notify = (id) => {
          clearTimeout(timer);
          resolve(id);
        };
      });
    },
    stop() {
      chrome.tabs.onCreated.removeListener(onCreated);
    }
  };
}

// 等一个已经在加载的标签页加载完，最多 10 秒，和 navigate 一样等不到也不算失败。
// 先挂监听再查状态：查之前就加载完了的，查的时候能看到；查之后才完成的，监听能收到
async function waitLoaded(tabId: number): Promise<PageInfo> {
  let cleanup = () => {};
  const loaded = new Promise<boolean>((resolve) => {
    const onUpdated: ListenerOf<typeof chrome.tabs.onUpdated> = (curTabId, changeInfo) => {
      if (curTabId === tabId && changeInfo.status === "complete") {
        cleanup();
        resolve(true);
      }
    };
    const timer = setTimeout(() => {
      cleanup();
      resolve(false);
    }, NAVIGATE_TIMEOUT_MS);
    cleanup = () => {
      clearTimeout(timer);
      chrome.tabs.onUpdated.removeListener(onUpdated);
    };
    chrome.tabs.onUpdated.addListener(onUpdated);
  });

  let isLoaded: boolean;
  if ((await chrome.tabs.get(tabId)).status === "complete") {
    cleanup();
    isLoaded = true;
  } else {
    isLoaded = await loaded;
  }
  const tab = await chrome.tabs.get(tabId);
  return { url: tab.url ?? tab.pendingUrl ?? "", title: tab.title ?? "", loaded: isLoaded };
}

// 把请求转发给标签页里的 content script，等它回复。
// content script 是按需注入的：页面里还没有，就先注入再发一次
async function sendToTab(tabId: number, req: TabRequest): Promise<unknown> {
  try {
    return await sendOnce(tabId, req);
  } catch (err) {
    if (!(err instanceof CommandError && err.code === "NO_CONTENT_SCRIPT")) {
      throw err;
    }
  }
  await injectContentScript(tabId);
  return sendOnce(tabId, req);
}

// 正在注入的标签页。两条命令同时发现没有 content script 时，只注入一次：
// 注入两份就会有两个监听器，一次 click 会被执行两遍
const injecting = new Map<number, Promise<void>>();

function injectContentScript(tabId: number): Promise<void> {
  let pending = injecting.get(tabId);
  if (!pending) {
    pending = inject(tabId).finally(() => injecting.delete(tabId));
    injecting.set(tabId, pending);
  }
  return pending;
}

// Chrome 拒绝注入时的报错，比如 "Cannot access a chrome:// URL"、"The extensions gallery cannot be scripted"
const NOT_SCRIPTABLE = /Cannot access|cannot be scripted/i;

async function inject(tabId: number): Promise<void> {
  try {
    await chrome.scripting.executeScript({ target: { tabId }, files: ["content.js"] });
  } catch (err) {
    const text = String(err);
    if (text.includes("No tab with id")) {
      throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页`);
    }
    // chrome://、应用商店、别的插件的页面，浏览器不允许注入
    if (NOT_SCRIPTABLE.test(text)) {
      throw new CommandError("PAGE_NOT_SCRIPTABLE", `标签页 ${tabId} 的页面不允许插件注入脚本：${text}`);
    }
    // 其他情况多半是插件自己的问题，比如加载的目录里没有 content.js
    throw new CommandError("EXEC_FAILED", `往标签页 ${tabId} 注入脚本失败：${text}`);
  }
}

async function sendOnce(tabId: number, req: TabRequest): Promise<unknown> {
  let res: TabResponse;
  try {
    res = await chrome.tabs.sendMessage(tabId, req);
  } catch (err) {
    // 页面里还没有注入过 content script，或者页面跳转后旧的那份被销毁了
    if (String(err).includes("Receiving end does not exist")) {
      throw new CommandError("NO_CONTENT_SCRIPT", `标签页 ${tabId} 里没有 content script`);
    }
    if (String(err).includes("No tab with id")) {
      throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页`);
    }
    throw err;
  }
  if (!res.ok) {
    throw new CommandError(res.error.code, res.error.message);
  }
  return res.data;
}
