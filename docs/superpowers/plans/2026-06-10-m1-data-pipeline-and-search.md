# M1:数据管道 + 搜索四端点 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建成柬埔寨 POI 数据管道(Overture + OSM → PostGIS 合并库 → OpenSearch),并以 Google Places API (New) 协议暴露 searchText / searchNearby / autocomplete / place details 四个端点。

**Architecture:** Python ETL(overturemaps CLI + DuckDB 抽取转换,pyosmium 抽 OSM POI,SQL 做 conflation)写入 PostGIS 权威库;Python 索引器经 alias 原子切换写 OpenSearch(ICU 高棉语断词 + smartcn 中文);Go 网关做 Google 协议翻译,搜索类请求走 OpenSearch,详情走 PostGIS。

**Tech Stack:** PostGIS 16-3.4 · OpenSearch 3.1(analysis-icu / analysis-smartcn)· Python 3.11+(duckdb, pyarrow, osmium, shapely, psycopg3)· Go 1.23+(stdlib net/http + pgx/v5)· docker-compose

**Spec:** `docs/superpowers/specs/2026-06-10-places-api-design.md`(本计划只覆盖其 M1;鉴权/配额/监控属 M5,不在本计划)

---

## 文件结构(全貌)

```
Makefile                          # 所有入口:up/migrate/etl-*/test/golden
docker-compose.yml                # postgis + opensearch + gateway
deploy/opensearch/Dockerfile      # OS 3.1 + analysis-icu + analysis-smartcn
db/migrations/0001_init.sql       # places / osm_pois / staging / country_boundary
db/migrate.sh                     # 迁移执行器(幂等)
etl/pyproject.toml                # Python 包定义
etl/etl/categories.py             # Overture/OSM 类目 → Google place type 映射
etl/etl/overture.py               # Overture 下载 + DuckDB 转换 → parquet
etl/etl/osm.py                    # pyosmium 抽 OSM POI → parquet
etl/etl/load.py                   # parquet → PostGIS(KH 边界裁剪 + upsert)
etl/etl/conflate.py               # OSM↔Overture 合并去重 SQL
etl/etl/index.py                  # PostGIS → OpenSearch 批量索引 + alias 切换
etl/etl/mappings.json             # OpenSearch 索引 settings/mappings
etl/tests/...                     # 各模块测试(unit + integration 标记)
gateway/go.mod                    # module github.com/danielnanuk/open-map-service/gateway
gateway/cmd/gateway/main.go       # 装配 + 路由
gateway/internal/gapi/types.go    # Google Places API (New) 请求/响应类型
gateway/internal/fieldmask/       # X-Goog-FieldMask 裁剪
gateway/internal/search/          # OpenSearch 查询构造 + 客户端
gateway/internal/store/           # PostGIS place details
gateway/internal/httpapi/         # 四个 handler + 错误码映射
gateway/Dockerfile
golden/cases.yaml                 # 黄金查询集(km/en/zh)
golden/run_golden.py              # 黄金查询执行器
data/                             # 下载与中间产物(gitignore)
```

约定:
- ETL 在宿主机 venv 运行(M5 再容器化进 cron);`DATABASE_URL=postgresql://places:places@localhost:5432/places`,`OPENSEARCH_URL=http://localhost:9200`。
- 集成测试用 pytest marker `integration`,要求 compose 的 postgis/opensearch 已启动;纯单测不依赖任何服务。
- 柬埔寨 bbox:`102.33,9.90,107.63,14.70`(后续用 KH 国界多边形精裁)。

---

### Task 1:仓库脚手架

**Files:**
- Create: `.gitignore`, `Makefile`, `data/.gitkeep`

- [ ] **Step 1: 写 .gitignore**

```gitignore
data/
!data/.gitkeep
*.parquet
__pycache__/
*.pyc
.venv/
.pytest_cache/
gateway/gateway
*.osm.pbf
```

- [ ] **Step 2: 写 Makefile 骨架**(目标后续任务逐步填充,先放公共变量与 help)

```makefile
SHELL := /bin/bash
export DATABASE_URL ?= postgresql://places:places@localhost:5432/places
export OPENSEARCH_URL ?= http://localhost:9200
PY := etl/.venv/bin/python
PIP := etl/.venv/bin/pip
PYTEST := etl/.venv/bin/pytest

.PHONY: help
help:
	@grep -E '^[a-z][a-zA-Z_-]*:' Makefile | cut -d: -f1 | sort
```

- [ ] **Step 3: 建 data 目录占位并提交**

```bash
mkdir -p data && touch data/.gitkeep
git add .gitignore Makefile data/.gitkeep
git commit -m "chore: repo scaffolding (gitignore, Makefile skeleton)"
```

---

### Task 2:docker-compose(PostGIS + OpenSearch 含插件)

**Files:**
- Create: `docker-compose.yml`, `deploy/opensearch/Dockerfile`
- Modify: `Makefile`(加 `up`/`down` 目标)

- [ ] **Step 1: 写 OpenSearch 自定义镜像**(ICU + smartcn 插件;版本必须与基镜像一致,`--batch` 自动匹配)

```dockerfile
# deploy/opensearch/Dockerfile
FROM opensearchproject/opensearch:3.1.0
RUN bin/opensearch-plugin install --batch analysis-icu && \
    bin/opensearch-plugin install --batch analysis-smartcn
```

- [ ] **Step 2: 写 docker-compose.yml**(gateway 服务在 Task 14 加入)

```yaml
services:
  postgis:
    image: postgis/postgis:16-3.4
    environment:
      POSTGRES_USER: places
      POSTGRES_PASSWORD: places
      POSTGRES_DB: places
    ports: ["5432:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U places -d places"]
      interval: 5s
      timeout: 3s
      retries: 20

  opensearch:
    build: deploy/opensearch
    environment:
      discovery.type: single-node
      DISABLE_SECURITY_PLUGIN: "true"
      OPENSEARCH_JAVA_OPTS: "-Xms1g -Xmx1g"
    ports: ["9200:9200"]
    volumes: [osdata:/usr/share/opensearch/data]
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:9200/_cluster/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 30

volumes:
  pgdata:
  osdata:
```

- [ ] **Step 3: Makefile 加目标**

```makefile
.PHONY: up down
up:
	docker compose up -d --build postgis opensearch
	docker compose ps

down:
	docker compose down
```

- [ ] **Step 4: 启动并验证两个分析器真的可用**(这是本任务的验收点:高棉语 ICU 断词、中文 smartcn 分词)

Run: `make up`(首次构建镜像需几分钟),等 healthcheck 变 healthy 后:

```bash
curl -s -X POST 'http://localhost:9200/_analyze' -H 'Content-Type: application/json' \
  -d '{"tokenizer":"icu_tokenizer","text":"ភ្នំពេញ"}'
curl -s -X POST 'http://localhost:9200/_analyze' -H 'Content-Type: application/json' \
  -d '{"tokenizer":"smartcn_tokenizer","text":"金边皇宫"}'
```

