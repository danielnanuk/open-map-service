# 柬埔寨自托管 Places + Routes API — 设计文档

- 日期:2026-06-10
- 状态:已获用户认可,待实现规划
- 范围:Google Places API + Routes API(Directions / Distance Matrix)的开源自托管替代,仅覆盖柬埔寨

## 1. 目标与约束

| 维度 | 决策 |
|---|---|
| 能力范围 | POI 搜索(Text/Nearby)、自动补全、地点详情、正/逆地理编码、Directions、Distance Matrix |
| 覆盖区域 | 柬埔寨全境 |
| 部署 | 生产级自托管,单机 docker-compose 起步 |
| API 形态 | Google 协议兼容(Places API New + Routes API 请求/响应格式),客户端改 base URL 即可迁移 |
| 查询语言 | 高棉语、英语、中文 |
| 载具 profile | 摩托(moto)、汽车、嘟嘟车(tuk-tuk);步行/自行车随 Valhalla 自带 |
| Matrix 规模 | 单次几百×几百以上(车队优化级),需专用矩阵引擎 |
| ETA | 静态 ETA(无实时路况);架构预留自有 GPS 轨迹做历史速度校准的接入点 |

### 非目标(明确不做)

- 评论、评分、照片等 UGC 数据(开源数据源不含,客户端如依赖这些字段需自行处理空值)
- 实时路况与 traffic-aware ETA
- 公共交通(transit)路由
- 柬埔寨以外的覆盖
- 把老版 Google Directions/DistanceMatrix 协议做完整兼容(仅做新版 Routes API;老版格式留兼容开关作为后续可选项)

## 2. 选型结论与理由

### 数据源
- **Overture Maps places/addresses/divisions**(月度发版):POI 主数据源。柬埔寨商户大量只存在于 Facebook,Overture 合并了 Meta/Microsoft/Foursquare 数据,对柬埔寨的 POI 覆盖显著优于纯 OSM——这是整个选型的最大依据。
- **OSM(Geofabrik cambodia,每日增量)**:路网(路由引擎唯一图源)、行政区划、门牌,以及 `opening_hours` 等 Overture 缺失的 POI 字段。

### 引擎
- **OpenSearch**:POI 搜索与自动补全。选它而非 Meilisearch/Typesense 的原因:ICU 插件自带高棉语词典断词(高棉文无词间空格,这是硬性要求)。
- **Nominatim 5.x**:正/逆地理编码。柬埔寨库仅 ~10-20GB,成本低,地址解析质量不必自己重造。
- **Valhalla**:Directions 主引擎。运行时动态 costing,一张图服务全部 profile,内置 `motor_scooter`/`motorcycle`/`low_speed_vehicle`,正好映射 moto 与 tuk-tuk,改参数不需重建图。
- **OSRM × 3 实例**:专扛大矩阵(`/table`)。OSRM 在超大矩阵上的性能是数量级优势;代价是 profile 烘焙进预处理、一 profile 一实例,柬埔寨图极小所以可接受。
- **API 网关:Go 自研**:唯一自写组件,纯协议翻译 + 转发 + 鉴权,Go 静态二进制、高并发、低内存。

### 被否方案(记录备查)
- Nominatim + Photon 纯 OSM 栈:最快上线,但柬埔寨 OSM POI 覆盖不足,Overture 数据无法并入。
- Pelias:一站式但 6-8 个服务运维重、钉在 ES7、维护放缓,接 Overture 仍需自写 importer。
- GraphHopper / openrouteservice(路由):前者 OSS 版无高性能 Matrix 端点,后者无摩托 profile 且每 profile 单独建图。

## 3. 架构

```
                        ┌──────────────────────────────┐
  客户端(Google 协议) →  │  API 网关 (Go)                │
                        │  鉴权/配额/FieldMask/错误码映射  │
                        └──┬──────┬──────┬──────┬───────┘
                           ▼      ▼      ▼      ▼
                    OpenSearch Nominatim Valhalla OSRM×3
                    搜索/补全   正逆编码   导航+小矩阵 大矩阵
                           ▲      ▲      ▲      ▲
        ┌──────────────────┴──────┴──────┴──────┴──────────────┐
        │ ETL:Overture(月) + OSM(日) → PostGIS 权威库(合并去重)    │
        │     → OpenSearch 索引重建 / Nominatim 增量 / 路由图重建   │
        └──────────────────────────────────────────────────────┘
```

