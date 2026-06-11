#!/usr/bin/env bash
# db/migrate.sh — 按文件名顺序应用所有迁移(SQL 自身幂等)
set -euo pipefail
shopt -s nullglob
docker compose ps --status running postgis >/dev/null 2>&1 || { echo "postgis not running; run 'make up' first" >&2; exit 1; }
for f in db/migrations/*.sql; do
  echo "applying $f"
  docker compose exec -T postgis psql -U places -d "${TARGET_DB:-places}" -v ON_ERROR_STOP=1 < "$f"
done
