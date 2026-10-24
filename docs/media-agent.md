# 素材采集 Agent：HTTP、MCP 和 MinIO

创作平台提交主题或来源页，服务内部使用 `purchase_ai/agent` 和浏览器插件完成搜索、选取、下载、验证、上传。外部应用只接任务接口，不需要复制浏览器或网站适配代码。

## 启动

需要 Go、`ffprobe`、`ffmpeg`，以及已经连上 `answer browser` 的插件。浏览器仍在安装插件的电脑运行；素材服务通过 `BROWSER_HUB_URL` 连接 Hub，不需要数据库或工作台沙箱。

先准备私有环境文件，例如 `tmp/assets.local.env`（不要提交）：

```dotenv
ASSET_API_TOKEN=<至少16个字符的服务访问令牌>
BROWSER_HUB_URL=http://127.0.0.1:8765
BROWSER_HUB_TOKEN=<插件和Hub使用的令牌>
ASSET_S3_ENDPOINT=127.0.0.1:9002
ASSET_S3_ACCESS_KEY_ID=<MinIO访问密钥>
ASSET_S3_SECRET_ACCESS_KEY=<MinIO密钥>
ASSET_S3_BUCKET=creative-assets
ASSET_S3_REGION=us-east-1
ASSET_S3_USE_SSL=false
ASSET_S3_PREFIX=assets
ASSET_LLM_API_HOST=<兼容Chat Completions的模型地址>
ASSET_LLM_API_KEY=<模型密钥>
ASSET_LLM_MODEL=<支持工具调用的模型>
```

`ASSET_S3_*` 未设置时，会读取对应的 `S3_*`，再使用默认值。`--env-file` 中的值覆盖当前进程和仓库 `.env` 的同名值；文件中的密码含特殊字符时应按 dotenv 规则引用。模型也可以使用现有的 `script/agentcase/llm.local.json`。

```powershell
go build -o tmp/answer-assets.exe ./cmd/answer
./tmp/answer-assets.exe assets --env-file tmp/assets.local.env --create-bucket

# 复用已有模型配置时：
./tmp/answer-assets.exe assets --env-file tmp/assets.local.env --llm-config script/agentcase/llm.local.json --create-bucket
```

`--create-bucket` 只创建配置的 Bucket，不修改已有权限策略。默认 Bucket 为 `creative-assets`，保持私有；文件通过一小时有效的预签名 GET 地址访问。每次查询任务都会刷新地址。

默认监听 `127.0.0.1:8766`，`--addr` 可调整。MinIO `9002` 是 S3 API；`9003` 是控制台。若调用方运行在其他电脑或容器中，必须将 S3 endpoint 配置成双方可访问的地址，不能给远端应用返回指向它自身的 `127.0.0.1` 链接。

## HTTP

全部接口使用 `Authorization: Bearer <ASSET_API_TOKEN>`。示例中的 `$headers` 应由调用端从自己的密钥配置构造。

```powershell
$headers = @{ Authorization = "Bearer $env:ASSET_API_TOKEN" }
$body = @{
  project_id = 'creative-demo'
  request_id = 'scene-001'
  query = '新疆那拉提，蓝天白云绿草地，选择一个适合旅行短片的风景视频'
  kind = 'video'
  count = 1
  min_short_side = 1080
  max_duration_seconds = 30
} | ConvertTo-Json

$task = Invoke-RestMethod http://127.0.0.1:8766/api/assets/tasks `
  -Method Post -Headers $headers -ContentType 'application/json; charset=utf-8' `
  -Body ([Text.Encoding]::UTF8.GetBytes($body))

Invoke-RestMethod "http://127.0.0.1:8766/api/assets/tasks/$($task.task_id)" -Headers $headers

Invoke-RestMethod "http://127.0.0.1:8766/api/assets/tasks/$($task.task_id)/cancel" `
  -Method Post -Headers $headers
```

创建返回 `202` 和 `task_id`，与请求连接断开后仍继续执行。建议每 3–5 秒查询一次。

`kind` 为 `video` 或 `image`。`count` 默认 1、最多 10；`min_short_side` 默认 1080；`max_duration_seconds` 默认 120、最多 900。视频当前支持抖音搜索和视频页。图片使用通用浏览器图片工具，建议提供真实来源页或图片链接：

```json
{
  "project_id": "creative-demo",
  "request_id": "image-001",
  "kind": "image",
  "query": "采集这些页面里与雪山主题相关的图片",
  "source_urls": ["https://example.com/photo-page"],
  "count": 1,
  "min_short_side": 1080
}
```

相同项目下，重复提交相同 `request_id` 和参数会返回原任务；同一个 ID 配合不同参数返回 `409`。省略 `request_id` 则每次创建新任务。

## MCP

Streamable HTTP endpoint：`http://127.0.0.1:8766/mcp`。请求头同样携带服务令牌。客户端连接配置示意（具体键名取决于客户端）：

