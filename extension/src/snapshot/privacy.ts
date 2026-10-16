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

// 隐私打码：快照要发给第三方 LLM，所有文字输出前都要经过这里（唯一出口，第 7 站坑 6）。
// 这里只处理"看格式就能认出来"的；账号昵称、收货地址要靠站点配置的选择器，等实测后再加。

// 前后都不能紧挨着别的数字（?<! 和 ?! 是"前面不是 / 后面不是"）。
// 不加这个条件，13 位的商品 id 1030811149465 里也能匹配出一段 11 位"手机号"。
const PHONE = /(?<!\d)1[3-9]\d{9}(?!\d)/g;
// 18 位：前 17 位数字，最后一位是数字或 X。同样不能紧挨着别的数字
const ID_CARD = /(?<!\d)\d{17}[\dXx](?![\dXx])/g;
const EMAIL = /[\w.+-]+@[\w-]+(\.[\w-]+)+/g;

export function maskPrivate(text: string): string {
  // 先打身份证号再打手机号：身份证号里也有可能出现像手机号的片段
  return text.replace(ID_CARD, "[身份证号]").replace(PHONE, "[手机号]").replace(EMAIL, "[邮箱]");
}
