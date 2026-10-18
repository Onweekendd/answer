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

// 把 src/*.ts 打包成 dist/*.js，再把 static/ 里的文件原样拷过去。
// Chrome 加载的是 dist/ 这个目录。
//   pnpm build   打包一次
//   pnpm watch   改了 ts 自动重新打包（改 static/ 里的文件要重新运行），
//                打包完通知插件重载自己：需要 Hub 在跑，并且设置了 BROWSER_HUB_TOKEN，否则跳过

import { cp, rm } from "node:fs/promises";

import * as esbuild from "esbuild";

const watch = process.argv.includes("--watch");
const hub = process.env.PAGEHAND_HUB ?? "http://127.0.0.1:8765";

// 改一个被三个入口共用的文件（比如 protocol.ts），三个入口会各打包一次，攒 300ms 只重载一次
let reloadTimer;
function reloadExtensionSoon() {
  clearTimeout(reloadTimer);
  reloadTimer = setTimeout(async () => {
    const token = process.env.BROWSER_HUB_TOKEN;
    if (!token) {
      console.log("[reload] 没有设置 BROWSER_HUB_TOKEN，跳过自动重载");
      return;
    }
    try {
      const res = await fetch(`${hub}/debug/call`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
        body: JSON.stringify({ tabId: 0, cmd: "reload" })
      });
      const body = await res.json();
      console.log(body.ok ? "[reload] 插件已重载" : `[reload] 没有重载：${body.error?.code}`);
    } catch {
      console.log("[reload] Hub 没在运行，跳过自动重载");
    }
  }, 300);
}

// 每次重新打包成功后通知插件重载；启动时的第一次打包不算
const reloadOnRebuild = {
  name: "reload-extension",
  setup(build) {
    let first = true;
    build.onEnd((result) => {
      if (first) {
        first = false;
        return;
      }
      if (result.errors.length === 0) {
        reloadExtensionSoon();
      }
    });
  }
};

const common = {
  bundle: true, // 把 import 的文件合进一个文件：content script 不支持 import
  target: "chrome120",
  sourcemap: true, // DevTools 里看到的是 ts 源码
  logLevel: "info",
  plugins: watch ? [reloadOnRebuild] : []
};

const entries = [
  // service worker 在 manifest 里声明了 "type": "module"，可以用 ESM
  { entryPoints: ["src/background.ts"], outfile: "dist/background.js", format: "esm" },
  // content script 和 popup 是普通脚本，用 iife 包一层，避免变量漏到全局
  { entryPoints: ["src/content.ts"], outfile: "dist/content.js", format: "iife" },
  { entryPoints: ["src/popup.ts"], outfile: "dist/popup.js", format: "iife" }
];

await rm("dist", { recursive: true, force: true });
await cp("static", "dist", { recursive: true });

if (watch) {
  for (const entry of entries) {
    const ctx = await esbuild.context({ ...common, ...entry });
    await ctx.watch();
  }
} else {
  await Promise.all(entries.map((entry) => esbuild.build({ ...common, ...entry })));
}
