#!/usr/bin/env bash
# db/migrate.sh — 按文件名顺序应用所有迁移(SQL 自身幂等)
set -euo pipefail
for f in db/migrations/*.sql; do
  echo "applying $f"
  docker compose exec -T postgis psql -U places -d places -v ON_ERROR_STOP=1 < "$f"
done
