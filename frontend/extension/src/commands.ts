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

import { type Command, CommandError, type TabRequest, type TabResponse } from "./protocol";

// 在 background 里执行 Go 发来的命令。
// 分工：跟"标签页"有关的（跳转、开关）在这里做；跟"页面内容"有关的转给 content script。

const DEFAULT_MAX_CHARS = 8000;

type ListenerOf<E> = E extends chrome.events.Event<infer F> ? F : never;

export async function execute(cmd: Command): Promise<unknown> {
  switch (cmd.cmd) {
    case "navigate":
      return navigate(cmd.tabId, cmd.args.url);
    case "snapshot":
      return sendToTab(cmd.tabId, {
        type: "snapshot",
        maxChars: cmd.args.maxChars ?? DEFAULT_MAX_CHARS
      });
    case "click":
      return sendToTab(cmd.tabId, { type: "click", index: cmd.args.index });
    case "type":
      return sendToTab(cmd.tabId, {
        type: "type",
        index: cmd.args.index,
        text: cmd.args.text,
        submit: cmd.args.submit ?? false
      });
    case "scroll":
      return sendToTab(cmd.tabId, { type: "scroll", direction: cmd.args.direction, pages: cmd.args.pages ?? 1 });
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
