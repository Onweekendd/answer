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

import { cdpClick, cdpHover, cdpPress, cdpResource, cdpScreenshot, cdpScroll, cdpType, withDebugger } from "./cdp";
import { downloadImage, isRaster, toJpeg } from "./imagefile";
import {
  type Capture,
  type Command,
  type CommandArgs,
  CommandError,
  GENERIC_THUMB_WIDTH,
  type HarvestResult,
  type ImageSource,
  type InputAction,
  type InputTarget,
  type ProductArea,
  type ShotArea,
  type TabRequest,
  type TabResponse
} from "./protocol";
import { RATE_LIMIT, siteRateLimiter } from "./rate-limit";

// 在 background 里执行 Go 发来的命令。
// 分工：跟"标签页"有关的（跳转、开关）在这里做；跟"页面内容"有关的转给 content script。

const DEFAULT_MAX_CHARS = 8000;
const DEFAULT_READ_CHARS = 3000;
// 截图的默认尺寸和质量，Go 一般会自己指定
const DEFAULT_SHOT_WIDTH = 1600;
const DEFAULT_SHOT_QUALITY = 80;
// 商品卡片的图只当缩略图用，不用看清小字
const DEFAULT_PRODUCT_WIDTH = 480;
// 下载的原图：要看清图里的字，比截图宽
const DEFAULT_IMAGE_WIDTH = 1600;
const DEFAULT_IMAGE_QUALITY = 85;
// 按键以后等多久再读网址
const PRESS_SETTLE_MS = 500;
// 列表收割：默认收多少张、最多滚几屏。上限防止一条命令把页面滚上几分钟
const DEFAULT_HARVEST_LIMIT = 100;
const MAX_HARVEST_LIMIT = 400;
const DEFAULT_HARVEST_SCROLLS = 3;
const MAX_HARVEST_SCROLLS = 5;
// 取缩略图：几百张从缓存里读、缩小，每张几毫秒到十几毫秒
const HARVEST_THUMBS_TIMEOUT_MS = 60_000;
// 缩略图的质量。压到多宽由页面的增强定（stats.thumbWidth），预算不够时退回 GENERIC_THUMB_WIDTH
const HARVEST_THUMB_QUALITY = 70;
// 一条结果里缩略图的 base64 加起来最多多少，留出余量给其余字段
const HARVEST_THUMBS_BUDGET = 3 << 20;

type ListenerOf<E> = E extends chrome.events.Event<infer F> ? F : never;

// 同一标签页串行执行，避免输入、快照和 debugger 会话相互干扰。
const queues = new Map<number, Promise<unknown>>();
const LIMITED_COMMANDS = new Set([
  "navigate",
  "open",
  "click",
  "hover",
  "press",
  "type",
  "scroll",
  "harvest",
  "screenshot",
  "product",
  "image",
  "netcapture",
  "mediaRange"
]);

export async function execute(cmd: Command): Promise<unknown> {
  const deadline = Date.now() + RATE_LIMIT.maxQueueWaitMs;
  const previous = queues.get(cmd.tabId) ?? Promise.resolve();
  const next = previous
    .catch(() => {})
    .then(async () => {
      // 纯读取不占额度；未知命令交给原来的分发器报告 UNKNOWN_COMMAND。
      if (!LIMITED_COMMANDS.has(cmd.cmd)) {
        return executeCommand(cmd);
      }
      const url =
        cmd.cmd === "open" || cmd.cmd === "navigate" || cmd.cmd === "netcapture"
          ? cmd.args.url
          : await tabUrl(cmd.tabId);
      return siteRateLimiter.run(url, () => executeCommand(cmd), deadline);
    });
  queues.set(cmd.tabId, next);
  try {
    return await next;
  } finally {
    if (queues.get(cmd.tabId) === next) {
      queues.delete(cmd.tabId);
    }
  }
}

