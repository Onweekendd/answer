#!/usr/bin/env bash
# 备份「买不买」：数据库 + /data（上传的截图、config.yaml），保留最近 KEEP_DAYS 天。
#
# 放在 docker-compose.yaml 同目录，由 cron 每天执行：
#   17 3 * * * /root/maibumai/backup.sh >> /root/maibumai/backups/backup.log 2>&1
#
# 恢复数据库（会覆盖现有数据，先确认文件对不对）：
#   gunzip -c backups/db-XXXX.sql.gz | docker compose exec -T db psql -U answer -d answer
# 恢复 /data：
#   docker compose exec -T answer tar xzf - -C /data < backups/data-XXXX.tar.gz

# -e 任一命令失败就退出；-u 用到未定义变量就退出；
# -o pipefail 管道里任一环节失败，整条管道算失败（否则 pg_dump 失败、gzip 成功也会被当成成功）
set -euo pipefail

# 切到脚本所在目录：cron 执行时的当前目录是 $HOME，docker compose 要在 compose 文件旁边才找得到项目
cd "$(dirname "$(readlink -f "$0")")"

BACKUP_DIR="${BACKUP_DIR:-$PWD/backups}"
KEEP_DAYS="${KEEP_DAYS:-7}"
stamp="$(date +%Y%m%d-%H%M%S)"

mkdir -p "$BACKUP_DIR"
# 备份里有全部用户数据和 config.yaml（含数据库密码），只给 root 读
chmod 700 "$BACKUP_DIR"

# 先写到 .part，成功后再改名：中途失败不会留下一个看起来完整、其实是半截的备份
partial=()
cleanup() { rm -f "${partial[@]}"; }
trap cleanup EXIT

# 1. 数据库。pg_dump 在 db 容器里执行，走本地 socket，不需要密码；
#    -T 不分配终端，否则输出的二进制流会被终端处理弄坏
db_file="$BACKUP_DIR/db-$stamp.sql.gz"
partial+=("$db_file.part")
docker compose exec -T db pg_dump -U answer -d answer --no-owner | gzip > "$db_file.part"
mv "$db_file.part" "$db_file"

# 2. /data 目录：上传的截图和 config.yaml。i18n 每次启动都会重新生成，不用备份；
#    cache 恢复回去反而有害：旧的站点配置缓存会盖住数据库里的新值
data_file="$BACKUP_DIR/data-$stamp.tar.gz"
partial+=("$data_file.part")
docker compose exec -T answer tar czf - -C /data --exclude=./i18n --exclude=./cache . > "$data_file.part"
mv "$data_file.part" "$data_file"

# 3. 删掉超过 KEEP_DAYS 天的旧备份
find "$BACKUP_DIR" -maxdepth 1 \( -name 'db-*.sql.gz' -o -name 'data-*.tar.gz' \) -mtime +"$KEEP_DAYS" -delete

echo "$(date '+%F %T') backup ok: $(du -h "$db_file" | cut -f1) db, $(du -h "$data_file" | cut -f1) data"
