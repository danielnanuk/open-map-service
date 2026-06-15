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

集成测试使用独立 places_test 库与 places_test alias(`make migrate-test` 一次性建库),不影响生产数据。
直接调 `pytest` 时 conftest 自动注入 places_test;若显式设 `DATABASE_URL=...places` 则会打生产库——勿这么做。

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
- 鉴权/配额已实现(API key + 每 key 令牌桶),默认关闭(`AUTH_ENABLED=false`)——
  生产开启见运维手册;错误体对齐 Google(403 PERMISSION_DENIED / 429 RESOURCE_EXHAUSTED)
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
6. OSRM 先停后重建再启(osrm-extract 会原地覆写在线 mmap 的图文件,
   带电重建会让运行实例 SIGSEGV——实测教训);Valhalla 清瓦片重建

OSRM 停机窗口内 matrix 请求会自动降级 Valhalla——属预期行为。

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

## 备份与恢复

### 脚本用法

```bash
# 备份(pg_dump -Fc + OpenSearch 文件系统快照 + 图构件 tar)
bash scripts/backup.sh
# → backups/<ts>/places.dump  (Postgres,~87 MB)
# → backups/<ts>/os_snapshot_name
# → backups/<ts>/graphs.tar.gz (OSRM+Valhalla 图构件,~689 MB)

# 恢复
bash scripts/restore.sh backups/<ts>
```

### Cron 示例

```cron
# 每日 01:30 备份(约 2 分钟;脚本自动保留最新 7 份并同步清理对应 OS 快照)
30 1 * * *  cd /home/daniel/places && bash scripts/backup.sh >> logs/backup.log 2>&1
```

注:pg_dump 与 OpenSearch 快照非同一时刻(秒级偏差);低负载窗口运行时实际一致,
高并发写入场景如需严格一致需停写后备份。restore.sh 覆盖图构件需要 sudo
(cron 场景需配置 NOPASSWD,或接受跳过图恢复——pg/OS 数据不受影响)。

### 对象存储上传(可选)

备份完成后将目录同步到远端:

```bash
# rclone(任意 S3/GCS/B2 兼容)
rclone copy backups/<ts> remote:places-backup/<ts>

# AWS CLI
aws s3 cp --recursive backups/<ts> s3://my-bucket/places-backup/<ts>
```

`backups/` 已加入 `.gitignore`。

### 首次部署:注册快照仓库

OpenSearch 快照仓库需一次性注册(compose 已挂 ossnapshots 卷并设 path.repo)。
新机器上卷由 root 初始化,先修正属主再注册:

```bash
docker exec --user root places-opensearch-1 chown opensearch:opensearch /snapshots
curl -s -X PUT 'localhost:9200/_snapshot/local' -H 'Content-Type: application/json' \
  -d '{"type":"fs","settings":{"location":"/snapshots"}}'
```

### 演练记录(2026-06-11)

**备份大小:**
- `places.dump` (Postgres pg_dump -Fc): 87 MB
- `graphs.tar.gz` (OSRM×3 + Valhalla 瓦片): 689 MB
- `os_snapshot_name` (OpenSearch 快照名引用): <1 KB
- OpenSearch 快照数据保存在 Docker volume `ossnapshots`

**破坏操作:**
```
DROP TABLE places CASCADE  → PostgreSQL: relation "places" does not exist ✓
DELETE places-* indices    → OpenSearch: index_not_found_exception ✓
```

**恢复耗时:** ~43 秒(主要为 graphs.tar.gz 解包;pg + OS 各约 5-10 秒)

**验证结果:**
- PostgreSQL: `SELECT count(*) FROM places` → **108373** ✓
- OpenSearch: `GET /places/_count` → **108373** ✓
- `make golden`: **18/18 PASS** ✓
- `make test-go`: **全部通过** ✓

**注:** Valhalla 图瓦片由 root 用户(容器内)写入,tar 解压需 sudo 权限;
脚本已处理(`sudo tar ... || echo "skipped"`)。pg 与 OS 是关键数据路径,图构件在磁盘完整时跳过覆盖无影响。

## Matrix 基准(2026-06-11,8C/32GB 单机)

- 500×500(25 万元素,OSRM):car 3.74s / moto 4.25s / tuktuk 4.42s(验收线 <10s,均 PASS)
- 60×60 经 OSRM:0.29s;经 Valhalla 降级(osrm-car 停机实测):5.29s——变慢但可用
- 阈值:>2,500 元素走 OSRM;≤2,500 走 Valhalla(单次上限 50×50,超出自动分块)
- 降级:OSRM 故障自动回退 Valhalla 分块(网关日志 "falling back");500×500 兜底
  外推约 ~100 秒(串行分块,并发优化记 M5)

