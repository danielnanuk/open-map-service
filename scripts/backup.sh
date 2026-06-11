#!/usr/bin/env bash
# scripts/backup.sh — pg_dump + OS snapshot + 路由图构件 tar → ./backups/<ts>/
# 用法: bash scripts/backup.sh
# 生产:完成后用 rclone 或 aws s3 cp 同步到对象存储(见 README)
set -euo pipefail
cd "$(dirname "$0")/.."
TS=$(date +%Y%m%d%H%M%S)
DIR="backups/$TS"
mkdir -p "$DIR"

echo "== postgres =="
docker exec places-postgis-1 pg_dump -U places -d places -Fc > "$DIR/places.dump"

echo "== opensearch snapshot =="
curl -sf -X PUT "localhost:9200/_snapshot/local/snap-$TS?wait_for_completion=true" >/dev/null
echo "snap-$TS" > "$DIR/os_snapshot_name"

echo "== route graphs =="
tar -czf "$DIR/graphs.tar.gz" data/osrm data/valhalla 2>/dev/null || true

du -sh "$DIR"/*

# 保留最新 KEEP 份(本地 + OS 快照同步清理):每份 ~800MB,无清理 14 天打满磁盘(M5 事故教训)
KEEP=7
for old in $(ls -1dt backups/*/ 2>/dev/null | tail -n +$((KEEP+1))); do
  if [ -f "$old/os_snapshot_name" ]; then
    curl -s -X DELETE "localhost:9200/_snapshot/local/$(cat "$old/os_snapshot_name")" >/dev/null || true
  fi
  rm -rf "$old"
  echo "pruned: $old"
done

echo "DONE → $DIR"
echo "(生产:rclone copy $DIR remote:places-backup/$TS  或  aws s3 cp --recursive $DIR s3://my-bucket/places-backup/$TS)"
