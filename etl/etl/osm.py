# etl/etl/osm.py
"""pyosmium 抽取有名字的 POI(node + 面状要素),面状取 representative point。"""
import osmium
import shapely
import pyarrow as pa
import pyarrow.parquet as pq
from etl.categories import osm_to_google

_COLUMNS = ["osm_id", "name_default", "name_km", "name_en", "name_zh",
            "google_type", "phone", "website", "opening_hours", "lon", "lat"]

class _POIHandler(osmium.SimpleHandler):
    def __init__(self):
        super().__init__()
        self.rows: list[dict] = []
        self.skipped: int = 0
        self._wkb = osmium.geom.WKBFactory()

    def _row(self, osm_id: str, tags: dict, lon: float, lat: float) -> None:
        gtype = osm_to_google(tags)
        if gtype is None or not tags.get("name"):
            return
        self.rows.append({
            "osm_id": osm_id,
            "name_default": tags.get("name"),
            "name_km": tags.get("name:km"),
            "name_en": tags.get("name:en"),
            "name_zh": tags.get("name:zh"),
            "google_type": gtype,
            "phone": tags.get("phone") or tags.get("contact:phone"),
            "website": tags.get("website") or tags.get("contact:website"),
            "opening_hours": tags.get("opening_hours"),
            "lon": lon, "lat": lat,
        })

    def node(self, n):
        tags = {t.k: t.v for t in n.tags}
        self._row(f"osm:node:{n.id}", tags, n.location.lon, n.location.lat)

    def area(self, a):
        tags = {t.k: t.v for t in a.tags}
        if osm_to_google(tags) is None or not tags.get("name"):
            return
        try:
            point = shapely.from_wkb(bytes.fromhex(self._wkb.create_multipolygon(a))).representative_point()
        except Exception:
            self.skipped += 1
            return
        kind = "way" if a.from_way() else "rel"                    # ★
        self._row(f"osm:{kind}:{a.orig_id()}", tags, point.x, point.y)

def extract(pbf_path: str, out_parquet: str) -> int:
    handler = _POIHandler()
    handler.apply_file(pbf_path, locations=True)
    table = pa.Table.from_pylist(handler.rows,
                                 schema=pa.schema([(c, pa.float64() if c in ("lon", "lat") else pa.string())
                                                   for c in _COLUMNS]))
    pq.write_table(table, out_parquet)
    if handler.skipped:
        print(f"osm.extract: skipped {handler.skipped} areas with broken geometry")
    return table.num_rows