## 压测数字(§14.5,2026-06-11,8C/32GB 单机)

`python3 scripts/load_test.py`(20 并发 × 1000 请求,8 个混合语言 searchText 轮转):

| 指标 | 值 |
|------|----|
| QPS | **304** |
| P50 | **47 ms** |
| P95 | **131 ms** |
| P99 | **605 ms** |
| errors | 0 |

**单节点余量判断(spec §14.5):** 304 QPS @ P95 131ms,单机 8C/32GB 有较大余量。
P99 605ms 因 OpenSearch 偶发 GC/flush 抖动,不影响中位数体验。
推荐生产限流 200 QPS/key(rpm_limit=12000),留 50% 余量给路由与矩阵流量。

注:以上为暖缓存空闲机数字;终审在容器刚重启、缓存未热的并发场景下复测得
119 QPS / P95 595ms(P50 一致 42ms)——冷启动后给系统 1-2 分钟热身再评估容量。

## 运维手册(生产部署 Checklist)

### 部署前

1. **强口令** — 复制 `.env.example` 为 `.env`，设置 `POSTGRES_PASSWORD` 与 `NOMINATIM_PASSWORD` 为随机强口令（`openssl rand -hex 24`）。
2. **鉴权开启** — 设置 `AUTH_ENABLED=true`；用 `make gen-api-key NAME=<client>` 发放 key。
3. **内核参数** — `sysctl -w vm.max_map_count=262144`（OpenSearch 必须）并写入 `/etc/sysctl.d/99-places.conf` 持久化。
4. **首次快照仓库注册**（新机器 ossnapshots 卷由 root 初始化，需先修正属主）：
   ```bash
   docker exec --user root places-opensearch-1 chown opensearch:opensearch /snapshots
   curl -s -X PUT 'localhost:9200/_snapshot/local' \
     -H 'Content-Type: application/json' \
     -d '{"type":"fs","settings":{"location":"/snapshots"}}'
   ```

### Cron 两条

```cron
# 数据更新(每周日 02:00,约 20-40 分钟)
0 2 * * 0  cd /home/daniel/places && bash scripts/update_pipeline.sh >> logs/update.log 2>&1

# 备份(每日 01:30,约 2 分钟;保留最新 7 份)
30 1 * * *  cd /home/daniel/places && bash scripts/backup.sh >> logs/backup.log 2>&1
```

### 对象存储同步

备份完成后将 `backups/<ts>/` 同步到远端（参见备份章节）。

### WriteTimeout 120s 上限说明

`http.Server.WriteTimeout` 设为 120s。降级路径上 >500×500 的 Valhalla 兜底约 ~400s（串行分块）
会被该超时掐断，客户端收到连接重置。**建议矩阵请求 ≤500/side**；超过 500 的兜底请求须知：
Valhalla 串行分块路径在单机上不在 SLA 内，响应会被截断。

### 15s 优雅退出 drain 说明

`Shutdown(ctx)` 设 15s drain 窗口。**部署窗口注意**：正在处理的 in-flight 长请求（矩阵/路由）
若超过 15s 将被截断。建议在低流量窗口滚动重启，或在重启前通过负载均衡将流量切走。

## K8s 部署(M6)

### 架构反转:数据即镜像 + 滚动 Deployment

单机模型的根本缺陷在于"原地覆写数据文件"——`osrm-extract` 以 O_TRUNC 改写 `.osrm` 文件时,在线 OSRM 进程 mmap 同一 inode 读到损坏数据,触发 SIGSEGV(exit 139);磁盘事故亦来自同一根因(M5 现场复现)。K8s 形态用**不可变版本化数据制品 + 滚动 Deployment** 从结构上消除这类缺陷:

- 引擎(OSRM×3、Valhalla)以"数据镜像"交付——镜像内已包含预烘焙路由图,容器启动即服务,无需运行期数据构建。
- 数据更新 = 新时间戳 tag 镜像 + 原生 rolling(maxUnavailable=0):旧 Pod 继续服务至新 Pod readiness 通过,任何时刻不存在"原地覆写正在 mmap 的文件"。
- 回滚 = `kubectl rollout undo`;扩容 = `replicas + 1`。OSRM-SIGSEGV / 磁盘事故在此模型下**不复存在**。

