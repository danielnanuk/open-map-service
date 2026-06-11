"""PostGIS → OpenSearch:时间戳索引名 + alias 原子切换,实现零停机重建。"""
import json
import time
import uuid
from pathlib import Path
from typing import Iterable, Iterator

import psycopg
import requests

ALIAS = "places"
_TIMEOUT = (5, 120)  # connect, read — 防止 OS 假死导致 ETL 永久挂起
_MAPPINGS = json.loads((Path(__file__).parent / "mappings.json").read_text())


def create_index(base_url: str) -> str:
    name = f"places-{time.strftime('%Y%m%d%H%M%S')}-{uuid.uuid4().hex[:6]}"
    requests.put(f"{base_url}/{name}", json=_MAPPINGS, timeout=_TIMEOUT).raise_for_status()
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
                         headers={"Content-Type": "application/x-ndjson"}, timeout=_TIMEOUT)
    resp.raise_for_status()
    body = resp.json()
    if body.get("errors"):
        failed = [i for i in body["items"] if i["index"].get("error")]
        raise RuntimeError(f"bulk errors: {failed[:3]}")
    n = len(buf) // 2
    buf.clear()
    return n


def swap_alias(base_url: str, new_index: str) -> list[str]:
    """原子切换 alias,返回先前的目标索引列表(供调用方决定保留/清理)。"""
    requests.post(f"{base_url}/{new_index}/_refresh", timeout=_TIMEOUT).raise_for_status()
    current = requests.get(f"{base_url}/_alias/{ALIAS}", timeout=_TIMEOUT)
    prev: list[str] = []
    actions = [{"add": {"index": new_index, "alias": ALIAS}}]
    if current.status_code == 200:
        prev = [old for old in current.json() if old != new_index]
        actions = [{"remove": {"index": old, "alias": ALIAS}} for old in prev] + actions
    requests.post(f"{base_url}/_aliases", json={"actions": actions}, timeout=_TIMEOUT).raise_for_status()
    return prev


def prune_old_indices(base_url: str, keep: set[str]) -> list[str]:
    """删除 keep 之外的 {ALIAS}-* 索引(每次重建泄漏一个旧索引,M5 磁盘事故的教训)。
    只应由 etl-index 生产路径调用——测试的 swap 不得触发删除。返回删除列表。"""
    resp = requests.get(f"{base_url}/_cat/indices/{ALIAS}-*?h=index&format=json", timeout=_TIMEOUT)
    if resp.status_code != 200:
        return []
    deleted = []
    for row in resp.json():
        idx = row["index"]
        if idx not in keep:
            requests.delete(f"{base_url}/{idx}", timeout=_TIMEOUT)
            deleted.append(idx)
    return deleted


def rows_from_postgis(conn: psycopg.Connection) -> Iterator[dict]:
    sql = """SELECT place_id, names, categories, confidence,
                    address, ST_X(geom) AS lon, ST_Y(geom) AS lat
             FROM places"""
    with conn.cursor(name="idx_cur") as cur:   # server-side cursor,流式
        cur.execute(sql)
        for pid, names, cats, conf, addr, lon, lat in cur:
            addr = addr or {}
            parts = [p for p in (addr.get("freeform"), addr.get("locality"), addr.get("region"))
                     if p and p.strip().lower() != "cambodia"]
            yield {
                "place_id": pid,
                "name_default": names.get("default"),
                "name_km": names.get("km"), "name_en": names.get("en"), "name_zh": names.get("zh"),
                "formatted_address": ", ".join(parts + ["Cambodia"]),
                "categories": cats, "confidence": conf,
                "location": {"lat": lat, "lon": lon},
            }
