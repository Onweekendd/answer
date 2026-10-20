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

// repeatedCards 是列表收割的通用 cards 步骤：在任意列表页上，认出"重复出现的卡片"。
// 页面有增强（strategies/pinterest.ts 的 cards）时用不到它；没有增强的网站全靠它。
//
// 契约：
//   - 返回页面上现在渲染着的卡片，按页面顺序，一张卡片一个元素，不嵌套（不要同时返回卡片和它里面的元素）
//   - 一张卡片 = 一个包着一张主图（和标题、链接）的块。导航、标签栏、页脚里的图不算
//   - 认不出来就返回空数组，不要抛错：骨架会照常结束，stats 里能看出一张都没收到
//   - 只读 DOM，不滚动、不点击
//
// TODO(你来写)：这是收割里最有意思的一块。几个可以琢磨的方向：
//   - 卡片的共同点：同一个父元素下面有很多个结构相似的兄弟（标签名 + 类名 + 子树形状），而且每个里面都有一张够大的图
//   - 怎么给"结构相似"打分：比如把每个元素的子树压成一个签名（"div>a>img+div>span"），签名相同的兄弟越多越像列表
//   - 瀑布流（Pinterest）的格子是绝对定位的兄弟；网格（淘宝）是 flex/grid 的子元素；两种都要认得
//   - 噪音：推荐词那一排小图标（图太小）、轮播图（同一时刻只显示一张）
// 测试在 cards.test.ts：先把那几个 it.todo 写成真的测试，再写实现
export function repeatedCards(_doc: Document): Element[] {
  return [];
}
