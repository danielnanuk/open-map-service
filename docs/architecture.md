# 架构文档

open-map-service 是一个面向柬埔寨区域的自托管地图 API 服务。它对外暴露 Google 风格的 Places、Geocoding、Routes 和 Distance Matrix API，对内组合 PostGIS、OpenSearch、Nominatim、Valhalla 和 OSRM。

## 设计目标

- 提供可自托管、可复现的地点搜索、地理编码、路线规划和距离矩阵能力。
- 尽量兼容 Google API 的请求/响应形态，降低客户端迁移成本。
- 支持 Khmer、English、Chinese 等多语言名称，具体覆盖度取决于源数据。
- 支持柬埔寨本地交通形态，包括驾车、摩托、步行和嘟嘟车。
- 数据处理、索引构建、路由图构建都可以通过脚本重复执行。

## 系统上下文

```mermaid
flowchart LR
    Client["客户端 / 调用方"] --> CF["Cloudflare / 可选公网入口"]
    CF --> Tunnel["cloudflared tunnel"]
    Tunnel --> Gateway["Go Gateway :8080"]

    Client -.本地或内网.-> Gateway

    Gateway --> PostGIS["PostGIS\n权威地点库"]
    Gateway --> OpenSearch["OpenSearch\n搜索索引"]
    Gateway --> Nominatim["Nominatim\nOSM 地理编码"]
    Gateway --> Valhalla["Valhalla\n路线 + 小矩阵"]
    Gateway --> OSRMCar["OSRM car"]
    Gateway --> OSRMMoto["OSRM moto"]
    Gateway --> OSRMTuktuk["OSRM tuktuk"]
    Prometheus["Prometheus"] --> Gateway
```

Gateway 是唯一需要对外暴露的应用服务。PostGIS、OpenSearch、Nominatim、Valhalla 和 OSRM 都是内部依赖。

## 运行时组件

| 组件 | 职责 | 主要位置 |
| --- | --- | --- |
| Gateway | HTTP API、Google 风格协议适配、鉴权、指标、后端编排 | `gateway/cmd/gateway/main.go`、`gateway/internal/httpapi` |
| PostGIS | 权威地点数据、API Key、边界和 staging 表 | `db/migrations`、`gateway/internal/store` |
| OpenSearch | 文本搜索、自动补全、附近搜索 | `etl/etl/index.py`、`gateway/internal/search` |
| Nominatim | OSM 地址正向/逆向地理编码 | `gateway/internal/geocode` |
| Valhalla | 路线规划、小矩阵、OSRM 降级路径 | `gateway/internal/route/valhalla.go` |
| OSRM | 大矩阵计算，分 car、moto、tuktuk 三套图 | `gateway/internal/route/osrm.go`、`profiles/` |
| Prometheus | 指标采集和告警规则评估 | `deploy/prometheus` |

## Gateway 路由

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/v1/places:searchText` | 文本地点搜索 |
| `POST` | `/v1/places:searchNearby` | 附近地点搜索 |
| `POST` | `/v1/places:autocomplete` | 地点自动补全 |
| `GET` | `/v1/places/{id}` | 地点详情 |
| `GET` | `/maps/api/geocode/json` | 地址正向/逆向地理编码 |
| `POST` | `/directions/v2:computeRoutes` | 路线规划 |
| `POST` | `/distanceMatrix/v2:computeRouteMatrix` | 距离矩阵 |
| `GET` | `/healthz` | 存活检查 |
| `GET` | `/metrics` | Prometheus 指标 |

所有 API 请求都会经过 request id、Prometheus metrics 和可选 API Key 鉴权中间件。鉴权由 `AUTH_ENABLED=true` 开启，默认关闭。

## Gateway 模块关系

```mermaid
classDiagram
    class GatewayMain {
        +main()
        +register routes
        +configure http.Server
    }
    class HTTPHandlers {
        +SearchText()
        +SearchNearby()
        +Autocomplete()
        +GetPlace()
        +Geocode()
        +ComputeRoutes()
        +ComputeRouteMatrix()
    }
    class AuthMiddleware {
        +WithAuth()
        +API key quota
    }
    class SearchClient {
        +SearchText()
        +SearchNearby()
        +Autocomplete()
    }
    class Store {
        +GetPlace()
        +NearbyPOI()
        +API keys
    }
    class Geocoder {
        +Forward()
        +Reverse()
    }
    class Router {
        +Route()
        +Matrix()
    }

    GatewayMain --> HTTPHandlers
    GatewayMain --> AuthMiddleware
    HTTPHandlers --> SearchClient
    HTTPHandlers --> Store
    HTTPHandlers --> Geocoder
    HTTPHandlers --> Router
```

## 地点搜索链路

```mermaid
sequenceDiagram
    participant C as Client
    participant G as Gateway
    participant OS as OpenSearch
    participant PG as PostGIS

    C->>G: POST /v1/places:searchText
    G->>OS: 查询文本、语言、位置偏置、返回数量
    OS-->>G: 搜索文档
    G-->>C: Google 风格 Places 响应

    C->>G: GET /v1/places/{id}
    G->>PG: 按 place_id 查询权威记录
    PG-->>G: 地点详情
    G-->>C: Place Detail 响应
