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

// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";

import { findClosers } from "./closers";

afterEach(() => {
  document.body.innerHTML = "";
});

const sized = (el: Element, width: number, height: number) => {
  el.getBoundingClientRect = () =>
    ({ x: 0, y: 0, left: 0, top: 0, right: width, bottom: height, width, height, toJSON: () => ({}) }) as DOMRect;
};

it("认得类名、图片地址里带 close 的小图标，和文字是 × 的元素", () => {
  // Arrange
  document.body.innerHTML = `<div id="layer">
      <div class="promo">秋冬上新95折<div class="btn">搜索更多95折新品</div></div>
      <div class="icon-close"></div>
      <img src="https://img.example.com/close.png">
      <button><span>×</span></button>
    </div>`;
  const layer = document.getElementById("layer")!;
  for (const el of Array.from(layer.querySelectorAll("*"))) {
    sized(el, 24, 24);
  }
  sized(layer.querySelector(".promo")!, 400, 300);

  // Act
  const found = findClosers(layer, () => true);

  // Assert：按钮自己没有文字，认出来的是里面写着 × 的 span，点它和点按钮一样
  expect(found.map((el) => el.outerHTML)).toEqual([
    '<div class="icon-close"></div>',
    '<img src="https://img.example.com/close.png">',
    "<span>×</span>"
  ]);
});

it("大块的、看不见的不算：整个弹窗的 class 里也可能带 close", () => {
  // Arrange
  document.body.innerHTML = `<div id="layer"><div class="closeable-dialog">内容</div><div class="close" hidden></div></div>`;
  const layer = document.getElementById("layer")!;
  sized(layer.querySelector(".closeable-dialog")!, 500, 400);
  sized(layer.querySelector(".close")!, 20, 20);

  // Act
  const found = findClosers(layer, (el) => !el.hasAttribute("hidden"));

  // Assert
  expect(found).toEqual([]);
});