export async function tabUrl(tabId: number): Promise<string> {
  try {
    const tab = await chrome.tabs.get(tabId);
    return tab.pendingUrl ?? tab.url ?? "about:blank";
  } catch {
    throw new CommandError("TAB_NOT_FOUND", `没有 id 为 ${tabId} 的标签页`);
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
        const maxWidth = cmd.args.maxWidth ?? DEFAULT_PRODUCT_WIDTH;
        const quality = cmd.args.quality ?? DEFAULT_SHOT_QUALITY;
        // 先用原图：截图受屏幕缩放限制，小图放进卡片是糊的；原图拿不到（防盗链、格式不对）再截
        let image = found.src
          ? await downloadImage(found.src, maxWidth, quality, { fromPage: (src) => cdpResource(target, src) }).catch(
              () => null
            )
          : null;
        if (!image && found.image) {
          image = await cdpScreenshot(target, found.image, maxWidth, quality);
        }
        const tab = await chrome.tabs.get(cmd.tabId);
        return { url: found.url, title: tab.title ?? "", image };
      });
    case "image":
      return withDebugger(cmd.tabId, async (target) => {
        const found = (await sendToTab(cmd.tabId, { type: "imageSource", index: cmd.args.index })) as ImageSource;
        const image = await downloadImage(
          found.src,
          cmd.args.maxWidth ?? DEFAULT_IMAGE_WIDTH,
          cmd.args.quality ?? DEFAULT_IMAGE_QUALITY,
          { fromPage: (src) => cdpResource(target, src) }
        );
        const tab = await chrome.tabs.get(cmd.tabId);
        return { ...image, url: tab.url ?? "", src: found.src };
      });
    case "inspect":
      return sendToTab(cmd.tabId, { type: "inspect", text: cmd.args.text, up: cmd.args.up });
    case "harvest":
      return harvest(cmd.tabId, cmd.args);
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
        const pages = cmd.args.pages ?? 1;
        if (!Number.isFinite(pages) || pages <= 0 || pages > 5) {
          throw new CommandError("BAD_MESSAGE", "一次最多滚动 5 屏，pages 必须大于 0");
        }
        await bringToFront(cmd.tabId);
        const viewport = (await sendToTab(cmd.tabId, { type: "viewport" })) as { width: number; height: number };
        for (let remaining = pages; remaining > 0; remaining -= 1) {
          if (remaining < pages) {
            await siteRateLimiter.acquire(await tabUrl(cmd.tabId));
          }
          await cdpScroll(target, viewport, cmd.args.direction, Math.min(1, remaining));
        }
        return sendToTab(cmd.tabId, { type: "scrollPosition" });
      });
    case "open":
      return openTab(cmd.args.url);
    case "close":
      return closeTab(cmd.tabId);
    case "netcapture":
      return netCapture(cmd.tabId, cmd.args);
    case "mediaRange":
      return sendToTab(cmd.tabId, {
        type: "fetchRange",
        url: cmd.args.url,
        start: cmd.args.start,
        end: cmd.args.end
      });
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
  // 新标签页还没加载完就自己关掉了：多半是文件下载（2026-10-21 秘塔来源里的「管理办法」，Chrome 开了又关）
  newTabClosed?: boolean;
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
      try {
        result.newTab = { tabId: newTabId, ...(await waitLoaded(newTabId)) };
      } catch {
        // waitLoaded 里 chrome.tabs.get 找不到它：标签页已经没了，不是动作失败
        result.newTabClosed = true;
      }
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

