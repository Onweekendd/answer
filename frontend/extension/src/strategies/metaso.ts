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

import type { PageProfile } from "./profile";

// 秘塔 AI 搜索（2026-10-21 用 inspect 看的 metaso.cn/?q=…，提问以后网址变成 /chat/<id>）：
//   - 回答正文在 div#search-content-container-<id> 里的 .markdown-body；同一屏还有「全网 文库 学术」
//     「生成幻灯片 展示海报」「深度研究 互动网页」这些按钮，read 整页会把它们的字混进来
//   - 来源是 .search-origin-box：一个网站图标加一行标题，**不是 <a>**，点击由页面的脚本处理，
//     所以通用规则不把它编进快照，模型想点开原文也点不到。同一个来源会出现两次（正文旁和右边栏），看不见的那份快照会滤掉
//   - 回答是边生成边显示的，要二三十秒才写完；写完了下面才出现「生成幻灯片 展示海报」那排按钮，来源列表也是最后才出来
const ANSWER_BLOCK = '[id^="search-content-container"]';
const ANSWER = `${ANSWER_BLOCK} .markdown-body`;
// 回答写完以后，下面才出现「生成幻灯片 展示海报」这一排按钮（data-testid=SearchResultGenerateButtonGroup…）
const ANSWER_DONE = '[data-testid^="SearchResultGenerateButtonGroup"]';
const SOURCE_LIST = ".search-origin-list-box";
const SOURCE = ".search-origin-box";

export const metaso: PageProfile = {
  name: "metaso",
  matches: (url) => url.hostname === "metaso.cn" || url.hostname === "www.metaso.cn",
  snapshot: {
    candidates: (doc) => Array.from(doc.querySelectorAll(SOURCE)),
    // 来源里只有一行标题，前面加上「来源」：模型一眼看出点它是去读原文
    label: (el) => {
      const title = el.textContent?.replace(/\s+/g, " ").trim();
      return title ? `来源：${title}` : undefined;
    }
  },
  read: {
    // 一个对话里追问几次就有几段回答，都读；来源列表接在后面
    main: (doc) => [...doc.querySelectorAll(ANSWER), doc.querySelector(SOURCE_LIST)],
    // 每段回答写完都有自己那排按钮：按钮比回答少，就是最后一段还在写。还没提过问的页面（首页）没有回答，不用等
    ready: (doc) => doc.querySelectorAll(ANSWER_BLOCK).length <= doc.querySelectorAll(ANSWER_DONE).length
  }
};
