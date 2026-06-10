"""parquet → staging → places 的入库与裁剪。boundary 从 Overture divisions 提取 KH 国界。"""
import json

import duckdb
import psycopg
import pyarrow.parquet as pq

_UPSERT_SQL = """
INSERT INTO places (place_id, primary_source, names, categories, raw_category, phone, website,
                    address, confidence, sources, geom)
SELECT s.place_id, 'overture',
       jsonb_strip_nulls(jsonb_build_object('default', s.name_default, 'km', s.name_km,
                                            'en', s.name_en, 'zh', s.name_zh)),
       ARRAY[COALESCE(s.google_type, 'point_of_interest')],
       s.raw_category, s.phone, s.website,
       jsonb_strip_nulls(jsonb_build_object('freeform', s.addr_freeform, 'locality', s.addr_locality,
                                            'region', s.addr_region, 'country', s.addr_country)),
       s.confidence, COALESCE(s.sources, '[]'::jsonb),
       ST_SetSRID(ST_MakePoint(s.lon, s.lat), 4326)
FROM staging_overture s
JOIN country_boundary b ON b.iso = 'KH' AND ST_Contains(b.geom, ST_SetSRID(ST_MakePoint(s.lon, s.lat), 4326))
ON CONFLICT (place_id) DO UPDATE SET
  names = EXCLUDED.names, categories = EXCLUDED.categories, raw_category = EXCLUDED.raw_category,
  phone = EXCLUDED.phone, website = EXCLUDED.website, address = EXCLUDED.address,
  confidence = EXCLUDED.confidence, sources = EXCLUDED.sources, geom = EXCLUDED.geom,
  updated_at = now()
"""

def load_boundary_wkt(conn: psycopg.Connection, iso: str, wkt: str) -> None:
    # ★ ST_CollectionExtract(...,3):混合几何(GEOMETRYCOLLECTION)只保留多边形,再 ST_Multi 保证列类型
    conn.execute("""INSERT INTO country_boundary (iso, geom)
                    VALUES (%s, ST_Multi(ST_CollectionExtract(ST_GeomFromText(%s, 4326), 3)))
                    ON CONFLICT (iso) DO UPDATE SET geom = EXCLUDED.geom""", (iso, wkt))

def extract_kh_boundary_wkt(divisions_parquet: str) -> str:
    # ★ divisions 的 geometry 与 places 一样是原生 GEOMETRY(OGC:CRS84),不是 WKB
    duckdb.sql("INSTALL spatial; LOAD spatial")
    return duckdb.sql("""
        SELECT ST_AsText(ST_Union_Agg(geometry::GEOMETRY))
        FROM read_parquet(?)
        WHERE subtype = 'country' AND country = 'KH'""", params=[divisions_parquet]).fetchone()[0]

def _copy_value(v):
    # parquet 里的 sources 已是 JSON 字符串,原样透传;仅结构化值才编码
    return json.dumps(v, ensure_ascii=False) if isinstance(v, (list, dict)) else v

def copy_parquet_to_staging(conn: psycopg.Connection, parquet_path: str, table: str, columns: list[str]) -> int:
    rows = pq.read_table(parquet_path).to_pylist()
    conn.execute(f"TRUNCATE {table}")
    with conn.cursor().copy(f"COPY {table} ({','.join(columns)}) FROM STDIN") as cp:
        for r in rows:
            cp.write_row([_copy_value(r.get(c)) for c in columns])
    return len(rows)

def upsert_places_from_staging(conn: psycopg.Connection) -> int:
    return conn.execute(_UPSERT_SQL).rowcount

def load_osm_pois(conn: psycopg.Connection, parquet_path: str) -> int:
    n = copy_parquet_to_staging(conn, parquet_path, "osm_pois",
        ["osm_id", "name_default", "name_km", "name_en", "name_zh",
         "google_type", "phone", "website", "opening_hours", "lon", "lat"])
    conn.execute("UPDATE osm_pois SET geom = ST_SetSRID(ST_MakePoint(lon, lat), 4326) WHERE geom IS NULL")
    return n
