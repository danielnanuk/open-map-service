#!/usr/bin/env python3
"""scripts/load_test.py — §14.5:searchText 并发压测,报告 QPS 与 P50/P95/P99。
用法:python3 scripts/load_test.py [base] [concurrency] [total]"""
import json
import statistics
import sys
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
CONC = int(sys.argv[2]) if len(sys.argv) > 2 else 20
TOTAL = int(sys.argv[3]) if len(sys.argv) > 3 else 1000
QUERIES = ["អង្គរវត្ត", "coffee", "Royal Palace", "ផ្សារ", "hotel", "金边", "bank", "school"]


def one(i):
    body = json.dumps({"textQuery": QUERIES[i % len(QUERIES)], "pageSize": 10}).encode()
    req = urllib.request.Request(BASE + "/v1/places:searchText", data=body,
                                 headers={"Content-Type": "application/json"})
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=30) as r:
        r.read()
        ok = r.status == 200
    return time.time() - t0, ok


t0 = time.time()
with ThreadPoolExecutor(max_workers=CONC) as ex:
    results = list(ex.map(one, range(TOTAL)))
wall = time.time() - t0
lats = sorted(d for d, _ in results)
errs = sum(1 for _, ok in results if not ok)
q = lambda p: lats[int(len(lats) * p)]
print(f"total={TOTAL} conc={CONC} wall={wall:.1f}s qps={TOTAL/wall:.0f} errors={errs}")
print(f"P50={q(.5)*1000:.0f}ms P95={q(.95)*1000:.0f}ms P99={q(.99)*1000:.0f}ms")
