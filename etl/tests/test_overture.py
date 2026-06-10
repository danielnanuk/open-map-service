import duckdb
import pyarrow.parquet as pq
from etl.overture import transform

FIXTURE_SQL = """
INSTALL spatial; LOAD spatial;
COPY (
  SELECT * FROM (VALUES
    ('gers-1',
     {'primary': 'អង្គរវត្ត', 'common': MAP(['en','zh'], ['Angkor Wat','吴哥窟'])},
     {'primary': 'tourist_attraction', 'alternate': NULL},
     0.93::DOUBLE,
     [{'dataset': 'meta', 'record_id': 'm1'}],
     ['https://angkor.example'],
     ['+855 12 000 000'],
     [{'freeform': 'Angkor Archaeological Park', 'locality': 'Siem Reap', 'region': NULL, 'country': 'KH'}],
     ST_Point(103.8670, 13.4125)),
    ('gers-2',
     {'primary': 'No Extras Noodle', 'common': NULL},
     {'primary': 'cambodian_restaurant', 'alternate': NULL},
     NULL::DOUBLE,
     [{'dataset': 'msft', 'record_id': 'x2'}],
     NULL, NULL, NULL,
     ST_Point(104.9, 11.5))
  ) AS t(id, names, categories, confidence, sources, websites, phones, addresses, geometry)
) TO '{out}' (FORMAT PARQUET)
"""

def test_transform(tmp_path):
    raw = tmp_path / "raw.parquet"
    out = tmp_path / "out.parquet"
    duckdb.sql(FIXTURE_SQL.replace("{out}", str(raw)))
    transform(str(raw), str(out))
    rows = {r["place_id"]: r for r in pq.read_table(out).to_pylist()}
    a = rows["gers-1"]
    assert a["name_default"] == "អង្គរវត្ត"
    assert a["name_en"] == "Angkor Wat" and a["name_zh"] == "吴哥窟"
    assert a["google_type"] == "tourist_attraction"
    assert a["phone"] == "+855 12 000 000"
    assert a["addr_locality"] == "Siem Reap"
    assert abs(a["lon"] - 103.8670) < 1e-6 and abs(a["lat"] - 13.4125) < 1e-6
    b = rows["gers-2"]
    assert b["google_type"] == "restaurant"      # 后缀规则
    assert b["name_km"] is None and b["confidence"] is None
