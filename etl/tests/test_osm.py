# etl/tests/test_osm.py
from pathlib import Path
import pyarrow.parquet as pq
from etl.osm import extract

FIXTURE = str(Path(__file__).parent / "fixtures" / "mini.osm")

def test_extract(tmp_path):
    out = tmp_path / "osm.parquet"
    n = extract(FIXTURE, str(out))
    rows = {r["osm_id"]: r for r in pq.read_table(out).to_pylist()}
    assert n == 2 and set(rows) == {"osm:node:1", "osm:way:20"}   # ★ way 前缀,见下
    node = rows["osm:node:1"]
    assert node["name_default"] == "Malis" and node["name_km"] == "ម្លិះ"
    assert node["google_type"] == "restaurant"
    assert node["opening_hours"] == "Mo-Su 07:00-22:00"
    area = rows["osm:way:20"]                                      # ★
    assert area["google_type"] == "school" and area["name_zh"] == "测试学校"
    assert 104.91 < area["lon"] < 104.93 and 11.56 < area["lat"] < 11.58  # 多边形代表点
