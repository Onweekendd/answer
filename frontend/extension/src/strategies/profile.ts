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

// 「能力 × 页面」：内核的每项能力（快照、读正文、列表收割……）都有完整的通用实现，分成几个可替换的步骤；
// 一类页面（PageProfile）按网址匹配，只给自己真正不同的那几步写增强。
//
// 规矩：
//   - 增强只覆盖步骤，不覆盖整套算法：没有任何增强也能跑，只是差一点
//   - 步骤都是纯函数：输入 DOM 或网址，输出数据。滚动、点击、等待只在内核的骨架里，增强碰不到，所以都能用 jsdom 测
//   - 每一步单独回退（fallback.ts）：增强没写、抛错、返回空，就用通用实现，失败记进 stats
//   - 增强要有实测依据：旁边写上哪天在哪个页面看到的

// 快照：通用规则编号以外，再补进来哪些元素、叫什么、链接是什么
export interface SnapshotSteps {
  // 补进快照的元素，和通用规则找到的合在一起。通用实现：不补
  candidates(doc: Document): Element[];
  // 给元素起个名字，快照里代替它的文字。没有文字、或者文字说不清它是什么的元素用得上
  // （小红书的图片区，单图笔记里一个字都没有）。undefined：照常用它的文字
  label(el: Element): string | undefined;
  // 商品卡片的商品页链接（product 命令用）。卡片里没有指向商品页的 <a> 时用得上
  // （京东的卡片靠 JS 跳转，里面唯一的 <a> 是客服）。undefined：照常在元素里找 <a>
  link(el: Element): string | undefined;
}

// read 不填 index、又没有浮层时读哪几块
export interface ReadSteps {
  // 页面的正文区，按顺序读。页面没有 <main> 又不想读整页时用得上
  // （京东详情页最上面是这个浏览器账号的"最近感兴趣"；1688 详情页的价格在 3000 字以后）。
  // 一块都没找到就照常找 <main>，找不到读整页
  main(doc: Document): (Element | null)[];
}

// 列表收割：搜索页、瀑布流里一张张卡片的图、标题、链接。骨架在 ../harvest/list.ts
export interface ListSteps {
  // 页面上现在渲染着的卡片。瀑布流会把滚出屏幕的卡片删掉，所以骨架每滚一屏问一次
  cards(doc: Document): Element[];
  // 是不是广告
  sponsored(card: Element): boolean;
  // 卡片打开的那个页面（出处）
  link(card: Element): string | undefined;
  // 卡片的标题
  title(card: Element): string | undefined;
  // 卡片的主图
  image(card: Element): HTMLImageElement | undefined;
  // 去重用的键：同一张卡片滚上滚下会被看到好几次。undefined：用链接，没有链接用缩略图地址
  key(card: Element): string | undefined;
}

// 一类页面：按网址匹配，对各项能力的增强都是可选的，每项能力里也只写不同的那几步
export interface PageProfile {
  name: string;
  matches(url: URL): boolean;
  snapshot?: Partial<SnapshotSteps>;
  read?: Partial<ReadSteps>;
  list?: Partial<ListSteps>;
}