```

OpenSearch 用于发现和排序，PostGIS 是详情接口的数据源。搜索结果中的 PostGIS/OpenSearch `place_id` 可以用于 `/v1/places/{id}`，但 Nominatim 返回的 `nominatim:way:123` 这类 ID 不能用于地点详情接口。

## 地理编码链路

```mermaid
flowchart TD
    Req["GET /maps/api/geocode/json"] --> Kind{"请求类型"}
    Kind -->|"address"| Forward["Nominatim forward geocode"]
    Kind -->|"latlng"| Reverse["Nominatim reverse geocode"]
    Forward --> Normalize["转换为 Google Geocoding 响应"]
    Reverse --> Nearby["从 PostGIS 补充附近 POI"]
    Nearby --> Normalize
    Normalize --> Resp["返回 results + status"]
```

正向和逆向地理编码主要依赖 Nominatim。逆向地理编码会额外尝试从 PostGIS 查询附近 POI，用于增强结果。

## 路线与矩阵链路

```mermaid
flowchart TD
    Routes["POST /directions/v2:computeRoutes"] --> Costing["travelMode + vehicleProfile 映射"]
    Costing --> ValhallaRoute["Valhalla route"]
    ValhallaRoute --> Polyline["Polyline 1e-6 转 Google 1e-5"]
    Polyline --> RouteResp["Routes 响应"]

    Matrix["POST /distanceMatrix/v2:computeRouteMatrix"] --> Size{"origins * destinations > 2500?"}
    Size -->|"否"| ValhallaMatrix["Valhalla matrix"]
    Size -->|"是"| PickOSRM["按 profile 选择 OSRM"]
    PickOSRM --> OSRMTable["OSRM table"]
    OSRMTable -->|"失败"| ValhallaMatrix
    OSRMTable --> MatrixResp["矩阵元素数组"]
    ValhallaMatrix --> MatrixResp
```

Valhalla 是路线规划和小矩阵的默认引擎。OSRM 用于大矩阵，因为 table 计算更快。OSRM 不可用时，矩阵请求会降级到 Valhalla。

## 数据流

```mermaid
flowchart TD
    Overture["Overture Places / Divisions"] --> Transform["转换嵌套结构"]
    Geofabrik["Geofabrik Cambodia OSM PBF"] --> OSMExtract["抽取有名称 POI"]
    Geofabrik --> Graphs["构建 OSRM / Valhalla 图"]

    Transform --> Load["写入 staging + KH 边界裁剪"]
    OSMExtract --> Load
    Load --> PostGIS["PostGIS places / osm_pois"]
    PostGIS --> Conflate["OSM 与 Overture 合并去重"]
    Conflate --> Canonical["权威 places 表"]
    Canonical --> Index["写入时间戳 OpenSearch 索引"]
    Index --> Alias["places alias 原子切换"]
```

完整数据管道由 `make etl-all` 和 `scripts/update_pipeline.sh` 承载。详细说明见 [数据管道](data-pipeline.md)。

## 部署拓扑

Docker Compose 是本地和单机参考部署。Kubernetes manifests 位于 `deploy/k8s`。

```mermaid
flowchart LR
    subgraph Runtime["运行时服务"]
        Gateway["gateway"]
        PostGIS["postgis"]
        OpenSearch["opensearch"]
        Nominatim["nominatim"]
        Valhalla["valhalla"]
        OSRM["osrm-car / osrm-moto / osrm-tuktuk"]
    end

    subgraph Batch["批处理"]
        ETL["ETL / 更新脚本"]
        Golden["golden 验收"]
        Backup["备份恢复"]
    end

    subgraph Observe["可观测性"]
        Prometheus["Prometheus"]
        Rules["告警规则"]
    end

    Gateway --> PostGIS
    Gateway --> OpenSearch
    Gateway --> Nominatim
    Gateway --> Valhalla
    Gateway --> OSRM
    ETL --> PostGIS
    ETL --> OpenSearch
    Golden --> Gateway
    Prometheus --> Gateway
    Rules --> Prometheus
```

## 可靠性设计

- OpenSearch 采用时间戳索引 + alias 原子切换，降低重建索引时的中断风险。
- 数据更新脚本有行数漂移闸，避免上游数据异常静默污染生产索引。
- 大矩阵优先 OSRM，失败后降级 Valhalla。
- Gateway `WriteTimeout` 为 120 秒，给大矩阵响应留出时间。
- OSRM 图文件不能在线覆盖，更新脚本会先停止 OSRM，再重建图。
- `/metrics` 暴露 Prometheus 指标，告警规则位于 `deploy/prometheus/rules.yml`。

## 代码结构入口

更多目录说明见 [代码结构](repository-map.md)。
