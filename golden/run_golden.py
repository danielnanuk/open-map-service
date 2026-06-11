"""黄金查询集执行器:hard 失败退出码 1,soft 失败仅告警。用法:python golden/run_golden.py [base_url]"""
import json
import sys

import requests
import yaml

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
ENDPOINTS = {"searchText": "/v1/places:searchText", "searchNearby": "/v1/places:searchNearby",
             "autocomplete": "/v1/places:autocomplete", "geocode": "/maps/api/geocode/json"}

def run_case(case: dict) -> tuple[bool, str]:
    if case["endpoint"] == "geocode":
        r = requests.get(BASE + ENDPOINTS["geocode"], params=case["params"], timeout=10)
    else:
        r = requests.post(BASE + ENDPOINTS[case["endpoint"]], json=case["body"], timeout=10)
    if r.status_code != 200:
        return False, f"HTTP {r.status_code}: {r.text[:200]}"
    body = r.json()
    if case.get("expect_status") and body.get("status") != case["expect_status"]:
        return False, f"status={body.get('status')} want {case['expect_status']}"
    if "expect_min_results" in case:
        n = len(body.get("places") or body.get("suggestions") or body.get("results") or [])
        return n >= case["expect_min_results"], f"results={n} (min {case['expect_min_results']})"
    blob = json.dumps(body, ensure_ascii=False)
    hit = next((e for e in case["expect_any"] if e in blob), None)
    return hit is not None, f"matched={hit!r}" if hit else f"none of {case['expect_any']} in top results"

def main() -> int:
    cases = yaml.safe_load(open("golden/cases.yaml"))
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
