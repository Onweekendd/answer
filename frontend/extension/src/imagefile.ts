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

import type { Shot } from "./cdp";
import { CommandError } from "./protocol";

// 页面上的原图，转成 jpeg（阶段 3 的引用图片）。
// 先拿页面已经加载好的那份（CDP Page.getResourceContent）：不用再下载，也不怕防盗链。
// 2026-10-20 小红书笔记的图在 background 里重新下载失败：后台的请求不带页面的 Referer，图床拒绝了。
// 拿不到（比如图在 iframe 里）再自己下载；插件有 <all_urls> 权限，不受跨域限制。
// 统一转成 jpeg：服务器只收 jpeg（webp、png 也转），透明的地方铺白底

// 原图最大多少字节：再大的不是商品图
const MAX_SOURCE_BYTES = 15 << 20;
// 转出来的 jpeg 不超过多少字节：服务器最多收 2MB，经过 Hub 时还要变成 base64
const MAX_JPEG_BYTES = 1536 << 10;
// 太大时降到这个质量再试一次
const FALLBACK_QUALITY = 60;

export interface ImageSources {
  // 页面已经加载好的那份，拿不到返回 null
  fromPage?: (src: string) => Promise<Uint8Array | null>;
  fetch?: typeof fetch;
}

export async function downloadImage(
  src: string,
  maxWidth: number,
  quality: number,
  sources: ImageSources = {}
): Promise<Shot> {
  if (!(maxWidth >= 64 && maxWidth <= 4096) || !(quality >= 1 && quality <= 100)) {
    throw new CommandError("BAD_MESSAGE", "Invalid image maxWidth or quality");
  }
  const bytes =
    (await sources.fromPage?.(src)) ?? (await fetchBytes(src, sources.fetch ?? ((...args) => fetch(...args))));
  if (bytes.length > MAX_SOURCE_BYTES) {
    throw new CommandError("EXEC_FAILED", "图片太大，可以用截图");
  }
  // 看文件头，不信 Content-Type：svg 能带脚本，html 错误页也常常标着 image/*
  if (!isRaster(bytes)) {
    throw new CommandError("EXEC_FAILED", "拿到的不是照片格式的图片（可能是 svg 或者错误页），可以用截图");
  }
  return toJpeg(new Blob([bytes as BlobPart]), maxWidth, quality);
}

async function fetchBytes(src: string, doFetch: typeof fetch): Promise<Uint8Array> {
  let res: Response;
  try {
    // 不带这个浏览器的 cookie：图片是公开的，没必要把账号带出去
    res = await doFetch(src, { credentials: "omit" });
  } catch {
    throw new CommandError("EXEC_FAILED", "图片下载失败，可以用截图");
  }
  if (!res.ok) {
    throw new CommandError("EXEC_FAILED", `图片下载失败（HTTP ${res.status}），可以用截图`);
  }
  return new Uint8Array(await res.arrayBuffer());
}

// isRaster 按文件头认 jpeg、png、gif、webp、avif、bmp
export function isRaster(b: Uint8Array): boolean {
  const ascii = (from: number, to: number) => String.fromCharCode(...b.subarray(from, to));
  return (
    (b[0] === 0xff && b[1] === 0xd8) ||
    (b[0] === 0x89 && ascii(1, 4) === "PNG") ||
    ascii(0, 3) === "GIF" ||
    (ascii(0, 4) === "RIFF" && ascii(8, 12) === "WEBP") ||
    (ascii(4, 8) === "ftyp" && /^avi[fs]$/.test(ascii(8, 12))) ||
    ascii(0, 2) === "BM"
  );
}

export async function toJpeg(blob: Blob, maxWidth: number, quality: number): Promise<Shot> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(blob);
  } catch {
    throw new CommandError("EXEC_FAILED", "图片解不开，可以用截图");
  }
  const scale = Math.min(1, maxWidth / bitmap.width);
  const width = Math.max(1, Math.round(bitmap.width * scale));
  const height = Math.max(1, Math.round(bitmap.height * scale));
  const canvas = new OffscreenCanvas(width, height);
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    throw new CommandError("EXEC_FAILED", "没法转换图片");
  }
  ctx.fillStyle = "#fff";
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(bitmap, 0, 0, width, height);
  bitmap.close();

  let jpeg = await canvas.convertToBlob({ type: "image/jpeg", quality: quality / 100 });
  if (jpeg.size > MAX_JPEG_BYTES) {
    jpeg = await canvas.convertToBlob({ type: "image/jpeg", quality: FALLBACK_QUALITY / 100 });
  }
  if (jpeg.size > MAX_JPEG_BYTES) {
    throw new CommandError("EXEC_FAILED", "图片转出来还是太大，可以用截图");
  }
  return { data: toBase64(new Uint8Array(await jpeg.arrayBuffer())), mime: "image/jpeg", width, height };
}

export function toBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunk = 0x8000; // 一次转太多，String.fromCharCode 的参数会超过上限
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}