Expected: 第一条返回若干高棉文 token(非整串一个 token);第二条返回 `金边`/`皇宫` 两个 token。若报 `unknown tokenizer` 说明插件未装上,检查镜像构建日志。

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml deploy/opensearch/Dockerfile Makefile
git commit -m "feat: docker-compose with postgis + opensearch (icu/smartcn plugins)"
```

---

### Task 3:数据库 migration

**Files:**
- Create: `db/migrations/0001_init.sql`, `db/migrate.sh`
- Modify: `Makefile`(加 `migrate`)

- [ ] **Step 1: 写 migration SQL**(全部 IF NOT EXISTS,幂等)

```sql
-- db/migrations/0001_init.sql
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS places (
  place_id       text PRIMARY KEY,            -- Overture GERS id 或 osm:<type>:<id>
  primary_source text NOT NULL,               -- 'overture' | 'osm'
  names          jsonb NOT NULL,              -- {"default":..,"km":..,"en":..,"zh":..}
  categories     text[] NOT NULL DEFAULT '{point_of_interest}',
  raw_category   text,
  phone          text,
  website        text,
  opening_hours  text,                        -- OSM opening_hours 原文
  address        jsonb,                       -- {"freeform":..,"locality":..,"region":..,"country":..}
  confidence     real,
  sources        jsonb NOT NULL DEFAULT '[]',
  geom           geometry(Point, 4326) NOT NULL,
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS places_geom_idx ON places USING GIST (geom);

CREATE TABLE IF NOT EXISTS staging_overture (
  place_id text PRIMARY KEY,
  name_default text, name_km text, name_en text, name_zh text,
  raw_category text, google_type text,
  phone text, website text,
  addr_freeform text, addr_locality text, addr_region text, addr_country text,
  confidence real, sources jsonb,
  lon double precision, lat double precision
);

CREATE TABLE IF NOT EXISTS osm_pois (
  osm_id text PRIMARY KEY,                    -- osm:node:123 / osm:area:456
  name_default text, name_km text, name_en text, name_zh text,
  google_type text,
  phone text, website text, opening_hours text,
  lon double precision, lat double precision,
  geom geometry(Point, 4326)
);
CREATE INDEX IF NOT EXISTS osm_pois_geom_idx ON osm_pois USING GIST (geom);

CREATE TABLE IF NOT EXISTS country_boundary (
  iso text PRIMARY KEY,
  geom geometry(MultiPolygon, 4326) NOT NULL
);
```

- [ ] **Step 2: 写迁移执行器**

```bash
#!/usr/bin/env bash
# db/migrate.sh — 按文件名顺序应用所有迁移(SQL 自身幂等)
set -euo pipefail
for f in db/migrations/*.sql; do
  echo "applying $f"
  docker compose exec -T postgis psql -U places -d places -v ON_ERROR_STOP=1 < "$f"
done
```

- [ ] **Step 3: Makefile 加目标**

```makefile
.PHONY: migrate
migrate:
	bash db/migrate.sh
```

- [ ] **Step 4: 跑两遍验证幂等**

Run: `chmod +x db/migrate.sh && make migrate && make migrate`
Expected: 两遍都成功退出(第二遍全是 already exists 的 NOTICE,无 ERROR)。

- [ ] **Step 5: Commit**

```bash
git add db/ Makefile
git commit -m "feat: postgis schema migrations (places, osm_pois, staging, boundary)"
```

---

### Task 4:ETL 脚手架 + 类目映射(TDD)

**Files:**
- Create: `etl/pyproject.toml`, `etl/etl/__init__.py`, `etl/etl/categories.py`, `etl/tests/test_categories.py`
- Modify: `Makefile`(加 `py-setup` / `test-py`)

- [ ] **Step 1: 写 pyproject 与 venv**

```toml
# etl/pyproject.toml
[project]
name = "omsetl"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
  "duckdb>=1.2,<2",
  "pyarrow>=19",
  "psycopg[binary]>=3.2",
  "osmium>=4.0",
  "shapely>=2.0",
  "overturemaps",
  "requests>=2.32",
  "pyyaml>=6",
]

[project.optional-dependencies]
dev = ["pytest>=8"]

[tool.pytest.ini_options]
markers = ["integration: needs docker compose services"]

[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.build_meta"

[tool.setuptools]
packages = ["etl"]
```

```makefile
.PHONY: py-setup test-py
py-setup:
	python3 -m venv etl/.venv
	$(PIP) install -e 'etl[dev]'

test-py:
	cd etl && .venv/bin/pytest -m "not integration" -q

test-py-integration:
	cd etl && .venv/bin/pytest -m integration -q
```

Run: `touch etl/etl/__init__.py && make py-setup`
Expected: 安装成功,无报错。

- [ ] **Step 2: 写失败测试**

```python
# etl/tests/test_categories.py
from etl.categories import overture_to_google, osm_to_google

def test_overture_known_leaf():
    assert overture_to_google("restaurant") == "restaurant"
    assert overture_to_google("hotel") == "lodging"
    assert overture_to_google("buddhist_temple") == "place_of_worship"

def test_overture_suffix_fallback():
    assert overture_to_google("cambodian_restaurant") == "restaurant"
    assert overture_to_google("boutique_hotel") == "lodging"

def test_overture_unknown_and_none():
    assert overture_to_google("weird_thing") == "point_of_interest"
    assert overture_to_google(None) == "point_of_interest"

def test_osm_mapping():
    assert osm_to_google({"amenity": "restaurant", "name": "x"}) == "restaurant"
    assert osm_to_google({"tourism": "guest_house"}) == "lodging"
    assert osm_to_google({"shop": "weird"}) == "point_of_interest"
    assert osm_to_google({"building": "yes"}) is None  # 非 POI
```

- [ ] **Step 3: 跑测试确认失败**

Run: `make test-py`
Expected: FAIL,`ModuleNotFoundError: No module named 'etl.categories'`

- [ ] **Step 4: 实现 categories.py**

```python
# etl/etl/categories.py
"""Overture/OSM 类目 → Google place type 的最小映射。未知类目兜底 point_of_interest。"""

OVERTURE_TO_GOOGLE = {
    "restaurant": "restaurant", "fast_food_restaurant": "restaurant",
    "cafe": "cafe", "coffee_shop": "cafe",
    "bar": "bar", "pub": "bar",
    "hotel": "lodging", "motel": "lodging", "hostel": "lodging",
    "guest_house": "lodging", "bed_and_breakfast": "lodging", "resort": "lodging",
    "supermarket": "supermarket", "grocery_store": "supermarket",
    "convenience_store": "convenience_store",
    "shopping_center": "shopping_mall", "shopping_mall": "shopping_mall",
    "market": "market", "hospital": "hospital", "clinic": "hospital",
    "pharmacy": "pharmacy", "school": "school", "university": "university",
    "bank": "bank", "atm": "atm", "atms": "atm",
    "gas_station": "gas_station", "airport": "airport",
    "tourist_attraction": "tourist_attraction", "landmark_and_historical_building": "tourist_attraction",
    "buddhist_temple": "place_of_worship", "pagoda": "place_of_worship",
    "church_cathedral": "place_of_worship", "mosque": "place_of_worship",
}

_SUFFIX_RULES = [
    ("_restaurant", "restaurant"), ("_cafe", "cafe"), ("_bar", "bar"),
    ("_hotel", "lodging"), ("_school", "school"), ("_market", "market"),
    ("_temple", "place_of_worship"), ("_hospital", "hospital"),
]

def overture_to_google(primary: str | None) -> str:
    if not primary:
        return "point_of_interest"
    if primary in OVERTURE_TO_GOOGLE:
        return OVERTURE_TO_GOOGLE[primary]
    for suffix, gtype in _SUFFIX_RULES:
        if primary.endswith(suffix):
            return gtype
    return "point_of_interest"

# POI 判定:出现这些 key 之一即视为 POI(还需有 name,由 osm.py 把关)
_POI_KEYS = ("amenity", "shop", "tourism", "leisure", "office", "craft", "healthcare", "aeroway")

OSM_TAG_TO_GOOGLE = {
    ("amenity", "restaurant"): "restaurant", ("amenity", "fast_food"): "restaurant",
    ("amenity", "cafe"): "cafe", ("amenity", "bar"): "bar", ("amenity", "pub"): "bar",
    ("amenity", "hospital"): "hospital", ("amenity", "clinic"): "hospital",
    ("amenity", "pharmacy"): "pharmacy", ("amenity", "school"): "school",
    ("amenity", "university"): "university", ("amenity", "bank"): "bank",
    ("amenity", "atm"): "atm", ("amenity", "fuel"): "gas_station",
    ("amenity", "place_of_worship"): "place_of_worship", ("amenity", "marketplace"): "market",
    ("tourism", "hotel"): "lodging", ("tourism", "guest_house"): "lodging",
    ("tourism", "hostel"): "lodging", ("tourism", "attraction"): "tourist_attraction",
    ("shop", "supermarket"): "supermarket", ("shop", "convenience"): "convenience_store",
    ("shop", "mall"): "shopping_mall", ("aeroway", "aerodrome"): "airport",
}

def osm_to_google(tags: dict) -> str | None:
    """返回 Google type;返回 None 表示这不是 POI(调用方应跳过)。"""
    for key in _POI_KEYS:
        value = tags.get(key)
        if value:
            return OSM_TAG_TO_GOOGLE.get((key, value), "point_of_interest")
    return None
```

- [ ] **Step 5: 跑测试确认通过**

Run: `make test-py`
Expected: PASS(4 passed)

- [ ] **Step 6: Commit**

```bash
git add etl/ Makefile
git commit -m "feat: etl scaffolding and category mapping (overture/osm -> google types)"
```

---

### Task 5:Overture 抽取与转换(TDD)

**Files:**
- Create: `etl/etl/overture.py`, `etl/tests/test_overture.py`
- Modify: `Makefile`(加 `etl-overture` / `probe-overture`)

- [ ] **Step 1: 写失败测试**(用 DuckDB 造同构 nested-schema 的 fixture parquet,不依赖网络)

```python
# etl/tests/test_overture.py
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
     ST_AsWKB(ST_Point(103.8670, 13.4125))),
    ('gers-2',
     {'primary': 'No Extras Noodle', 'common': NULL},
     {'primary': 'cambodian_restaurant', 'alternate': NULL},
     NULL::DOUBLE,
     [{'dataset': 'msft', 'record_id': 'x2'}],
     NULL, NULL, NULL,
     ST_AsWKB(ST_Point(104.9, 11.5)))
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `make test-py`
Expected: FAIL,`ImportError: cannot import name 'transform'`

- [ ] **Step 3: 实现 overture.py**

```python
# etl/etl/overture.py
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
  ST_X(ST_GeomFromWKB(geometry))                AS lon,
  ST_Y(ST_GeomFromWKB(geometry))                AS lat
FROM read_parquet(?)
WHERE names."primary" IS NOT NULL
"""

def download(out_places: str, out_divisions: str) -> None:
    subprocess.run(["overturemaps", "download", f"--bbox={CAMBODIA_BBOX}",
                    "-f", "geoparquet", "--type=place", "-o", out_places], check=True)
    subprocess.run(["overturemaps", "download", f"--bbox={CAMBODIA_BBOX}",
                    "-f", "geoparquet", "--type=division_area", "-o", out_divisions], check=True)

def transform(raw_parquet: str, out_parquet: str) -> int:
    table = duckdb.sql(_TRANSFORM_SQL.replace("?", f"'{raw_parquet}'")).arrow()
    gtypes = pa.array([overture_to_google(c) for c in table.column("raw_category").to_pylist()],
                      type=pa.string())
    table = table.append_column("google_type", gtypes)
    pq.write_table(table, out_parquet)
    return table.num_rows
```

- [ ] **Step 4: 跑测试确认通过**

Run: `make test-py`
Expected: PASS

- [ ] **Step 5: Makefile 加真实抽取目标 + schema 探针**(探针用于实现期验证 spec §14.1 假设;真实 Overture schema 若与转换 SQL 不符,在这里第一时间暴露)

```makefile
.PHONY: etl-overture probe-overture
etl-overture:
	$(PY) -c "from etl.overture import download, transform; \
download('data/overture_places_raw.parquet','data/overture_divisions_raw.parquet'); \
n = transform('data/overture_places_raw.parquet','data/overture_places.parquet'); \
print(f'transformed {n} places')"

probe-overture:
	$(PY) -c "import duckdb; print(duckdb.sql(\"DESCRIBE SELECT * FROM read_parquet('data/overture_places_raw.parquet')\"))"
```

Run: `make etl-overture`(真实下载,几分钟)然后 `make probe-overture`
Expected: 打印 `transformed N places`,N 预期为数十万级;探针输出的列结构与 `_TRANSFORM_SQL` 引用一致。**把 N 记录到 commit message 里**(spec §14.1 的验证数据点)。

- [ ] **Step 6: Commit**

```bash
git add etl/ Makefile
git commit -m "feat: overture cambodia extract + duckdb transform (N=<填实际数量>)"
```

---

### Task 6:OSM POI 抽取(TDD)

**Files:**
- Create: `etl/etl/osm.py`, `etl/tests/test_osm.py`, `etl/tests/fixtures/mini.osm`
- Modify: `Makefile`(加 `etl-osm`)

- [ ] **Step 1: 写 XML fixture**(node POI、area POI、无名 node 三种情况)

```xml
<?xml version='1.0' encoding='UTF-8'?>
<osm version="0.6" generator="test">
  <node id="1" lat="11.5621" lon="104.9160">
    <tag k="amenity" v="restaurant"/>
    <tag k="name" v="Malis"/>
    <tag k="name:km" v="ម្លិះ"/>
    <tag k="phone" v="+855 15 814 888"/>
    <tag k="opening_hours" v="Mo-Su 07:00-22:00"/>
  </node>
  <node id="2" lat="11.5" lon="104.9"/>
  <node id="10" lat="11.5700" lon="104.9200"/>
  <node id="11" lat="11.5700" lon="104.9210"/>
  <node id="12" lat="11.5710" lon="104.9210"/>
  <node id="13" lat="11.5710" lon="104.9200"/>
  <way id="20">
    <nd ref="10"/><nd ref="11"/><nd ref="12"/><nd ref="13"/><nd ref="10"/>
    <tag k="amenity" v="school"/>
    <tag k="name" v="Test School"/>
    <tag k="name:zh" v="测试学校"/>
  </way>
</osm>
```

- [ ] **Step 2: 写失败测试**

```python
# etl/tests/test_osm.py
from pathlib import Path
import pyarrow.parquet as pq
from etl.osm import extract

FIXTURE = str(Path(__file__).parent / "fixtures" / "mini.osm")

def test_extract(tmp_path):
    out = tmp_path / "osm.parquet"
    n = extract(FIXTURE, str(out))
    rows = {r["osm_id"]: r for r in pq.read_table(out).to_pylist()}
    assert n == 2 and set(rows) == {"osm:node:1", "osm:area:20"}
    node = rows["osm:node:1"]
    assert node["name_default"] == "Malis" and node["name_km"] == "ម្លិះ"
    assert node["google_type"] == "restaurant"
    assert node["opening_hours"] == "Mo-Su 07:00-22:00"
    area = rows["osm:area:20"]
    assert area["google_type"] == "school" and area["name_zh"] == "测试学校"
    assert 104.91 < area["lon"] < 104.93 and 11.56 < area["lat"] < 11.58  # 多边形代表点
```

- [ ] **Step 3: 跑测试确认失败**

Run: `make test-py`
Expected: FAIL,`ModuleNotFoundError: No module named 'etl.osm'`

- [ ] **Step 4: 实现 osm.py**

```python
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
        self._row(f"osm:node:{n.id}", dict(n.tags), n.location.lon, n.location.lat)

    def area(self, a):
        tags = dict(a.tags)
        if osm_to_google(tags) is None or not tags.get("name"):
            return
        point = shapely.from_wkb(bytes.fromhex(self._wkb.create_multipolygon(a))).representative_point()
        self._row(f"osm:area:{a.orig_id()}", tags, point.x, point.y)

def extract(pbf_path: str, out_parquet: str) -> int:
    handler = _POIHandler()
    handler.apply_file(pbf_path, locations=True)
    table = pa.Table.from_pylist(handler.rows,
                                 schema=pa.schema([(c, pa.float64() if c in ("lon", "lat") else pa.string())
                                                   for c in _COLUMNS]))
    pq.write_table(table, out_parquet)
    return table.num_rows
```

- [ ] **Step 5: 跑测试确认通过**

Run: `make test-py`
Expected: PASS

- [ ] **Step 6: Makefile 加真实抽取目标并验证**

```makefile
.PHONY: etl-osm
etl-osm:
	test -f data/cambodia-latest.osm.pbf || curl -L -o data/cambodia-latest.osm.pbf \
		https://download.geofabrik.de/asia/cambodia-latest.osm.pbf
	$(PY) -c "from etl.osm import extract; print('osm pois:', extract('data/cambodia-latest.osm.pbf','data/osm_pois.parquet'))"
```

Run: `make etl-osm`
Expected: 下载 PBF(~150MB)后打印 `osm pois: N`,N 预期数万级。

- [ ] **Step 7: Commit**

```bash
git add etl/ Makefile
git commit -m "feat: osm poi extraction via pyosmium (nodes + areas)"
```

---

### Task 7:入库 + KH 边界裁剪(集成测试)

**Files:**
- Create: `etl/etl/load.py`, `etl/tests/test_load.py`
- Modify: `Makefile`(加 `etl-load`)

- [ ] **Step 1: 写失败的集成测试**(需要 `make up && make migrate` 后运行;用微型 KH 方形边界 + 两行 staging 数据验证"界内保留、界外丢弃、upsert 幂等")

```python
# etl/tests/test_load.py
import os, json
import pytest, psycopg
from etl.load import load_boundary_wkt, upsert_places_from_staging

pytestmark = pytest.mark.integration
DSN = os.environ.get("DATABASE_URL", "postgresql://places:places@localhost:5432/places")
# 把"国界"造成金边附近 1 度见方的盒子,gers-in 在内、gers-out 在外
BOX = "MULTIPOLYGON(((104 11,106 11,106 12,104 12,104 11)))"

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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `make up && make migrate && make test-py-integration`
Expected: FAIL,`ModuleNotFoundError: No module named 'etl.load'`

- [ ] **Step 3: 实现 load.py**

```python
# etl/etl/load.py
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
    conn.execute("""INSERT INTO country_boundary (iso, geom)
                    VALUES (%s, ST_Multi(ST_GeomFromText(%s, 4326)))
                    ON CONFLICT (iso) DO UPDATE SET geom = EXCLUDED.geom""", (iso, wkt))

def extract_kh_boundary_wkt(divisions_parquet: str) -> str:
    duckdb.sql("INSTALL spatial; LOAD spatial")
    return duckdb.sql(f"""
        SELECT ST_AsText(ST_Union_Agg(ST_GeomFromWKB(geometry)))
        FROM read_parquet('{divisions_parquet}')
        WHERE subtype = 'country' AND country = 'KH'""").fetchone()[0]

def copy_parquet_to_staging(conn: psycopg.Connection, parquet_path: str, table: str, columns: list[str]) -> int:
    rows = pq.read_table(parquet_path).to_pylist()
    conn.execute(f"TRUNCATE {table}")
    with conn.cursor().copy(f"COPY {table} ({','.join(columns)}) FROM STDIN") as cp:
        for r in rows:
            cp.write_row([json.dumps(r[c]) if c in ("sources_json", "sources") and r.get(c) else r.get(c)
                          for c in columns])
    return len(rows)

def upsert_places_from_staging(conn: psycopg.Connection) -> int:
    return conn.execute(_UPSERT_SQL).rowcount

def load_osm_pois(conn: psycopg.Connection, parquet_path: str) -> int:
    n = copy_parquet_to_staging(conn, parquet_path, "osm_pois",
        ["osm_id", "name_default", "name_km", "name_en", "name_zh",
         "google_type", "phone", "website", "opening_hours", "lon", "lat"])
    conn.execute("UPDATE osm_pois SET geom = ST_SetSRID(ST_MakePoint(lon, lat), 4326) WHERE geom IS NULL")
    return n
```

注意:staging 的 `sources` 列在 parquet 里叫 `sources_json`,copy 时列名映射为 staging 的 `sources`,所以 `etl-load` 入口里 columns 传 staging 列名、从 parquet 行取值时用 `sources_json` —— 见 Step 5 入口代码,它把 parquet 列重命名后再 copy,测试里直接插 staging 不经过该路径。

- [ ] **Step 4: 跑测试确认通过**

Run: `make test-py-integration`
Expected: PASS

- [ ] **Step 5: Makefile 加完整入库目标**

```makefile
.PHONY: etl-load
etl-load:
	$(PY) -c "import psycopg, os, pyarrow.parquet as pq, pyarrow as pa; \
from etl.load import *; \
conn = psycopg.connect(os.environ['DATABASE_URL'], autocommit=True); \
load_boundary_wkt(conn, 'KH', extract_kh_boundary_wkt('data/overture_divisions_raw.parquet')); \
t = pq.read_table('data/overture_places.parquet'); \
t = t.rename_columns([{'sources_json':'sources'}.get(c, c) for c in t.column_names]); \
pq.write_table(t, 'data/overture_places_staged.parquet'); \
print('staged:', copy_parquet_to_staging(conn, 'data/overture_places_staged.parquet', 'staging_overture', \
  ['place_id','name_default','name_km','name_en','name_zh','raw_category','google_type','phone','website', \
   'addr_freeform','addr_locality','addr_region','addr_country','confidence','sources','lon','lat'])); \
print('places upserted:', upsert_places_from_staging(conn)); \
print('osm pois loaded:', load_osm_pois(conn, 'data/osm_pois.parquet'))"
```

Run: `make etl-load`
Expected: 打印 staged/upserted/loaded 三个计数;upserted ≤ staged(界外被裁掉)。

- [ ] **Step 6: Commit**

```bash
git add etl/ Makefile
git commit -m "feat: postgis loader with KH boundary clip and idempotent upsert"
```

---

### Task 8:Conflation(集成测试)

**Files:**
- Create: `etl/etl/conflate.py`, `etl/tests/test_conflate.py`
- Modify: `Makefile`(加 `etl-conflate`)

- [ ] **Step 1: 写失败的集成测试**(三个场景:近距离同名 → 合并补字段;远距离 → 独立成行;重跑幂等)

```python
# etl/tests/test_conflate.py
import os, json
import pytest, psycopg
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `make test-py-integration`
Expected: FAIL,`ModuleNotFoundError: No module named 'etl.conflate'`

- [ ] **Step 3: 实现 conflate.py**(规则 = spec §4:距离<50m + 任一语言名相似 + 类目相容;Overture 字段优先,OSM 补缺)

```python
# etl/etl/conflate.py
"""OSM POI 与 Overture places 的合并去重。运行于 load 之后、index 之前。"""
import psycopg

_SQL = """
CREATE TEMP TABLE _matches AS
SELECT o.osm_id, p.place_id,
       ROW_NUMBER() OVER (PARTITION BY o.osm_id
                          ORDER BY ST_Distance(o.geom::geography, p.geom::geography)) AS rn
FROM osm_pois o
JOIN places p ON p.primary_source = 'overture'
            AND ST_DWithin(o.geom::geography, p.geom::geography, 50)
WHERE (
        similarity(lower(o.name_default), lower(p.names->>'default')) > 0.45
     OR similarity(lower(COALESCE(o.name_en, o.name_default)),
                   lower(COALESCE(p.names->>'en', p.names->>'default'))) > 0.45
     OR (o.name_km IS NOT NULL AND o.name_km = p.names->>'km')
      )
  AND (o.google_type = 'point_of_interest' OR p.categories[1] = 'point_of_interest'
       OR o.google_type = p.categories[1]);

UPDATE places p SET
  phone         = COALESCE(p.phone, o.phone),
  website       = COALESCE(p.website, o.website),
  opening_hours = COALESCE(p.opening_hours, o.opening_hours),
  names = p.names || jsonb_strip_nulls(jsonb_build_object(
            'km', COALESCE(p.names->>'km', o.name_km),
            'en', COALESCE(p.names->>'en', o.name_en),
            'zh', COALESCE(p.names->>'zh', o.name_zh))),
  sources = CASE WHEN p.sources @> jsonb_build_array(jsonb_build_object('dataset','osm','record_id',o.osm_id))
                 THEN p.sources
                 ELSE p.sources || jsonb_build_array(jsonb_build_object('dataset','osm','record_id',o.osm_id))
            END,
  updated_at = now()
FROM osm_pois o
JOIN _matches m ON m.osm_id = o.osm_id AND m.rn = 1
WHERE p.place_id = m.place_id;

INSERT INTO places (place_id, primary_source, names, categories, phone, website, opening_hours, sources, geom)
SELECT o.osm_id, 'osm',
       jsonb_strip_nulls(jsonb_build_object('default', o.name_default, 'km', o.name_km,
                                            'en', o.name_en, 'zh', o.name_zh)),
       ARRAY[o.google_type], o.phone, o.website, o.opening_hours,
       jsonb_build_array(jsonb_build_object('dataset', 'osm', 'record_id', o.osm_id)),
       o.geom
FROM osm_pois o
WHERE NOT EXISTS (SELECT 1 FROM _matches m WHERE m.osm_id = o.osm_id)
ON CONFLICT (place_id) DO UPDATE SET
  names = EXCLUDED.names, phone = EXCLUDED.phone, website = EXCLUDED.website,
  opening_hours = EXCLUDED.opening_hours, geom = EXCLUDED.geom, updated_at = now();
"""

def conflate(conn: psycopg.Connection) -> tuple[int, int]:
    with conn.transaction():
        cur = conn.cursor()
        cur.execute(_SQL.split(";", 1)[0])                       # CREATE TEMP TABLE
        merged = cur.execute(_SQL.split(";")[1]).rowcount        # UPDATE
        inserted = cur.execute(";".join(_SQL.split(";")[2:])).rowcount  # INSERT
        cur.execute("DROP TABLE _matches")
        return merged, inserted
```

- [ ] **Step 4: 跑测试确认通过**

Run: `make test-py-integration`
Expected: PASS(含幂等断言)

- [ ] **Step 5: Makefile 加目标并对真实数据跑一遍**

```makefile
.PHONY: etl-conflate
etl-conflate:
	$(PY) -c "import psycopg, os; from etl.conflate import conflate; \
print('merged, inserted =', conflate(psycopg.connect(os.environ['DATABASE_URL'], autocommit=True)))"
```

Run: `make etl-conflate`
Expected: 打印 merged/inserted 计数;`SELECT count(*) FROM places` 应为 Overture 界内数 + OSM 未匹配数。

- [ ] **Step 6: Commit**

```bash
git add etl/ Makefile
git commit -m "feat: osm-overture conflation (50m + name similarity + category gate)"
```

---

### Task 9:OpenSearch mappings + 索引器(集成测试)

**Files:**
- Create: `etl/etl/mappings.json`, `etl/etl/index.py`, `etl/tests/test_index.py`
- Modify: `Makefile`(加 `etl-index`)

- [ ] **Step 1: 写 mappings.json**(分析器矩阵:ICU 通用文本、ICU 自动补全、高棉→拉丁转写、smartcn 中文;搜索侧用非 ngram 分析器)

```json
{
  "settings": {
    "index": {"number_of_shards": 1, "number_of_replicas": 0},
    "analysis": {
      "filter": {
        "ac_edge": {"type": "edge_ngram", "min_gram": 1, "max_gram": 20},
        "to_latin": {"type": "icu_transform", "id": "Any-Latin; NFD; [:Nonspacing Mark:] Remove; NFC; Lower"}
      },
      "analyzer": {
        "icu_text": {"type": "custom", "tokenizer": "icu_tokenizer", "filter": ["icu_folding"]},
        "icu_ac":   {"type": "custom", "tokenizer": "icu_tokenizer", "filter": ["icu_folding", "ac_edge"]},
        "latn_text":{"type": "custom", "tokenizer": "icu_tokenizer", "filter": ["to_latin"]},
        "latn_ac":  {"type": "custom", "tokenizer": "icu_tokenizer", "filter": ["to_latin", "ac_edge"]},
        "zh_text":  {"type": "custom", "tokenizer": "smartcn_tokenizer", "filter": ["lowercase"]},
        "zh_ac":    {"type": "custom", "tokenizer": "smartcn_tokenizer", "filter": ["lowercase", "ac_edge"]}
      }
    }
  },
  "mappings": {
    "properties": {
      "place_id":  {"type": "keyword"},
      "name_default": {"type": "text", "analyzer": "icu_text",
        "fields": {"ac": {"type": "text", "analyzer": "icu_ac", "search_analyzer": "icu_text"}}},
      "name_en": {"type": "text", "analyzer": "icu_text",
        "fields": {"ac": {"type": "text", "analyzer": "icu_ac", "search_analyzer": "icu_text"}}},
      "name_km": {"type": "text", "analyzer": "icu_text",
        "fields": {
          "ac":      {"type": "text", "analyzer": "icu_ac",  "search_analyzer": "icu_text"},
          "latn":    {"type": "text", "analyzer": "latn_text"},
          "latn_ac": {"type": "text", "analyzer": "latn_ac", "search_analyzer": "latn_text"}}},
      "name_zh": {"type": "text", "analyzer": "zh_text",
        "fields": {"ac": {"type": "text", "analyzer": "zh_ac", "search_analyzer": "zh_text"}}},
      "formatted_address": {"type": "text", "analyzer": "icu_text"},
      "categories": {"type": "keyword"},
      "confidence": {"type": "float"},
      "location":   {"type": "geo_point"}
    }
  }
}
```

- [ ] **Step 2: 写失败的集成测试**(建索引 → 灌 3 条文档 → 验证高棉语/中文/罗马音三种查询路径 + alias 原子切换)

```python
# etl/tests/test_index.py
import os, time
import pytest, requests
from etl.index import create_index, bulk_index, swap_alias, ALIAS

pytestmark = pytest.mark.integration
OS = os.environ.get("OPENSEARCH_URL", "http://localhost:9200")

DOCS = [
    {"place_id": "p1", "name_default": "អង្គរវត្ត", "name_en": "Angkor Wat", "name_zh": "吴哥窟",
     "name_km": "អង្គរវត្ត", "formatted_address": "Siem Reap, Cambodia",
     "categories": ["tourist_attraction"], "confidence": 0.95, "location": {"lat": 13.4125, "lon": 103.8670}},
    {"place_id": "p2", "name_default": "Malis Restaurant", "name_en": "Malis Restaurant",
     "formatted_address": "Phnom Penh, Cambodia",
     "categories": ["restaurant"], "confidence": 0.9, "location": {"lat": 11.5621, "lon": 104.9160}},
    {"place_id": "p3", "name_default": "金边中餐馆", "name_zh": "金边中餐馆",
     "formatted_address": "Phnom Penh, Cambodia",
     "categories": ["restaurant"], "confidence": 0.8, "location": {"lat": 11.57, "lon": 104.92}},
]

def _search(body):
    r = requests.post(f"{OS}/{ALIAS}/_search", json=body)
    r.raise_for_status()
    return [h["_source"]["place_id"] for h in r.json()["hits"]["hits"]]

def test_index_and_query_paths():
    name = create_index(OS)
    bulk_index(OS, name, iter(DOCS))
    swap_alias(OS, name)
    requests.post(f"{OS}/{name}/_refresh")
    assert "p1" in _search({"query": {"match": {"name_km": "អង្គរវត្ត"}}})          # 高棉语
    assert "p3" in _search({"query": {"match": {"name_zh": "中餐"}}})               # smartcn 分词
    assert "p1" in _search({"query": {"match": {"name_km.latn": "angkor"}}})        # 罗马音转写
    assert "p2" in _search({"query": {"match": {"name_en.ac": "mali"}}})            # 前缀补全
    # alias 切换原子性:再建一个空索引并切换,旧索引应被摘除
    name2 = create_index(OS)
    swap_alias(OS, name2)
    aliases = requests.get(f"{OS}/_alias/{ALIAS}").json()
    assert list(aliases.keys()) == [name2]
```

- [ ] **Step 3: 跑测试确认失败**

Run: `make test-py-integration`
Expected: FAIL,`ModuleNotFoundError: No module named 'etl.index'`

- [ ] **Step 4: 实现 index.py**

```python
# etl/etl/index.py
"""PostGIS → OpenSearch:时间戳索引名 + alias 原子切换,实现零停机重建。"""
import json, time
from pathlib import Path
from typing import Iterable, Iterator
import psycopg
import requests

ALIAS = "places"
_MAPPINGS = json.loads((Path(__file__).parent / "mappings.json").read_text())

def create_index(base_url: str) -> str:
    name = f"places-{time.strftime('%Y%m%d%H%M%S')}"
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
    resp = requests.post(f"{base_url}/_bulk", data="\n".join(buf) + "\n",
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
        actions = [{"remove": {"index": old, "alias": ALIAS}} for old in current.json()] + actions
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
```

- [ ] **Step 5: 跑测试确认通过**

Run: `make test-py-integration`
Expected: PASS

- [ ] **Step 6: Makefile 加全量索引目标并对真实数据执行**

```makefile
.PHONY: etl-index etl-all
etl-index:
	$(PY) -c "import psycopg, os; from etl.index import *; \
url = os.environ['OPENSEARCH_URL']; \
conn = psycopg.connect(os.environ['DATABASE_URL']); \
name = create_index(url); \
print('indexed:', bulk_index(url, name, rows_from_postgis(conn))); \
swap_alias(url, name); print('alias ->', name)"

etl-all: etl-overture etl-osm etl-load etl-conflate etl-index
```

Run: `make etl-index`
Expected: `indexed: N`(与 places 行数一致),`alias -> places-<ts>`。

- [ ] **Step 7: Commit**

```bash
git add etl/ Makefile
git commit -m "feat: opensearch mappings (icu/khmer-latin/smartcn) and zero-downtime indexer"
```

---

### Task 10:Go 脚手架 + Google 类型 + FieldMask(TDD)

**Files:**
- Create: `gateway/go.mod`, `gateway/internal/gapi/types.go`, `gateway/internal/fieldmask/fieldmask.go`, `gateway/internal/fieldmask/fieldmask_test.go`

- [ ] **Step 1: 初始化 module**

```bash
cd gateway && go mod init github.com/danielnanuk/open-map-service/gateway && cd ..
```

- [ ] **Step 2: 写 Google 协议类型**(Places API New 的最小子集;字段名严格对齐官方 JSON)

```go
// gateway/internal/gapi/types.go
package gapi

type LatLng struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Circle struct {
	Center LatLng  `json:"center"`
	Radius float64 `json:"radius"`
}

type Bias struct {
	Circle *Circle `json:"circle,omitempty"`
}

type LocalizedText struct {
	Text         string `json:"text"`
	LanguageCode string `json:"languageCode,omitempty"`
}

type OpeningHours struct {
	WeekdayDescriptions []string `json:"weekdayDescriptions,omitempty"`
}

type Place struct {
	Name                     string         `json:"name,omitempty"` // "places/<id>"
	ID                       string         `json:"id,omitempty"`
	DisplayName              *LocalizedText `json:"displayName,omitempty"`
	FormattedAddress         string         `json:"formattedAddress,omitempty"`
	Location                 *LatLng        `json:"location,omitempty"`
	Types                    []string       `json:"types,omitempty"`
	InternationalPhoneNumber string         `json:"internationalPhoneNumber,omitempty"`
	WebsiteURI               string         `json:"websiteUri,omitempty"`
	RegularOpeningHours      *OpeningHours  `json:"regularOpeningHours,omitempty"`
}

type SearchTextRequest struct {
	TextQuery    string `json:"textQuery"`
	LanguageCode string `json:"languageCode,omitempty"`
	PageSize     int    `json:"pageSize,omitempty"`
	LocationBias *Bias  `json:"locationBias,omitempty"`
}

type SearchNearbyRequest struct {
	LocationRestriction Bias     `json:"locationRestriction"`
	IncludedTypes       []string `json:"includedTypes,omitempty"`
	MaxResultCount      int      `json:"maxResultCount,omitempty"`
	RankPreference      string   `json:"rankPreference,omitempty"` // POPULARITY | DISTANCE
	LanguageCode        string   `json:"languageCode,omitempty"`
}

type PlacesResponse struct {
	Places []Place `json:"places"`
}

type AutocompleteRequest struct {
	Input        string `json:"input"`
	LanguageCode string `json:"languageCode,omitempty"`
	LocationBias *Bias  `json:"locationBias,omitempty"`
}

type StructuredFormat struct {
	MainText      *LocalizedText `json:"mainText,omitempty"`
	SecondaryText *LocalizedText `json:"secondaryText,omitempty"`
}

type PlacePrediction struct {
	Place            string            `json:"place"`   // "places/<id>"
	PlaceID          string            `json:"placeId"`
	Text             *LocalizedText    `json:"text,omitempty"`
	StructuredFormat *StructuredFormat `json:"structuredFormat,omitempty"`
	Types            []string          `json:"types,omitempty"`
}

type Suggestion struct {
	PlacePrediction *PlacePrediction `json:"placePrediction,omitempty"`
}

type AutocompleteResponse struct {
	Suggestions []Suggestion `json:"suggestions"`
}

// Google 风格错误体:{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}
```

- [ ] **Step 3: 写 FieldMask 失败测试**

```go
// gateway/internal/fieldmask/fieldmask_test.go
package fieldmask

import (
	"encoding/json"
	"reflect"
	"testing"
)

func apply(t *testing.T, doc, mask string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return Apply(m, mask)
}

func TestApplyPrunesNestedAndArrays(t *testing.T) {
	doc := `{"places":[{"id":"a","displayName":{"text":"X"},"location":{"latitude":1}},
	                   {"id":"b","displayName":{"text":"Y"},"location":{"latitude":2}}]}`
	got := apply(t, doc, "places.id,places.displayName")
	want := map[string]any{"places": []any{
		map[string]any{"id": "a", "displayName": map[string]any{"text": "X"}},
		map[string]any{"id": "b", "displayName": map[string]any{"text": "Y"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestStarReturnsAll(t *testing.T) {
	doc := `{"places":[{"id":"a"}]}`
	if got := apply(t, doc, "*"); len(got["places"].([]any)) != 1 {
		t.Fatalf("star mask should keep everything, got %v", got)
	}
}

func TestEmptyMaskReturnsAll(t *testing.T) {
	doc := `{"places":[{"id":"a"}]}`
	if got := apply(t, doc, ""); len(got["places"].([]any)) != 1 {
		t.Fatalf("empty mask should keep everything, got %v", got)
	}
}
```

- [ ] **Step 4: 跑测试确认失败**

Run: `cd gateway && go test ./internal/fieldmask/`
Expected: FAIL,`undefined: Apply`

- [ ] **Step 5: 实现 fieldmask.go**

```go
// gateway/internal/fieldmask/fieldmask.go
// Apply 实现 X-Goog-FieldMask 语义的响应裁剪:逗号分隔的点路径,数组元素逐个应用。
// 与 Google 的差异(刻意放宽):缺失 header 时返回全量而非报错。
package fieldmask

import "strings"

type node map[string]node

func Apply(doc map[string]any, mask string) map[string]any {
	mask = strings.TrimSpace(mask)
	if mask == "" || mask == "*" {
		return doc
	}
	root := node{}
	for _, path := range strings.Split(mask, ",") {
		cur := root
		for _, part := range strings.Split(strings.TrimSpace(path), ".") {
			if cur[part] == nil {
				cur[part] = node{}
			}
			cur = cur[part]
		}
	}
	return pruneMap(doc, root)
}

func pruneMap(m map[string]any, n node) map[string]any {
	out := map[string]any{}
	for key, sub := range n {
		val, ok := m[key]
		if !ok {
			continue
		}
		if len(sub) == 0 { // 叶子:整棵保留
			out[key] = val
			continue
		}
		out[key] = pruneValue(val, sub)
	}
	return out
}

func pruneValue(v any, n node) any {
	switch tv := v.(type) {
	case map[string]any:
		return pruneMap(tv, n)
	case []any:
		arr := make([]any, 0, len(tv))
		for _, item := range tv {
			arr = append(arr, pruneValue(item, n))
		}
		return arr
	default:
		return v
	}
}
```

- [ ] **Step 6: 跑测试确认通过**

Run: `cd gateway && go test ./...`
Expected: PASS(3 tests)

- [ ] **Step 7: Commit**

```bash
git add gateway/
git commit -m "feat: gateway scaffolding, google api types, fieldmask pruning"
```

---

### Task 11:OpenSearch 查询构造 + searchText / searchNearby(TDD)

**Files:**
- Create: `gateway/internal/search/client.go`, `gateway/internal/search/query.go`, `gateway/internal/search/query_test.go`, `gateway/internal/httpapi/handlers.go`, `gateway/internal/httpapi/respond.go`, `gateway/internal/httpapi/handlers_test.go`

- [ ] **Step 1: 写查询构造的失败测试**(纯函数,断言 DSL 形状)

```go
// gateway/internal/search/query_test.go
package search

import "testing"

func TestSearchTextBodyWithBias(t *testing.T) {
	body := searchTextBody("noodle", &Geo{Lat: 11.5, Lon: 104.9}, 10)
	fs := body["query"].(map[string]any)["function_score"].(map[string]any)
	mm := fs["query"].(map[string]any)["multi_match"].(map[string]any)
	if mm["query"] != "noodle" {
		t.Fatalf("query text lost: %v", mm)
	}
	if len(fs["functions"].([]map[string]any)) != 2 { // confidence + gauss
		t.Fatalf("want 2 scoring functions, got %v", fs["functions"])
	}
	if body["size"] != 10 {
		t.Fatalf("size: %v", body["size"])
	}
}

func TestSearchTextBodyNoBias(t *testing.T) {
	body := searchTextBody("noodle", nil, 5)
	fs := body["query"].(map[string]any)["function_score"].(map[string]any)
	if len(fs["functions"].([]map[string]any)) != 1 { // 仅 confidence
		t.Fatalf("want 1 scoring function, got %v", fs["functions"])
	}
}

func TestNearbyBodyDistanceRank(t *testing.T) {
	body := nearbyBody(Geo{Lat: 11.5, Lon: 104.9}, 500, []string{"restaurant"}, 20, true)
	filters := body["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]map[string]any)
	if len(filters) != 2 { // geo_distance + terms
		t.Fatalf("filters: %v", filters)
	}
	if _, ok := body["sort"]; !ok {
		t.Fatal("DISTANCE rank must set sort by _geo_distance")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd gateway && go test ./internal/search/`
Expected: FAIL,`undefined: searchTextBody`

- [ ] **Step 3: 实现 query.go 与 client.go**

```go
// gateway/internal/search/query.go
package search

type Geo struct{ Lat, Lon float64 }

var textFields = []string{"name_default^3", "name_en^3", "name_km^3", "name_zh^3",
	"name_km.latn^2", "formatted_address"}
var acFields = []string{"name_default.ac", "name_en.ac", "name_km.ac", "name_zh.ac", "name_km.latn_ac"}

func confidenceFn() map[string]any {
	return map[string]any{"field_value_factor": map[string]any{"field": "confidence", "missing": 0.7}}
}

func gaussFn(g Geo) map[string]any {
	return map[string]any{"gauss": map[string]any{"location": map[string]any{
		"origin": map[string]any{"lat": g.Lat, "lon": g.Lon}, "scale": "5km", "decay": 0.5}}}
}

func functionScore(query map[string]any, bias *Geo) map[string]any {
	fns := []map[string]any{confidenceFn()}
	if bias != nil {
		fns = append(fns, gaussFn(*bias))
	}
	return map[string]any{"function_score": map[string]any{
		"query": query, "functions": fns, "score_mode": "multiply", "boost_mode": "multiply"}}
}

func searchTextBody(q string, bias *Geo, size int) map[string]any {
	mm := map[string]any{"multi_match": map[string]any{"query": q, "type": "best_fields", "fields": textFields}}
	return map[string]any{"size": size, "query": functionScore(mm, bias)}
}

func autocompleteBody(input string, bias *Geo, size int) map[string]any {
	mm := map[string]any{"multi_match": map[string]any{"query": input, "type": "best_fields", "fields": acFields}}
	return map[string]any{"size": size, "query": functionScore(mm, bias)}
}

func nearbyBody(center Geo, radiusMeters float64, types []string, size int, rankByDistance bool) map[string]any {
	filters := []map[string]any{{"geo_distance": map[string]any{
		"distance": fmtMeters(radiusMeters),
		"location": map[string]any{"lat": center.Lat, "lon": center.Lon}}}}
	if len(types) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"categories": types}})
	}
	boolq := map[string]any{"bool": map[string]any{"filter": filters}}
	body := map[string]any{"size": size}
	if rankByDistance {
		body["query"] = boolq
		body["sort"] = []map[string]any{{"_geo_distance": map[string]any{
			"location": map[string]any{"lat": center.Lat, "lon": center.Lon},
			"order":    "asc", "unit": "m"}}}
	} else {
		body["query"] = functionScore(boolq, &center)
	}
	return body
}

func fmtMeters(m float64) string {
	return fmtFloat(m) + "m"
}

func fmtFloat(f float64) string {
	return strconvFormat(f)
}
```

注意:`strconvFormat` 不存在——直接用 `strconv.FormatFloat(m, 'f', -1, 64)`,上面两个包装函数删掉,`fmtMeters` 写成:

```go
import "strconv"

func fmtMeters(m float64) string {
	return strconv.FormatFloat(m, 'f', -1, 64) + "m"
}
```

```go
// gateway/internal/search/client.go
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Doc 与 etl/etl/index.py 写入的 _source 字段一一对应。
type Doc struct {
	PlaceID          string   `json:"place_id"`
	NameDefault      string   `json:"name_default"`
	NameEn           string   `json:"name_en"`
	NameKm           string   `json:"name_km"`
	NameZh           string   `json:"name_zh"`
	FormattedAddress string   `json:"formatted_address"`
	Categories       []string `json:"categories"`
	Confidence       float64  `json:"confidence"`
	Location         struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"location"`
}

type Client struct {
	BaseURL string
	Index   string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, Index: "places", HTTP: http.DefaultClient}
}

func (c *Client) SearchText(ctx context.Context, q string, bias *Geo, size int) ([]Doc, error) {
	return c.run(ctx, searchTextBody(q, bias, size))
}

func (c *Client) SearchNearby(ctx context.Context, center Geo, radius float64, types []string, size int, byDistance bool) ([]Doc, error) {
	return c.run(ctx, nearbyBody(center, radius, types, size, byDistance))
}

func (c *Client) Autocomplete(ctx context.Context, input string, bias *Geo, size int) ([]Doc, error) {
	return c.run(ctx, autocompleteBody(input, bias, size))
}

func (c *Client) run(ctx context.Context, body map[string]any) ([]Doc, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/"+c.Index+"/_search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opensearch status %d", resp.StatusCode)
	}
	var parsed struct {
		Hits struct {
			Hits []struct {
				Source Doc `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	docs := make([]Doc, 0, len(parsed.Hits.Hits))
	for _, h := range parsed.Hits.Hits {
		docs = append(docs, h.Source)
	}
	return docs, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd gateway && go test ./internal/search/`
Expected: PASS

- [ ] **Step 5: 写 handler 失败测试**(fake Searcher,断言协议形状与错误码)

```go
// gateway/internal/httpapi/handlers_test.go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

type fakeSearcher struct{ docs []search.Doc }

func (f *fakeSearcher) SearchText(context.Context, string, *search.Geo, int) ([]search.Doc, error) {
	return f.docs, nil
}
func (f *fakeSearcher) SearchNearby(context.Context, search.Geo, float64, []string, int, bool) ([]search.Doc, error) {
	return f.docs, nil
}
func (f *fakeSearcher) Autocomplete(context.Context, string, *search.Geo, int) ([]search.Doc, error) {
	return f.docs, nil
}

func doc() search.Doc {
	d := search.Doc{PlaceID: "p1", NameDefault: "អង្គរវត្ត", NameEn: "Angkor Wat",
		NameZh: "吴哥窟", FormattedAddress: "Siem Reap, Cambodia",
		Categories: []string{"tourist_attraction"}, Confidence: 0.95}
	d.Location.Lat, d.Location.Lon = 13.4125, 103.867
	return d
}

func TestSearchTextResponseShapeAndLanguage(t *testing.T) {
	h := New(&fakeSearcher{docs: []search.Doc{doc()}}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText",
		strings.NewReader(`{"textQuery":"angkor","languageCode":"zh"}`))
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	place := resp["places"].([]any)[0].(map[string]any)
	if place["id"] != "p1" || place["name"] != "places/p1" {
		t.Fatalf("place identity wrong: %v", place)
	}
	if place["displayName"].(map[string]any)["text"] != "吴哥窟" { // languageCode=zh 选中文名
		t.Fatalf("displayName should honor languageCode: %v", place)
	}
	loc := place["location"].(map[string]any)
	if loc["latitude"].(float64) != 13.4125 {
		t.Fatalf("location: %v", loc)
	}
}

func TestSearchTextEmptyQueryIsInvalidArgument(t *testing.T) {
	h := New(&fakeSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "INVALID_ARGUMENT") {
		t.Fatalf("want google-style 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSearchNearbyRequiresRestriction(t *testing.T) {
	h := New(&fakeSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchNearby", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.SearchNearby(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestFieldMaskApplied(t *testing.T) {
	h := New(&fakeSearcher{docs: []search.Doc{doc()}}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText", strings.NewReader(`{"textQuery":"x"}`))
	req.Header.Set("X-Goog-FieldMask", "places.id")
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	place := resp["places"].([]any)[0].(map[string]any)
	if len(place) != 1 || place["id"] != "p1" {
		t.Fatalf("fieldmask not applied: %v", place)
	}
}
```

- [ ] **Step 6: 跑测试确认失败**

Run: `cd gateway && go test ./internal/httpapi/`
Expected: FAIL,`undefined: New`

- [ ] **Step 7: 实现 respond.go 与 handlers.go**

```go
// gateway/internal/httpapi/respond.go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/fieldmask"
	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	mask := r.Header.Get("X-Goog-FieldMask")
	if mask != "" && status == http.StatusOK {
		raw, _ := json.Marshal(payload)
		var m map[string]any
		json.Unmarshal(raw, &m)
		payload = fieldmask.Apply(m, mask)
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, code int, status, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(gapi.ErrorBody{Error: gapi.ErrorDetail{Code: code, Message: msg, Status: status}})
}

func invalidArgument(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", msg)
}

func internal(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
}
```

```go
// gateway/internal/httpapi/handlers.go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
	"github.com/danielnanuk/open-map-service/gateway/internal/store"
)

type Searcher interface {
	SearchText(ctx context.Context, q string, bias *search.Geo, size int) ([]search.Doc, error)
	SearchNearby(ctx context.Context, center search.Geo, radius float64, types []string, size int, byDistance bool) ([]search.Doc, error)
	Autocomplete(ctx context.Context, input string, bias *search.Geo, size int) ([]search.Doc, error)
}

type PlaceStore interface {
	GetPlace(ctx context.Context, id string) (*store.PlaceRow, error)
}

type Handlers struct {
	searcher Searcher
	store    PlaceStore
}

func New(s Searcher, ps PlaceStore) *Handlers {
	return &Handlers{searcher: s, store: ps}
}

func geoFromBias(b *gapi.Bias) *search.Geo {
	if b == nil || b.Circle == nil {
		return nil
	}
	return &search.Geo{Lat: b.Circle.Center.Latitude, Lon: b.Circle.Center.Longitude}
}

// chooseName 按 languageCode 选展示名,缺失回退 default。
func chooseName(d search.Doc, lang string) string {
	switch lang {
	case "km":
		if d.NameKm != "" {
			return d.NameKm
		}
	case "zh":
		if d.NameZh != "" {
			return d.NameZh
		}
	case "en":
		if d.NameEn != "" {
			return d.NameEn
		}
	}
	return d.NameDefault
}

func docToPlace(d search.Doc, lang string) gapi.Place {
	return gapi.Place{
		Name:             "places/" + d.PlaceID,
		ID:               d.PlaceID,
		DisplayName:      &gapi.LocalizedText{Text: chooseName(d, lang), LanguageCode: lang},
		FormattedAddress: d.FormattedAddress,
		Location:         &gapi.LatLng{Latitude: d.Location.Lat, Longitude: d.Location.Lon},
		Types:            d.Categories,
	}
}

func docsToPlacesResponse(docs []search.Doc, lang string) gapi.PlacesResponse {
	out := gapi.PlacesResponse{Places: []gapi.Place{}}
	for _, d := range docs {
		out.Places = append(out.Places, docToPlace(d, lang))
	}
	return out
}

func (h *Handlers) SearchText(w http.ResponseWriter, r *http.Request) {
	var req gapi.SearchTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TextQuery == "" {
		invalidArgument(w, "textQuery is required")
		return
	}
	size := req.PageSize
	if size <= 0 || size > 20 {
		size = 10
	}
	docs, err := h.searcher.SearchText(r.Context(), req.TextQuery, geoFromBias(req.LocationBias), size)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, docsToPlacesResponse(docs, req.LanguageCode))
}

func (h *Handlers) SearchNearby(w http.ResponseWriter, r *http.Request) {
	var req gapi.SearchNearbyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LocationRestriction.Circle == nil {
		invalidArgument(w, "locationRestriction.circle is required")
		return
	}
	c := req.LocationRestriction.Circle
	if c.Radius <= 0 || c.Radius > 50000 {
		invalidArgument(w, "radius must be in (0, 50000]")
		return
	}
	size := req.MaxResultCount
	if size <= 0 || size > 20 {
		size = 20
	}
	docs, err := h.searcher.SearchNearby(r.Context(),
		search.Geo{Lat: c.Center.Latitude, Lon: c.Center.Longitude},
		c.Radius, req.IncludedTypes, size, req.RankPreference == "DISTANCE")
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, docsToPlacesResponse(docs, req.LanguageCode))
}
```

- [ ] **Step 8: 临时打桩 store 包**(本任务编译需要;Task 13 实现)

```go
// gateway/internal/store/store.go
package store

type PlaceRow struct {
	PlaceID      string
	Names        map[string]string
	Categories   []string
	Phone        string
	Website      string
	OpeningHours string
	Address      map[string]string
	Lon, Lat     float64
}
```

- [ ] **Step 9: 跑测试确认通过**

Run: `cd gateway && go test ./...`
Expected: PASS(httpapi 4 tests + 其余包)

- [ ] **Step 10: Commit**

```bash
git add gateway/
git commit -m "feat: searchText/searchNearby handlers with opensearch query builder"
```

---

### Task 12:autocomplete handler(TDD)

**Files:**
- Modify: `gateway/internal/httpapi/handlers.go`, `gateway/internal/httpapi/handlers_test.go`

- [ ] **Step 1: 写失败测试**

```go
// 追加到 gateway/internal/httpapi/handlers_test.go
func TestAutocompleteShape(t *testing.T) {
	h := New(&fakeSearcher{docs: []search.Doc{doc()}}, nil)
	req := httptest.NewRequest("POST", "/v1/places:autocomplete",
		strings.NewReader(`{"input":"ang","languageCode":"en"}`))
	rec := httptest.NewRecorder()
	h.Autocomplete(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	pred := resp["suggestions"].([]any)[0].(map[string]any)["placePrediction"].(map[string]any)
	if pred["placeId"] != "p1" || pred["place"] != "places/p1" {
		t.Fatalf("prediction identity: %v", pred)
	}
	sf := pred["structuredFormat"].(map[string]any)
	if sf["mainText"].(map[string]any)["text"] != "Angkor Wat" {
		t.Fatalf("mainText: %v", sf)
	}
	if sf["secondaryText"].(map[string]any)["text"] != "Siem Reap, Cambodia" {
		t.Fatalf("secondaryText: %v", sf)
	}
}

func TestAutocompleteEmptyInput(t *testing.T) {
	h := New(&fakeSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:autocomplete", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Autocomplete(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd gateway && go test ./internal/httpapi/`
Expected: FAIL,`h.Autocomplete undefined`

- [ ] **Step 3: 实现 Autocomplete handler**

```go
// 追加到 gateway/internal/httpapi/handlers.go
func (h *Handlers) Autocomplete(w http.ResponseWriter, r *http.Request) {
	var req gapi.AutocompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Input == "" {
		invalidArgument(w, "input is required")
		return
	}
	docs, err := h.searcher.Autocomplete(r.Context(), req.Input, geoFromBias(req.LocationBias), 5)
	if err != nil {
		internal(w, err)
		return
	}
	resp := gapi.AutocompleteResponse{Suggestions: []gapi.Suggestion{}}
	for _, d := range docs {
		name := chooseName(d, req.LanguageCode)
		resp.Suggestions = append(resp.Suggestions, gapi.Suggestion{PlacePrediction: &gapi.PlacePrediction{
			Place:   "places/" + d.PlaceID,
			PlaceID: d.PlaceID,
			Text:    &gapi.LocalizedText{Text: name},
			StructuredFormat: &gapi.StructuredFormat{
				MainText:      &gapi.LocalizedText{Text: name},
				SecondaryText: &gapi.LocalizedText{Text: d.FormattedAddress},
			},
			Types: d.Categories,
		}})
	}
	writeJSON(w, r, http.StatusOK, resp)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd gateway && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/
git commit -m "feat: autocomplete endpoint"
```

---

### Task 13:place details + PostGIS store(TDD)

**Files:**
- Modify: `gateway/internal/store/store.go`(加 pgx 查询), `gateway/internal/httpapi/handlers.go`, `gateway/internal/httpapi/handlers_test.go`

- [ ] **Step 1: 写失败测试**(fake store;含 404 NOT_FOUND 路径)

```go
// 追加到 gateway/internal/httpapi/handlers_test.go
type fakeStore struct{ row *store.PlaceRow }

func (f *fakeStore) GetPlace(_ context.Context, id string) (*store.PlaceRow, error) {
	if f.row != nil && f.row.PlaceID == id {
		return f.row, nil
	}
	return nil, nil
}

func TestGetPlaceDetails(t *testing.T) {
	row := &store.PlaceRow{
		PlaceID:      "p1",
		Names:        map[string]string{"default": "Malis", "km": "ម្លិះ"},
		Categories:   []string{"restaurant"},
		Phone:        "+855 15 814 888",
		Website:      "https://malis.example",
		OpeningHours: "Mo-Su 07:00-22:00",
		Address:      map[string]string{"freeform": "St 123", "locality": "Phnom Penh"},
		Lon:          104.916, Lat: 11.5621,
	}
	h := New(&fakeSearcher{}, &fakeStore{row: row})
	req := httptest.NewRequest("GET", "/v1/places/p1?languageCode=km", nil)
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	h.GetPlace(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var place map[string]any
	json.Unmarshal(rec.Body.Bytes(), &place)
	if place["displayName"].(map[string]any)["text"] != "ម្លិះ" {
		t.Fatalf("km name expected: %v", place)
	}
	if place["internationalPhoneNumber"] != "+855 15 814 888" {
		t.Fatalf("phone: %v", place)
	}
	if place["formattedAddress"] != "St 123, Phnom Penh, Cambodia" {
		t.Fatalf("address: %v", place)
	}
	oh := place["regularOpeningHours"].(map[string]any)["weekdayDescriptions"].([]any)
	if oh[0] != "Mo-Su 07:00-22:00" {
		t.Fatalf("hours: %v", oh)
	}
}

func TestGetPlaceNotFound(t *testing.T) {
	h := New(&fakeSearcher{}, &fakeStore{})
	req := httptest.NewRequest("GET", "/v1/places/nope", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	h.GetPlace(rec, req)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "NOT_FOUND") {
		t.Fatalf("want google-style 404, got %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd gateway && go test ./internal/httpapi/`
Expected: FAIL,`h.GetPlace undefined`

- [ ] **Step 3: 实现 store pgx 查询 + GetPlace handler**

```bash
cd gateway && go get github.com/jackc/pgx/v5@latest
```

```go
// gateway/internal/store/store.go 全量替换为:
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PlaceRow struct {
	PlaceID      string
	Names        map[string]string
	Categories   []string
	Phone        string
	Website      string
	OpeningHours string
	Address      map[string]string
	Lon, Lat     float64
}

type PG struct{ Pool *pgxpool.Pool }

func NewPG(ctx context.Context, dsn string) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &PG{Pool: pool}, nil
}

// GetPlace 返回 (nil, nil) 表示不存在。
func (p *PG) GetPlace(ctx context.Context, id string) (*PlaceRow, error) {
	row := p.Pool.QueryRow(ctx, `
		SELECT place_id, names, categories,
		       COALESCE(phone,''), COALESCE(website,''), COALESCE(opening_hours,''),
		       COALESCE(address, '{}'::jsonb), ST_X(geom), ST_Y(geom)
		FROM places WHERE place_id = $1`, id)
	var out PlaceRow
	err := row.Scan(&out.PlaceID, &out.Names, &out.Categories,
		&out.Phone, &out.Website, &out.OpeningHours, &out.Address, &out.Lon, &out.Lat)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}
```

```go
// 追加到 gateway/internal/httpapi/handlers.go
import "strings" // 加到文件顶部 import 块

func formatAddress(addr map[string]string) string {
	parts := []string{}
	for _, k := range []string{"freeform", "locality", "region"} {
		if v := addr[k]; v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(append(parts, "Cambodia"), ", ")
}

func (h *Handlers) GetPlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := h.store.GetPlace(r.Context(), id)
	if err != nil {
		internal(w, err)
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "place not found: "+id)
		return
	}
	lang := r.URL.Query().Get("languageCode")
	name := row.Names[lang]
	if name == "" {
		name = row.Names["default"]
	}
	place := gapi.Place{
		Name:                     "places/" + row.PlaceID,
		ID:                       row.PlaceID,
		DisplayName:              &gapi.LocalizedText{Text: name, LanguageCode: lang},
		FormattedAddress:         formatAddress(row.Address),
		Location:                 &gapi.LatLng{Latitude: row.Lat, Longitude: row.Lon},
		Types:                    row.Categories,
		InternationalPhoneNumber: row.Phone,
		WebsiteURI:               row.Website,
	}
	if row.OpeningHours != "" {
		// 简化:OSM opening_hours 原文作为单条 weekdayDescriptions,不解析为 periods(见 spec §4)
		place.RegularOpeningHours = &gapi.OpeningHours{WeekdayDescriptions: []string{row.OpeningHours}}
	}
	writeJSON(w, r, http.StatusOK, place)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd gateway && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/
git commit -m "feat: place details endpoint backed by postgis"
```

---

### Task 14:main.go + Dockerfile + 黄金查询集 + 端到端

**Files:**
- Create: `gateway/cmd/gateway/main.go`, `gateway/Dockerfile`, `golden/cases.yaml`, `golden/run_golden.py`, `README.md`
- Modify: `docker-compose.yml`(加 gateway), `Makefile`(加 `golden` / `test-go` / `e2e`)

- [ ] **Step 1: 写 main.go**

```go
// gateway/cmd/gateway/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/danielnanuk/open-map-service/gateway/internal/httpapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
	"github.com/danielnanuk/open-map-service/gateway/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	osURL := env("OPENSEARCH_URL", "http://localhost:9200")
	dsn := env("DATABASE_URL", "postgresql://places:places@localhost:5432/places")
	port := env("PORT", "8080")

	pg, err := store.NewPG(context.Background(), dsn)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	h := httpapi.New(search.New(osURL), pg)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/places:searchText", h.SearchText)
	mux.HandleFunc("POST /v1/places:searchNearby", h.SearchNearby)
	mux.HandleFunc("POST /v1/places:autocomplete", h.Autocomplete)
	mux.HandleFunc("GET /v1/places/{id}", h.GetPlace)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Printf("gateway listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
```

- [ ] **Step 2: 写 Dockerfile 并接入 compose**

```dockerfile
# gateway/Dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12
COPY --from=build /gateway /gateway
EXPOSE 8080
ENTRYPOINT ["/gateway"]
```

```yaml
# 追加到 docker-compose.yml services:
  gateway:
    build: gateway
    environment:
      DATABASE_URL: postgresql://places:places@postgis:5432/places
      OPENSEARCH_URL: http://opensearch:9200
    ports: ["8080:8080"]
    depends_on:
      postgis: {condition: service_healthy}
      opensearch: {condition: service_healthy}
```

```makefile
.PHONY: test-go up-all
test-go:
	cd gateway && go test ./...

up-all:
	docker compose up -d --build
```

- [ ] **Step 3: 写黄金查询集与执行器**(`soft: true` 的用例只告警不挂;zh 数据覆盖待验证,先标 soft——spec §14.1/§14.4 验证点)

```yaml
# golden/cases.yaml
- name: angkor_wat_khmer
  endpoint: searchText
  body: {textQuery: "អង្គរវត្ត"}
  expect_any: ["Angkor", "អង្គរ"]
- name: angkor_wat_english
  endpoint: searchText
  body: {textQuery: "Angkor Wat"}
  expect_any: ["Angkor", "អង្គរ"]
- name: angkor_wat_romanized_khmer
  endpoint: searchText
  body: {textQuery: "angkor vat"}
  expect_any: ["Angkor", "អង្គរ"]
- name: royal_palace_en
  endpoint: searchText
  body: {textQuery: "Royal Palace Phnom Penh"}
  expect_any: ["Royal Palace", "ព្រះបរមរាជវាំង"]
- name: central_market_khmer
  endpoint: searchText
  body: {textQuery: "ផ្សារធំថ្មី"}
  expect_any: ["Central Market", "ផ្សារធំថ្មី", "Phsar Thmei"]
- name: zh_angkor
  endpoint: searchText
  body: {textQuery: "吴哥窟"}
  expect_any: ["Angkor", "吴哥", "អង្គរ"]
  soft: true
- name: autocomplete_prefix_en
  endpoint: autocomplete
  body: {input: "angk"}
  expect_any: ["Angkor", "អង្គរ"]
- name: nearby_restaurants_pp
  endpoint: searchNearby
  body:
    locationRestriction: {circle: {center: {latitude: 11.5621, longitude: 104.9160}, radius: 2000}}
    includedTypes: ["restaurant"]
  expect_min_results: 3
```

```python
# golden/run_golden.py
"""黄金查询集执行器:hard 失败退出码 1,soft 失败仅告警。用法:python golden/run_golden.py [base_url]"""
import json, sys
import requests, yaml

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
ENDPOINTS = {"searchText": "/v1/places:searchText", "searchNearby": "/v1/places:searchNearby",
             "autocomplete": "/v1/places:autocomplete"}

def texts_in(resp: dict) -> str:
    return json.dumps(resp, ensure_ascii=False)

def run_case(case: dict) -> tuple[bool, str]:
    r = requests.post(BASE + ENDPOINTS[case["endpoint"]], json=case["body"], timeout=10)
    if r.status_code != 200:
        return False, f"HTTP {r.status_code}: {r.text[:200]}"
    body = r.json()
    if "expect_min_results" in case:
        n = len(body.get("places", body.get("suggestions", [])))
        ok = n >= case["expect_min_results"]
        return ok, f"results={n} (min {case['expect_min_results']})"
    blob = texts_in(body)
    hit = next((e for e in case["expect_any"] if e in blob), None)
    return hit is not None, f"matched={hit!r}" if hit else f"none of {case['expect_any']} in top results"

def main() -> int:
    cases = yaml.safe_load(open("golden/cases.yaml"))
    hard_failures = 0
    for case in cases:
        ok, detail = run_case(case)
        tag = "PASS" if ok else ("SOFT-FAIL" if case.get("soft") else "FAIL")
        print(f"[{tag}] {case['name']}: {detail}")
        if not ok and not case.get("soft"):
            hard_failures += 1
    print(f"\n{len(cases)} cases, {hard_failures} hard failures")
    return 1 if hard_failures else 0

if __name__ == "__main__":
    sys.exit(main())
```

```makefile
.PHONY: golden
golden:
	$(PY) golden/run_golden.py
```

- [ ] **Step 4: 写 README 运行手册**

```markdown
# open-map-service

柬埔寨自托管 Places + Routes API(Google 协议兼容)。设计文档见
`docs/superpowers/specs/2026-06-10-places-api-design.md`。

## M1 快速开始

```bash
make up            # postgis + opensearch
make migrate       # 建表
make py-setup      # python venv
make etl-all       # overture 下载→转换→osm 抽取→入库→conflation→索引(首次约 10-30 分钟)
make up-all        # 启动 gateway
make golden        # 黄金查询集验收
```

## 示例

```bash
curl -s -X POST localhost:8080/v1/places:searchText \
  -H 'Content-Type: application/json' \
  -H 'X-Goog-FieldMask: places.id,places.displayName,places.formattedAddress' \
  -d '{"textQuery":"អង្គរវត្ត","languageCode":"km"}'
```

## 测试

- Go:`make test-go`
- Python 单测:`make test-py`
- Python 集成(需 `make up && make migrate`):`make test-py-integration`
- 端到端:`make golden`

## 与 Google 协议的已知差异

- 缺 `X-Goog-FieldMask` 时返回全量字段(Google 会报错)
- `regularOpeningHours.weekdayDescriptions` 为 OSM `opening_hours` 原文,未解析成 periods
- 鉴权/配额在 M5 落地,当前无鉴权
```

- [ ] **Step 5: 全量端到端验证**

Run:

```bash
make test-go && make up-all
curl -s localhost:8080/healthz
make golden
```

Expected: go 测试全过;healthz 返回 `ok`;golden 输出 hard failures = 0(soft 用例允许告警)。若 golden 有 hard 失败,逐条核对:先用 `curl localhost:9200/places/_count` 确认索引非空,再用 OpenSearch `_analyze` 检查对应语言分析路径。

- [ ] **Step 6: 记录数据质量基线并 Commit**(spec §14.1 验证收尾)

```bash
docker compose exec -T postgis psql -U places -d places -c \
  "SELECT primary_source, count(*) FROM places GROUP BY 1; \
   SELECT count(*) FILTER (WHERE names ? 'km') AS km, \
          count(*) FILTER (WHERE names ? 'zh') AS zh, \
          count(*) FILTER (WHERE names ? 'en') AS en, count(*) AS total FROM places;"
git add gateway/ golden/ docker-compose.yml Makefile README.md
git commit -m "feat: gateway main + dockerfile + golden query suite (M1 complete)

数据基线: <粘贴上面两个查询的输出>"
```

---

## 自审记录(写完计划后跑过)

1. **Spec 覆盖(M1 部分)**:四端点 ✓(Task 11/12/13)、Overture+OSM→PostGIS+conflation ✓(Task 5-8)、KH 裁剪 ✓(Task 7)、三语言分析器+罗马音 ✓(Task 9)、GERS place_id ✓、FieldMask/错误码 ✓(Task 10/11)、黄金查询集 ✓(Task 14)、§14.1/§14.4 验证点落在 Task 5 Step 5 与 Task 14 Step 6 ✓。alias 零停机 ✓(Task 9)。M2-M5(Nominatim/Valhalla/OSRM/鉴权监控)不在本计划。
2. **占位符扫描**:无 TBD;所有代码块完整可编译;唯一"待填"是 commit message 里的实测数据量,属于运行产物非占位符。
3. **类型一致性**:`search.Doc` 字段 ↔ `etl/index.py` 写入的 `_source` 字段 ↔ `mappings.json` 属性名逐一核对一致;`store.PlaceRow` 在 Task 11 打桩与 Task 13 全量替换签名一致;`ALIAS="places"` 与 gateway `Index: "places"` 一致;staging 列名与 `_TRANSFORM_SQL` 输出列、`etl-load` 的 columns 列表一致(`sources_json→sources` 重命名已在 Task 7 Step 5 显式处理)。
