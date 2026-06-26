# 开发手册

本文档说明如何在本地启动、开发和调试 open-map-service。

## 环境要求

- Docker / Docker Compose
- Go，版本以 `gateway/go.mod` 为准
- Python 3.11+
- `make`
- `curl`

OpenSearch 需要主机设置：

```bash
sudo sysctl -w vm.max_map_count=262144
```

macOS 上该参数不适用 Linux 内核语义；如果通过 Docker Desktop 运行，需要在 Docker VM 或目标 Linux 主机上处理。

## 初始化 Python 环境

```bash
make py-setup
```

该命令会创建 `etl/.venv` 并以 editable 模式安装 ETL 包和测试依赖。

## 准备 OSM 数据

```bash
make etl-osm
```

该命令会下载 `data/cambodia-latest.osm.pbf`，并抽取有名称的 OSM POI 到 `data/osm_pois.parquet`。

## 构建 OSRM 图

```bash
make osrm-build
```

会为三种 profile 构建图：

- `car`
- `moto`
- `tuktuk`

输出位于：

```text
data/osrm/car
data/osrm/moto
data/osrm/tuktuk
```

## 启动基础服务

```bash
make up
```

该命令启动：

- PostGIS
- OpenSearch
- Nominatim
- Valhalla
- OSRM car/moto/tuktuk
- Prometheus

检查状态：

```bash
docker compose ps
curl -s http://localhost:8081/status
curl -s http://localhost:8002/status
```

首次启动 Nominatim 会导入柬埔寨 PBF，通常需要数分钟。

## 建表

```bash
make migrate
```

迁移脚本位于 `db/migrations`。

## 执行完整 ETL

```bash
make etl-all
```

该命令依次执行：

1. 下载并转换 Overture Places / Divisions。
2. 下载或复用 Cambodia OSM PBF。
3. 加载 Overture 数据和 KH 边界。
4. 加载 OSM POI。
5. 执行 OSM 与 Overture 合并去重。
6. 写入 OpenSearch 新索引并切换 alias。

## 启动 Gateway

```bash
make up-all
```

验证：

```bash
curl -s http://localhost:8080/healthz
```

## 常用 Make 命令

| 命令 | 说明 |
| --- | --- |
| `make help` | 列出可用 target |
| `make up` | 启动基础服务 |
| `make up-all` | 启动全部服务，包括 gateway |
| `make down` | 停止 Compose 服务 |
| `make migrate` | 初始化或更新主库 schema |
| `make migrate-test` | 初始化测试库 `places_test` |
| `make py-setup` | 创建 Python venv 并安装依赖 |
| `make test-go` | Go 单元测试 |
| `make test-py` | Python 非集成测试 |
| `make test-py-integration` | Python 集成测试 |
| `make golden` | 端到端 golden 验收 |
| `make update-all` | 执行生产式数据更新管道 |
| `make gen-api-key NAME=xxx` | 生成 API Key |

## 常见问题

### `data/cambodia-latest.osm.pbf missing`

先执行：

```bash
make etl-osm
```

### Postgres 端口冲突

`docker-compose.yml` 默认绑定 `127.0.0.1:5432:5432`。如果本机已有 PostgreSQL，可以临时改 Compose 端口，或停止本机 PostgreSQL。

### OpenSearch 启动失败

检查 `vm.max_map_count` 是否足够：

```bash
sysctl vm.max_map_count
```

Linux 生产机建议持久化到 `/etc/sysctl.d/`。

### Python 版本错误

ETL 要求 Python 3.11+。如果系统默认 `python3` 版本过低，请用 Python 3.11 创建 venv 后再安装。
