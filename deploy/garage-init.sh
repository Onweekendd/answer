#!/usr/bin/env bash
# 初始化 Garage：给唯一的节点分配存储、建桶、导入 answer 用的密钥。每一步都可以重复执行。
# deploy.sh 每次部署都会调用；也可以手动执行：./garage-init.sh
#
# Garage 镜像里只有 garage 这一个程序，没有 shell，所以命令都从宿主机用 docker compose exec 发进去。
# 密钥在 .env 里（S3_ACCESS_KEY_ID、S3_SECRET_ACCESS_KEY），这里只读，不打印

set -euo pipefail
cd "$(dirname "$(readlink -f "$0")")"

env_value() { grep -E "^$1=" .env | tail -1 | cut -d= -f2-; }
garage() { docker compose exec -T -e RUST_LOG=warn garage /garage "$@"; }

bucket="$(env_value S3_BUCKET)"
bucket="${bucket:-maibumai}"
key_id="$(env_value S3_ACCESS_KEY_ID)"
secret="$(env_value S3_SECRET_ACCESS_KEY)"
if [[ -z "$key_id" || -z "$secret" ]]; then
	echo "S3_ACCESS_KEY_ID / S3_SECRET_ACCESS_KEY are not set in .env" >&2
	exit 1
fi

# 等 garage 能回答（容器刚起来时 RPC 还没准备好）
for _ in $(seq 1 30); do
	if garage status >/dev/null 2>&1; then
		break
	fi
	sleep 1
done

# 1. 节点还没分配存储时，分配 1G 并生效（单节点，zone 随便起个名字）
if garage status | grep -q "NO ROLE ASSIGNED"; then
	node="$(garage node id -q | cut -d@ -f1)"
	garage layout assign -z dc1 -c 1G "$node"
	version="$(garage layout show | sed -n 's/.*apply --version \([0-9]*\).*/\1/p' | head -1)"
	garage layout apply --version "${version:-1}"
fi

# 2. 桶和密钥，已经有了就跳过
if ! garage bucket info "$bucket" >/dev/null 2>&1; then
	garage bucket create "$bucket"
fi
if ! garage key info "$key_id" >/dev/null 2>&1; then
	garage key import --yes -n answer "$key_id" "$secret" >/dev/null
fi
garage bucket allow --read --write "$bucket" --key "$key_id" >/dev/null
echo "garage ready: bucket $bucket"
