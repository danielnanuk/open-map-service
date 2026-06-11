# open-map-service

柬埔寨自托管 Places + Routes API(Google 协议兼容)。设计文档见
`docs/superpowers/specs/2026-06-10-places-api-design.md`。

## 快速开始

```bash
make osrm-build    # 首次需要:提取三套 OSRM 图(car/moto/tuktuk,~3-5 分钟)
make up            # postgis + opensearch + nominatim + valhalla + 三个 OSRM 实例
make migrate       # 建表
make py-setup      # python venv
make etl-all       # overture 下载→转换→osm 抽取→入库→conflation→索引(首次约 10-30 分钟)
make up-all        # 启动 gateway
make golden        # 黄金查询集验收(18 cases: 12 places/geocode + 4 routes + 2 matrix)
```

首次启动 Nominatim 会执行一次性导入(柬埔寨 ~5-15 分钟,`docker logs places-nominatim-1` 看进度,
就绪标志 `curl localhost:8081/status`)。

Valhalla 首次启动会从 OSM PBF 建图,约 1-3 分钟,就绪标志 `curl localhost:8002/status`。

生产机前置要求:OpenSearch 需要 `vm.max_map_count ≥ 262144`:
`sudo sysctl -w vm.max_map_count=262144`(写入 /etc/sysctl.d/ 持久化)。

## 示例

```bash
curl -s -X POST localhost:8080/v1/places:searchText \
  -H 'Content-Type: application/json' \
  -H 'X-Goog-FieldMask: places.id,places.displayName,places.formattedAddress' \
  -d '{"textQuery":"អង្គរវត្ត","languageCode":"km"}'

# 正向地理编码(地址→坐标)
curl -s 'localhost:8080/maps/api/geocode/json?address=Street+271,+Phnom+Penh&language=km'
# 逆向(坐标→地址+附近 POI)
curl -s 'localhost:8080/maps/api/geocode/json?latlng=11.5621,104.9160'

# 路线规划(嘟嘟车,协议扩展 vehicleProfile)
curl -s -X POST localhost:8080/directions/v2:computeRoutes -H 'Content-Type: application/json' \
  -d '{"origin":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}},"destination":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}},"travelMode":"TWO_WHEELER","vehicleProfile":"tuktuk"}'

# 距离矩阵(2 起点 × 2 终点 = 4 元素,返回 JSON 数组)
curl -s -X POST localhost:8080/distanceMatrix/v2:computeRouteMatrix -H 'Content-Type: application/json' \
  -d '{"origins":[{"waypoint":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}}},{"waypoint":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}}}],"destinations":[{"waypoint":{"location":{"latLng":{"latitude":11.5984,"longitude":104.9192}}}},{"waypoint":{"location":{"latLng":{"latitude":11.5625,"longitude":104.9311}}}}],"travelMode":"DRIVE"}'
```

## 测试

- Go:`make test-go`
- Python 单测:`make test-py`
- Python 集成(需 `make up && make migrate`):`make test-py-integration`
- 端到端:`make golden`

⚠️ 集成测试对共享开发库是破坏性的(TRUNCATE 表、切换索引 alias):跑过
`test-py-integration` 之后,先 `make etl-load etl-conflate etl-index` 恢复真实数据
再 `make golden`。(M5 将引入独立测试库。)

## 数据基线(2026-06-10)

- Overture 柬埔寨境内 POI:98,172(bbox 全量 696,119,KH 国界裁剪后)
- OSM 带名 POI:13,726(node 10,662 / way 3,006 / rel 58)
- Conflation:3,371 个 OSM POI 并入 Overture 记录,10,201 个独立入库 → places 共 108,373
- 多语言名:km 2,864 / en 3,819 / zh 162;营业时间 1,409

## 与 Google 协议的已知差异

- 缺 `X-Goog-FieldMask` 时返回全量字段(Google 会报错)
- `regularOpeningHours.weekdayDescriptions` 为 OSM `opening_hours` 原文,未解析成 periods
- autocomplete 的 `text` 无 `matches` 高亮偏移
- 罗马音检索仅覆盖主名本身为拉丁字的地点(柬埔寨商户多数如此);纯高棉文名的
  Khmer→Latin 转写 ICU 不支持,M2 计划引入别名表/ETL 期转写
- 鉴权/配额在 M5 落地,当前无鉴权
- geocode 结果中来自 Nominatim 的 `place_id` 形如 `nominatim:way:123`,不能用于
  `/v1/places/{id}` 详情(两套数据域);OpenSearch 来源的结果可以
- legacy Geocoding 形态没有 attribution 字段;数据署名义务由本 README 许可说明承担
- Directions 为静态 ETA(无实时路况);`vehicleProfile:"tuktuk"` 为协议扩展
  (motor_scooter + top_speed 40 + use_highways 0.1,定参见 scripts/tuktuk_calibration.sh)
- polyline 为 Google 标准 precision 1e-5(已从 Valhalla 1e-6 转码)
- computeRoutes 响应暂不含逐向指令(maneuvers);languageCode 已透传 Valhalla 备用。
  后续暴露指令时的现状:Valhalla 3.7 无 km/zh locale——动词回退英文,
  高棉文路名(OSM name:km)正常呈现;zh 完全回退英文
