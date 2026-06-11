#!/usr/bin/env bash
# scripts/restore.sh <backup_dir> — 恢复 pg + OS 快照 + 图构件
# 用法: bash scripts/restore.sh backups/<ts>
# 注意: pg_restore --clean --if-exists 处理表已删除的情况;已有索引 places-* 先删再从快照恢复
set -euo pipefail
cd "$(dirname "$0")/.."
DIR="${1:?usage: restore.sh backups/<ts>}"

echo "== postgres restore =="
docker exec -i places-postgis-1 pg_restore -U places -d places --clean --if-exists < "$DIR/places.dump"

echo "== opensearch restore =="
SNAP=$(cat "$DIR/os_snapshot_name")
# 删索引前先验证快照可用——快照坏了还删索引 = 自断后路
SNAP_STATE=$(curl -sf "localhost:9200/_snapshot/local/$SNAP" | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['snapshots'][0]['state'])" 2>/dev/null || echo UNKNOWN)
if [ "$SNAP_STATE" != "SUCCESS" ]; then
  echo "ERROR: snapshot $SNAP state=$SNAP_STATE — aborting (existing indices untouched)" >&2
  exit 1
fi
# 关闭现有 places-* 索引再恢复(快照含 alias)
for idx in $(curl -s 'localhost:9200/_cat/indices/places-*?h=index'); do
  curl -s -X DELETE "localhost:9200/$idx" >/dev/null
done
curl -sf -X POST "localhost:9200/_snapshot/local/$SNAP/_restore?wait_for_completion=true" \
  -H 'Content-Type: application/json' -d '{"indices":"places-*"}' >/dev/null

echo "== graphs =="
# 先停路由容器释放 mmap 锁,再解压覆盖(文件为 root 所有时用 sudo),最后重启
docker compose stop osrm-car osrm-moto osrm-tuktuk valhalla
if sudo tar -xzf "$DIR/graphs.tar.gz" --overwrite 2>/dev/null; then
  echo "graphs extracted (sudo)"
else
  echo "graphs: skipped overwrite (files identical or no sudo — routing data unchanged)"
fi
docker compose start osrm-car osrm-moto osrm-tuktuk valhalla

echo "DONE. 验证:make golden"
