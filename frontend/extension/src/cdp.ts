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

import { CommandError, type InputTarget, PRESS_KEYS, type PressKey, type ShotArea } from "./protocol";

// 一个动作最多做多久。比 Go 那边的工具超时（30 秒）短：插件自己先放弃，才能断开会话、让后面排队的命令接着走，
// 否则卡住的那条（比如后台标签页不出帧，滚动手势一直等）会把这个标签页的队列整个堵住
export const ACTION_TIMEOUT_MS = 20_000;
// 空闲这么久才断开。会话是跨动作保留的：每次动作都挂上、断开，浏览器顶部的"正在调试"提示条会跟着一出一收，
// 页面高度跟着变，刚算好的点击坐标就偏了
export const IDLE_DETACH_MS = 120_000;
// 刚挂上时提示条要过一会儿才出现，等它出来、页面高度定下来再算坐标
export const ATTACH_SETTLE_MS = 300;

const attached = new Set<number>();
const idleTimers = new Map<number, ReturnType<typeof setTimeout>>();
let listening = false;

// 会话被别人断开的：标签页关了、用户点了提示条上的"取消"、打开了开发者工具
function listenDetach(): void {
  if (listening) {
    return;
  }
  listening = true;
  chrome.debugger.onDetach.addListener((source) => {
    if (source.tabId !== undefined) {
      forget(source.tabId);
    }
  });
}

function forget(tabId: number): void {
  attached.delete(tabId);
  clearTimeout(idleTimers.get(tabId));
  idleTimers.delete(tabId);
}

async function detach(tabId: number): Promise<void> {
  forget(tabId);
  await chrome.debugger.detach({ tabId }).catch(() => {});
}

async function ensureAttached(target: chrome.debugger.Debuggee & { tabId: number }): Promise<void> {
  if (attached.has(target.tabId)) {
    return;
  }
  listenDetach();
  try {
    await chrome.debugger.attach(target, "1.3");
  } catch (err) {
    throw new CommandError("EXEC_FAILED", `CDP attach failed for tab ${target.tabId}: ${String(err)}`);
  }
  attached.add(target.tabId);
  await new Promise((resolve) => setTimeout(resolve, ATTACH_SETTLE_MS));
}

// 在 tabId 上执行一个 CDP 动作：没挂上就先挂上，做完不断开，空闲 IDLE_DETACH_MS 以后再断开。
// 超过 ACTION_TIMEOUT_MS 就断开会话：还挂着的 sendCommand 会因此失败，不会在背后接着点
export async function withDebugger<T>(
  tabId: number,
  run: (target: chrome.debugger.Debuggee) => Promise<T>,
  timeoutMs = ACTION_TIMEOUT_MS
): Promise<T> {
  const target = { tabId };
  clearTimeout(idleTimers.get(tabId));
  idleTimers.delete(tabId);
  await ensureAttached(target);

  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(
      () => reject(new CommandError("TIMEOUT", `标签页 ${tabId} 的输入 ${timeoutMs}ms 没有完成`)),
      timeoutMs
    );
  });
  const running = run(target);
  running.catch(() => {}); // 超时以后它才失败，没人等它了
  try {
    return await Promise.race([running, timeout]);
  } catch (err) {
    if (err instanceof CommandError && err.code === "TIMEOUT") {
      await detach(tabId);
    }
    throw err;
  } finally {
    clearTimeout(timer);
    if (attached.has(tabId)) {
      idleTimers.set(
        tabId,
        setTimeout(() => void detach(tabId), IDLE_DETACH_MS)
      );
    }
  }
}

export async function cdpHover(target: chrome.debugger.Debuggee, point: InputTarget): Promise<void> {
  await chrome.debugger.sendCommand(target, "Input.dispatchMouseEvent", { type: "mouseMoved", ...point });
}

export async function cdpClick(target: chrome.debugger.Debuggee, point: InputTarget): Promise<void> {
  await cdpHover(target, point);
  await chrome.debugger.sendCommand(target, "Input.dispatchMouseEvent", {
    type: "mousePressed",
    ...point,
    button: "left",
    buttons: 1,
    clickCount: 1
  });
  await chrome.debugger.sendCommand(target, "Input.dispatchMouseEvent", {
    type: "mouseReleased",
    ...point,
    button: "left",
    buttons: 0,
    clickCount: 1
  });
}

