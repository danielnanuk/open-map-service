SHELL := /bin/bash
export DATABASE_URL ?= postgresql://places:places@localhost:5432/places
export OPENSEARCH_URL ?= http://localhost:9200
PY := etl/.venv/bin/python
PIP := etl/.venv/bin/pip
PYTEST := etl/.venv/bin/pytest

.PHONY: help
help:
	@grep -E '^[a-z][a-zA-Z0-9_-]*:' $(firstword $(MAKEFILE_LIST)) | cut -d: -f1 | sort

.PHONY: up down
up:
	docker compose up -d --build postgis opensearch
	docker compose ps

down:
	docker compose down

.PHONY: migrate
migrate:
	bash db/migrate.sh

.PHONY: py-setup test-py test-py-integration
py-setup:
	python3 -m venv etl/.venv
	$(PIP) install -e 'etl[dev]'

test-py:
	cd etl && .venv/bin/pytest -m "not integration" -q

test-py-integration:
	cd etl && .venv/bin/pytest -m integration -q

.PHONY: etl-overture probe-overture
etl-overture:
	mkdir -p data
	$(PY) -c "from etl.overture import download, transform; \
download('data/overture_places_raw.parquet','data/overture_divisions_raw.parquet'); \
n = transform('data/overture_places_raw.parquet','data/overture_places.parquet'); \
print(f'transformed {n} places')"

probe-overture:
	$(PY) -c "import duckdb; print(duckdb.sql(\"DESCRIBE SELECT * FROM read_parquet('data/overture_places_raw.parquet')\"))"

.PHONY: etl-load
# 注意:etl-load 不是自洽的——它会用 Overture 字段覆盖 places,务必随后跑 etl-conflate(或直接用 etl-all)
etl-load:
	$(PY) -c "import psycopg, os, pyarrow.parquet as pq, pyarrow as pa; \
from etl.load import *; \
conn = psycopg.connect(os.environ['DATABASE_URL'], autocommit=True); \
load_boundary_wkt(conn, 'KH', extract_kh_boundary_wkt('data/overture_divisions_raw.parquet')); \
assert not conn.execute(\"SELECT ST_IsEmpty(geom) FROM country_boundary WHERE iso='KH'\").fetchone()[0], 'KH boundary empty — divisions extract degenerate'; \
t = pq.read_table('data/overture_places.parquet'); \
t = t.rename_columns([{'sources_json':'sources'}.get(c, c) for c in t.column_names]); \
pq.write_table(t, 'data/overture_places_staged.parquet'); \
print('staged:', copy_parquet_to_staging(conn, 'data/overture_places_staged.parquet', 'staging_overture', \
  ['place_id','name_default','name_km','name_en','name_zh','raw_category','google_type','phone','website', \
   'addr_freeform','addr_locality','addr_region','addr_country','confidence','sources','lon','lat'])); \
n = upsert_places_from_staging(conn); print('places upserted:', n); \
assert n > 0, 'zero places upserted — boundary or staging broken'; \
print('osm pois loaded:', load_osm_pois(conn, 'data/osm_pois.parquet'))"

.PHONY: etl-conflate
etl-conflate:
	$(PY) -c "import psycopg, os; from etl.conflate import conflate; \
print('merged, inserted =', conflate(psycopg.connect(os.environ['DATABASE_URL'], autocommit=True)))"

.PHONY: etl-osm
etl-osm:
	test -f data/cambodia-latest.osm.pbf || curl -L -o data/cambodia-latest.osm.pbf \
		https://download.geofabrik.de/asia/cambodia-latest.osm.pbf
	$(PY) -c "from etl.osm import extract; print('osm pois:', extract('data/cambodia-latest.osm.pbf','data/osm_pois.parquet'))"
