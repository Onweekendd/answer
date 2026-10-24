# 线上应用使用本机 Hub 和 CubeSandbox

服务器 `117.72.206.37` 上的 Answer 已通过反向 SSH 隧道访问本机服务。Hub、CubeAPI、CubeProxy 共用一条 SSH 连接；不需要本机公网 IP，也不需要更改 SSH 的 `GatewayPorts`。

平时只运行一条命令（仓库根目录，Windows PowerShell）：

```powershell
powershell -File .\script\local-services-tunnel.ps1
```

配置使用已有的 `screeps-server` SSH 别名。脚本断线后每 5 秒尝试重连，并用文件锁防止重复启动。此工作环境中已在后台启动，无须再开第二个。运行状态保存在 `tmp/local-services-tunnel.json`，SSH 错误保存在 `tmp/local-services-tunnel.stderr.log`。

对应的原始 SSH 命令是：

```sh
ssh -NT -o BatchMode=yes -o StrictHostKeyChecking=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -R 127.0.0.1:28765:127.0.0.1:8765 -R 127.0.0.1:23000:127.0.0.1:3000 -R 127.0.0.1:23443:127.0.0.1:443 screeps-server
```

| 服务 | 本机入口 | 服务器 SSH 监听 | Docker 私有入口 |
| --- | --- | --- | --- |
| 浏览器 Hub | `127.0.0.1:8765` | `127.0.0.1:28765` | `172.17.0.1:18765` |
| CubeAPI | `127.0.0.1:3000` | `127.0.0.1:23000` | `172.17.0.1:13000` |
| CubeProxy（TLS） | `127.0.0.1:443` | `127.0.0.1:23443` | `172.17.0.1:13443` |

本机原来的 Hub 和 WSL CubeSandbox 需要保持运行，浏览器插件继续连接本机 `ws://127.0.0.1:8765/agent`。本机关机或睡眠期间，线上浏览器和沙箱工具暂不可用；网站自身仍在服务器运行。

## 已完成的一次性服务器配置

- `/usr/local/lib/maibumai/local-tunnel-relay.py` 和 `maibumai-local-relay.service`：将 Docker 私有网关的三个入口转到 SSH 的回环监听；服务随服务器启动，并在失败时重启。
- `~/maibumai-repo/deploy/docker-compose.override.yaml`：覆盖 Answer 的 Hub 地址和令牌、CubeAPI 地址、模板 ID、CubeProxy 地址及 CA 路径，添加 `host.docker.internal:host-gateway`。其中 `CUBE_PROXY_SCHEME=https` 必须显式设置：SDK 仅在端口为 443 时自动选择 HTTPS，而转发端口是 13443。
- `~/maibumai-repo/deploy/local-services/ca.crt`：挂载本机 CubeProxy 的 CA 公钥证书。TLS 仍按 `*.cube.app` 域名校验，未关闭证书验证。

Compose 自动加载该覆盖文件，后续 `deploy.sh` 部署会继续使用隧道配置。覆盖文件含 Hub 令牌，权限为 `600`；它和本机配置、运行记录均不提交到 Git，也不进入镜像。

如需在另一台服务器重建，复制 `deploy/local-tunnel-relay.py` 到 `/usr/local/lib/maibumai/`、复制对应 `.service` 到 `/etc/systemd/system/`，按该服务器的 Docker 网关调整 `--bind`，再写入该机器的 Compose 覆盖配置。不要将私有入口绑定到 `0.0.0.0`。