Nominatim 唯一例外:其数据是导入后的 PG 数据目录,kaniko 构建期无法跑 `nominatim import`,故走 initContainer + S3 tar 方式;月更 CronJob(`14-nominatim-data-cronjob.yaml`)完成打包与推送。

---

### EDIT-ME 参数表

所有需要按集群定制的值均在对应文件顶部以 `# EDIT-ME` 注释标出。下表汇总全部参数化点:

| 文件 | 参数 | k3d 默认值 | 生产指引 |
|------|------|-----------|---------|
| `01-secrets.example.yaml` / `01-secrets.yaml` | `POSTGRES_PASSWORD` | 示例弱密码 | `openssl rand -hex 24` 生成强密码,不入库 |
| `01-secrets.example.yaml` / `01-secrets.yaml` | `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | `placesminio` / `placesminio` | 替换为真集群已有 S3 凭据 |
| `01-secrets.example.yaml` / `01-secrets.yaml` | `S3_ENDPOINT` | `http://minio.places.svc:9000` | 替换为真集群 S3 端点 |
| `01-secrets.example.yaml` / `01-secrets.yaml` | `registry-cred` | _(k3d 无需鉴权)_ | 真集群按 registry 鉴权创建 docker-config Secret |
| `02-postgis.yaml` | `storageClassName` | `local-path`(k3d 默认) | 改为集群已有 StorageClass(如 `standard`、`gp3`) |
| `03-opensearch.yaml` | `storageClassName` | `local-path` | 同上 |
| `03-opensearch.yaml` | `busybox` init 镜像 | `busybox:1.36.1` | 生产钉 digest(`sha256:...`) |
| `05-valhalla.yaml` | image registry 前缀 | `k3d-places-reg:5500/places` | 替换为集群 registry 前缀 |
| `06-osrm.yaml` | image registry 前缀 | `k3d-places-reg:5500/places` | 替换为集群 registry 前缀 |
| `07-gateway.yaml` | `image` registry 前缀 | `k3d-places-reg:5500/places/gateway:dev` | 替换为集群 registry/tag |
| `07-gateway.yaml` | cpu limit | _(未设,Burstable QoS)_ | 生产可加 `"500m"` 限制单 Pod 最坏 CPU |
| `08-ingress.yaml` | `ingressClassName` | `traefik` | 改为集群 ingress controller(`nginx` 等) |
| `08-ingress.yaml` | `host` | `places.local` | 改为生产域名 |
| `09-jobs.yaml` | bootstrap-data `image` | `k3d-places-reg:5500/places/builder:dev` | 钉固定 digest tag |
| `10-golden.yaml` | `image` registry 前缀 | `k3d-places-reg:5500/places/builder:dev` | 替换为集群 registry 前缀 |
| `11-etl-cronjob.yaml` | `image` registry 前缀 / tag | `k3d-places-reg:5500/places/builder:dev` | 替换为固定 digest tag(如 `builder:202506XXXXXX`) |
| `11-etl-cronjob.yaml` | REGISTRY env | `k3d-places-reg:5000/places` | 替换为集群 registry 前缀 |
| `13-backup-cronjob.yaml` | `image` registry 前缀 | `k3d-places-reg:5500/places/builder:dev` | 钉固定 digest tag |
| `13-backup-cronjob.yaml` | 调度时间 / KEEP | `0 1 * * *` / `7` | 生产可改 `14` 或 `30` |
| `14-nominatim-data-cronjob.yaml` | `PBF_URL` | 柬埔寨 PBF | 真集群调整目标区域 URL |
| `14-nominatim-data-cronjob.yaml` | 两处 `image` | `k3d-places-reg:5500/places/...` | 钉固定 digest(生产) |
| `14-nominatim-data-cronjob.yaml` | emptyDir `sizeLimit` | `8Gi` | 真集群建议改为独立 PVC |
| `99-minio-k3d.yaml` | minio `image` | `minio/minio:RELEASE.2025-04-22T22-12-26Z` | 仅 k3d 用;真集群改用已有 S3,删除此文件 |
| `99-minio-k3d.yaml` | MinIO 凭据 env | 明文 `placesminio` | 仅 k3d 本地验证;真集群必须改用 Secret |

**一键替换 registry 前缀示例**(无需 Kustomize):

```bash
# 将所有 manifest 中的 k3d-places-reg:5500/places 替换为真集群 registry 前缀
OLD="k3d-places-reg:5500/places"
NEW="registry.example.com/places"
grep -rl "$OLD" deploy/k8s/ | xargs sed -i "s|$OLD|$NEW|g"
```

