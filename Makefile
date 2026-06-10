SHELL := /bin/bash
export DATABASE_URL ?= postgresql://places:places@localhost:5432/places
export OPENSEARCH_URL ?= http://localhost:9200
PY := etl/.venv/bin/python
PIP := etl/.venv/bin/pip
PYTEST := etl/.venv/bin/pytest

.PHONY: help
help:
	@grep -E '^[a-z][a-zA-Z0-9_-]*:' $(firstword $(MAKEFILE_LIST)) | cut -d: -f1 | sort
