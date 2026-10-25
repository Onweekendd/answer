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

import { execute, tabUrl } from "./commands";
import { HubConnection } from "./connection";
import { type Command, type RateLimitRequest, type TabInfo, type TabResponse, toErrorBody } from "./protocol";
import { siteRateLimiter } from "./rate-limit";
import { loadConfig } from "./storage";

// 插件的"后台进程"（MV3 service worker）。
//
// 它的生命周期和普通网页不一样，写代码前一定要知道：
//   - Chrome 会在它空闲约 30 秒后把它关掉，有事件时再重新启动它；
//   - 每次启动，这个文件都会从头执行一遍，内存里的变量全部重来；
//   - 所以事件监听必须在顶层同步注册。写在 await 后面的监听，被唤醒时可能来不及注册，事件就丢了。

// 只报普通网页。chrome:// 页面、新标签页这些插件本来就操作不了
const WEB_PAGES = ["http://*/*", "https://*/*"];
// 打开一个网页，onUpdated 会连着触发好几次（loading、标题、图标、complete），攒一会儿再报
const TABS_REPORT_DELAY_MS = 300;

const conn = new HubConnection({
  onOpen: () => void sendHello(),
  onMessage: (text) => void handleMessage(text)
});

async function listTabs(): Promise<TabInfo[]> {
  const tabs = await chrome.tabs.query({ url: WEB_PAGES });
  return tabs.flatMap((t) => (t.id === undefined ? [] : [{ id: t.id, title: t.title ?? "" }]));
}

async function sendHello(): Promise<void> {
  const { token } = await loadConfig();
  conn.send({
    type: "event",
    event: "hello",
    data: { version: chrome.runtime.getManifest().version, token, tabs: await listTabs() }
  });
}

let tabsReportTimer: ReturnType<typeof setTimeout> | undefined;

function reportTabsSoon(): void {
  clearTimeout(tabsReportTimer);
  tabsReportTimer = setTimeout(() => {
    void listTabs().then((tabs) => conn.send({ type: "event", event: "tabs", data: tabs }));
  }, TABS_REPORT_DELAY_MS);
}

async function handleMessage(text: string): Promise<void> {
  let cmd: Command;
  try {
    cmd = JSON.parse(text) as Command;
  } catch {
    console.warn("hub sent a non-JSON message, ignored");
    return;
  }
  if (cmd.type !== "command" || typeof cmd.id !== "string") {
    console.warn("hub sent an unexpected message, ignored");
    return; // 没有 id 就没法回复，只能忽略
  }

  // 只记命令名，不记参数：参数里可能有用户的搜索词
  console.debug("command", cmd.id, cmd.cmd);
  try {
    const data = await execute(cmd);
    conn.send({ id: cmd.id, type: "result", ok: true, data });
  } catch (err) {
    conn.send({ id: cmd.id, type: "result", ok: false, error: toErrorBody(err) });
  }
}

// ---- 下面都是顶层同步注册 ----

chrome.runtime.onMessage.addListener((message: RateLimitRequest, sender, respond) => {
  if (
    message?.type !== "rateLimit" ||
    message.action !== "harvestScroll" ||
    sender.tab?.id === undefined ||
    sender.frameId !== 0
  ) {
    return;
  }
  void tabUrl(sender.tab.id)
    .then((url) => siteRateLimiter.acquire(url))
    .then(
      () => respond({ ok: true, data: null } satisfies TabResponse),
      (err) => respond({ ok: false, error: toErrorBody(err) } satisfies TabResponse)
    );
  return true;
});

// 没连上时 send 会直接丢掉；不要紧，重连后的 hello 会带上完整的列表
chrome.tabs.onCreated.addListener(reportTabsSoon);
chrome.tabs.onRemoved.addListener(reportTabsSoon);
chrome.tabs.onUpdated.addListener((_tabId, info) => {
  if (info.url !== undefined || info.title !== undefined) {
    reportTabsSoon();
  }
});

// 用户在 popup 里改了 Hub 地址或 token：用新配置重连
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === "local" && (changes.hubUrl || changes.token)) {
    void conn.reconnect();
  }
});

// 保底：万一 worker 被关掉、连接也断了，闹钟每 30 秒把 worker 叫醒一次，
// 醒来时顶层的 connect() 会再跑一遍。闹钟本身由浏览器保管，worker 被关掉也不会丢。
chrome.alarms.create("ensure-connected", { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === "ensure-connected") {
    void conn.connect();
  }
});

// 每次 worker 启动（装上、浏览器启动、被事件唤醒）都会执行到这里
void conn.connect();
