import os

# 集成测试一律打独立库/独立 alias,不再破坏开发数据(M1 教训,M5 兑现)
os.environ.setdefault("DATABASE_URL", "postgresql://places:places@localhost:5432/places_test")
os.environ.setdefault("OPENSEARCH_ALIAS", "places_test")