```json
{
  "mcpServers": {
    "answer-media": {
      "url": "http://127.0.0.1:8766/mcp",
      "headers": { "Authorization": "Bearer <ASSET_API_TOKEN>" }
    }
  }
}
```

工具：

| 工具 | 输入 | 输出 |
| --- | --- | --- |
| `create_asset_task` | 与 HTTP 创建请求一致 | 任务 ID 和状态，立即返回 |
| `get_asset_task` | `task_id` | 进度、实际上传的素材、刷新后的访问地址 |
| `cancel_asset_task` | `task_id` | 取消后的状态，保留已经上传的素材 |

结果同时包含文本 JSON 和 MCP `structuredContent`。当前没有实现 A2A 协议；将来可以给同一个 `assets.Service` 增加适配器。

## 返回与状态

```json
{
  "task_id": "...",
  "status": "completed",
  "stage": "completed",
  "request": { "project_id": "creative-demo", "kind": "video", "count": 1 },
  "assets": [{
    "asset_id": "...",
    "type": "video",
    "bucket": "creative-assets",
    "object_key": "assets/creative-demo/<task-id>/<sha256>.mp4",
    "url": "http://<minio>/<bucket>/<key>?<signature>",
    "url_expires_at": "...",
    "source_url": "https://www.douyin.com/video/...",
    "title": "...",
    "author": "...",
    "content_type": "video/mp4",
    "width": 1080,
    "height": 1920,
    "duration_ms": 13813,
    "bytes": 10767866,
    "sha256": "..."
  }]
}
```

应保存 `bucket + object_key` 作为持久标识；`url` 是临时访问能力，过期后重新查询任务。创作平台拿到结果后可将元信息登记进自己的素材表。当前对象写入配置的一个 S3 目标，项目只影响对象前缀；尚无调用方预签名 PUT / 素材登记回调接口。

| 状态 | 含义 |
| --- | --- |
| `queued` / `running` | 等待或执行中 |
| `completed` | 请求的数量已实际上传并记录 |
| `partial` | 部分已上传，其余未完成，已上传的仍返回 |
| `input_required` | 插件未连接或需要登录/验证码；人工处理后使用新 `request_id` 重提任务 |
| `failed` | 无素材完成或任务执行失败，查看 `error` |
| `canceled` | 已取消；已经上传的对象不会被删除 |

## 实现边界

- 使用现有 `purchase_ai/agent`、`browsertool`、`guard` 和 `video.ParseDouyin` / `Fetch`。新增 `PickCreative` 强制素材分辨率并优先选择 H.264；原 `Pick` 的视频分析策略保留。
- 视频只选独立 MP4，跳过 DASH 档位。上传前使用 ffprobe 检查真实尺寸/时长，再使用 ffmpeg 完整解码。视频最多 100 MiB。
- 图片走插件原图读取通道，转换成 JPEG（质量 92、宽度最多 4096），不是字节不变的原始格式文件。图片会完整解码验证，并检查实际短边。
- 存储逻辑不进入模型。对象使用内容 SHA-256 命名；上传成功后检查对象大小，随后记录任务素材。S3 凭证和文件访问 URL 不放入模型上下文。
- 同一服务进程一次执行一个任务，共享插件站点限速；模型返回的“成功”不会代替上传记录。取消会中止后续工作，但已完成或与取消同时完成的上传仍会记录。
- 状态在 `--state-dir` 的 JSON 文件中保存，重启时将未完成任务标为失败，不自动重复下载。当前限制 100 个排队任务、1000 个保留任务；状态目录仅允许一个服务进程使用。
- 当前是单个受信应用的服务令牌，`project_id` 是存储命名空间，不是租户权限边界。远程部署应在现有鉴权网关之后运行，并为每个浏览器执行节点指定实例；尚无分布式调度。

## 验证

```powershell
go test ./internal/service/purchase_ai/... ./cmd
go vet ./internal/service/purchase_ai/assets ./internal/service/purchase_ai/video ./cmd
```

测试覆盖实际上传记录判定、部分失败、幂等、取消、重启恢复、HTTP 鉴权/参数、MCP 工具发现和结构化结果、图片校验、DASH 过滤和素材清晰度选择。
