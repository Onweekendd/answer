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

// 沿用 funBI 的 packages/eslint-config/prettier.config.js
/** 配置项文档：https://prettier.io/docs/en/configuration.html */
export default {
  /** 每一行的宽度 */
  printWidth: 120,
  /** Tab 键的空格数 */
  tabWidth: 2,
  /** 在对象中的括号之间是否用空格来间隔 */
  bracketSpacing: true,
  /** 箭头函数的参数无论有几个，都要括号包裹 */
  arrowParens: "always",
  /** 换行符的使用 */
  endOfLine: "auto",
  /** 是否采用单引号 */
  singleQuote: false,
  /** 对象或者数组的最后一个元素后面不要加逗号 */
  trailingComma: "none",
  /** 是否加分号 */
  semi: true,
  /** 是否使用 Tab 格式化 */
  useTabs: false
};