---

### 节点前提(NODE PREREQ)

在目标节点上执行以下操作后再 apply manifests:

```bash
# 1. OpenSearch 必须:vm.max_map_count >= 262144
sudo sysctl -w vm.max_map_count=262144
echo 'vm.max_map_count=262144' | sudo tee /etc/sysctl.d/99-places.conf

# 2. k3d 默认 StorageClass:local-path(已内置,无需额外安装)
#    真集群:确认 storageClassName 存在:kubectl get storageclass

# 3. k3d 默认 ingressClassName:traefik(已内置)
#    真集群:确认 ingressClassName 对应 controller:kubectl get ingressclass

# 4. Registry 地址约定(k3d 本地):
#    宿主机推送:  localhost:5500  (k3d 宿主端口映射)
#    集群内拉取:  k3d-places-reg:5000  (containerd 内部 DNS,manifests 里用此地址)
#    kaniko 构建后推送必须用集群内地址:k3d-places-reg:5000/places/<image>:<tag>
```

**repository-s3 keystore 一次性注入**(OpenSearch S3 快照仓库):

```bash
# 注入 S3 凭据到 OpenSearch keystore(Pod 启动后执行一次)
kubectl -n places exec deploy/opensearch -- bash -c "
  echo 'placesminio' | opensearch-keystore add --stdin s3.client.default.access_key
  echo 'placesminio' | opensearch-keystore add --stdin s3.client.default.secret_key
"
# 重载 secure settings(无需重启)
kubectl -n places exec deploy/opensearch -- curl -s -X POST \
  'http://localhost:9200/_nodes/reload_secure_settings'
# 注册快照仓库
kubectl -n places exec deploy/opensearch -- curl -s -X PUT \
  'http://localhost:9200/_snapshot/s3-backup' \
  -H 'Content-Type: application/json' \
  -d '{"type":"s3","settings":{"bucket":"places","base_path":"opensearch-snapshots"}}'
```

---

### 引导顺序 Runbook

完整引导顺序对应 spec §8。每步用 `kubectl wait` 确认就绪后再继续。

```bash
# ── 步骤 0:前提 ─────────────────────────────────────────────────
# 复制并填写 secrets(不入库)
cp deploy/k8s/01-secrets.example.yaml deploy/k8s/01-secrets.yaml
# 编辑 01-secrets.yaml,填入真实密码和 S3 凭据

# ── 步骤 1:Secrets ──────────────────────────────────────────────
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl apply -f deploy/k8s/01-secrets.yaml

# ── 步骤 2:基座层(PostGIS / OpenSearch / RBAC)────────────────
kubectl apply -f deploy/k8s/02-postgis.yaml
kubectl apply -f deploy/k8s/03-opensearch.yaml
kubectl apply -f deploy/k8s/12-rbac.yaml
kubectl wait pod -l app=postgis    -n places --for=condition=Ready --timeout=120s
kubectl wait pod -l app=opensearch -n places --for=condition=Ready --timeout=180s

# ── 步骤 3:migrate Job ──────────────────────────────────────────
kubectl apply -f deploy/k8s/09-jobs.yaml
kubectl wait job/migrate -n places --for=condition=Complete --timeout=120s

# ── 步骤 4:首次数据引导 Job ──────────────────────────────────────
# bootstrap-data Job 跑 ETL + OpenSearch 索引(首次,跳过漂移闸)
# 预计 20-40 分钟(柬埔寨全量 OSM + Overture)
kubectl wait job/bootstrap-data -n places --for=condition=Complete --timeout=3600s

# ── 步骤 5:引擎层(Nominatim / Valhalla / OSRM)───────────────
kubectl apply -f deploy/k8s/04-nominatim.yaml
kubectl apply -f deploy/k8s/05-valhalla.yaml
kubectl apply -f deploy/k8s/06-osrm.yaml
kubectl wait pod -l app=nominatim -n places --for=condition=Ready --timeout=900s
kubectl wait pod -l app=valhalla  -n places --for=condition=Ready --timeout=300s
kubectl wait pod -l app=osrm-car  -n places --for=condition=Ready --timeout=120s

# ── 步骤 6:Gateway + Ingress ────────────────────────────────────
kubectl apply -f deploy/k8s/07-gateway.yaml
kubectl apply -f deploy/k8s/08-ingress.yaml
kubectl wait pod -l app=gateway -n places --for=condition=Ready --timeout=60s

# ── 步骤 7:验收 ─────────────────────────────────────────────────
kubectl apply -f deploy/k8s/10-golden.yaml
kubectl wait job/golden -n places --for=condition=Complete --timeout=120s
kubectl logs job/golden -n places | tail -5
# 期望输出:18/18 PASS

# ── 可选:CronJob + MinIO(k3d)+ 监控 ──────────────────────────
kubectl apply -f deploy/k8s/11-etl-cronjob.yaml
kubectl apply -f deploy/k8s/13-backup-cronjob.yaml
kubectl apply -f deploy/k8s/14-nominatim-data-cronjob.yaml
kubectl apply -f deploy/k8s/99-minio-k3d.yaml   # 仅 k3d 本地验证
kubectl apply -f deploy/k8s/15-monitoring.yaml  # 需 Prometheus Operator CRD(见监控接法)
```

