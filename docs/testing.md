# 测试文档

open-map-service 使用 Go 单元测试、Python 单元测试、Python 集成测试、golden 端到端测试和 curl smoke test 覆盖主要风险。

## 测试层级

| 层级 | 命令 | 覆盖范围 |
| --- | --- | --- |
| Go 单元测试 | `make test-go` | Gateway handler、auth、field mask、route/geocode/search adapter |
| Python 单元测试 | `make test-py` | ETL 转换、分类、索引逻辑、OSM 解析 |
| Python 集成测试 | `make test-py-integration` | PostGIS/OpenSearch 集成逻辑 |
| Golden 测试 | `make golden` | 端到端 API 行为 |
| Curl smoke test | `scripts/curl_public_smoke.sh` | 部署后公网或本地快速验证 |

## Go 单元测试

```bash
make test-go
```

等价于：

```bash
cd gateway && go test ./...
```

## Python 单元测试

首次运行：

```bash
make py-setup
```

执行：

```bash
make test-py
```

该命令排除 `integration` 标记，不需要真实 PostGIS/OpenSearch。

## Python 集成测试

前置条件：

```bash
make up
make migrate-test
```

执行：

```bash
make test-py-integration
```

集成测试使用：

```text
DATABASE_URL=postgresql://places:places@localhost:5432/places_test
OPENSEARCH_ALIAS=places_test
```

不要手动把集成测试指向生产 `places` 库。

## Golden 测试

前置条件：

```bash
make up-all
```

执行：

```bash
make golden
```

golden 用例位于：

```text
golden/cases.yaml
```

执行器：

```text
golden/run_golden.py
```

覆盖内容：

- Places 搜索
- Autocomplete
- Nearby Search
- Geocode / Reverse Geocode
- Routes
- Distance Matrix

也可以指定 base URL：

```bash
etl/.venv/bin/python golden/run_golden.py --base-url https://map.wildog.net
```

如果开启 API Key：

```bash
etl/.venv/bin/python golden/run_golden.py --base-url https://map.wildog.net --api-key "$API_KEY"
```

## Curl smoke test

脚本：

```text
scripts/curl_public_smoke.sh
```

本地：

```bash
BASE_URL=http://127.0.0.1:8080 scripts/curl_public_smoke.sh
```

公网：

```bash
BASE_URL=https://map.wildog.net scripts/curl_public_smoke.sh
```

开启 API Key：

```bash
API_KEY="$API_KEY" BASE_URL=https://map.wildog.net scripts/curl_public_smoke.sh
```

脚本覆盖：

- `/healthz`
- `/v1/places:searchText`
- `/maps/api/geocode/json`
- `/directions/v2:computeRoutes`
- `/distanceMatrix/v2:computeRouteMatrix`

## 当前验证记录

最近一次本地完整验证结果：

- Go 测试通过。
- Python 单元测试通过。
- Python 集成测试通过。
- Golden 测试通过。
- 本地 curl smoke test 通过。

如果公网测试失败，应先区分是业务服务问题还是 Cloudflare Tunnel 问题。公网 530 优先看 tunnel connector，502 优先看 Gateway 是否可达。
