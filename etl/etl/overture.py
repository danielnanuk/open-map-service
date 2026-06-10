"""Overture 柬埔寨抽取:download() 调 overturemaps CLI;transform() 用 DuckDB 拍平 nested schema。"""
import subprocess
import duckdb
import pyarrow as pa
import pyarrow.parquet as pq
from etl.categories import overture_to_google

CAMBODIA_BBOX = "102.33,9.90,107.63,14.70"

# map_extract 返回 list,取 [1];struct 用点号访问。geometry 是 WKB blob。
_TRANSFORM_SQL = """
INSTALL spatial; LOAD spatial;
SELECT
  id                                            AS place_id,
  names."primary"                               AS name_default,
  map_extract(names.common, 'km')[1]            AS name_km,
  map_extract(names.common, 'en')[1]            AS name_en,
  map_extract(names.common, 'zh')[1]            AS name_zh,
  categories."primary"                          AS raw_category,
  phones[1]                                     AS phone,
  websites[1]                                   AS website,
  addresses[1].freeform                         AS addr_freeform,
  addresses[1].locality                         AS addr_locality,
  addresses[1].region                           AS addr_region,
  addresses[1].country                          AS addr_country,
  confidence::REAL                              AS confidence,
  to_json(sources)::VARCHAR                     AS sources_json,
  ST_X(geometry::GEOMETRY)                      AS lon,
  ST_Y(geometry::GEOMETRY)                      AS lat
FROM read_parquet(?)
WHERE names."primary" IS NOT NULL
"""

def download(out_places: str, out_divisions: str) -> None:
    # NOTE: overturemaps 1.0.0 uses -t flag (short form) instead of --type=
    subprocess.run(["overturemaps", "download", f"--bbox={CAMBODIA_BBOX}",
                    "-f", "geoparquet", "-t", "place", "-o", out_places], check=True)
    subprocess.run(["overturemaps", "download", f"--bbox={CAMBODIA_BBOX}",
                    "-f", "geoparquet", "-t", "division_area", "-o", out_divisions], check=True)

def transform(raw_parquet: str, out_parquet: str) -> int:
    table = duckdb.sql(_TRANSFORM_SQL.replace("?", f"'{raw_parquet}'")).arrow().read_all()
    gtypes = pa.array([overture_to_google(c) for c in table.column("raw_category").to_pylist()],
                      type=pa.string())
    table = table.append_column("google_type", gtypes)
    pq.write_table(table, out_parquet)
    return table.num_rows
