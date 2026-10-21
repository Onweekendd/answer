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

// 列表页的卡片（2026-10-21 用 inspect 看的搜索页 /search_result?keyword=…，首页发现流 /explore 也是这个组件）：
//   - 每张卡片是 section.note-item[data-note-id=笔记 id]，data-width、data-height 是原图尺寸
//   - 封面 a.cover 的 href 是 /search_result/<id>?xsec_token=…&xsec_source=…，图是它里面的 img[data-xhs-img]；
//     旁边还有一个隐藏的 a[href=/explore/<id>]，不带 token，打不开（300031「当前笔记暂时无法浏览」）
//   - 标题是 div.footer 里的 a.title；同一个 footer 里还有 a.author（作者昵称和头像），
//     通用实现取整张卡片的文字会把昵称收进来，所以标题只取 a.title
//   - 「大家都在搜」也是一个 section.note-item，里面是 div.query-note-wrapper、没有 a.cover，data-note-id 不是笔记 id
//   - 广告卡片这次没看到，长什么样不猜
const NOTE_CARD = "section.note-item";
const COVER = "a.cover";

// noteLink 是卡片打开的笔记页：换成分享出去的那种 /explore/<id>，带上 xsec_token。
// 不带 token 的笔记页打不开，没有 token 时照样给出 /explore/<id>，至少知道是哪篇
function noteLink(card: Element): string | undefined {
  const href = card.querySelector(COVER)?.getAttribute("href");
  if (!href) {
    return undefined;
  }
  const from = new URL(href, "https://www.xiaohongshu.com");
  const id = from.pathname.split("/").filter(Boolean).pop();
  if (!id) {
    return undefined;
  }
  const to = new URL(`/explore/${id}`, "https://www.xiaohongshu.com");
  for (const key of ["xsec_token", "xsec_source"]) {
    const value = from.searchParams.get(key);
    if (value) {
      to.searchParams.set(key, value);
    }
  }
  return to.href;
}

export const xiaohongshu: PageProfile = {
  name: "xiaohongshu",
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
  },
  list: {
    cards: (doc) => Array.from(doc.querySelectorAll(NOTE_CARD)).filter((card) => card.querySelector(COVER)),
    link: noteLink,
    title: (card) => card.querySelector("a.title")?.textContent?.replace(/\s+/g, " ").trim() || undefined,
    image: (card) => card.querySelector<HTMLImageElement>(`${COVER} img`) ?? undefined,
    key: (card) => {
      const id = card.getAttribute("data-note-id");
      return id ? `xhs:${id}` : undefined;
    },
    // 封面 img 没有 srcset，页面上最大的就是这张 640 宽的预览图（2026-10-21 收割实测 640×853）。
    // 原图要点开笔记才有，一篇一篇点会触发风控，所以把预览图原样带回去当素材
    thumbWidth: () => 640
  }
};
