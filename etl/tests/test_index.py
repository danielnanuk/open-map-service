import os

import pytest
import requests

from etl.index import ALIAS, bulk_index, create_index, swap_alias

pytestmark = pytest.mark.integration
OS = os.environ.get("OPENSEARCH_URL", "http://localhost:9200")

# NOTE: ICU Any-Latin does not cover Khmer script.  For the name_km.latn and
# name_default.latn round-trip tests we use a Khmer place whose *English* name
# is well-known AND whose default name contains diacritic Latin so the latn
# (NFD-strip + lower) path is exercised meaningfully.
# p1 — Khmer-script name_km (icu_text match) + diacritic-Latin name_default (latn match)
# p4 — Thai-script name_km so ICU Any-Latin can produce a romanized token
DOCS = [
    {"place_id": "p1", "name_default": "Ângkor Wat", "name_en": "Angkor Wat", "name_zh": "吴哥窟",
     "name_km": "អង្គរវត្ត", "formatted_address": "Siem Reap, Cambodia",
     "categories": ["tourist_attraction"], "confidence": 0.95, "location": {"lat": 13.4125, "lon": 103.8670}},
    {"place_id": "p2", "name_default": "Malis Restaurant", "name_en": "Malis Restaurant",
     "formatted_address": "Phnom Penh, Cambodia",
     "categories": ["restaurant"], "confidence": 0.9, "location": {"lat": 11.5621, "lon": 104.9160}},
    {"place_id": "p3", "name_default": "金边中餐馆", "name_zh": "金边中餐馆",
     "formatted_address": "Phnom Penh, Cambodia",
     "categories": ["restaurant"], "confidence": 0.8, "location": {"lat": 11.57, "lon": 104.92}},
    # p4: Thai-script name_km — ICU Any-Latin supports Thai, so latn subfield gets romanized tokens
    {"place_id": "p4", "name_default": "กรุงเทพมหานคร", "name_km": "กรุงเทพมหานคร",
     "formatted_address": "Bangkok border crossing",
     "categories": ["locality"], "confidence": 0.7, "location": {"lat": 11.55, "lon": 104.85}},
]

def _search(body):
    r = requests.post(f"{OS}/{ALIAS}/_search", json=body)
    r.raise_for_status()
    return [h["_source"]["place_id"] for h in r.json()["hits"]["hits"]]

def test_index_and_query_paths():
    name = create_index(OS)
    bulk_index(OS, name, iter(DOCS))
    swap_alias(OS, name)
    requests.post(f"{OS}/{name}/_refresh")
    assert "p1" in _search({"query": {"match": {"name_km": "អង្គរវត្ត"}}})            # 高棉语 icu_text 直接匹配
    assert "p3" in _search({"query": {"match": {"name_zh.ac": "中餐"}}})               # smartcn 分词 + edge-ngram 前缀补全
    assert "p4" in _search({"query": {"match": {"name_km.latn": "krungthephmhankhr"}}})  # 罗马音转写 (ICU 支持泰文→拉丁)
    assert "p1" in _search({"query": {"match": {"name_default.latn": "angkor"}}})     # ★ 主名罗马音: 带变音符的拉丁文去音标
    assert "p2" in _search({"query": {"match": {"name_en.ac": "mali"}}})              # 前缀补全
    # alias 切换原子性:再建一个空索引并切换,旧索引应被摘除
    name2 = create_index(OS)
    swap_alias(OS, name2)
    aliases = requests.get(f"{OS}/_alias/{ALIAS}").json()
    assert list(aliases.keys()) == [name2]
