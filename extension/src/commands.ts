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
  }
}
const NAVIGATE_TIMEOUT_MS = 30_000;

// 跳转必须由 background 来做：页面一跳走，旧页面里的 content script 就被销毁了，
// 它没法替自己等"新页面加载完"。
async function navigate(tabId: number, url: string): Promise<{ url: string; title: string }> {
  let cleanup = () => {};
  const loaded = new Promise<void>((resolve, reject) => {
    const onFinish: ListenerOf<typeof chrome.tabs.onUpdated> = (curTabId, changeInfo) => {
      if (curTabId !== tabId || changeInfo.status !== "complete") {
        return;
      }
      cleanup();
      resolve();
    };

    const timer = setTimeout(() => {
      cleanup();
      reject(new CommandError("TIMEOUT", `标签页 ${tabId} ${NAVIGATE_TIMEOUT_MS / 1000} 秒内没有加载完`));
    }, NAVIGATE_TIMEOUT_MS);

    cleanup = () => {
      clearTimeout(timer);
      chrome.tabs.onUpdated.removeListener(onFinish);
    };

    chrome.tabs.onUpdated.addListener(onFinish); // ① 先监听
  });

  try {
    await chrome.tabs.update(tabId, { url }); // ② 再跳转
  } catch (err) {
    cleanup(); // 跳转都没开始，不用再等了
    loaded.catch(() => {}); // loaded 不会再被 await，免得控制台报 Uncaught (in promise)
    throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页：${String(err)}`);
  }

  await loaded; // ③ 等加载完

  const tab = await chrome.tabs.get(tabId);
  return { url: tab.url ?? url, title: tab.title ?? "" };
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

async function inject(tabId: number): Promise<void> {
  try {
    await chrome.scripting.executeScript({ target: { tabId }, files: ["content.js"] });
  } catch (err) {
    if (String(err).includes("No tab with id")) {
      throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页`);
    }
    // chrome://、应用商店、别的插件的页面，浏览器不允许注入
    throw new CommandError("PAGE_NOT_SCRIPTABLE", `标签页 ${tabId} 的页面不允许插件注入脚本：${String(err)}`);
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