## 4. 数据层(PostGIS 权威库)

- 抽取:`overturemaps` CLI 按柬埔寨 bbox 抽 places/addresses/divisions;`osm2pgsql` 导入 OSM POI。
- **合并去重(conflation)规则**:
  - Overture **GERS ID 作为对外 `place_id`**(稳定主键,对标 Google place_id);
  - OSM POI 与 Overture 记录按"名称模糊匹配(含多语言名)+ 距离 < 50m + 类目相容"判定同一实体,合并后 OSM 独有字段(`opening_hours`、`phone` 等)补入;
  - 未匹配的 OSM 独有 POI 以 `osm:<type>:<id>` 派生稳定 ID 入库;
  - 同一实体保留来源追溯字段(`sources[]`),便于排查与 license 归属。
- 多语言名称:`name`(默认)、`name_km`、`name_en`、`name_zh`,取自 Overture names 结构与 OSM `name:*` 标签。
- 详情字段对齐 Google Place Details 常用子集:displayName、formattedAddress、location、types(类目映射表:Overture/OSM 类目 → Google place types)、internationalPhoneNumber、websiteUri、regularOpeningHours、businessStatus。营业时间覆盖率天然低于 Google,响应中字段缺失即省略(与 Google 行为一致)。

## 5. 检索层(OpenSearch)

- 单节点起步 + 定期快照;索引经 alias 暴露,重建后原子切换,零停机。
- 分析器:
  - 高棉语:`analysis-icu` ICU 词典断词;另用 ICU `km-Latn` 转写生成罗马音子字段,使英文键盘用户可搜高棉文独有名称;
  - 中文:`analysis-smartcn`;英语:standard。
- 自动补全:各语言名称字段建 edge n-gram 子字段;目标 P95 < 100ms。
- 排序:文本相关性 × 地理距离衰减(Nearby/有 locationBias 时)× Overture confidence × 类目权重。

## 6. 地理编码(Nominatim)

- Nominatim 5.x 容器,柬埔寨 PBF + Geofabrik 每日增量复制。
- **正向分流规则(网关实现)**:查询含门牌/道路/行政区模式(数字+街道词、村/区/省名)→ Nominatim;其余自由文本 → OpenSearch,零结果时 fallback Nominatim。
- 逆向:Nominatim 为主,叠加 PostGIS 最近 POI 查询,组装 Google 风格多结果 `results[]`。

## 7. 路由层

- **Valhalla**(Directions 全 profile + 小矩阵):
  - `travelMode` 映射:`DRIVE` → `auto`;`TWO_WHEELER` → `motor_scooter`(柬埔寨 moto 调参:允许干道、城区限速);嘟嘟车 → `low_speed_vehicle` 或降速 `motor_scooter` costing(两者择优,实现期用真实路线校验定参);`WALK`/`BICYCLE` 自带。
  - 嘟嘟车是 Google 协议外模式,以扩展字段表达:`travelMode: TWO_WHEELER` + `vehicleProfile: "tuktuk"`;未用嘟嘟车的客户端零改动。
  - 静态 ETA:按道路等级调默认速度;预留 Valhalla 外挂历史速度文件的接入点(后期接自有 GPS 轨迹,不动架构)。
- **OSRM × 3**(car.lua / moto.lua / tuktuk.lua,tuktuk 为 moto 降速变体):专用 `/table` 大矩阵。
- **矩阵分流阈值**:起点数×终点数 > 2,500(约 50×50)走 OSRM,否则走 Valhalla;OSRM 不可用时自动降级 Valhalla 分块计算(可用性优先,延迟容忍)。

## 8. API 网关(Go)

| Google 端点 | 后端 |
|---|---|
| `POST /v1/places:searchText` | OpenSearch |
| `POST /v1/places:searchNearby` | OpenSearch |
| `POST /v1/places:autocomplete` | OpenSearch |
| `GET /v1/places/{place_id}` | PostGIS |
| `GET /maps/api/geocode/json`(正/逆) | Nominatim(+OpenSearch 分流) |
| `POST /directions/v2:computeRoutes` | Valhalla |
| `POST /directions/v2:computeRouteMatrix` | OSRM / Valhalla(按阈值分流) |