**本地 k3d 一键验证**:

```bash
# 脚本封装上述完整流程,含 k3d 集群/registry 创建
bash scripts/k3d_up.sh
```

> 重跑 Job:`kubectl -n places delete job <name> && kubectl apply -f deploy/k8s/09-jobs.yaml`

---

### 运维速查

#### 扩容

```bash
# 手动扩容 OSRM(即起即服务,新 Pod 就绪约 8s)
kubectl scale deployment osrm-car -n places --replicas=2

# Gateway HPA(CPU 70% 触发,2-6 副本)——会自动扩缩
kubectl get hpa gateway -n places

# 查看 endpoints 是否已更新
kubectl get endpoints gateway -n places
```

#### 滚动发布数据(周更)

```bash
# 方式 A:手动触发一次周更 CronJob
kubectl create job --from=cronjob/etl-pipeline manual-etl-$(date +%s) -n places

# 方式 B:仅更新特定 Deployment 的数据镜像(已有新 tag)
kubectl set image deployment/osrm-car  osrm-car=registry.example.com/places/osrm-data-car:202506141200  -n places
kubectl set image deployment/osrm-moto osrm-moto=registry.example.com/places/osrm-data-moto:202506141200 -n places
kubectl set image deployment/osrm-tuktuk osrm-tuktuk=registry.example.com/places/osrm-data-tuktuk:202506141200 -n places
kubectl set image deployment/valhalla  valhalla=registry.example.com/places/valhalla-data:202506141200   -n places

# 查看滚动进度(maxUnavailable=0 保证零中断)
kubectl rollout status deployment/osrm-car -n places
```

#### 回滚

```bash
# 回滚到上一个数据版本(任意引擎)
kubectl rollout undo deployment/osrm-car   -n places
kubectl rollout undo deployment/valhalla   -n places

# 查看历史修订
kubectl rollout history deployment/osrm-car -n places
```

#### 备份与恢复

- **备份**:由 `13-backup-cronjob.yaml` 每日 01:30 触发,`pg_dump → S3`,保留 7 份。
- **OpenSearch 快照**:S3 repository-s3 仓库(`_snapshot/s3-backup`),快照 CronJob 同步保留 7 份。路由图数据镜像不备份——tag 不可变,可直接从 registry 重得(方案 A 红利)。
- **恢复**:参见 `scripts/restore.sh`(单机 compose 版本参考流程);K8s 形态下 PostgreSQL 恢复从 S3 下载 dump 执行 `pg_restore`,OpenSearch 恢复执行 `_snapshot/s3-backup/<name>/_restore`。

#### 周更管道(CronJob)

`11-etl-cronjob.yaml` 默认每周日 02:00 触发,执行完整六步(ETL → 漂移闸 → 索引 → kaniko 构建镜像 → `kubectl set image` → golden 验收)。漂移闸(±15%)不过则 CronJob 以非零退出,由集群告警捕获。

---

### 监控接法

根据集群是否安装 Prometheus Operator 二选一:

#### 方案 A:ServiceMonitor + PrometheusRule(有 Operator)

适用集群已安装 `kube-prometheus-stack` 或 Prometheus Operator。

```bash
kubectl apply -f deploy/k8s/15-monitoring.yaml
```

`15-monitoring.yaml` 包含:
- **ServiceMonitor**:告知 Operator 每 15s 抓取 gateway Service 的 `:8080/metrics`。
- **PrometheusRule**:四条告警规则原文移植自 M5(`deploy/prometheus/rules.yml`),比例分母过滤 `/metrics|/healthz|_unmatched` 防低流量误报。

