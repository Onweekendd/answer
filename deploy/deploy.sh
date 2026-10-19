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

# .env 里没有 $1（或者是空的）时写进 $2。生成的密钥只写文件，不打印
ensure_env() {
	if grep -qE "^$1=." .env; then
		return
	fi
	if grep -qE "^$1=" .env; then
		sed -i "s|^$1=.*|$1=$2|" .env
	else
		echo "$1=$2" >>.env
	fi
	echo "generated $1 in .env"
}

# 拉取并启动 .env 里 TAG 指定的版本
start() {
	docker compose pull answer
	docker compose up -d
}

# 等 answer 的 /healthz 返回 200，最多等 $1 秒；等到了返回 0，超时返回 1
wait_healthy() {
	local deadline=$((SECONDS + $1))
	# a new container runs init and upgrade (migrations) before it listens, so
	# refused connections are expected for a while. curl sits in the until
	# condition, where its failure does not trip set -e.
	# -f: a non-2xx answer counts as a failure; -m 3: one stuck request cannot eat the wait
	until curl -fsS -o /dev/null -m 3 "$health_url" 2>/dev/null; do
		if ((SECONDS >= deadline)); then
			return 1
		fi
		sleep 2
	done
	return 0
}

# 0. dockerd 拉镜像要经过 mihomo；代理没开时 pull 要卡几分钟才超时，不如一开始就报错
if ! systemctl is-active --quiet mihomo; then
	echo "mihomo is not running, docker pull would fail; start it with: clashctl on" >&2
	exit 1
fi

old_tag="$(current_tag)"
echo "deploying $new_tag (running $old_tag)"

# Garage 的密钥：第一次部署时生成，以后一直用 .env 里的。要在第一条 docker compose 之前：
# compose 文件里这几个变量是必填的，缺了连备份（docker compose exec）都跑不了
ensure_env GARAGE_RPC_SECRET "$(openssl rand -hex 32)"
ensure_env S3_ACCESS_KEY_ID "GK$(openssl rand -hex 12)"
ensure_env S3_SECRET_ACCESS_KEY "$(openssl rand -hex 32)"

# 1. 先备份：新版本的迁移出了问题，还能把数据恢复回来
./backup.sh

# 2. 切到新版本
set_tag "$new_tag"
start
# 建桶、导入密钥，每次都跑（已经有的跳过）。失败只影响存图，网站照常，所以不让部署失败
bash ./garage-init.sh || echo "garage-init.sh failed: the agent cannot save pictures until it succeeds" >&2

# 3. 健康检查，失败就回滚
if wait_healthy 90; then
	echo "deployed $new_tag"
	exit 0
fi

# the log goes to the CI output, so the reason is visible without logging in
echo "$new_tag is not healthy after 90s, rolling back to $old_tag" >&2
docker compose logs --tail 50 answer >&2

# Rollback only swaps the image back. If the new version already ran a
# migration the old code cannot read, restore the backup taken in step 1.
set_tag "$old_tag"
start
if wait_healthy 90; then
	echo "rolled back to $old_tag" >&2
else
	echo "rollback to $old_tag is not healthy either; the site is down, check by hand" >&2
fi
exit 1