- 鉴权:API key(header 或 query param,行为对齐 Google);按 key 配额与限流(令牌桶,配置存 Postgres)。
- 支持 `X-Goog-FieldMask` 字段掩码(实现 Google 语义的字段裁剪)。
- 错误码映射为 Google 风格:`ZERO_RESULTS` / `INVALID_ARGUMENT` / `RESOURCE_EXHAUSTED` / `PERMISSION_DENIED` 等。
- 响应附 attribution 字段(见 §11)。

## 9. 更新管道与可观测性

- OSM:每日增量 → Nominatim 复制;每周从最新 PBF 重建 Valhalla tiles 与 OSRM 三套图,蓝绿切换(离线建图 → 滚动替换容器)。
- Overture:月度发版后重抽 bbox,按 GERS ID upsert 入 PostGIS,触发 OpenSearch 索引重建 + alias 切换。
- 监控:网关导出 Prometheus 指标(QPS、P95/P99 延迟、各后端错误率、**ZERO_RESULTS 率**——数据质量哨兵);各服务健康检查接入 compose healthcheck。
- 告警:数据量漂移(两次发版 POI 总数变化超阈值)、ETL 失败、磁盘水位。

## 10. 测试策略

- **黄金查询集**:人工维护的柬埔寨真实查询(高棉文/英文/中文地名、地标、连锁品牌、易混淆名称),断言 top-N 包含期望 place_id;每次数据更新后回归。搜索质量的生命线,随产品反馈持续扩充。
- **协议契约测试**:以 Google 真实响应录制 fixture,对兼容层做字段结构/类型校验。
- **路由理智测试**:金边→暹粒、金边市内典型 OD 的距离/时长区间断言;moto 与 car 的路线差异断言(moto 可走小路/窄巷)。
- **矩阵基准**:500×500 三 profile 延迟基准,纳入 CI 趋势跟踪。
- **ETL 测试**:幂等性(重跑结果一致)、conflation 规则单测(合并/不合并的边界用例)。

## 11. 许可合规

- OSM 衍生部分(Nominatim 库、路由图、合并入库的 OSM 字段)遵 **ODbL**:对外署名 "© OpenStreetMap contributors";合并库视为 ODbL 衍生数据库管理。
- Overture places:**CDLA-Permissive 2.0**,其中 Foursquare 来源数据 **Apache 2.0**,商用宽松。
- 网关响应统一携带 attribution 字段;面向最终用户的产品界面需展示署名。

## 12. 部署与硬件

- 单机 docker-compose:gateway、OpenSearch、PostGIS、Nominatim、Valhalla、OSRM×3、ETL(cron 容器)。
- 硬件基线:8 核 / 32GB / 250GB SSD。
- 备份:Postgres 逻辑备份 + OpenSearch 快照 + 路由图构件,上传对象存储;恢复演练纳入 M5。

## 13. 里程碑

| 里程碑 | 内容 | 验收 |
|---|---|---|
| M1 | ETL 管道 + PostGIS 权威库 + OpenSearch;searchText/searchNearby/autocomplete/details 四端点 | 黄金查询集首版通过;三语言搜索可用 |
| M2 | Nominatim 接入,geocode 正/逆 + 分流 | 地址/行政区查询样本通过 |
| M3 | Valhalla 接入,computeRoutes(moto/car/tuk-tuk/步行/骑行) | 路由理智测试通过;tuk-tuk costing 定参 |
| M4 | OSRM 三实例 + computeRouteMatrix 分流 | 500×500 基准达标;降级路径验证 |
| M5 | 生产加固:鉴权配额、监控告警、蓝绿更新管道、备份恢复演练 | 全链路演练通过 |

## 14. 实现期需验证的假设(每项有明确验证动作)

1. **Overture 柬埔寨 POI 实际量与质量**:M1 第一步抽数后统计总量/类目分布/多语言名称覆盖率,与 OSM 对比;若低于预期,调高 OSM 权重并评估补充源。
2. **Valhalla locale 是否含 km/zh 播报文案**:M3 检查上游 locale 列表,缺失则补 locale JSON(一次性工作,不阻塞 M3 验收,可后置)。
3. **tuk-tuk profile 取 `low_speed_vehicle` 还是降速 `motor_scooter`**:M3 用 ≥20 条金边真实路线对比择优。
4. **ICU `km-Latn` 转写的检索效果**:M1 黄金查询集中加入罗马音查询用例验证;不达标则引入人工别名表。
5. **OpenSearch 单节点在目标 QPS 下的余量**:M5 压测;不足则加副本节点(架构已支持)。
