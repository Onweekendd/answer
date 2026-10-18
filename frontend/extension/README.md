# Pagehand（Chrome 插件）

让 Go 后端的 Agent 通过你自己的浏览器查看任意网页。插件本身不认识任何网站，站点适配放在 `src/strategies/`（见 [docs/page-snapshot-design.md](../../docs/page-snapshot-design.md) 第 4 节）。设计见 [docs/buy-or-not-design.md](../../docs/buy-or-not-design.md) 第 5 节。

仅供个人学习、低频使用。遇到验证码或登录页要停下来请人处理，不尝试绕过。

## 先看进程：谁在哪里跑

```
你的电脑：Chrome（多进程）                                    Answer（Go）
┌──────────────────────────────────────────────┐
│ 插件进程：background.ts（service worker）      │── WebSocket ──▶ Hub 127.0.0.1:8765
│   connection.ts  连接、保活、断线重连            │   插件主动连上
│   commands.ts    执行 Go 发来的命令              │   Go 通过它下命令
│        ⇅ chrome.tabs.sendMessage（进程间消息）  │
│ 某个网页标签页的渲染进程                         │
│   ├ 网页自己的 JS                               │
│   └ content.ts（同一个进程，隔离的 JS 世界）      │
│                                               │
│ popup.ts：点插件图标弹出的小窗，又是一个进程       │
└──────────────────────────────────────────────┘
```

这几块不共享内存，只能发消息：

| 文件            | 跑在哪                    | 能做什么                                              | 不能做什么                                      |
| --------------- | ------------------------- | ----------------------------------------------------- | ----------------------------------------------- |
| `background.ts` | 插件自己的 service worker | 调 `chrome.tabs` 等 API、连 WebSocket                 | 看不到任何网页的 DOM                            |
| `content.ts`    | 被操作过的页面里各一份    | 读写这个页面的 DOM                                    | 调不了大部分 `chrome.*` API；页面一跳转就被销毁 |
| `popup.ts`      | 弹出的小窗口              | 显示状态、改配置                                      | 关掉就销毁，不能做长期的事                      |
| `protocol.ts`   | 不是进程，是共享的类型    | 所有消息的格式                                        |                                                 |
| `storage.ts`    | 不是进程                  | 配置和状态存在 `chrome.storage`，几个进程靠它交换数据 |                                                 |

## content script 是按需注入的

manifest 里没有 `content_scripts`，插件不会在你打开的每个网页里自动运行。Go 第一次对某个标签页发 `snapshot` / `click` 时：

1. background 先 `chrome.tabs.sendMessage`，收到 "Receiving end does not exist"，说明页面里还没有 content script；
2. 用 `chrome.scripting.executeScript` 把 `content.js` 注入进去（同一个标签页同时只注入一次）；
3. 再发一次。

页面跳转后旧的那份被销毁，下一条命令会再注入一份。`chrome://` 页面、新标签页、应用商店浏览器不允许任何插件注入，会返回 `PAGE_NOT_SCRIPTABLE`。

代价是 `host_permissions` 是 `<all_urls>`，安装时 Chrome 会提示"读取和更改您在所有网站上的数据"。

## 构建和加载

```bash
cd frontend/extension
pnpm install
pnpm watch      # 改了 ts 会自动重新打包到 dist/
```

写代码时常用的检查：

```bash
pnpm test:watch # 单元测试，保存就重跑
pnpm lint       # ESLint：代码问题 + 排版（prettier/prettier 规则）+ 导入顺序
pnpm lint:fix   # 能自动修的都修掉（排版、导入顺序）
pnpm format     # 只用 Prettier 排版，也会处理 README、popup.html 这些非 TS 文件
pnpm check      # 提交前跑一遍：类型检查 + lint + 排版检查 + 测试
```

ESLint 和 Prettier 的规则沿用 funBI（`packages/eslint-config`）：双引号、不加尾逗号、每行 120 字、`if` 必须带大括号、`import type` 单独写、导入按分组排序。

版本约束：ESLint 锁在 9（`eslint-plugin-import` 还不支持 10），TypeScript 锁在 6.0.x（typescript-eslint 目前只支持到 6.0）。

第一次加载：

1. 打开 `chrome://extensions`，打开右上角「开发者模式」；
2. 点「加载已解压的扩展程序」，选择 `frontend/extension/dist` 目录；
3. 点工具栏上的插件图标，填 Token（和 Go Hub 配置的一致），保存。

改了代码以后，**自动重载**：Hub 在跑、插件连着的时候，带上 token 运行 watch，每次保存都会自动打包并让插件重载自己，不用去点刷新：

```bash
BROWSER_HUB_TOKEN=<token> pnpm watch
```

原理：打包完调用 Hub 的调试接口发一条 `reload` 命令，插件先回复、再调用 `chrome.runtime.reload()` 从磁盘重新加载。网页里旧的 content script 会和插件断开，下一条命令会自动注入新的（按需注入），网页不用刷新。

没有开 Hub 时：

- 改了 `background.ts` / `commands.ts` / `connection.ts`：在 `chrome://extensions` 里点插件卡片上的刷新按钮；
- 改了 `content.ts`：刷新插件以后，下一条命令会自动注入新的 content script；
- 改了 `static/` 里的文件（manifest、popup.html）：重新运行 `pnpm watch`，再刷新插件。

