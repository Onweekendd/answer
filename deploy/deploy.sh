#!/usr/bin/env bash
# 把「买不买」切到指定的镜像版本；新版本起不来就回滚到原来的版本。
#
# CI 通过 SSH 调用，也可以在服务器上手动执行：
#   ./deploy.sh <tag>        # tag 是镜像对应的 commit 号，比如 b11e974a
# 成功退出码 0；失败（含回滚）退出码非 0，CI 那一步会变红。

set -euo pipefail
cd "$(dirname "$(readlink -f "$0")")"

new_tag="${1:?用法：./deploy.sh <镜像 tag>}"
port="$(grep -E '^ANSWER_PORT=' .env | tail -1 | cut -d= -f2 || true)"
health_url="http://127.0.0.1:${port:-8080}/healthz"

# .env 里当前的 TAG
current_tag() { grep -E '^TAG=' .env | tail -1 | cut -d= -f2; }

# 把 .env 里的 TAG 改成 $1。写进文件而不是只在命令行传：服务器重启或手动 up 时，
# compose 读的都是 .env，线上跑哪个版本始终以文件为准
set_tag() { sed -i "s/^TAG=.*/TAG=$1/" .env; }

# 拉取并启动 .env 里 TAG 指定的版本
start() {
	docker compose pull answer
	docker compose up -d
}

# 等 answer 的 /healthz 返回 200，最多等 $1 秒；等到了返回 0，超时返回 1
wait_healthy() {
	local timeout="$1"
	# TODO(你) ①：每 2 秒用 curl 请求一次 $health_url，返回 200 就 return 0；超过 timeout 秒就 return 1。
	#   提示：
	#   - curl -fsS -o /dev/null -m 3 "$health_url"：-f 让非 2xx 也算失败，-m 3 防止单次请求卡住
	#   - 新容器启动时要先跑 init、upgrade（迁移），这段时间连接会被拒绝，curl 失败是正常的，继续等
	#   - 计时可以用 bash 内置的 $SECONDS（脚本启动后经过的秒数），也可以数循环次数
	#   - 注意 set -e：curl 失败会让整个脚本退出，所以要放在 if 或者 until 的条件里
	return 1
}

# 0. dockerd 拉镜像要经过 mihomo；代理没开时 pull 要卡几分钟才超时，不如一开始就报错
if ! systemctl is-active --quiet mihomo; then
	echo "mihomo is not running, docker pull would fail; start it with: clashctl on" >&2
	exit 1
fi

old_tag="$(current_tag)"
echo "deploying $new_tag (running $old_tag)"

# 1. 先备份：新版本的迁移出了问题，还能把数据恢复回来
./backup.sh

# 2. 切到新版本
set_tag "$new_tag"
start

# 3. 健康检查，失败就回滚
# TODO(你) ②：调用 wait_healthy（给 90 秒）。
#   成功：打印一行 "deployed $new_tag"，exit 0。
#   失败：
#     - 先打印 answer 最近 50 行日志（docker compose logs --tail 50 answer），CI 的日志里就能看到原因
#     - 把 TAG 改回 $old_tag，再 start 一次，再 wait_healthy 一次
#     - 最后 exit 1，让 CI 变红
#   想一想：回滚只换回了镜像。如果新版本已经跑过一个迁移（比如删了一列），
#   旧版本的代码还能用吗？这时候第 1 步的备份就派上用场了。
echo "TODO: health check and rollback" >&2
exit 1
