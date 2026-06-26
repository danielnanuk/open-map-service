# API 文档

本文档说明 Gateway 对外暴露的 HTTP API。接口形态尽量兼容 Google Places、Geocoding 和 Routes API，但不是逐字段完整实现。

默认本地地址：

```bash
BASE_URL=http://localhost:8080
```

公网地址：

```bash
BASE_URL=https://map.wildog.net
```

如果开启了 API Key 鉴权，请在请求中加入：

```bash
-H "X-Goog-Api-Key: $API_KEY"
```

## 通用响应和错误

- 成功响应通常为 JSON。
- `/healthz` 返回纯文本 `ok`。
- 鉴权开启后，无效或缺失 API Key 会返回 Google 风格错误。
- 配额耗尽时返回 `RESOURCE_EXHAUSTED`。

## 文本搜索

```http
POST /v1/places:searchText
```

示例：

```bash
curl -s -X POST "$BASE_URL/v1/places:searchText" \
  -H 'Content-Type: application/json' \
  -H 'X-Goog-FieldMask: places.id,places.displayName,places.formattedAddress' \
  -d '{"textQuery":"Angkor Wat","languageCode":"en"}'
```

常用字段：

| 字段 | 说明 |
| --- | --- |
| `textQuery` | 搜索文本 |
| `languageCode` | 返回名称语言偏好 |
| `locationBias.circle.center` | 搜索位置偏置 |
| `locationBias.circle.radius` | 搜索半径偏置 |

## 附近搜索

```http
POST /v1/places:searchNearby
```

示例：

```bash
curl -s -X POST "$BASE_URL/v1/places:searchNearby" \
  -H 'Content-Type: application/json' \
  -d '{"locationRestriction":{"circle":{"center":{"latitude":11.5621,"longitude":104.9160},"radius":2000}},"includedTypes":["restaurant"],"languageCode":"en"}'
```

## 自动补全

```http
POST /v1/places:autocomplete
```

示例：

```bash
curl -s -X POST "$BASE_URL/v1/places:autocomplete" \
  -H 'Content-Type: application/json' \
  -d '{"input":"angk","languageCode":"en"}'
```

## 地点详情

```http
GET /v1/places/{id}
```

示例：

```bash
curl -s "$BASE_URL/v1/places/ef6a3124-362f-47dc-9d23-d6153f05f526?languageCode=en"
```

注意：来自 Nominatim 的 `place_id`，例如 `nominatim:way:908673330`，不能用于该接口。

## 正向地理编码

```http
GET /maps/api/geocode/json?address=...
```

示例：

```bash
curl -s "$BASE_URL/maps/api/geocode/json?address=Street+271,+Phnom+Penh&language=en"
```

## 逆向地理编码

```http
GET /maps/api/geocode/json?latlng=...
```

示例：

```bash
curl -s "$BASE_URL/maps/api/geocode/json?latlng=11.5621,104.9160&language=en"
```

## 路线规划

```http
POST /directions/v2:computeRoutes
```

示例：

```bash
curl -s -X POST "$BASE_URL/directions/v2:computeRoutes" \
  -H 'Content-Type: application/json' \
  -d '{"origin":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}},"destination":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}},"travelMode":"TWO_WHEELER","vehicleProfile":"tuktuk"}'
```

支持的主要模式：

| `travelMode` | 说明 |
| --- | --- |
| `DRIVE` | 驾车 |
| `TWO_WHEELER` | 两轮车 |
| `WALK` | 步行 |

扩展字段：

| 字段 | 说明 |
| --- | --- |
| `vehicleProfile:"tuktuk"` | 嘟嘟车 profile，基于两轮车语义扩展 |

## 距离矩阵

```http
POST /distanceMatrix/v2:computeRouteMatrix
```

示例：

```bash
curl -s -X POST "$BASE_URL/distanceMatrix/v2:computeRouteMatrix" \
  -H 'Content-Type: application/json' \
  -d '{"origins":[{"waypoint":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}}},{"waypoint":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}}}],"destinations":[{"waypoint":{"location":{"latLng":{"latitude":11.5984,"longitude":104.9192}}}},{"waypoint":{"location":{"latLng":{"latitude":11.5625,"longitude":104.9311}}}}],"travelMode":"TWO_WHEELER","vehicleProfile":"tuktuk"}'
```

响应为 JSON 数组，每个元素包含：

| 字段 | 说明 |
| --- | --- |
| `originIndex` | 起点下标 |
| `destinationIndex` | 终点下标 |
| `distanceMeters` | 距离，单位米 |
| `duration` | 时间，例如 `258s` |
| `condition` | `ROUTE_EXISTS` 等状态 |

## 健康检查与指标

```bash
curl -s "$BASE_URL/healthz"
curl -s "$BASE_URL/metrics"
```

## 与 Google API 的已知差异

- 缺少 `X-Goog-FieldMask` 时，本服务返回默认字段；Google 部分接口会报错。
- `regularOpeningHours.weekdayDescriptions` 直接使用 OSM `opening_hours` 原文。
- autocomplete 暂不提供 `matches` 高亮偏移。
- Geocoding 返回的 Nominatim ID 不等于 Places ID。
- Routes 暂不返回逐向导航 instructions。
- Directions 使用静态 ETA，不包含实时路况。
- Distance Matrix 一次性返回完整 JSON 数组，Google API 通常是流式响应。
- `vehicleProfile:"tuktuk"` 是本项目扩展字段。