// 按一个键：一次按下、一次松开。不带 text，不会往输入框里输入字符
export async function cdpPress(target: chrome.debugger.Debuggee, key: PressKey): Promise<void> {
  const spec = PRESS_KEYS[key];
  if (!spec) {
    throw new CommandError("BAD_MESSAGE", `Unsupported key ${String(key)}`);
  }
  const event = { key, code: spec.code, windowsVirtualKeyCode: spec.keyCode };
  await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", { type: "rawKeyDown", ...event });
  await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", { type: "keyUp", ...event });
}

export async function cdpType(target: chrome.debugger.Debuggee, text: string, submit: boolean): Promise<void> {
  // Replace the selected value; insertText supports Chinese text without synthetic DOM events.
  if (text) {
    await chrome.debugger.sendCommand(target, "Input.insertText", { text });
  } else {
    await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", {
      type: "rawKeyDown",
      key: "Backspace",
      code: "Backspace",
      windowsVirtualKeyCode: 8
    });
    await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", {
      type: "keyUp",
      key: "Backspace",
      code: "Backspace",
      windowsVirtualKeyCode: 8
    });
  }
  if (submit) {
    // One Enter sequence, without a second form submission.
    await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", {
      type: "keyDown",
      key: "Enter",
      code: "Enter",
      windowsVirtualKeyCode: 13,
      text: "\r",
      unmodifiedText: "\r"
    });
    await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", {
      type: "keyUp",
      key: "Enter",
      code: "Enter",
      windowsVirtualKeyCode: 13
    });
  }
}

// 截图参数。clip 用 CSS 像素；输出的实际像素 = 宽 × scale × dpr，scale 让它不超过 maxWidth。
// 小区域不放大（scale 最大 1）：放大不会多出细节
export function screenshotParams(area: ShotArea, maxWidth: number, quality: number) {
  if (!(maxWidth >= 64 && maxWidth <= 4096) || !(quality >= 1 && quality <= 100)) {
    throw new CommandError("BAD_MESSAGE", "Invalid screenshot maxWidth or quality");
  }
  if (!(area.width > 0 && area.height > 0 && area.dpr > 0)) {
    throw new CommandError("EXEC_FAILED", "截图区域是空的");
  }
  const scale = Math.min(1, maxWidth / (area.width * area.dpr));
  return {
    format: "jpeg",
    quality: Math.round(quality),
    captureBeyondViewport: false,
    clip: { x: area.x, y: area.y, width: area.width, height: area.height, scale }
  };
}

export interface Shot {
  data: string; // base64，不带 data: 前缀
  mime: string;
  width: number; // 输出图片的实际像素
  height: number;
}

// 用 CDP 截图：Chrome 直接输出缩小过的 jpeg，不用在插件里再解码、缩放、编码一遍
export async function cdpScreenshot(
  target: chrome.debugger.Debuggee,
  area: ShotArea,
  maxWidth: number,
  quality: number
): Promise<Shot> {
  const params = screenshotParams(area, maxWidth, quality);
  const res = (await chrome.debugger.sendCommand(target, "Page.captureScreenshot", params)) as { data?: string };
  if (!res?.data) {
    throw new CommandError("EXEC_FAILED", "截图没有返回图片");
  }
  const factor = params.clip.scale * area.dpr;
  return {
    data: res.data,
    mime: "image/jpeg",
    width: Math.round(area.width * factor),
    height: Math.round(area.height * factor)
  };
}

export async function cdpScroll(
  target: chrome.debugger.Debuggee,
  viewport: { width: number; height: number },
  direction: "up" | "down",
  pages: number
): Promise<void> {
  if (!Number.isFinite(pages) || pages <= 0 || !["up", "down"].includes(direction)) {
    throw new CommandError("BAD_MESSAGE", "Invalid scroll direction or page count");
  }
  await chrome.debugger.sendCommand(target, "Input.synthesizeScrollGesture", {
    x: viewport.width / 2,
    y: viewport.height / 2,
    yDistance: (direction === "down" ? -1 : 1) * pages * viewport.height * 0.9,
    gestureSourceType: "mouse",
    preventFling: true,
    speed: 1500
  });
}