> 注意:bare k3d 无 Operator CRD,`kubectl apply` 会报 "no matches for kind" — 属预期。真实集群安装 kube-prometheus-stack 后 CRD 即存在,apply 正常。

#### 方案 B:pod annotations(无 Operator)

若集群以 helm chart 或静态配置部署 Prometheus(无 `monitoring.coreos.com` CRD):

1. 在 `07-gateway.yaml` 的 `Deployment.spec.template.metadata` 下增加 annotations:

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"
  prometheus.io/path: "/metrics"
```

2. 将 `deploy/prometheus/rules.yml` 中的四条规则内容加入 Prometheus 服务端 rule 文件(helm values 的 `serverFiles.alerting_rules.yml` 或静态 `rules/` 目录)。

方案 B 下无需 apply `15-monitoring.yaml`。两套接法在该文件中均有注释说明。

---

### 验收记录(本地 k3d 实测)

| # | 验收项 | 结果 |
|---|--------|------|
| 1 | 全栈部署:golden 测试集 | 18/18 PASS(K8s 网关,AUTH 开,经 port-forward) |
| 2 | 数据滚动零中断 | re-tag → `kubectl set image`(maxUnavailable=0),滚动期间持续请求 **0 真实失败**(port-forward 工件已排除);surge 期间旧 Pod 继续服务至新 Pod Ready |
| 3 | 回滚验证 | `kubectl rollout undo` → golden 18/18 PASS(DRIVE 200 OK) |
| 4 | 横向扩容 | `osrm-car replicas=2`,第 2 个 Pod **8.2s Ready**,endpoints=2,HPA `ScalingActive=True` |
| 5 | 周更管道 | kaniko 无特权构建已证(executor 设计如此) ✓;全量 ETL 构建延后真集群执行 |
| 假设④ | OpenSearch repository-s3 快照 | state=SUCCESS(keystore 注入 + 插件安装验证) |

---

### 已知限制 / 延后到真集群

- **dev box 磁盘 96-97%** 是本地验证的环境约束(非设计缺陷),限制了 k3d 内全量 PBF 处理与全量 kaniko 构建的执行。
- **OpenSearch watermark 本地覆盖**:本地 k3d 将 flood/high/low watermark 临时调高以绕过 96% 磁盘限制;真集群使用 OpenSearch 默认值(85%/90%/95%)无需覆盖。
- **单机 compose 回退已不可用**:M6 期间卷/备份数据在磁盘事故中清理;M6 目标为完整 K8s 化,compose 形态不再维护,不影响 M6 验收。

**延后到真集群执行的项**:
- 全量 ETL(Overture + OSM 完整 PBF 拉取 + 入库)
- kaniko 全量数据镜像重建(OSRM×3 + Valhalla 完整路由图)
- 端到端周更管道闭环一轮(验收 #5 的全量路径)
- Nominatim 全量导入(完整 PBF 约 5-15 分钟 + S3 打包)
- HPA 真实负载压测触发扩容(dev box 资源不足以稳定触发)

---

## M6 候选清单

以下改进项已在 M5 终审确认在外，记录为 M6 候选（README 注明，不影响 M5 验收）：

| 候选项 | 背景 | spec 引用 |
|--------|------|-----------|
| **类目映射调优** | ABA Bank 等 OSM `amenity=bank` 映射到 `point_of_interest`（占比 72%），搜索质量可通过细粒度类目树改善 | §14.1 |
| **纯高棉文罗马音别名表** | 柬埔寨地名纯高棉文与罗马音映射不完整，导致英文查询部分遗漏；需构建别名索引 | §14.4 |
| **OSRM/Valhalla 速度模型校准** | 速度档位阈值不连续（moto/tuktuk 切换点有跳变），需与实际道路数据对齐 | M4 终审注 |
| **Valhalla 分块并发化** | 当前降级路径串行分块（500×500 兜底约 ~100s），并发化可降至 ~15s | M4/M5 注 |
| **km/zh 导航 locale** | 路线导航 `language` 参数支持 `km`（高棉）与 `zh`（中文），需 Valhalla locale 文件 | M4 终审注 |
| **maneuvers 暴露** | `/directions` 端点已有路线数据，转弯指令（maneuvers）尚未在响应中暴露 | M4 终审注 |
| **Alertmanager 通知接入** | 告警规则已就位，M5 仅交付规则；通知渠道（PagerDuty/Slack/Email）接入 Alertmanager | §9 |
