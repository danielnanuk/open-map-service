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


@pytest.fixture()
def managed_indexes():
    prev = requests.get(f"{OS}/_alias/{ALIAS}", timeout=10)
    prev_targets = list(prev.json().keys()) if prev.status_code == 200 else []
    created: list[str] = []
    yield created
    for idx in created:
        requests.delete(f"{OS}/{idx}", timeout=10)
    if prev_targets:
        requests.post(f"{OS}/_aliases", timeout=10,
                      json={"actions": [{"add": {"index": t, "alias": ALIAS}} for t in prev_targets]})


def _search(body):
    r = requests.post(f"{OS}/{ALIAS}/_search", json=body)
    r.raise_for_status()
    return [h["_source"]["place_id"] for h in r.json()["hits"]["hits"]]

def test_index_and_query_paths(managed_indexes):
    name = create_index(OS)
    managed_indexes.append(name)
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
    managed_indexes.append(name2)
    swap_alias(OS, name2)
    aliases = requests.get(f"{OS}/_alias/{ALIAS}").json()
    assert list(aliases.keys()) == [name2]


def test_rows_formatted_address_no_country_duplication():
    import psycopg
    from etl.index import rows_from_postgis
    dsn = os.environ.get("DATABASE_URL", "postgresql://places:places@localhost:5432/places_test")
    # Use autocommit connection for DDL-style INSERT/DELETE so the row is
    # visible to the second connection; rows_from_postgis needs a regular
    # transaction block for its server-side (DECLARE) cursor.
    with psycopg.connect(dsn, autocommit=True) as ac_conn:
        ac_conn.execute("""INSERT INTO places (place_id, primary_source, names, categories, sources, address, geom)
            VALUES ('idx-test-addr', 'overture', '{"default":"X"}', '{cafe}', '[]',
                    '{"freeform":"Cambodia"}', ST_SetSRID(ST_MakePoint(104.9, 11.5), 4326))
            ON CONFLICT (place_id) DO NOTHING""")
        try:
            with psycopg.connect(dsn) as read_conn:
                row = next(r for r in rows_from_postgis(read_conn) if r["place_id"] == "idx-test-addr")
            assert row["formatted_address"] == "Cambodia"   # 不是 "Cambodia, Cambodia"
        finally:
            ac_conn.execute("DELETE FROM places WHERE place_id = 'idx-test-addr'")

def test_prune_old_indices_keeps_specified(managed_indexes):
    existing = {r["index"] for r in requests.get(
        f"{OS}/_cat/indices/{ALIAS}-*?h=index&format=json", timeout=10).json()}
    a = create_index(OS)
    managed_indexes.append(a)
    b = create_index(OS)
    managed_indexes.append(b)
    from etl.index import prune_old_indices
    deleted = prune_old_indices(OS, existing | {b})  # 只允许删 a
    assert a in deleted and b not in deleted
    left = {r["index"] for r in requests.get(
        f"{OS}/_cat/indices/{ALIAS}-*?h=index&format=json", timeout=10).json()}
    assert a not in left and b in left and existing <= left
