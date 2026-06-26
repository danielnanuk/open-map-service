# 部署文档

本文档说明 open-map-service 的部署方式。当前推荐路径是 Docker Compose 单机部署，公网入口通过 Cloudflare Tunnel 暴露。

## 部署形态

| 形态 | 用途 |
| --- | --- |
| Docker Compose | 本地开发、单机部署、当前 `map.wildog.net` 运行方式 |
| Cloudflare Tunnel | 将本机或内网 Gateway 暴露到公网域名 |
| Kubernetes | 集群化部署入口，manifests 位于 `deploy/k8s` |

## Docker Compose 单机部署

准备数据和图：

```bash
make py-setup
make etl-osm
make osrm-build
```

启动依赖：

```bash
make up
make migrate
make etl-all
make up-all
```

验证：

```bash
curl -s http://127.0.0.1:8080/healthz
docker compose ps
```

## 端口

| 服务 | 本机端口 | 说明 |
| --- | --- | --- |
| Gateway | `8080` | 对外 API |
| PostGIS | `5432` | 默认仅本机 |
| OpenSearch | `9200` | 默认仅本机 |
| Nominatim | `8081` | 默认仅本机 |
| Valhalla | `8002` | 默认仅本机 |
| OSRM car | `5000` | 默认仅本机 |
| OSRM moto | `5001` | 默认仅本机 |
| OSRM tuktuk | `5002` | 默认仅本机 |
| Prometheus | `9090` | 默认仅本机 |

## Cloudflare Tunnel

当前 `map.wildog.net` 的链路：

```mermaid
flowchart LR
    Client["公网客户端"] --> Edge["Cloudflare Edge"]
    Edge --> Tunnel["cloudflared limix-local"]
    Tunnel --> Local["127.0.0.1:8080"]
    Local --> Gateway["open-map-service gateway"]
```

Cloudflare 配置文件：

```text
~/.cloudflared/config.yml
```

关键 ingress：

```yaml
ingress:
  - hostname: map.wildog.net
    service: http://127.0.0.1:8080
```

校验配置：

```bash
cloudflared tunnel --config ~/.cloudflared/config.yml ingress validate
cloudflared tunnel ingress rule https://map.wildog.net/healthz --config ~/.cloudflared/config.yml
```

注册 DNS 路由：

```bash
cloudflared tunnel route dns limix-local map.wildog.net
```

## cloudflared 常驻服务

macOS 上使用 `launchd` 保持 tunnel 常驻：

```text
~/Library/LaunchAgents/net.wildog.cloudflared.limix-local.plist
```

查看状态：

```bash
launchctl print gui/$(id -u)/net.wildog.cloudflared.limix-local
cloudflared tunnel list
cloudflared tunnel info limix-local
```

日志：

```bash
tail -f /private/tmp/limix-logs/cloudflared-limix-local.err.log
```

如果公网返回 530，优先检查：

```bash
cloudflared tunnel list
```

`limix-local` 必须显示活跃连接，例如 `1xsin13, 1xsin18`。

## 公网验证

```bash
BASE_URL=https://map.wildog.net scripts/curl_public_smoke.sh
```

如果本机网络使用 WARP、fake-ip 或特殊 DNS，直接 curl 公网域名可能被本地 `198.18.x.x` 路径影响。此时应从手机网络、外部服务器或未启用该网络路径的环境验证。

## 生产配置

建议生产环境开启鉴权：

```bash
AUTH_ENABLED=true docker compose up -d --build gateway
```

生成 API Key：

```bash
make gen-api-key NAME=production-client
```

如果暴露到公网，还建议：

- 接入 Cloudflare Access 或 API Shield。
- 限制非 Gateway 服务端口只监听本机或内网。
- 备份 PostGIS、OpenSearch snapshot 和路由图构件。
- 保留 `scripts/curl_public_smoke.sh` 作为部署后 smoke test。

## Kubernetes 部署入口

Kubernetes manifests 位于：

```text
deploy/k8s
```

主要文件：

| 文件 | 说明 |
| --- | --- |
| `00-namespace.yaml` | namespace |
| `01-secrets.example.yaml` | 示例 Secret |
| `02-postgis.yaml` | PostGIS |
| `03-opensearch.yaml` | OpenSearch |
| `04-nominatim.yaml` | Nominatim |
| `05-valhalla.yaml` | Valhalla |
| `06-osrm.yaml` | OSRM |
| `07-gateway.yaml` | Gateway |
| `08-ingress.yaml` | Ingress |
| `09-jobs.yaml` | 初始化 Job |
| `10-golden.yaml` | golden 验收 Job |
| `11-etl-cronjob.yaml` | ETL 定时任务 |
| `13-backup-cronjob.yaml` | 备份定时任务 |
| `15-monitoring.yaml` | 监控资源 |

生产集群需要替换 registry、storageClass、Secret 和 Ingress host。
