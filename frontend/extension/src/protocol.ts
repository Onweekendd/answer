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

// 两条线上传的消息都定义在这里：
//   Go Hub ⇄ background（WebSocket，JSON 文本）
//   background ⇄ content script（Chrome 的进程间消息）
// Go 那边的 struct 要和这里一一对应，改一边就要改另一边。

// ---------- Go → 插件：命令 ----------

// 每种命令的参数。加新命令时在这里加一行，下面的 Command 类型会自动跟着变。
export interface CommandArgs {
  navigate: { url: string };
  snapshot: { maxChars?: number };
  // 读页面上的文字（正文、评论），快照里没有这些。不填 index：开着浮层只读浮层，否则读主要内容；
  // 填了只读那个元素。最多 maxChars 个字
  read: { index?: number; maxChars?: number };
  // 截图：不填 index 截当前这一屏，填了只截那个元素。输出 jpeg，宽不超过 maxWidth 个实际像素
  screenshot: { index?: number; maxWidth?: number; quality?: number };
  // 商品卡片要的链接和商品图。不填 index 是当前页面（商品详情页）：网址和页面上最大的那张图；
  // 填了是那个元素（搜索结果里的一张卡片）：它的链接和它里面最大的那张图。只截图片，不截整屏
  product: { index?: number; maxWidth?: number; quality?: number };
  click: { index: number };
  hover: { index: number };
  // 按一个键，发给页面当前有焦点的地方。Escape 关弹窗、浮层、下拉菜单；左右方向键翻轮播图（小红书笔记的图片）
  press: { key: PressKey };
  // 在后台新开一个标签页打开 url，等它加载完，返回新标签页的 id。tabId 填 0，不会用到
  open: { url: string };
  // 关掉 tabId 这个标签页
  close: Record<string, never>;
  // 往编号为 index 的输入框里填文字；submit 为 true 时再按一下回车
  type: { index: number; text: string; submit?: boolean };
  // 上下滚动 pages 屏（默认 1），触发懒加载、看到下面的内容
  scroll: { direction: "up" | "down"; pages?: number };
  // 调试用：text 所在的元素往上一层层的样子，up 不为空时再展开往上 up 层那个元素的子树。Agent 不用，写站点策略时用
  inspect: { text: string; up?: number };
  // 插件从磁盘重新加载自己（开发用：pnpm watch 打包完自动调用）。tabId 填 0
  reload: Record<string, never>;
}

export type CommandName = keyof CommandArgs;

// 可辨识联合：判断了 cmd 以后，TS 就知道 args 是哪一种
export type Command = {
  [K in CommandName]: {
    id: string; // Go 生成，结果原样带回，用来配对
    type: "command";
    cmd: K;
    tabId: number;
    args: CommandArgs[K];
  };
}[CommandName];

// ---------- 插件 → Go：结果 ----------

export type ErrorCode =
  | "BAD_MESSAGE" // 收到的东西不是合法命令
  | "UNKNOWN_COMMAND"
  | "NOT_IMPLEMENTED"
  | "TAB_NOT_FOUND"
  | "NO_CONTENT_SCRIPT" // 标签页里没有 content script，注入也没成功
  | "PAGE_NOT_SCRIPTABLE" // chrome:// 页面、应用商店这类浏览器不允许插件注入脚本的页面
  | "ELEMENT_NOT_FOUND"
  | "NOT_EDITABLE" // type 的目标不是输入框
  | "TIMEOUT"
  | "EXEC_FAILED";

export interface ErrorBody {
  code: ErrorCode;
  message: string;
}

export type Result =
  { id: string; type: "result"; ok: true; data: unknown } | { id: string; type: "result"; ok: false; error: ErrorBody };

// ---------- 插件 → Go：主动上报的事件（没有 id） ----------

// 一个标签页。不带网址：网址里可能有搜索词，标题已经够人和模型认出是哪一页
export interface TabInfo {
  id: number;
  title: string;
}

export type AgentEvent =
  // 连接建立后的第一条消息。token 放在这里而不是 URL 里：URL 容易被打进日志
  | { type: "event"; event: "hello"; data: { version: string; token: string; tabs: TabInfo[] } }
  // 保活：MV3 的 service worker 30 秒没有活动就会被关掉，定时发一条消息能续命
  | { type: "event"; event: "ping" }
  | { type: "event"; event: "captcha_detected"; tabId: number; url: string }
  // 标签页有变化（新开、关闭、跳转、改标题）：整份列表重新报一次，Go 直接替换
  | { type: "event"; event: "tabs"; data: TabInfo[] };

export type OutgoingMessage = Result | AgentEvent;

// ---------- background ⇄ content script ----------

// 要截的区域：CSS 像素，相对整个文档（CDP 的 clip 要这样），dpr 是屏幕缩放
export interface ShotArea {
  x: number;
  y: number;
  width: number;
  height: number;
  dpr: number;
}

// 商品卡片的链接和商品图所在的区域。image 为 null：没找到够大的图
export interface ProductArea {
  url: string;
  image: ShotArea | null;
}

// press 能按的键。先只放用得上的，要加别的键在这里加
export const PRESS_KEYS = {
  Escape: { code: "Escape", keyCode: 27 },
  ArrowLeft: { code: "ArrowLeft", keyCode: 37 },
  ArrowRight: { code: "ArrowRight", keyCode: 39 }
} as const;
export type PressKey = keyof typeof PRESS_KEYS;

export interface InputTarget {
  x: number;
  y: number;
}

export type InputAction =
  { type: "click" | "hover"; index: number } | { type: "type"; index: number; text: string; submit: boolean };

export type TabRequest =
  | { type: "snapshot"; maxChars: number }
  | { type: "read"; index?: number; maxChars: number }
  | { type: "prepareInput"; index: number; editable: boolean }
  | { type: "selectInput"; index: number }
  | { type: "viewport" }
  | { type: "screenshotArea"; index?: number }
  | { type: "productArea"; index?: number }
  | { type: "inspect"; text: string; up?: number }
  | { type: "visibility" }
  | { type: "scrollPosition" };

export type TabResponse = { ok: true; data: unknown } | { ok: false; error: ErrorBody };

// 带错误码的异常，各层都用它，最后由 background 转成 Result 发给 Go
export class CommandError extends Error {
  constructor(
    readonly code: ErrorCode,
    message: string
  ) {
    super(message);
  }
}

export function toErrorBody(err: unknown): ErrorBody {
  if (err instanceof CommandError) {
    return { code: err.code, message: err.message };
  }
  return { code: "EXEC_FAILED", message: String(err) };
}
