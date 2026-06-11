#!/usr/bin/env bash
# scripts/update_pipeline.sh — 数据更新蓝绿管道(spec §9)
# 顺序:OSM 刷新 → ETL 链(含漂移闸)→ 索引 alias 切换 → OSRM 三图重建+滚动替换 → Valhalla 重建
#
# cron 示例(每周日 02:00):
#   0 2 * * 0 cd /home/daniel/places && bash scripts/update_pipeline.sh >> logs/update.log 2>&1
#
# Nominatim 增量复制取舍:mediagis/nominatim 支持 REPLICATION_URL 增量更新。
# M5 选择全量周更(重导整个 PBF)而非常驻增量复制容器,原因:
#   1. 单节点柬埔寨数据量小(PBF ~50MB),全量导入 <15 分钟,和增量复制差距可接受;
#   2. 全量管道更简单,无累积状态,rollback 只需重换 PBF;
#   3. 增量复制需要 REPLICATION_URL=https://download.geofabrik.de/asia/cambodia-updates/
#      并在 compose 中开 NOMINATIM_REPLICATION_* 系列 env,适合高频地理数据场景;
#      运维可按需在 compose 中加 nominatim-update 服务覆写此策略。
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== [1/6] 刷新 OSM PBF =="
START_TOTAL=$(date +%s)
START=$(date +%s)
curl -fsSL --progress-bar -o data/cambodia-latest.osm.pbf.new \
    https://download.geofabrik.de/asia/cambodia-latest.osm.pbf
mv data/cambodia-latest.osm.pbf.new data/cambodia-latest.osm.pbf
echo "  PBF size: $(du -h data/cambodia-latest.osm.pbf | cut -f1)"
echo "  elapsed: $(($(date +%s)-START))s"

echo "== [2/6] 记录基线计数 =="
BEFORE=$(docker exec places-postgis-1 psql -U places -d places -t -A \
    -c "SELECT count(*) FROM places")
echo "  places before: $BEFORE"

echo "== [3/6] ETL 链(overture+osm+load+conflate)==="
START=$(date +%s)
# Overture 下载需要 overturemaps CLI(pip install overturemaps);
# 若 CLI 不可用则跳过(复用上次缓存的 parquet);OSM+load+conflate 仍刷新。
if command -v overturemaps &>/dev/null || \
   test -f "etl/.venv/bin/overturemaps" || \
   etl/.venv/bin/python -c "import overturemaps" 2>/dev/null; then
    make etl-overture
else
    echo "  [WARN] overturemaps CLI unavailable — skipping Overture download, reusing cached parquet"
    echo "  cached: $(du -h data/overture_places_raw.parquet 2>/dev/null | cut -f1) ($(cat data/overture_places_raw.parquet.state 2>/dev/null | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get(\"last_release\",\"unknown\"))' 2>/dev/null || echo 'unknown'))"
fi
make etl-osm etl-load etl-conflate
echo "  ETL elapsed: $(($(date +%s)-START))s"

echo "== [4/6] 漂移闸(±15%)=="
AFTER=$(docker exec places-postgis-1 psql -U places -d places -t -A \
    -c "SELECT count(*) FROM places")
python3 -c "
b, a = $BEFORE, $AFTER
drift = abs(a - b) / max(b, 1)
print(f'places: {b} -> {a} (drift {drift:.1%})')
assert drift < 0.15, f'ABORT: drift {drift:.1%} exceeds 15% — investigate before indexing'
print('drift check PASSED')
"

echo "== [5/6] 重建检索索引(alias 原子切换)=="
START=$(date +%s)
make etl-index
echo "  index elapsed: $(($(date +%s)-START))s"

echo "== [6/6] 路由图重建 + 滚动替换 =="
echo "  注:OSRM restart 窗口内 matrix 请求自动降级 Valhalla——属预期行为"
START=$(date +%s)
# OSRM 三图重建(写入 data/osrm/*;在线实例继续 mmap 旧文件)
make osrm-build
echo "  osrm-build elapsed: $(($(date +%s)-START))s"

# 滚动重启三个 OSRM 容器(逐个重启,降低同时不可用窗口)
for svc in osrm-car osrm-moto osrm-tuktuk; do
    echo "  restarting $svc ..."
    docker compose restart "$svc"
done

# Valhalla 重建:清旧瓦片,拷入新 PBF,重启后自动重建
START_V=$(date +%s)
rm -rf data/valhalla/valhalla_tiles data/valhalla/tiles 2>/dev/null || true
cp -f data/cambodia-latest.osm.pbf data/valhalla/
docker compose restart valhalla
echo "  valhalla restart elapsed: $(($(date +%s)-START_V))s"
echo "  (Valhalla will rebuild tiles on startup, watch: docker logs -f places-valhalla-1)"

TOTAL=$(($(date +%s)-START_TOTAL))
echo ""
echo "DONE. 总耗时: ${TOTAL}s"
echo "验证:make golden"
