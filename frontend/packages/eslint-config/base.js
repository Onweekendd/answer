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

// 从 extension/eslint.config.js 抽出来的公共规则，web 和 extension 共用。
// 那份配置沿用 funBI 的 packages/eslint-config/base.js 和 apps/app/eslint.config.js 里的导入排序。
// 和原配置的差别：
//   - 去掉 @typescript-eslint/ban-types：typescript-eslint v8 已删除这条规则，留着会报"找不到规则"
//   - 导入排序的分组去掉了 Vue 和样式，加了 "@/" 别名一组（web 用）
//   - import/resolver 换成 typescript：既能解析不带扩展名的 .ts 导入，也能解析 tsconfig 里的 "@/" 别名，
//     这样 import/no-cycle 在 web 里也能看到别名导入

import eslint from "@eslint/js";
import prettierConfig from "eslint-config-prettier";
import importPlugin from "eslint-plugin-import";
import prettierPlugin from "eslint-plugin-prettier";
import simpleImportSort from "eslint-plugin-simple-import-sort";
import globals from "globals";
import tseslint from "typescript-eslint";

/** @type {import("eslint").Linter.Config[]} */
export default [
  // 忽略的文件和目录
  {
    ignores: ["**/node_modules/**", "**/dist/**", "**/coverage/**", "**/*.min.js", "**/eslint.config.js"]
  },

  // JavaScript 推荐配置
  eslint.configs.recommended,

  ...tseslint.configs.recommended,
  {
    rules: {
      "@typescript-eslint/consistent-type-imports": "error"
    }
  },

  // 全局配置
  {
    files: ["**/*.{js,mjs,ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2020,
      sourceType: "module",
      globals: {
        ...globals.browser,
        ...globals.node,
        ...globals.es2020
      }
    },
    plugins: {
      prettier: prettierPlugin,
      import: importPlugin
    },
    settings: {
      "import/resolver": { typescript: { alwaysTryTypes: true } }
    },
    rules: {
      // Import
      "import/no-cycle": ["error", { maxDepth: 10 }],
      // TS
      "@typescript-eslint/explicit-function-return-type": "off",
      "@typescript-eslint/no-explicit-any": "warn",
      "no-debugger": "off",
      "@typescript-eslint/explicit-module-boundary-types": "off",
      "@typescript-eslint/ban-ts-comment": "off",
      "@typescript-eslint/no-empty-function": "off",
      "@typescript-eslint/no-non-null-assertion": "off",
      "@typescript-eslint/consistent-type-definitions": ["warn", "interface"],
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_"
        }
      ],
      "no-unused-vars": "off",
      // Prettier
      "prettier/prettier": [
        "error",
        {
          endOfLine: "auto"
        }
      ]
    }
  },

  // 导入排序
  {
    plugins: {
      "simple-import-sort": simpleImportSort
    },
    rules: {
      "simple-import-sort/imports": [
        "error",
        {
          groups: [
            // Vitest 测试框架
            ["^vitest"],
            // Node.js 内置模块
            ["^node:"],
            // 第三方包
            ["^@?\\w"],
            // 项目内 "@/" 别名
            ["^@/"],
            // 相对路径导入（../ 和 ./）
            ["^\\."]
          ]
        }
      ],
      "simple-import-sort/exports": "error"
    }
  },

  // Prettier 配置（需要放在最后以覆盖其他配置）
  prettierConfig,

  // 放在 prettierConfig 之后，避免 eslint-config-prettier 把 curly 关闭
  {
    rules: {
      curly: ["error", "all"]
    }
  }
];
