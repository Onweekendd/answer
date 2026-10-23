# 社区每日话题

Node.js 22+，无第三方依赖。复用本站 `/purchase/chat` → `/purchase/ai/chat`（SSE `draft_post`）→ `/question`，使用服务器现有的模型配置，无需另配模型密钥。运行时需要能访问社区后端，且后端 AI 服务已配置。

发帖后的旧版后台 AI 分析流程已移除，发布帖子不会再自动启动联网调研、机器人点评或分析投票。

## 配置账号和内容

复制本目录的 `community.example.json` 为 `community.local.json`，`credentials.example.env` 为 `credentials.local.env`。把 `baseUrl` 改成社区地址（不带 `/answer/api/v1`），在私有环境文件中填入你控制的账号邮箱和密码。所有账号都应属于你自己的社区运营。

示例包含 5 个普通昵称、各自的表达风格和兴趣，以及 24 个讨论方向。昵称不附加 AI 标签，正文也不自动插入身份说明。话题着重生活里的取舍，避免广告语、重复句式和虚构亲测。可以直接修改 `persona`、`categories` 和 `topics[].request`，增加你想讨论的方向。

已有账号：把示例账号配置改成对应凭据即可。也支持 `tokenEnv` 指向环境变量中的已有访问令牌；令牌过期后更新环境变量或改用邮箱登录。凭据不会写入状态或日志。

需要新账号时，在仓库根目录执行一次：

```sh
node --env-file=script/community/credentials.local.env script/community/community.mjs --init-accounts
```

这会使用普通注册接口，注册昵称取 `nickname`；已注册的邮箱会报错，不会改密码或绕过验证码。如果站点要求邮箱验证，需要先按正常流程完成验证。已存在的账号不必执行注册命令。`nickname` 不会修改已有账号的资料，已有账号应自行在站点设置对应昵称。

## 先预览，再运行

默认仅调用 AI 生成草稿，不发布，也不占用每日发布额度（会产生正常的 AI 调用费用和聊天记录）：

```sh
node --env-file=script/community/credentials.local.env script/community/community.mjs
```

草稿写入 `out/preview-日期-账号.json`。要发布当天的一批：

```sh
node --env-file=script/community/credentials.local.env script/community/community.mjs --publish
```

每天最多 `postsPerDay` 帖，每账号每天最多一帖，账号按日期轮换。默认间隔随机 15～60 分钟，因此一批会持续一段时间；预览也遵守该间隔，调试时可在配置中暂改为 1 秒。最多允许配置每天 12 帖；账号数不足时以账号数为限。脚本只发起问题，不批量投票或伪造讨论数据。

默认 `topicCooldownDays=7`，同一方向一周内不重复，标题还会做相似度检查；无可用话题或遇到重复会少发，建议持续补充题库。AI 不一定每次都调用草稿工具，未拿到完整有效草稿就不会发布。

有真实配图时，可在话题里填 `images`（本站已上传的 http(s) 图片地址，最多 5 张，会随请求交给 AI）。草稿无图时，使用 `coverFile` 指定的本地 PNG/JPEG/WebP 封面；相对路径以配置目录为基准。没有配置时自动生成并上传一张抽象问号封面，满足本站发帖至少一张图的要求。这张封面仅作讨论插图。

## 每日定时

前台守护模式：

```sh
node --env-file=script/community/credentials.local.env script/community/community.mjs --daemon --publish
```

示例每天 `Asia/Shanghai` 的 `10:00` 开始，可通过 `timeZone` 和 `dailyAt` 调整。守护模式必须保持进程运行；启动时若已过当日时间，会补跑当天未完成的任务。未加 `--publish` 时，每天只生成预览。

Linux 生产环境建议用 cron：将脚本目录复制到 `/opt/answer/script/community`，服务器安装 Node.js 22+ 后，使用运行用户的 `crontab -e` 加入下面这一行。这里假设 **cron 所用的服务器时区已设为 Asia/Shanghai**，与配置一致；如果不是，请先换算时间或使用上面的守护模式（守护模式直接按配置时区执行，不依赖系统时区）。

```cron
0 10 * * * cd /opt/answer && /usr/bin/node --env-file=script/community/credentials.local.env script/community/community.mjs --publish >> /opt/answer/script/community/community.log 2>&1
```

确认 `node` 的实际安装路径后调整 `/usr/bin/node`。给私有配置、环境文件和 `out/` 设置仅运行用户可读写的权限，并为日志配置轮转。外部 cron 决定启动时间；此时 `dailyAt` 不参与触发，`timeZone` 仍决定每日额度的日期边界。

Windows 可以在任务计划程序中建立每日任务：程序填 `node.exe` 的绝对路径，起始目录填仓库目录，参数填 `--env-file=script/community/credentials.local.env script/community/community.mjs --publish`。任务触发时间应与配置时区对应；也可让进程管理工具常驻运行 `--daemon --publish`。

## 重跑与排查

- `out/state.json` 记录日期、账号、话题、会话 ID、标题和帖子 ID；重复启动会跳过已发布的账号。同一账号当天失败最多尝试两次，守护进程不会持续重试失败任务。
- `.lock` 防止任务同时运行。若进程被强杀，确认旧进程已停止后才能删除 `out/state.json.lock`。保留状态文件，否则无法判断哪些帖子已发。
- `publishing` 表示发帖请求已发出，但可能没收到响应；为避免重复不会自动重发。先按账号、标题在社区核对：已发则补全状态为 `published` 和 `questionId`，确实未发才改为 `failed` 并保留尝试次数。
- 验证码、发帖权限、频率限制、审核等全部沿用站点规则，脚本不会绕过。HTTP 失败记录接口路径、状态和 reason；具体问题可到管理后台或对应聊天查看。
- 普通失败退出码为 1，成功为 0。预览、私有配置、环境文件和状态文件均已加入 Git 忽略。
- 切换社区地址请使用独立 `stateFile`，脚本会拒绝混用不同站点的状态。

测试：`node --test script/community/community.test.mjs`，使用临时本地模拟服务器，不访问线上社区。
