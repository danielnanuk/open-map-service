# open-map-service

柬埔寨自托管地图服务，提供 Google 风格的 Places、Geocoding、Routes 和 Distance Matrix API。项目使用 Overture Maps 与 OpenStreetMap 数据，运行时由 Go Gateway、PostGIS、OpenSearch、Nominatim、Valhalla 和 OSRM 组成。

## 能力概览

- 地点搜索：文本搜索、附近搜索、自动补全、地点详情。
- 地理编码：地址转坐标、坐标反查地址，并可补充附近 POI。
- 路线规划：支持驾车、两轮车、步行，以及扩展的 `vehicleProfile:"tuktuk"`。
- 距离矩阵：小矩阵走 Valhalla，大矩阵走 OSRM，OSRM 故障时可降级 Valhalla。
- 数据管道：Overture + OSM 抽取、入库、合并去重、OpenSearch 蓝绿索引切换。
- 运维能力：Prometheus 指标、告警规则、备份恢复、golden 验收测试。

## 快速开始

首次运行需要 Docker、Go、Python 3.11+。OpenSearch 主机需要设置：

```bash
sudo sysctl -w vm.max_map_count=262144
```

本地完整启动：

```bash
make py-setup
make etl-osm
make osrm-build
make up
make migrate
make etl-all
make up-all
make golden
```

常用健康检查：

```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:8081/status
curl -s http://localhost:8002/status
```

## API 示例

```bash
curl -s -X POST localhost:8080/v1/places:searchText \
  -H 'Content-Type: application/json' \
  -H 'X-Goog-FieldMask: places.id,places.displayName,places.formattedAddress' \
  -d '{"textQuery":"Angkor Wat","languageCode":"en"}'
```

```bash
curl -s 'localhost:8080/maps/api/geocode/json?address=Street+271,+Phnom+Penh&language=en'
```

```bash
curl -s -X POST localhost:8080/directions/v2:computeRoutes \
  -H 'Content-Type: application/json' \
  -d '{"origin":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}},"destination":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}},"travelMode":"TWO_WHEELER","vehicleProfile":"tuktuk"}'
```

更多接口见 [API 文档](docs/api.md)。

## 测试

```bash
make test-go
make test-py
make migrate-test
make test-py-integration
make golden
```

公网 smoke test：

```bash
BASE_URL=https://map.wildog.net scripts/curl_public_smoke.sh
```

完整说明见 [测试文档](docs/testing.md)。

## 文档目录

文档使用 GitHub 原生 Markdown，流程图和模块关系图使用 Mermaid。

| 文档 | 说明 |
| --- | --- |
| [架构文档](docs/architecture.md) | 系统组件、请求链路、数据流、部署拓扑 |
| [API 文档](docs/api.md) | HTTP 接口、请求示例、响应说明、Google 兼容差异 |
| [开发手册](docs/development.md) | 本地环境、启动流程、常用 Make 命令 |
| [数据管道](docs/data-pipeline.md) | Overture/OSM ETL、合并去重、索引切换、更新流程 |
| [部署文档](docs/deployment.md) | Docker Compose、Cloudflare Tunnel、Kubernetes 部署入口 |
| [运维手册](docs/operations.md) | 健康检查、日志、监控、备份恢复、故障排查 |
| [测试文档](docs/testing.md) | 单测、集成测试、golden、公网 curl 验证 |
| [代码结构](docs/repository-map.md) | 仓库目录和模块职责 |

## 生产注意事项

- 默认 `AUTH_ENABLED=false`，生产公网暴露前应开启 API Key 鉴权或接入 Cloudflare Access。
- `map.wildog.net` 当前通过 Cloudflare Tunnel 指向本机 `127.0.0.1:8080`。
- Nominatim、OSRM、Valhalla 图数据都来自 OpenStreetMap，需要遵守 OSM 署名要求。
- Overture 数据需要遵守 Overture Maps Foundation 数据许可。
- 备份恢复、数据更新和漂移闸说明见 [运维手册](docs/operations.md) 与 [数据管道](docs/data-pipeline.md)。
