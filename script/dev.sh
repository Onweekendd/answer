#!/bin/bash
# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements.  See the NOTICE file
# distributed with this work for additional information
# regarding copyright ownership.  The ASF licenses this file
# to you under the Apache License, Version 2.0 (the
# "License"); you may not use this file except in compliance
# with the License.  You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# 本地开发一条命令起后端：浏览器 Hub（answer browser，插件连它）和 air 热重载的 answer run。
# Ctrl+C 两个一起停。前端（frontend/web 的 vite）另外起。
#
#   bash script/dev.sh          # Hub + 后端
#   bash script/dev.sh --cube   # 再带上沙箱：WSL 里的 CubeSandbox 要先开着
#
# 两个进程分开：air 每次保存都重启后端，Hub 不跟着重启，插件的连接就不会断。
# 后端靠 BROWSER_HUB_URL 和 BROWSER_HUB_TOKEN 找到 Hub（internal/service/purchase_ai/browser.go），
# 没有它们就当作没有浏览器，Agent 降级成直接回答。

set -euo pipefail
cd "$(dirname "$0")/.."

hub_addr=127.0.0.1:8765

# .env 不会自己进到 air 起的进程里，这里导出。文件是 Windows 换行：去掉 \r，
# 不然 token 末尾多一个 \r，和插件里填的对不上
if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1090
  source <(tr -d '\r' < .env)
  set +a
fi
if [[ -z "${BROWSER_HUB_TOKEN:-}" ]]; then
  echo "没有 BROWSER_HUB_TOKEN：在 .env 里写上插件 popup 里填的那个 token" >&2
  exit 1
fi
export BROWSER_HUB_URL="http://$hub_addr"

if [[ "${1:-}" == "--cube" ]]; then
  # 本机 WSL 里的 CubeSandbox：CubeAPI 在 127.0.0.1:3000，CubeProxy 在 WSL 网卡的 443，证书是安装时 mkcert 签的
  export CUBE_TEMPLATE_ID="${CUBE_TEMPLATE_ID:-tpl-f4b59ef3005544189859a7ca}"
  export CUBE_PROXY_NODE_IP="${CUBE_PROXY_NODE_IP:-172.21.107.18}"
  export CUBE_PROXY_PORT_HTTP="${CUBE_PROXY_PORT_HTTP:-443}"
  export CUBE_CA_FILE="${CUBE_CA_FILE:-$(cygpath -m "$PWD/answer-data/cube/ca.crt")}"
  if ! curl -s -m 3 -o /dev/null "${CUBE_API_URL:-http://127.0.0.1:3000}"; then
    echo "连不上 CubeAPI（${CUBE_API_URL:-http://127.0.0.1:3000}）：先在 WSL 里把 CubeSandbox 开起来" >&2
    exit 1
  fi
fi

# 端口被占着就别起：8765 多半是连线上 Hub 的 SSH 隧道，8081 是后端已经在跑了
listening() {
  netstat -ano | grep -E "LISTENING" | grep -E "[:.]$1 " || true
}
for port in 8765 8081; do
  if [[ -n "$(listening "$port")" ]]; then
    echo "端口 $port 已经被占用：" >&2
    listening "$port" >&2
    if [[ "$port" == 8765 ]]; then
      echo "是连线上 Hub 的 SSH 隧道的话先关掉，插件 popup 也换回本地的 token" >&2
    fi
    exit 1
  fi
done

air_bin="$(command -v air || echo "$HOME/go/bin/air")"
if [[ ! -x "$air_bin" && ! -x "$air_bin.exe" ]]; then
  echo "没有 air：go install github.com/air-verse/air@latest" >&2
  exit 1
fi

# Hub 编成一个文件再跑：go run 会再起一个子进程，停的时候停不干净
mkdir -p tmp
echo "编译 Hub ..."
go build -o tmp/answer-hub.exe ./cmd/answer
./tmp/answer-hub.exe browser --console=false --addr "$hub_addr" &
hub_pid=$!
trap 'kill "$hub_pid" 2>/dev/null || true' EXIT

# 等 Hub 的调试接口能用了再起后端，后端一起来就可能调它
for _ in $(seq 1 30); do
  code="$(curl -s -m 2 -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $BROWSER_HUB_TOKEN" "$BROWSER_HUB_URL/debug/tabs" || true)"
  if [[ "$code" == 200 ]]; then
    break
  fi
  if ! kill -0 "$hub_pid" 2>/dev/null; then
    echo "Hub 没起来" >&2
    exit 1
  fi
  sleep 0.5
done
echo "Hub 在 ws://$hub_addr/agent，插件连上以后后端就能用浏览器"

# air 在前台：Ctrl+C 停掉它，退出时 trap 再把 Hub 停掉
"$air_bin" -c .air.toml
