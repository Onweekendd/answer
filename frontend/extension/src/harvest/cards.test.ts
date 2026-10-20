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

import { describe, it } from "vitest";

// repeatedCards（通用的卡片识别）的测试。实现留给你写：先把这些 todo 写成真的测试（Arrange 用一段 HTML），再写实现。
// 这里有布局的判断（图够不够大）在 jsdom 里拿不到，可以给 repeatedCards 加一个参数，测试里传一个假的尺寸函数进去
describe("repeatedCards", () => {
  it.todo("网格：同一个父元素下面 20 个结构相同、各有一张图的 <div>，都认成卡片，按页面顺序");
  it.todo("不嵌套：认出卡片以后，不再返回卡片里面的 <a> 或 <img>");
  it.todo("导航栏、页脚里的小图标不算：图太小，或者兄弟太少");
  it.todo("一个页面上有两组列表（结果和侧栏推荐）：取卡片多的那组");
  it.todo("瀑布流：绝对定位、高矮不一的格子也认得（结构签名相同就行，不看尺寸是否一致）");
  it.todo("认不出来返回空数组，不抛错");
});