## 调试

| 想看什么                   | 去哪里                                                           |
| -------------------------- | ---------------------------------------------------------------- |
| background 的日志、断点    | `chrome://extensions` → 插件卡片上的「Service Worker」链接       |
| content script 的日志      | 网页按 F12 → Console，左上角下拉框从 top 切到「Pagehand」        |
| popup 的日志               | 右键插件图标 →「审查弹出内容」                                   |
| 连接状态                   | 点插件图标                                                       |
| WebSocket 收发的每一条消息 | Service Worker 的 DevTools → Network → 点那条 ws 连接 → Messages |

### 从命令行控制插件

`answer browser` 有两种用法，都在仓库根目录运行，token 和 popup 里填的一致：

```bash
# 交互式：敲 tabs、navigate、snapshot、click
BROWSER_HUB_TOKEN=<token> go run ./cmd/answer browser

# 只开 HTTP 调试接口，一直跑到 Ctrl+C：给 curl、脚本和自动化测试用
BROWSER_HUB_TOKEN=<token> go run ./cmd/answer browser --console=false
```

两种用法都会开调试接口，请求头带上同一个 token：

```bash
H="Authorization: Bearer $BROWSER_HUB_TOKEN"
curl -s -H "$H" http://127.0.0.1:8765/debug/tabs
curl -s -H "$H" -d '{"tabId":123,"cmd":"snapshot","args":{"maxChars":8000}}' http://127.0.0.1:8765/debug/call
curl -s -H "$H" -d '{"tabId":123,"cmd":"navigate","args":{"url":"https://github.com"},"timeoutMs":60000}' http://127.0.0.1:8765/debug/call
```

返回格式和插件回给 Go 的结果一样：`{"ok":true,"data":…}` 或 `{"ok":false,"error":{"code":…,"message":…}}`。状态码：

| 状态码 | code            | 意思                                        |
| ------ | --------------- | ------------------------------------------- |
| 200    |                 | 成功                                        |
| 400    | `BAD_REQUEST`   | 请求体不是 JSON，或者缺 `cmd`               |
| 401    | `UNAUTHORIZED`  | 没带 token 或 token 不对                    |
| 403    | `FORBIDDEN`     | 请求带了 Origin，也就是网页发来的           |
| 422    | 插件的错误码    | 插件执行失败，比如 `ELEMENT_NOT_FOUND`      |
| 499    | `CANCELED`      | 结果出来之前客户端断开了                    |
| 502    | `DISCONNECTED`  | 等结果的时候插件断开了                      |
| 503    | `NOT_CONNECTED` | 插件没连上                                  |
| 504    | `TIMEOUT`       | 超过 `timeoutMs`（默认 30 秒，最多 2 分钟） |

### 量一遍快照：pagebench

改了压缩规则以后，在固定的几个页面上重量一遍，和基线比较（页面列表在 `script/pagebench/pages.json`）：

```bash
BROWSER_HUB_TOKEN=<token> PAGEBENCH_REDACT=<你的昵称> go run ./script/pagebench
BROWSER_HUB_TOKEN=<token> go run ./script/pagebench -update-baseline   # 满意了就存成新基线
```

它在后台新开一个标签页，每个页面只打开一次、快照一次，页面之间等 4 秒，不点任何东西；跳到登录页或验证码页就跳过这个网站。基线 `baseline.json` 只有数字，可以提交；完整的快照写到 `script/pagebench/out/`，不提交，写之前把 `script/pagebench/redact.txt`（一行一个词，不提交）和 `PAGEBENCH_REDACT` 里的词换成 `[已屏蔽]`。

## Service worker 会"睡着"

MV3 的 background 不是常驻进程：空闲约 30 秒后 Chrome 会关掉它，有事件时再启动。每次启动，`background.ts` 都从头执行一遍。框架里做了三件事来应对：

1. **保活**：连上以后每 20 秒发一条 `ping`。WebSocket 上有消息往来，worker 就不会被关掉（Chrome 116 起）。
2. **断线重连**：指数退避，1、2、4……最多 30 秒。
3. **闹钟兜底**：`chrome.alarms` 每 30 秒唤醒一次 worker，醒来时顶层的 `connect()` 会检查连接。

还有一条规则：**事件监听必须在文件顶层同步注册**，不能写在 `await` 后面。

## 现在的进度

- [x] ① 骨架：manifest、构建、连接 Hub、发 `hello`、保活、重连、popup
- [x] ② Go Hub：接受连接、校验 hello 里的 token（在 `internal/service/browser/`）
- [x] ③ `navigate`
- [x] ④ `snapshot` P1，参考 [第 4 站笔记](../../docs/browser-use-study/04-页面压缩.md) 和 [压缩方案](../../docs/page-snapshot-design.md)
- [x] ⑤ 通用化：上报所有网页标签页，content script 按需注入

终端调试：`BROWSER_HUB_TOKEN=<popup 里填的> go run ./cmd/answer browser`，然后敲 `tabs`、`snapshot <id>`。
