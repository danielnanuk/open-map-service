#!/usr/bin/env bash
# scripts/tuktuk_calibration.sh — spec §14.3: motor_scooter 默认 vs 调参 对比 (Valhalla 3.7.0)
#
# KNOB VERIFICATION FINDINGS (Step 1):
# - Valhalla 3.7.0 has NO low_speed_vehicle costing (error 125 confirmed by T1).
# - motor_scooter top_speed knob IS functional. JSON format "costing_options":{"motor_scooter":{...}} is correct.
# - Urban Phnom Penh roads are tagged at ≤30kph (secondary/residential). Since motor_scooter
#   default top_speed=45kph, any top_speed value ≥30 produces IDENTICAL urban results —
#   the cap is never binding because road speeds are already below it.
# - On inter-city OD (PP→Siem Reap 316km): top_speed:45=default (30950s, 349.9km),
#   top_speed:40 (33644s, 354.0km, +8.7%), top_speed:50 (25101s, 315.6km, -19%).
#   The jump at 50 reveals NR6 highway segments tagged >45kph → scooter avoids them by default.
# - use_highways:0.1 alone on inter-city gives same route as top_speed:40 (33644s).
#   Combined top_speed:40+use_highways:0.1 = same as top_speed:40 alone (no additive effect).
# - CONCLUSION: top_speed:40 is the effective tuk-tuk differentiator for inter-city routes.
#   Urban results are identical to default by design (appropriate: tuk-tuk IS slow in city
#   because roads are slow, not because we cap it further). The costing_options JSON
#   format is confirmed correct; the "suspicion" from T1 was due to testing urban-only ODs.
#
# 用法: bash scripts/tuktuk_calibration.sh [valhalla_url]
set -euo pipefail
V="${1:-http://localhost:8002}"
ODS=(
  "11.5564,104.9282,11.5696,104.9210" "11.5564,104.9282,11.5446,104.9160"
  "11.5696,104.9210,11.5446,104.9160" "11.5625,104.9311,11.5696,104.9210"
  "11.5564,104.9282,11.5526,104.9282" "11.5446,104.9160,11.5984,104.9192"
  "11.5984,104.9192,11.5696,104.9210" "11.5526,104.9282,11.5625,104.9311"
  "11.5468,104.8943,11.5564,104.9282" "11.5468,104.8943,11.5696,104.9210"
  "11.5625,104.9311,11.5446,104.9160" "11.5984,104.9192,11.5564,104.9282"
  "11.5526,104.9282,11.5446,104.9160" "11.5468,104.8943,11.5984,104.9192"
  "11.5762,104.9230,11.5564,104.9282" "11.5762,104.9230,11.5446,104.9160"
  "11.5762,104.9230,11.5468,104.8943" "11.5625,104.9311,11.5984,104.9192"
  "11.5696,104.9210,11.5762,104.9230" "11.5526,104.9282,11.5984,104.9192"
)

q() {
  IFS=, read -r alat alon blat blon <<<"$2"
  curl -s -X POST "$V/route" -H 'Content-Type: application/json' -d "{
    \"locations\":[{\"lat\":$alat,\"lon\":$alon},{\"lat\":$blat,\"lon\":$blon}],
    $1,\"units\":\"kilometers\"}" |
    python3 -c "import sys,json
try:
  t=json.load(sys.stdin).get('trip',{}); s=t.get('summary',{})
  print(f\"{s.get('length',0):.2f},{s.get('time',0):.0f}\")
except Exception:
  print('ERR,ERR')"
}

echo "od,auto_km,auto_s,scooter_km,scooter_s,tuktuk_km,tuktuk_s"
for od in "${ODS[@]}"; do
  a=$(q '"costing":"auto"' "$od")
  m=$(q '"costing":"motor_scooter"' "$od")
  # tuktuk profile: top_speed:40 (below NR-class highway speeds >45kph) + use_highways:0.1
  # top_speed:40 confirmed effective on inter-city; urban ODs identical by design (road speeds ≤30kph)
  k=$(q '"costing":"motor_scooter","costing_options":{"motor_scooter":{"top_speed":40,"use_highways":0.1}}' "$od")
  echo "$od,$a,$m,$k"
done
