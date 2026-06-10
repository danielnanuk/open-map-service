"""PostGIS → OpenSearch:时间戳索引名 + alias 原子切换,实现零停机重建。"""
import json
import time
from pathlib import Path
from typing import Iterable, Iterator

import psycopg
import requests

ALIAS = "places"
_MAPPINGS = json.loads((Path(__file__).parent / "mappings.json").read_text())


def create_index(base_url: str) -> str:
    name = f"places-{time.strftime('%Y%m%d%H%M%S')}-{int(time.time()*1000) % 1000:03d}"
    requests.put(f"{base_url}/{name}", json=_MAPPINGS).raise_for_status()
    return name


def bulk_index(base_url: str, index: str, docs: Iterable[dict], batch: int = 1000) -> int:
    total, buf = 0, []
    for doc in docs:
        buf.append(json.dumps({"index": {"_index": index, "_id": doc["place_id"]}}))
        buf.append(json.dumps(doc, ensure_ascii=False))
        if len(buf) >= batch * 2:
            total += _flush(base_url, buf)
    total += _flush(base_url, buf)
    return total


def _flush(base_url: str, buf: list[str]) -> int:
    if not buf:
        return 0
    resp = requests.post(f"{base_url}/_bulk", data=("\n".join(buf) + "\n").encode("utf-8"),
                         headers={"Content-Type": "application/x-ndjson"})
    resp.raise_for_status()
    if resp.json().get("errors"):
        failed = [i for i in resp.json()["items"] if i["index"].get("error")]
        raise RuntimeError(f"bulk errors: {failed[:3]}")
    n = len(buf) // 2
    buf.clear()
    return n


def swap_alias(base_url: str, new_index: str) -> None:
    current = requests.get(f"{base_url}/_alias/{ALIAS}")
    actions = [{"add": {"index": new_index, "alias": ALIAS}}]
    if current.status_code == 200:
        actions = [{"remove": {"index": old, "alias": ALIAS}} for old in current.json()
                   if old != new_index] + actions
    requests.post(f"{base_url}/_aliases", json={"actions": actions}).raise_for_status()


def rows_from_postgis(conn: psycopg.Connection) -> Iterator[dict]:
    sql = """SELECT place_id, names, categories, confidence,
                    address, ST_X(geom) AS lon, ST_Y(geom) AS lat
             FROM places"""
    with conn.cursor(name="idx_cur") as cur:   # server-side cursor,流式
        cur.execute(sql)
        for pid, names, cats, conf, addr, lon, lat in cur:
            addr = addr or {}
            parts = [addr.get("freeform"), addr.get("locality"), addr.get("region")]
            yield {
                "place_id": pid,
                "name_default": names.get("default"),
                "name_km": names.get("km"), "name_en": names.get("en"), "name_zh": names.get("zh"),
                "formatted_address": ", ".join([p for p in parts if p] + ["Cambodia"]),
                "categories": cats, "confidence": conf,
                "location": {"lat": lat, "lon": lon},
            }
