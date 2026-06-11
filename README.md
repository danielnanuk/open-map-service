# open-map-service

柬埔寨自托管 Places + Routes API(Google 协议兼容)。设计文档见
`docs/superpowers/specs/2026-06-10-places-api-design.md`。

## M1 快速开始

```bash
make up            # postgis + opensearch
make migrate       # 建表
make py-setup      # python venv
make etl-all       # overture 下载→转换→osm 抽取→入库→conflation→索引(首次约 10-30 分钟)
make up-all        # 启动 gateway
make golden        # 黄金查询集验收
```

首次启动 Nominatim 会执行一次性导入(柬埔寨 ~5-15 分钟,`docker logs places-nominatim-1` 看进度,
就绪标志 `curl localhost:8081/status`)。

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