- computeRouteMatrix 一次性返回完整 JSON 数组(Google 为流式),且元素上限远超
  Google(625):单侧 ≤1000(100 万元素);>2,500 元素走 OSRM,小矩阵走 Valhalla,
  OSRM 故障自动降级 Valhalla 分块(变慢但可用)
- 阈值两侧 duration 建模不同(实测同 OD 同路线,Valhalla 含转弯/路口惩罚,
  时长约为 OSRM 自由流的 ~2 倍;distance 两侧一致)——跨阈值对比时长需留意,
  速度模型校准记 M5

## 数据更新管道

### 一键更新

```bash
make update-all   # 等价于 bash scripts/update_pipeline.sh
```

管道按序执行六步:

1. 刷新 OSM PBF(Geofabrik 柬埔寨每日更新)
2. 记录 Postgres 基线计数
3. ETL 链:overture-download → osm-extract → load → conflate
4. 漂移闸:行数变化 >±15% 则中止,防数据质量事故
5. OpenSearch 索引重建 + alias 原子切换(蓝绿无停机)
6. OSRM 三图重建 + 逐个滚动重启;Valhalla 清瓦片重建

OSRM restart 窗口内 matrix 请求会自动降级 Valhalla——属预期行为。

### Cron 示例

```cron
# 每周日 02:00 全量更新(柬埔寨 ~20-40 分钟)
0 2 * * 0  cd /home/daniel/places && bash scripts/update_pipeline.sh >> logs/update.log 2>&1
```

### 漂移闸说明

步骤 4 对比 ETL 前后的 `places` 表行数。若变化幅度超过 ±15%,脚本以非零状态退出,
终止后续索引与图重建——防止上游数据源异常(bbox 变化/schema 漂移)静默污染生产索引。
正常 OSM 日更导致的微小变化(通常 <1%)可通过闸门。

### Nominatim 增量复制取舍

M5 选择**全量周更**:每次管道下载完整 PBF 并由 `mediagis/nominatim` 镜像一次性重导。
优点:管道无状态、回滚只需换 PBF、单节点柬埔寨导入耗时可接受(<15 分钟)。

如需**增量复制**(实时跟 OSM 变化),可在 `docker-compose.yml` 中为 `nominatim` 服务
添加 `REPLICATION_URL=https://download.geofabrik.de/asia/cambodia-updates/` 及
`NOMINATIM_REPLICATION_*` 相关 env,并启动常驻 `nominatim-update` 容器。
适合高更新频率场景,但会引入持久状态,M5 不默认开启。

## 监控

### 指标

网关暴露四类 Prometheus 指标,路径 `http://localhost:8080/metrics`：

| 指标 | 类型 | 说明 |
|------|------|------|
| `gateway_http_requests_total{path,status}` | Counter | 每条路径×状态码的请求计数 |
| `gateway_http_request_duration_seconds{path}` | Histogram | 请求延迟(桶:10ms–30s) |
| `gateway_backend_errors_total{backend}` | Counter | 上游后端(nominatim/opensearch/osrm/valhalla)失败计数 |
| `gateway_zero_results_total{path}` | Counter | 返回空结果的请求数(spec §9 数据质量哨兵) |

### Prometheus UI

启动后访问 `http://127.0.0.1:9090`（仅本机可达）。

### 告警规则

规则文件 `deploy/prometheus/rules.yml`，共四条：

| 告警 | 条件 | 等级 |
|------|------|------|
| `GatewayHighErrorRate` | 5xx 比例 >5% 持续 5 分钟 | critical |
| `BackendErrorsSpiking` | 上游错误率 >0.5/s 持续 5 分钟 | warning |
| `ZeroResultsSurge` | ZERO_RESULTS 比例 >30% 持续 15 分钟 | warning |
| `GatewayP99High` | P99 延迟 >10s 持续 10 分钟 | warning |

通知渠道由 Alertmanager 接入，M5 仅交付规则与 `ALERTS` 序列，不含 Alertmanager 配置。

**分母过滤说明：** 比例类规则(`GatewayHighErrorRate`、`ZeroResultsSurge`)的分母
过滤掉 `/metrics`、`/healthz`、`_unmatched`（路由未命中路径），避免低流量或健康检查
流量导致噪音误报；分母同时用 `clamp_min(..., 0.001)` 防除零。

## Matrix 基准(2026-06-11,8C/32GB 单机)

- 500×500(25 万元素,OSRM):car 3.74s / moto 4.25s / tuktuk 4.42s(验收线 <10s,均 PASS)
- 60×60 经 OSRM:0.29s;经 Valhalla 降级(osrm-car 停机实测):5.29s——变慢但可用
- 阈值:>2,500 元素走 OSRM;≤2,500 走 Valhalla(单次上限 50×50,超出自动分块)
- 降级:OSRM 故障自动回退 Valhalla 分块(网关日志 "falling back");500×500 兜底
  外推约 ~100 秒(串行分块,并发优化记 M5)
