// @ts-check
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

// 规则在 packages/eslint-config，和 extension 共用；这里再叠加 React 规则。
import { base, react } from "@buy-or-not/eslint-config";

export default [
  ...base,
  ...react,
  {
    // shadcn 和 assistant-ui 的 CLI 生成的代码，保持 CLI 写出来的样子，不套我们的规则
    ignores: [
      "src/components/ui/**",
      "src/components/assistant-ui/**",
      "src/hooks/use-attachment-src.ts",
      "src/hooks/use-copy-to-clipboard.ts"
    ]
  }
];