// harvest 列表收割：先切到前台（后台标签页不渲染，滚动以后不出新卡片），滚动和收集在页面里做；
// 要缩略图的话，再从页面已经加载好的资源里取，不重新下载
async function harvest(tabId: number, args: CommandArgs["harvest"]): Promise<HarvestResult> {
  await bringToFront(tabId);
  const result = (await sendToTab(tabId, {
    type: "harvestList",
    limit: Math.min(args.limit ?? DEFAULT_HARVEST_LIMIT, MAX_HARVEST_LIMIT),
    maxScrolls: Math.min(args.maxScrolls ?? DEFAULT_HARVEST_SCROLLS, MAX_HARVEST_SCROLLS)
  })) as HarvestResult;
  if (args.thumbs) {
    // 缩略图压到 stats.thumbWidth 宽：一般是 200，只用来拼总览图初选；页面上拿不到更大的图时更宽，它就是素材本身。
    // 一条结果总共不超过 HARVEST_THUMBS_BUDGET：Hub 一条消息最多收 4MB，超了它会断开连接。
    // 宽图放不下了就退回 200 宽：素材小一点，总览图还是齐的
    let budget = HARVEST_THUMBS_BUDGET;
    let skipped = 0;
    await withDebugger(
      tabId,
      async (target) => {
        for (const item of result.items) {
          const bytes = await cdpResource(target, item.thumb);
          if (!bytes || !isRaster(bytes) || budget <= 0) {
            skipped++;
            continue;
          }
          try {
            const blob = new Blob([new Uint8Array(bytes)]);
            let data = (await toJpeg(blob, result.stats.thumbWidth, HARVEST_THUMB_QUALITY)).data;
            if (data.length > budget && result.stats.thumbWidth > GENERIC_THUMB_WIDTH) {
              data = (await toJpeg(blob, GENERIC_THUMB_WIDTH, HARVEST_THUMB_QUALITY)).data;
            }
            if (data.length > budget) {
              skipped++;
              continue;
            }
            item.thumbData = data;
            budget -= data.length;
          } catch {
            skipped++;
          }
        }
      },
      HARVEST_THUMBS_TIMEOUT_MS
    );
    result.stats.thumbsSkipped = skipped;
  }
  return result;
}

// netcapture 默认等多久、最多等多久；抄下来的返回最多多少字（Hub 一条消息最多收 4MB）
const DEFAULT_CAPTURE_MS = 8000;
const MAX_CAPTURE_MS = 30_000;
const CAPTURE_BUDGET = 3 << 20;

// netCapture 打开 url，把网址里含 match 的 JSON 返回抄下来。
// 先挂上监听再跳转；返回要等 loadingFinished 以后才取得到内容，取早了 CDP 报 "No data found"。
// 抄到一份以后再多等 1 秒（同一页常常连着来几份），或者等满 waitMs
async function netCapture(tabId: number, args: CommandArgs["netcapture"]): Promise<{ captures: Capture[] }> {
  const waitMs = Math.min(args.waitMs ?? DEFAULT_CAPTURE_MS, MAX_CAPTURE_MS);
  return withDebugger(
    tabId,
    async (target) => {
      const pending = new Map<string, { url: string; status: number }>();
      const captures: Capture[] = [];
      let budget = CAPTURE_BUDGET;
      let firstAt = 0;
      const onEvent = (source: chrome.debugger.Debuggee, method: string, params?: object) => {
        if (source.tabId !== tabId) {
          return;
        }
        const p = params as {
          requestId: string;
          response?: { url: string; status: number; mimeType: string };
        };
        if (method === "Network.responseReceived" && p.response) {
          if (p.response.url.includes(args.match) && p.response.mimeType.includes("json")) {
            pending.set(p.requestId, { url: p.response.url, status: p.response.status });
          }
        } else if (method === "Network.loadingFinished" && pending.has(p.requestId)) {
          const info = pending.get(p.requestId)!;
          pending.delete(p.requestId);
          void chrome.debugger
            .sendCommand(target, "Network.getResponseBody", { requestId: p.requestId })
            .then((res) => {
              const { body } = res as { body: string };
              if (body.length <= budget) {
                budget -= body.length;
                captures.push({ ...info, body });
                firstAt ||= Date.now();
              }
            })
            .catch(() => {});
        }
      };
      chrome.debugger.onEvent.addListener(onEvent);
      try {
        await chrome.debugger.sendCommand(target, "Network.enable");
        await navigate(tabId, args.url);
        const deadline = Date.now() + waitMs;
        while (Date.now() < deadline && !(firstAt && Date.now() - firstAt > 1000)) {
          await new Promise((resolve) => setTimeout(resolve, 200));
        }
        return { captures };
      } finally {
        chrome.debugger.onEvent.removeListener(onEvent);
        await chrome.debugger.sendCommand(target, "Network.disable").catch(() => {});
      }
    },
    NAVIGATE_TIMEOUT_MS + MAX_CAPTURE_MS + 5000
  );
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
