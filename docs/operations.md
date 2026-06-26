# 运维手册

本文档覆盖健康检查、日志、监控、备份恢复、数据更新和常见故障处理。

## 健康检查

Gateway：

```bash
curl -s http://127.0.0.1:8080/healthz
```

Nominatim：

```bash
curl -s http://127.0.0.1:8081/status
```

Valhalla：

```bash
curl -s http://127.0.0.1:8002/status
```

Compose 状态：

```bash
docker compose ps
```

Cloudflare Tunnel：

```bash
cloudflared tunnel list
cloudflared tunnel info limix-local
```

## 日志

Docker 服务日志：

```bash
docker compose logs -f gateway
docker compose logs -f opensearch
docker compose logs -f nominatim
docker compose logs -f valhalla
docker compose logs -f osrm-car
```

Cloudflare Tunnel 日志：

```bash
tail -f /private/tmp/limix-logs/cloudflared-limix-local.err.log
```

## 监控指标

Gateway 暴露：

```bash
curl -s http://127.0.0.1:8080/metrics
```

主要指标：

| 指标 | 类型 | 说明 |
| --- | --- | --- |
| `gateway_http_requests_total{path,status}` | Counter | 请求计数 |
| `gateway_http_request_duration_seconds{path}` | Histogram | 请求延迟 |
| `gateway_backend_errors_total{backend}` | Counter | 后端错误计数 |
| `gateway_zero_results_total{path}` | Counter | 空结果计数 |

Prometheus UI：

```text
http://127.0.0.1:9090
```

告警规则：

```text
deploy/prometheus/rules.yml
```

## 备份

执行备份：

```bash
bash scripts/backup.sh
```

备份内容：

- PostGIS `pg_dump -Fc`
- OpenSearch filesystem snapshot
- OSRM / Valhalla 图构件 tar 包

输出目录：

```text
backups/<timestamp>
```

## 恢复

```bash
bash scripts/restore.sh backups/<timestamp>
```

注意：

- 恢复图构件可能需要 `sudo`。
- PostGIS 与 OpenSearch 快照不是严格同一时刻，建议低写入窗口执行备份。
- 高一致性要求场景应停写后备份。

## 数据更新

执行：

```bash
make update-all
```

该命令会运行：

```bash
bash scripts/update_pipeline.sh
```

更新内容：

1. 刷新 OSM PBF。
2. 执行 ETL。
3. 执行行数漂移检查。
4. 重建 OpenSearch 索引并切换 alias。
5. 停止并重建 OSRM 图。
6. 重建 Valhalla tiles。

## 常见故障

### 公网返回 530

含义：Cloudflare 找不到可用 tunnel connector，通常不是 Gateway 业务错误。

检查：

```bash
cloudflared tunnel list
cloudflared tunnel info limix-local
launchctl print gui/$(id -u)/net.wildog.cloudflared.limix-local
```

修复方向：

- 确认 `limix-local` 有活跃连接。
- 确认 `launchd` 服务处于 running。
- 查看 cloudflared 日志。
- 确认 `~/.cloudflared/config.yml` 中 `map.wildog.net` 指向 `http://127.0.0.1:8080`。

### 公网返回 502

可能原因：

- Tunnel 正常，但 Gateway 不可达。
- Gateway 没启动。
- `127.0.0.1:8080` 没有监听。

检查：

```bash
curl -s http://127.0.0.1:8080/healthz
docker compose ps gateway
docker compose logs --tail=100 gateway
```

### 搜索无结果

检查 OpenSearch alias：

```bash
curl -s http://127.0.0.1:9200/_cat/aliases/places?v
curl -s http://127.0.0.1:9200/places/_count
```

如果索引为空，重新执行：

```bash
make etl-index
```

### Geocoding 异常

检查 Nominatim：

```bash
curl -s http://127.0.0.1:8081/status
docker compose logs --tail=100 nominatim
```

首次导入期间接口可能未就绪。

### Matrix 慢或失败

检查 OSRM：

```bash
docker compose ps osrm-car osrm-moto osrm-tuktuk
docker compose logs --tail=100 osrm-car
```

OSRM 故障时 Gateway 会降级 Valhalla，能用但会慢。

### OpenSearch 启动失败

Linux 主机检查：

```bash
sysctl vm.max_map_count
```

需要至少：

```text
262144
```

## 部署后 smoke test

本地：

```bash
BASE_URL=http://127.0.0.1:8080 scripts/curl_public_smoke.sh
```

公网：

```bash
BASE_URL=https://map.wildog.net scripts/curl_public_smoke.sh
```
