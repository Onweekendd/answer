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
//   pnpm watch   改了 ts 自动重新打包（改 static/ 里的文件要重新运行）

import { cp, rm } from "node:fs/promises";

import * as esbuild from "esbuild";

const watch = process.argv.includes("--watch");

const common = {
  bundle: true, // 把 import 的文件合进一个文件：content script 不支持 import
  target: "chrome120",
  sourcemap: true, // DevTools 里看到的是 ts 源码
  logLevel: "info"
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
