# M6:K8s 部署、横向扩容与数据滚动更新 — 设计文档

- 日期:2026-06-12
- 状态:已获用户认可,待实现规划
- 前置:M1-M5 已合入 main(单机 docker-compose 形态,全链路验收通过)

## 1. 目标与约束

| 维度 | 决策 |
|---|---|
| 目标 | 快速部署、横向扩容、应用滚动发布、**地图数据滚动更新** |
| 集群 | 用户已有 K8s 集群(集群相关值参数化,本地 k3d 全程验证后交付) |
| 打包 | **原生 YAML**(不引入 Helm/Kustomize);集群相关值集中在文件顶部 `# EDIT-ME` 注释块 |
| 数据交付 | **方案 A:数据即镜像**(OSRM×3、Valhalla);**唯一例外:Nominatim 走 initContainer+S3 tar**(其数据是导入后的 PG 数据目录,kaniko 无法在构建期跑 import)——用户已明示认可 |
| 更新编排 | 集群内 CronJob(builder Pod 内嵌 kaniko executor) |
| 基础设施现状 | 集群已有可推 registry 与 S3 兼容存储(manifests 只填地址);监控接集群已有 Prometheus 栈 |
| 规模 | 单节点起步,各引擎可随时 `replicas` 扩;gateway 上 HPA |

### 核心架构反转

单机模型"原地重建数据"(曾导致 OSRM 带电重建 SIGSEGV 与磁盘事故);K8s 形态改为**不可变版本化数据制品 + 滚动 Deployment**:引擎是"装载只读数据后无状态"的服务,数据更新 = 新镜像 tag + 原生 rolling(maxUnavailable=0),回滚 = `kubectl rollout undo`,扩容 = `replicas`+1。原地覆写缺陷类在此模型下结构性消失。

### 非目标(明确不做,列为扩展路径)

- PostGIS 高可用(CNPG 主备)、多区域部署、Argo CD/GitOps、service mesh、集群自身的搭建

## 2. 工作负载形态

| 组件 | 形态 | 数据来源 | 副本/扩容 | readiness |
|---|---|---|---|---|
| gateway | Deployment + HPA(CPU 70%,2-6) | 无状态 | HPA 自动 | GET /healthz |
| osrm-car/moto/tuktuk | Deployment ×3 | 数据镜像 `osrm-data-<p>:<ts>` | replicas 手动,即起即服务 | TCP 5000 |
| valhalla | Deployment | 数据镜像 `valhalla-data:<ts>` | replicas 手动 | GET /status |
| nominatim | Deployment | initContainer 从 S3 拉 `nominatim-data-<ts>.tar.zst` 解到 emptyDir | replicas 手动(启动多一次下载) | GET /status |
| opensearch | StatefulSet + PVC(单节点起步) | 沿用 alias 原子切换(索引器变 Job) | 节点数 + index replica 数 | cluster health |
| postgis | StatefulSet + PVC(单副本) | 权威库 | 读量小(details/KNN);HA 留扩展 | pg_isready |

- 镜像全部 digest/tag 固定(继承 M5 pin 纪律);数据镜像 tag 用构建时间戳,不可变。
- 资源基线(k3d 验证后按实测微调入 manifests):gateway 64Mi/256Mi;OSRM 各 300Mi/1Gi;Valhalla 512Mi/2Gi;OS 2Gi/3Gi(heap 1g);PostGIS 1Gi/4Gi;Nominatim 1Gi/4Gi。
- gateway `terminationGracePeriodSeconds: 30` 与既有 15s 优雅退出协同;PDB:gateway minAvailable 1(单副本组件不设)。

## 3. 数据镜像体系

```
deploy/docker/
  builder.Dockerfile          # etl venv + kubectl + psql/curl + kaniko executor 二进制
  osrm-data.Dockerfile        # FROM osrm-backend@digest + COPY <profile>/*.osrm* ;CMD osrm-routed --mld --max-table-size 2000
  valhalla-data.Dockerfile    # FROM valhalla-scripted@digest + COPY valhalla_tiles + 配置(检测已有 tiles 直接 serve)
```

- tag 约定:`<REGISTRY>/places/osrm-data-car:<YYYYMMDDHHMM>`;patch 用具体 tag,保证 rollout 修订历史可回滚。
- Nominatim 数据包:月更 CronJob 起 mediagis 镜像 Job 跑 import → `tar --zstd` 数据目录 → 推 S3;包名含时间戳,Deployment 的 initContainer 按 ConfigMap 中的版本号拉取。

## 4. 周更数据管道(CronJob → builder Pod)

单机 `update_pipeline.sh` 的等价 K8s 化,六步,漂移闸保留:

