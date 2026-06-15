"""黄金查询集执行器:hard 失败退出码 1,soft 失败仅告警。用法:python golden/run_golden.py [--base-url URL] [--api-key KEY]"""
import argparse
import json
import sys

import requests
import yaml

parser = argparse.ArgumentParser()
parser.add_argument("base_url", nargs="?", default="http://localhost:8080",
                    help="Gateway base URL (positional, for backward compat)")
parser.add_argument("--base-url", dest="base_url_flag", default=None,
                    help="Gateway base URL (flag form)")
parser.add_argument("--api-key", dest="api_key", default=None,
                    help="API key sent as X-Goog-Api-Key header")
args = parser.parse_args()
BASE = args.base_url_flag if args.base_url_flag is not None else args.base_url
API_KEY = args.api_key
HEADERS = {"X-Goog-Api-Key": API_KEY} if API_KEY else {}

ENDPOINTS = {"searchText": "/v1/places:searchText", "searchNearby": "/v1/places:searchNearby",
             "autocomplete": "/v1/places:autocomplete", "geocode": "/maps/api/geocode/json",
             "routes": "/directions/v2:computeRoutes",
             "matrix": "/distanceMatrix/v2:computeRouteMatrix"}

def run_case(case: dict) -> tuple[bool, str]:
    if case["endpoint"] == "geocode":
        r = requests.get(BASE + ENDPOINTS["geocode"], params=case["params"], headers=HEADERS, timeout=10)
    else:
        r = requests.post(BASE + ENDPOINTS[case["endpoint"]], json=case["body"], headers=HEADERS, timeout=10)
    if r.status_code != 200:
        return False, f"HTTP {r.status_code}: {r.text[:200]}"
    body = r.json()
    if case.get("expect_status") and isinstance(body, dict) and body.get("status") != case["expect_status"]:
        return False, f"status={body.get('status')} want {case['expect_status']}"
    if "expect_min_results" in case:
        n = len(body.get("places") or body.get("suggestions") or body.get("results") or [])
        return n >= case["expect_min_results"], f"results={n} (min {case['expect_min_results']})"
    if "expect_distance_km" in case:
        routes = body.get("routes") or []
        if not routes:
            return False, "no routes"
        km = routes[0]["distanceMeters"] / 1000
        lo, hi = case["expect_distance_km"]
        return lo <= km <= hi, f"distance={km:.1f}km (want {lo}-{hi})"
    if "expect_matrix_elements" in case:
        if not isinstance(body, list):
            return False, f"matrix response not a list: {str(body)[:120]}"
        exists = sum(1 for e in body if e.get("condition") == "ROUTE_EXISTS")
        want = case["expect_matrix_elements"]
        return len(body) == want and exists > 0, \
            f"elements={len(body)} exists={exists} (want {want})"
    blob = json.dumps(body, ensure_ascii=False)
    hit = next((e for e in case["expect_any"] if e in blob), None)
    return hit is not None, f"matched={hit!r}" if hit else f"none of {case['expect_any']} in top results"

def main() -> int:
    cases_path = __file__.replace("run_golden.py", "cases.yaml")
    cases = yaml.safe_load(open(cases_path))
    hard_failures = 0
    for case in cases:
        ok, detail = run_case(case)
        tag = "PASS" if ok else ("SOFT-FAIL" if case.get("soft") else "FAIL")
        print(f"[{tag}] {case['name']}: {detail}")
        if not ok and not case.get("soft"):
            hard_failures += 1
    print(f"\n{len(cases)} cases, {hard_failures} hard failures")
    return 1 if hard_failures else 0

if __name__ == "__main__":
    sys.exit(main())
