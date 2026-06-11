import os

import psycopg
import pytest

from etl.conflate import conflate

pytestmark = pytest.mark.integration
DSN = os.environ.get("DATABASE_URL", "postgresql://places:places@localhost:5432/places")

@pytest.fixture()
def conn():
    with psycopg.connect(DSN, autocommit=True) as c:
        c.execute("TRUNCATE places, osm_pois")
        c.execute("""INSERT INTO places (place_id, primary_source, names, categories, confidence, sources, geom)
            VALUES ('gers-malis', 'overture', '{"default":"Malis Restaurant"}', '{restaurant}', 0.9,
                    '[{"dataset":"meta","record_id":"m1"}]',
                    ST_SetSRID(ST_MakePoint(104.91600, 11.56210), 4326))""")
        # 同名餐厅,距离 ~30m → 应合并;带 OSM 独有字段
        c.execute("""INSERT INTO osm_pois (osm_id, name_default, name_km, google_type, phone, opening_hours, geom)
            VALUES ('osm:node:1', 'Malis', 'ម្លិះ', 'restaurant', '+855 15 814 888', 'Mo-Su 07:00-22:00',
                    ST_SetSRID(ST_MakePoint(104.91610, 11.56235), 4326))""")
        # 远处(>50m)POI → 应独立入库
        c.execute("""INSERT INTO osm_pois (osm_id, name_default, google_type, geom)
            VALUES ('osm:node:2', 'Far Noodle Shop', 'restaurant',
                    ST_SetSRID(ST_MakePoint(104.92600, 11.56210), 4326))""")
        yield c

def test_conflate_merge_insert_idempotent(conn):
    merged, inserted = conflate(conn)
    assert (merged, inserted) == (1, 1)
    row = conn.execute("""SELECT phone, opening_hours, names, sources FROM places
                          WHERE place_id='gers-malis'""").fetchone()
    assert row[0] == "+855 15 814 888" and row[1] == "Mo-Su 07:00-22:00"
    assert row[2]["km"] == "ម្លិះ"
    assert any(s["dataset"] == "osm" for s in row[3])
    assert conn.execute("SELECT count(*) FROM places").fetchone()[0] == 2
    conflate(conn)  # 重跑
    assert conn.execute("SELECT count(*) FROM places").fetchone()[0] == 2  # 幂等:不重复插入
    sources = conn.execute("SELECT sources FROM places WHERE place_id='gers-malis'").fetchone()[0]
    assert sum(1 for s in sources if s["dataset"] == "osm") == 1          # 幂等:不重复追加来源


def test_conflate_rerun_stable_with_two_osm_matches(conn):
    # B(更近)经 default 分支匹配并把自己的 en 填入;C(稍远)default 是高棉文,只能靠 en 分支。
    # 旧代码重跑时 C 的 en 会去比对 B 填入的 en 而掉出匹配集,被重复插入。
    conn.execute("""INSERT INTO places (place_id, primary_source, names, categories, sources, geom)
        VALUES ('gers-plaza', 'overture', '{"default":"Sunrise Plaza Hotel"}', '{lodging}',
                '[]', ST_SetSRID(ST_MakePoint(104.92000, 11.57000), 4326))""")
    conn.execute("""INSERT INTO osm_pois (osm_id, name_default, name_en, google_type, geom)
        VALUES ('osm:node:8', 'Sunrise Plaza', 'SP Grand', 'lodging',
                ST_SetSRID(ST_MakePoint(104.92001, 11.57001), 4326))""")
    conn.execute("""INSERT INTO osm_pois (osm_id, name_default, name_en, google_type, geom)
        VALUES ('osm:node:9', 'សាន់រ៉ាយ', 'Sunrise Plaza Hotel', 'lodging',
                ST_SetSRID(ST_MakePoint(104.92003, 11.57003), 4326))""")
    conflate(conn)
    n1 = conn.execute("SELECT count(*) FROM places").fetchone()[0]
    conflate(conn)
    n2 = conn.execute("SELECT count(*) FROM places").fetchone()[0]
    assert n1 == n2, f"rerun changed places count {n1} -> {n2}"