1. 拉新 OSM PBF + Overture(venv 内 CLI,M5 修复的解析方式)→ ETL 链写 PostGIS(Service DNS)
2. **漂移闸 ±15%**:不过 → exit 1(CronJob 失败由集群既有 kube 告警捕获),不触发后续
3. etl-index:新索引 + alias 原子切换 + prune 旧索引(M5 既有逻辑)
4. kaniko(builder 内子进程)顺序构建 3×OSRM + 1×Valhalla 数据镜像并推 registry
5. `kubectl set image` patch 四个 Deployment(RBAC:本 namespace 内 deployments get/patch + jobs create)→ 原生滚动
6. golden 冒烟 Job(集群内 curl gateway 全 18 用例)→ 失败:`kubectl rollout undo` 四个 Deployment + exit 1

## 5. 应用发布与数据发布解耦

- 应用:CI 构建 `gateway:<git-sha>` → set image → readiness 驱动滚动(零中断)。
- 数据:仅经 §4 管道;两者互不阻塞。

## 6. 配置、机密、监控、备份

- Secrets:`places-secrets`(POSTGRES_PASSWORD)、`registry-cred`(kaniko dockerconfig)、`s3-cred`(Nominatim 包 + 备份);提供 `01-secrets.example.yaml`,真值不入库。AUTH_ENABLED=true 为 K8s 默认。
- 参数化:每个 manifest 顶部 `# EDIT-ME` 块集中 storageClassName / ingressClassName / REGISTRY / S3_ENDPOINT;README 给 sed 一行示例。
- 监控:`ServiceMonitor`(gateway /metrics)+ `PrometheusRule`(移植 M5 四规则原样);无 Operator 的集群退化用 pod annotations 接法(两文件并存,README 二选一)。
- 备份:CronJob 每日 `pg_dump → S3`(保留 7,沿用 M5 策略);OpenSearch 注册 **S3 快照仓库**(repository-s3,替代单机 fs 仓库),快照 CronJob 同步保留 7。路由图不备份(可由数据镜像 tag 重得——方案 A 红利)。

## 7. 文件结构

```
deploy/k8s/
  00-namespace.yaml          05-valhalla.yaml          10-etl-cronjob.yaml
  01-secrets.example.yaml    06-osrm.yaml(×3 同文件)   11-nominatim-data-cronjob.yaml(月更)
  02-postgis.yaml            07-gateway.yaml(含 HPA)   12-backup-cronjob.yaml
  03-opensearch.yaml         08-ingress.yaml            13-monitoring.yaml(SM+Rule;annotations 备选)
  04-nominatim.yaml          09-jobs.yaml(migrate/首次引导/golden 冒烟)
deploy/docker/(§3 三个 Dockerfile)
```

## 8. 引导顺序(README runbook)

secrets → 00-03(基座)→ migrate Job → 首次数据引导 Job(等价跑一次 §4 管道;或从现有单机制品上传)→ 04-06(引擎)→ 07-08(gateway/ingress)→ golden Job 验收。

## 9. 验收(本地 k3d 集群全程验证后交付)

| # | 验收项 | 标准 |
|---|---|---|
| 1 | 全栈部署 | k3d 上 apply 全套,所有 Pod Ready,golden 18/18(经 port-forward/ingress) |
| 2 | 数据滚动 | builder 构建新数据镜像 → patch → 滚动期间持续请求**零失败**(maxUnavailable=0 实证) |
| 3 | 回滚 | `rollout undo` 后 golden 仍 18/18 |
| 4 | 横向扩容 | gateway HPA 压测触发扩容;osrm-car `replicas: 2` 新 Pod 秒级 Ready |
| 5 | 管道闭环 | 周更 CronJob 手动触发一轮全程成功(k3d 内带 k3d registry) |

k3d 本地验证使用 `k3d registry`(集群内可推),交付时 EDIT-ME 换成已有集群 registry。

## 10. 实现期需验证的假设

1. **kaniko 在 k3d/目标集群无特权可跑**(executor 设计如此;若集群 PSP/PSS 限制,fallback 用 buildkit rootless,实现期实测定)。
2. **valhalla-scripted 镜像对"预置 tiles 无 PBF"的启动路径**(M3 实测过 use_tiles_ignore_pbf 跳过重建;数据镜像形态下需复验)。
3. **mediagis Nominatim 数据目录 tar 还原可启动**(M5 备份演练只验证了同机;跨 Pod 还原需复验 PG 版本/属主)。
4. **OpenSearch repository-s3 插件**在所用镜像可装(自定义镜像已有 Dockerfile,加一个插件层)。
5. builder 镜像体积与 CronJob 拉取时间(etl venv + kubectl + kaniko ≈ 1GB 级,可接受性实测)。
