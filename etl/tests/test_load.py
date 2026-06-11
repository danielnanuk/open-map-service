import json
import os

import psycopg
import pyarrow as pa
import pyarrow.parquet as pq
import pytest

from etl.load import copy_parquet_to_staging, load_boundary_wkt, upsert_places_from_staging

pytestmark = pytest.mark.integration
DSN = os.environ.get("DATABASE_URL", "postgresql://places:places@localhost:5432/places")
# 把"国界"造成金边附近的盒子,gers-in 在内、gers-out 在外
BOX = "MULTIPOLYGON(((104 11,106 11,106 12,104 12,104 11)))"

STAGING_COLUMNS = ["place_id", "name_default", "name_km", "name_en", "name_zh",
                   "raw_category", "google_type", "phone", "website",
                   "addr_freeform", "addr_locality", "addr_region", "addr_country",
                   "confidence", "sources", "lon", "lat"]

@pytest.fixture()
def conn():
    with psycopg.connect(DSN, autocommit=True) as c:
        c.execute("TRUNCATE places, staging_overture, country_boundary")
        yield c

def _stage(conn, pid, lon, lat):
    conn.execute("""INSERT INTO staging_overture
        (place_id,name_default,google_type,confidence,sources,lon,lat)
        VALUES (%s,%s,'restaurant',0.9,%s,%s,%s)""",
        (pid, f"name-{pid}", json.dumps([{"dataset": "meta", "record_id": pid}]), lon, lat))

def test_clip_and_idempotent_upsert(conn):
    load_boundary_wkt(conn, "KH", BOX)
    _stage(conn, "gers-in", 104.9, 11.5)
    _stage(conn, "gers-out", 100.0, 11.5)
    assert upsert_places_from_staging(conn) == 1
    assert upsert_places_from_staging(conn) == 1   # 幂等
    ids = [r[0] for r in conn.execute("SELECT place_id FROM places")]
    assert ids == ["gers-in"]
    names = conn.execute("SELECT names FROM places WHERE place_id='gers-in'").fetchone()[0]
    assert names["default"] == "name-gers-in"

# ★ COPY 路径回归测试:parquet 里 sources 是 JSON 字符串,经 COPY → staging → upsert 后必须是 jsonb 数组
def test_copy_path_sources_is_jsonb_array(conn, tmp_path):
    load_boundary_wkt(conn, "KH", BOX)
    row = {c: None for c in STAGING_COLUMNS}
    row.update({"place_id": "gers-copy", "name_default": "Copy Shop", "google_type": "cafe",
                "confidence": 0.8, "sources": '[{"dataset":"meta","record_id":"m9"}]',
                "lon": 104.95, "lat": 11.55})
    schema = pa.schema([(c, pa.float64() if c in ("lon", "lat") else
                         (pa.float32() if c == "confidence" else pa.string())) for c in STAGING_COLUMNS])
    p = tmp_path / "stage.parquet"
    pq.write_table(pa.Table.from_pylist([row], schema=schema), p)
    assert copy_parquet_to_staging(conn, str(p), "staging_overture", STAGING_COLUMNS) == 1
    assert upsert_places_from_staging(conn) == 1
    typ, src = conn.execute("""SELECT jsonb_typeof(sources), sources
                               FROM places WHERE place_id='gers-copy'""").fetchone()
    assert typ == "array" and src[0]["dataset"] == "meta"

# ★ 边界几何加固:混入线状几何时仍能得到 MultiPolygon
def test_boundary_hardened_against_collections(conn):
    load_boundary_wkt(conn, "KH",
        "GEOMETRYCOLLECTION(POLYGON((104 11,106 11,106 12,104 12,104 11)),LINESTRING(100 10,101 11))")
    gtype = conn.execute("SELECT GeometryType(geom) FROM country_boundary WHERE iso='KH'").fetchone()[0]
    assert gtype == "MULTIPOLYGON"