def test_conflate_fanin_preserves_all_nodes(conn):
    # 两个 OSM 节点扇入同一地点:近者 phone 胜出,远者的 km 名也要补进来,sources 记录两者
    conn.execute("""INSERT INTO places (place_id, primary_source, names, categories, sources, geom)
        VALUES ('gers-fanin', 'overture', '{"default":"Brown Coffee"}', '{cafe}', '[]',
                ST_SetSRID(ST_MakePoint(104.93000, 11.58000), 4326))""")
    conn.execute("""INSERT INTO osm_pois (osm_id, name_default, phone, google_type, geom)
        VALUES ('osm:node:21', 'Brown Coffee', '+855 11 111 111', 'cafe',
                ST_SetSRID(ST_MakePoint(104.93001, 11.58001), 4326))""")
    conn.execute("""INSERT INTO osm_pois (osm_id, name_default, name_km, google_type, geom)
        VALUES ('osm:way:22', 'Brown Coffee', 'ប្រាវន៍', 'cafe',
                ST_SetSRID(ST_MakePoint(104.93010, 11.58010), 4326))""")
    conflate(conn)
    phone, names, sources = conn.execute("""SELECT phone, names, sources FROM places
                                            WHERE place_id='gers-fanin'""").fetchone()
    assert phone == "+855 11 111 111"
    assert names["km"] == "ប្រាវន៍"
    ids = {s["record_id"] for s in sources if s["dataset"] == "osm"}
    assert ids == {"osm:node:21", "osm:way:22"}
    total = conn.execute("SELECT count(*) FROM places").fetchone()[0]
    conflate(conn)  # 重跑:不新增行、不重复追加来源
    assert conn.execute("SELECT count(*) FROM places").fetchone()[0] == total
    sources2 = conn.execute("SELECT sources FROM places WHERE place_id='gers-fanin'").fetchone()[0]
    assert sum(1 for s in sources2 if s["dataset"] == "osm") == 2


def test_conflate_category_gate_rejects_real_category_conflict(conn):
    conn.execute("""INSERT INTO places (place_id, primary_source, names, categories, sources, geom)
        VALUES ('gers-bank', 'overture', '{"default":"Golden Tower"}', '{bank}', '[]',
                ST_SetSRID(ST_MakePoint(104.94000, 11.59000), 4326))""")
    conn.execute("""INSERT INTO osm_pois (osm_id, name_default, google_type, geom)
        VALUES ('osm:node:23', 'Golden Tower', 'restaurant',
                ST_SetSRID(ST_MakePoint(104.94001, 11.59001), 4326))""")
    conflate(conn)
    assert conn.execute("SELECT count(*) FROM places WHERE place_id='osm:node:23'").fetchone()[0] == 1
    assert conn.execute("""SELECT count(*) FROM places WHERE place_id='gers-bank'
                           AND sources @> '[{"dataset":"osm"}]'""").fetchone()[0] == 0


def test_conflate_similarity_below_threshold_not_merged(conn):
    # word_similarity('Java Coffee Shop', 'Java Cafe House') = 0.3529412 (< 0.45 threshold)
    conn.execute("""INSERT INTO places (place_id, primary_source, names, categories, sources, geom)
        VALUES ('gers-thresh', 'overture', '{"default":"Java Cafe House"}', '{cafe}', '[]',
                ST_SetSRID(ST_MakePoint(104.95000, 11.60000), 4326))""")
    conn.execute("""INSERT INTO osm_pois (osm_id, name_default, google_type, geom)
        VALUES ('osm:node:24', 'Java Coffee Shop', 'cafe',
                ST_SetSRID(ST_MakePoint(104.95001, 11.60001), 4326))""")
    conflate(conn)
    assert conn.execute("SELECT count(*) FROM places WHERE place_id='osm:node:24'").fetchone()[0] == 1
