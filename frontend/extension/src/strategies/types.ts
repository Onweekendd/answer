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

// 策略只补充候选元素，统一经过内核的可见性、编号、隐私和长度限制。
export interface SiteStrategy {
  name: string;
  matches(url: URL): boolean;
  interactive?(doc: Document): Element[];
  // 给补充进来的元素起个名字，快照里代替它的文字。没有文字、或者文字说不清它是什么的元素用得上
  // （小红书的图片区，单图笔记里一个字都没有）。返回 undefined 就照常用它的文字
  label?(el: Element): string | undefined;
  // 商品卡片的商品页链接，给商品卡片（product 命令）用。卡片里没有指向商品页的 <a> 时用得上
  // （京东的卡片靠 JS 跳转，里面唯一的 <a> 是客服）。返回 undefined 就照常在元素里找 <a>
  link?(el: Element): string | undefined;
  // 页面的正文区，read 不填 index、又没有浮层时按顺序读这几块。页面没有 <main> 又不想读整页时用得上
  // （京东详情页最上面是这个浏览器账号的"最近感兴趣"；1688 详情页的价格在 3000 字以后）。
  // 一块都没找到就照常找 <main>，找不到读整页
  main?(doc: Document): (Element | null)[];
}
