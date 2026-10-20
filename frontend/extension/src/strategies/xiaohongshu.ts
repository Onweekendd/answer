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

export const xiaohongshu: PageProfile = {
  name: "xiaohongshu-note",
  matches: (url) => url.hostname === "www.xiaohongshu.com",
  snapshot: {
    candidates: (doc) =>
      // 笔记浮层左边的图片区（2026-10-19 实测）。它不能点，编进快照是为了让 screenshot 填它的编号、只截图片：
      // 很多笔记的正文写在图里，整屏截图里图片只占一半宽，字密的图看不清。
      // 翻页用 press ArrowRight / ArrowLeft
      Array.from(doc.querySelectorAll("#noteContainer .media-container")),
    // 它自己的文字只有右下角的"2/6"，单图笔记连这个都没有，快照里就是一行空的 <div>，模型认不出来（2026-10-19 实测漏看）
    label: (el) => {
      if (el.querySelector("video")) {
        return "笔记视频";
      }
      const fraction = el.querySelector(".fraction")?.textContent?.trim();
      return fraction ? `笔记图片 第${fraction}张` : "笔记图片（1 张）";
    }
  }
};
