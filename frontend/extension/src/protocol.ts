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
  // 页面上的原图：填 index 是那个元素里最大的图，不填是屏幕上最大的图。插件自己下载图片文件、转成 jpeg，
  // 比截图清楚（截图受屏幕缩放限制）。宽不超过 maxWidth
  image: { index?: number; maxWidth?: number; quality?: number };
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
  // 列表收割：在搜索页、瀑布流里边滚边收卡片（图、标题、链接），去掉广告和重复的，收够 limit 张或者滚了 maxScrolls 屏为止。
  // thumbs 为 true 时顺便带上缩略图本身（页面已经加载好的，不重新下载），给模型拼总览图初选
  harvest: { limit?: number; maxScrolls?: number; thumbs?: boolean };
  // 插件从磁盘重新加载自己（开发用：pnpm watch 打包完自动调用）。tabId 填 0
  reload: Record<string, never>;
  // 试验（2026-10-22 抖音）：在 tabId 里打开 url，用 CDP 听这个标签页的网络，把网址里含 match 的 JSON 返回抄下来。
  // 只听不发：请求是页面自己发的，签名、cookie 都是页面自己的。最多等 waitMs
  netcapture: { url: string; match: string; waitMs?: number };
  // 试验：在页面里下载媒体文件的一段（start 到 end 字节，含两端），带着页面的 Referer。一段最多 MEDIA_RANGE_MAX 字节
  mediaRange: { url: string; start: number; end: number };
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
  // 商品图的原始地址：有就先用原图，截图只是退路。图是画出来的、内嵌的就没有
  src?: string;
}

// 页面上一张图的原始地址和它本身的尺寸（不是显示出来的尺寸）
export interface ImageSource {
  src: string;
  naturalWidth: number;
  naturalHeight: number;
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
  | { type: "imageSource"; index?: number }
  | { type: "inspect"; text: string; up?: number }
  | { type: "visibility" }
  | { type: "scrollPosition" }
  | { type: "harvestList"; limit: number; maxScrolls: number }
  | { type: "fetchRange"; url: string; start: number; end: number };

// mediaRange 一段最多多少字节：base64 以后约 2.7MB，Hub 一条消息最多收 4MB
export const MEDIA_RANGE_MAX = 2 << 20;

// mediaRange 的结果。total 是整个文件多大：服务器没给（没暴露 Content-Range）就没有。
// HTTP 错误也是正常返回：status 是 403 这类、data 是空的（签名网址过期了要不要重新拿，由 Go 决定）
export interface MediaRange {
  status: number;
  contentType: string;
  total?: number;
  data: string; // base64
}

// netcapture 抄下来的一个返回
export interface Capture {
  url: string;
  status: number;
  body: string;
}

// 列表收割收到的一张卡片
export interface HarvestItem {
  key: string; // 去重用：站点给的 id，没有就是链接，再没有就是缩略图地址
  url?: string; // 卡片打开的那个页面（出处）
  title: string;
  thumb: string; // 现在显示的那张（缩略图）
  original: string; // srcset 里最大的那张，没有 srcset 就和 thumb 一样
  width?: number; // 显示的那张图本身的尺寸
  height?: number;
  thumbData?: string; // thumbs 为 true 时：缩略图本身，base64，拿不到就没有
}

// 收割的缩略图默认压到多宽：2026-10-20 Pinterest 的缩略图平均 59KB，压到 200 宽以后几 KB。
// 页面的增强可以要更宽的（ListSteps.thumbWidth）；预算不够时 background 也退回这个宽度
export const GENERIC_THUMB_WIDTH = 200;

export interface HarvestResult {
  items: HarvestItem[];
  stats: {
    strategy?: string; // 用了哪类页面的增强，没有就是纯通用实现
    strategyError?: string; // 哪一步的增强抛了错（已经退回通用实现）
    scrolls: number; // 滚了几屏
    seen: number; // 一共见过几张不同的卡片（含广告）
    sponsored: number; // 其中几张是广告，没收
    noImage: number; // 其中几张没有图（还没加载出来、或者是视频），没收
    ended: boolean; // 滚到底了，或者连着几屏都没有新卡片
    thumbWidth: number; // 缩略图要压到多宽（页面的增强定的，见 ListSteps.thumbWidth）
    thumbsSkipped?: number; // thumbs 为 true 时：几张没带上缩略图（缓存里没有、解不开、或者超了总量）
    ms: number;
  };
}

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
