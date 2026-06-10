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
