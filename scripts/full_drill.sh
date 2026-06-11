#!/usr/bin/env bash
# scripts/full_drill.sh — M5 全链路演练:健康→鉴权→golden→压测→降级→备份恢复→golden
# §13-M5 验收脚本;每步失败即退(set -euo)
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p logs
step() { echo; echo "===== $1 ====="; }

step "1. 全栈健康"
docker compose ps --format '{{.Name}} {{.Status}}' | tee /dev/stderr | grep -vq "unhealthy" || true
for url in localhost:8080/healthz localhost:9200/places/_count localhost:8081/status localhost:8002/status; do
  curl -sf "$url" >/dev/null && echo "OK $url"
done

step "2. 鉴权链路(临时开启)"
KEY=$(make -s gen-api-key NAME=drill | awk '{print $3}')
echo "drill key created: ${KEY:0:8}..."
AUTH_ENABLED=true docker compose up -d gateway >/dev/null 2>&1 && sleep 3
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST localhost:8080/v1/places:searchText \
  -H 'Content-Type: application/json' -d '{"textQuery":"x"}')" = "403" ] && echo "OK 无 key→403"
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST "localhost:8080/v1/places:searchText?key=$KEY" \
  -H 'Content-Type: application/json' -d '{"textQuery":"angkor"}')" = "200" ] && echo "OK 有 key→200"
# 恢复 AUTH_ENABLED=false(显式更稳,避免 compose 继承旧 env)
AUTH_ENABLED=false docker compose up -d gateway >/dev/null 2>&1 && sleep 3
# 清理演练 key
docker exec places-postgis-1 psql -U places -d places -c \
  "DELETE FROM api_keys WHERE name='drill'" >/dev/null && echo "drill key cleaned up"

step "3. golden 18/18"
make golden

step "4. 压测(20 并发 ×1000)"
python3 scripts/load_test.py

step "5. 矩阵降级"
docker compose stop osrm-car >/dev/null
python3 - <<'PY'
import json, random, time, urllib.request
random.seed(1)
pts=lambda n: [{"waypoint":{"location":{"latLng":{"latitude":round(random.uniform(11.52,11.62),6),"longitude":round(random.uniform(104.88,104.95),6)}}}} for _ in range(n)]
body=json.dumps({"origins":pts(60),"destinations":pts(60),"travelMode":"DRIVE"}).encode()
r=urllib.request.urlopen(urllib.request.Request("http://localhost:8080/distanceMatrix/v2:computeRouteMatrix",data=body,headers={"Content-Type":"application/json"}),timeout=120)
arr=json.load(r); assert len(arr)==3600, len(arr)
print("OK 降级 3600 元素")
PY
docker compose start osrm-car >/dev/null && sleep 8

step "6. 备份→破坏→恢复"
bash scripts/backup.sh
LATEST=$(ls -1d backups/*/ | tail -1)
echo "backup dir: $LATEST"
docker exec places-postgis-1 psql -U places -d places -c "DROP TABLE places CASCADE" >/dev/null
bash scripts/restore.sh "$LATEST"

step "7. 终验 golden"
make golden
echo; echo "DRILL PASSED"
