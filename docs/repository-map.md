# 代码结构

本文档说明仓库主要目录和模块职责。

```text
.
├── gateway/          # Go Gateway
├── etl/              # Python ETL 包和测试
├── db/               # PostGIS 迁移
├── deploy/           # Docker、Kubernetes、Prometheus、OpenSearch 部署资产
├── profiles/         # OSRM Lua profile
├── golden/           # 端到端验收用例
├── scripts/          # 运维、更新、备份、压测脚本
├── data/             # 本地生成数据，通常不进 git
└── docs/             # 中文项目文档
```

## Gateway

```text
gateway/
├── cmd/gateway/main.go
├── internal/auth
├── internal/fieldmask
├── internal/geocode
├── internal/httpapi
├── internal/route
├── internal/search
└── internal/store
```

| 模块 | 说明 |
| --- | --- |
| `cmd/gateway` | 程序入口、路由注册、HTTP server 配置 |
| `internal/httpapi` | API handler、中间件、响应转换 |
| `internal/auth` | API Key 和配额 |
| `internal/search` | OpenSearch 查询构造和客户端 |
| `internal/store` | PostGIS 读取 |
| `internal/geocode` | Nominatim 客户端和 geocoding 响应转换 |
| `internal/route` | Valhalla、OSRM、polyline、matrix |
| `internal/fieldmask` | Google FieldMask 风格字段过滤 |

## ETL

```text
etl/
├── etl/
│   ├── categories.py
│   ├── conflate.py
│   ├── index.py
│   ├── load.py
│   ├── mappings.json
│   ├── osm.py
│   └── overture.py
└── tests/
```

| 模块 | 说明 |
| --- | --- |
| `overture.py` | 下载和转换 Overture 数据 |
| `osm.py` | 从 OSM PBF 抽取 POI |
| `load.py` | 加载 PostGIS staging 和权威表 |
| `conflate.py` | OSM 与 Overture 合并去重 |
| `index.py` | OpenSearch 建索引、批量写入、alias 切换 |
| `mappings.json` | OpenSearch mapping 和 analyzer 配置 |

## 数据库

```text
db/
├── migrate.sh
└── migrations/
```

迁移包括：

- `places`
- `staging_overture`
- `osm_pois`
- `country_boundary`
- `api_keys`

## 部署资产

```text
deploy/
├── docker/
├── k8s/
├── opensearch/
└── prometheus/
```

| 目录 | 说明 |
| --- | --- |
| `deploy/docker` | 构建数据镜像的 Dockerfile |
| `deploy/k8s` | Kubernetes manifests |
| `deploy/opensearch` | OpenSearch 镜像扩展 |
| `deploy/prometheus` | Prometheus 配置和告警规则 |

## 脚本

| 脚本 | 说明 |
| --- | --- |
| `scripts/update_pipeline.sh` | 生产式数据更新 |
| `scripts/backup.sh` | 备份 PostGIS、OpenSearch、图构件 |
| `scripts/restore.sh` | 恢复备份 |
| `scripts/curl_public_smoke.sh` | 本地/公网 curl 验证 |
| `scripts/load_test.py` | 简单压测 |
| `scripts/matrix_benchmark.sh` | 矩阵性能测试 |
| `scripts/tuktuk_calibration.sh` | 嘟嘟车 profile 校准 |

## Golden

```text
golden/
├── cases.yaml
└── run_golden.py
```

`cases.yaml` 是高信号验收用例集合，覆盖搜索、地理编码、路线和矩阵。
