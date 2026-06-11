#!/usr/bin/env bash
# scripts/matrix_benchmark.sh — spec §13-M4:500×500 基准 + 阈值/降级路径计时
# 用法:bash scripts/matrix_benchmark.sh [gateway_url]
set -euo pipefail
G="${1:-http://localhost:8080}"

python3 - "$G" <<'PY'
import json, random, sys, time, urllib.request

G = sys.argv[1]
random.seed(42)

def pts(n):
    return [{"waypoint":{"location":{"latLng":{
        "latitude": round(random.uniform(11.52, 11.62), 6),
        "longitude": round(random.uniform(104.88, 104.95), 6)}}}} for _ in range(n)]

def run(n, m, mode="DRIVE", profile=None, label="", timeout=300):
    body = {"origins": pts(n), "destinations": pts(m), "travelMode": mode}
    if profile:
        body["vehicleProfile"] = profile
    data = json.dumps(body).encode()
    t0 = time.time()
    req = urllib.request.Request(G + "/distanceMatrix/v2:computeRouteMatrix",
                                 data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        arr = json.load(r)
    dt = time.time() - t0
    exists = sum(1 for e in arr if e.get("condition") == "ROUTE_EXISTS")
    print(f"{label or f'{n}x{m}'}: {len(arr)} elements, {exists} routable, {dt:.2f}s")
    return dt, len(arr), exists

run(10, 10, label="warmup 10x10 (Valhalla)")
dt, total, exists = run(500, 500, label="BENCH 500x500 DRIVE (OSRM)")
assert total == 250000, total
assert exists > 200000, f"routable too low: {exists}"
print(f"500x500 acceptance (<10s): {'PASS' if dt < 10 else 'FAIL'} ({dt:.2f}s)")
run(500, 500, mode="TWO_WHEELER", label="BENCH 500x500 moto (OSRM)")
run(500, 500, mode="TWO_WHEELER", profile="tuktuk", label="BENCH 500x500 tuktuk (OSRM)")
run(60, 60, label="60x60 DRIVE (OSRM path)")
run(40, 40, label="40x40 DRIVE (Valhalla path)")
PY
