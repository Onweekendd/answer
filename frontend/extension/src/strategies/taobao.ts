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

import type { SiteStrategy } from "./types";

export const taobao: SiteStrategy = {
  name: "taobao-search",
  matches: (url) => url.hostname === "s.taobao.com" && url.pathname === "/search",
  interactive: (doc) =>
    // 实测价格菜单是没有 role 的 div；只匹配该菜单的类名前缀，不依赖构建 hash。
    Array.from(doc.querySelectorAll('[class*="priceTagWrap--"] > [class*="priceTag--"]'))
};
