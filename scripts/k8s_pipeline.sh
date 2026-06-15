#!/usr/bin/env bash
# scripts/k8s_pipeline.sh — K8s 数据管道(bootstrap + 周更六步)
#
# 用法:
#   PIPELINE_MODE=bootstrap bash k8s_pipeline.sh   # 步骤 1-3,首次数据导入,不构建镜像
#   PIPELINE_MODE=weekly   bash k8s_pipeline.sh    # 完整六步周更
#
# 必须环境变量:
#   DATABASE_URL      — in-cluster PostgreSQL  (postgresql://places:$PW@postgis:5432/places)
#   OPENSEARCH_URL    — in-cluster OpenSearch  (http://opensearch:9200)
#
# 可选环境变量:
#   PIPELINE_MODE          — bootstrap | weekly  (默认 weekly)
#   BOOTSTRAP_SKIP_DRIFT   — 1 = 跳过漂移闸(bootstrap 首次导入,before=0)
#   PLACES_COUNT_BEFORE    — weekly 模式:ETL 前记录的 places 行数(必须,由 CronJob env 注入)
#   REGISTRY               — 镜像仓库前缀 (默认 k3d-places-reg:5000/places,内部端口)
#                            k3d 本地:k3d-places-reg:5000/places (containerd 内部映射)
#                            真实集群:EDIT-ME 替换为集群 registry
#   NAMESPACE              — K8s 命名空间  (默认 places)
#   KANIKO_IMAGE           — kaniko executor 镜像 (默认固定 tag v1.23.2)
#   S3_ENDPOINT            — S3/MinIO 端点    (由 s3-cred Secret 注入)
#   S3_ACCESS_KEY          — S3 Access Key    (由 s3-cred Secret 注入)
#   S3_SECRET_KEY          — S3 Secret Key    (由 s3-cred Secret 注入)
#   S3_BUCKET              — S3 存储桶名称    (由 s3-cred Secret 注入,默认 places)
#
# SIGSEGV 历史备忘(结构性消除):
#   单机模型"原地覆写数据文件"曾导致 osrm-extract 以 O_TRUNC 改写 .osrm 文件时,
#   在线 OSRM 进程 mmap 同一 inode 读到损坏数据,触发 SIGSEGV(exit 139)(M5 现场复现)。
#   K8s 形态:数据更新 = 新 tag 不可变镜像 + rolling Deployment(maxUnavailable=0),
#   旧 Pod 继续服务至新 Pod readiness;任何时刻都不存在"原地覆写正在 mmap 的文件"场景,
#   此类缺陷在此架构下结构性消失。
set -euo pipefail

# init container がshared emptyDir に書いた baseline count を source する
# (CronJob: /shared/before.env に PLACES_COUNT_BEFORE=N が書かれている)
if [ -f /shared/before.env ]; then
    # shellcheck source=/dev/null
    source /shared/before.env
    export PLACES_COUNT_BEFORE
fi

MODE="${PIPELINE_MODE:-weekly}"
NAMESPACE="${NAMESPACE:-places}"
# k3d 本地:kaniko 从 Pod 内推送到 k3d-places-reg:5000(内部端口;:5500 是宿主机映射)
# EDIT-ME 真实集群:改为集群 registry 前缀(如 registry.example.com/places)
REGISTRY="${REGISTRY:-k3d-places-reg:5000/places}"
# kaniko executor — tag 固定,避免意外升级
KANIKO_IMAGE="${KANIKO_IMAGE:-gcr.io/kaniko-project/executor:v1.23.2}"
TS="$(date -u +%Y%m%d%H%M)"
WORKDIR="${WORKDIR:-/tmp/pipeline-${TS}}"
mkdir -p "${WORKDIR}"

log() { echo "=== [$(date -u +%H:%M:%S)] $*" >&2; }

log "k8s_pipeline.sh  mode=${MODE}  ts=${TS}"

###############################################################################
# 步骤 [1/6] 数据拉取 + ETL 链                                                #
###############################################################################
log "[1/6] 下载 Cambodia OSM PBF"
START=$(date +%s)
curl -fsSL --progress-bar -o "${WORKDIR}/cambodia-latest.osm.pbf" \
    https://download.geofabrik.de/asia/cambodia-latest.osm.pbf
log "  PBF: $(du -h "${WORKDIR}/cambodia-latest.osm.pbf" | cut -f1)  elapsed: $(($(date +%s)-START))s"

log "[1/6] Overture 下载 + transform"
START=$(date +%s)
python -c "
import sys; sys.path.insert(0, '/app')
from etl.overture import download, transform
download('${WORKDIR}/overture_places_raw.parquet', '${WORKDIR}/overture_divisions_raw.parquet')
n = transform('${WORKDIR}/overture_places_raw.parquet', '${WORKDIR}/overture_places.parquet')
print(f'transformed {n} places')
"
log "  Overture elapsed: $(($(date +%s)-START))s"

log "[1/6] OSM POI 抽取"
START=$(date +%s)
python -c "
import sys; sys.path.insert(0, '/app')
from etl.osm import extract
n = extract('${WORKDIR}/cambodia-latest.osm.pbf', '${WORKDIR}/osm_pois.parquet')
print('osm pois:', n)
"
log "  OSM elapsed: $(($(date +%s)-START))s"

log "[1/6] 入库(load)"
START=$(date +%s)
python -c "
import psycopg, os, sys
sys.path.insert(0, '/app')
import pyarrow.parquet as pq
from etl.load import (load_boundary_wkt, extract_kh_boundary_wkt,
                      copy_parquet_to_staging, upsert_places_from_staging, load_osm_pois)
conn = psycopg.connect(os.environ['DATABASE_URL'], autocommit=True)
wkt = extract_kh_boundary_wkt('${WORKDIR}/overture_divisions_raw.parquet')
load_boundary_wkt(conn, 'KH', wkt)
t = pq.read_table('${WORKDIR}/overture_places.parquet')
t = t.rename_columns([{'sources_json': 'sources'}.get(c, c) for c in t.column_names])
pq.write_table(t, '${WORKDIR}/overture_places_staged.parquet')
staged = copy_parquet_to_staging(conn, '${WORKDIR}/overture_places_staged.parquet', 'staging_overture',
    ['place_id','name_default','name_km','name_en','name_zh','raw_category','google_type','phone',
     'website','addr_freeform','addr_locality','addr_region','addr_country','confidence','sources',
     'lon','lat'])
print('staged:', staged)
n = upsert_places_from_staging(conn)
print('places upserted:', n)
assert n > 0, 'zero places upserted — boundary or staging broken'
osm_n = load_osm_pois(conn, '${WORKDIR}/osm_pois.parquet')
print('osm pois loaded:', osm_n)
"
log "  load elapsed: $(($(date +%s)-START))s"

log "[1/6] 合并去重(conflate)"
START=$(date +%s)
python -c "
import psycopg, os, sys; sys.path.insert(0, '/app')
from etl.conflate import conflate
merged, inserted = conflate(psycopg.connect(os.environ['DATABASE_URL'], autocommit=True))
print('merged, inserted =', merged, inserted)
"
log "  conflate elapsed: $(($(date +%s)-START))s"

###############################################################################
# 步骤 [2/6] 漂移闸 ±15%                                                      #
###############################################################################
log "[2/6] 漂移闸"
python -c "
import psycopg, os, sys
conn = psycopg.connect(os.environ['DATABASE_URL'])
after = conn.execute('SELECT count(*) FROM places').fetchone()[0]
print(f'places count after ETL: {after}')
skip = os.environ.get('BOOTSTRAP_SKIP_DRIFT', '0') == '1'
if skip:
    print('BOOTSTRAP_SKIP_DRIFT=1 — skipping drift check (first import)')
else:
    before_str = os.environ.get('PLACES_COUNT_BEFORE', '')
    if not before_str:
        print('ERROR: PLACES_COUNT_BEFORE not set — weekly mode requires this env var', file=sys.stderr)
        sys.exit(1)
    before = int(before_str)
    drift = abs(after - before) / max(before, 1)
    print(f'places: {before} -> {after} (drift {drift:.1%})')
    if drift >= 0.15:
        print(f'ABORT: drift {drift:.1%} exceeds 15% — investigate before indexing', file=sys.stderr)
        sys.exit(1)
    print('drift check PASSED')
"

###############################################################################
# 步骤 [3/6] 索引重建 + alias 原子切换                                          #
###############################################################################
log "[3/6] 重建 OpenSearch 索引(alias 原子切换)"
START=$(date +%s)
python -c "
import psycopg, os, sys; sys.path.insert(0, '/app')
from etl.index import create_index, bulk_index, rows_from_postgis, swap_alias, prune_old_indices
url  = os.environ['OPENSEARCH_URL']
conn = psycopg.connect(os.environ['DATABASE_URL'])
name = create_index(url)
print('indexing into:', name)
indexed = bulk_index(url, name, rows_from_postgis(conn))
print('indexed:', indexed)
prev = swap_alias(url, name)
print('alias ->', name)
keep = {name}
if prev:
    keep.add(prev[0])
pruned = prune_old_indices(url, keep)
print('pruned:', pruned)
"
log "  index elapsed: $(($(date +%s)-START))s"

###############################################################################
# bootstrap モード は步骤 1-3 後に終了(不构建镜像,不 patch Deployment)          #
###############################################################################
if [ "${MODE}" = "bootstrap" ]; then
    log "=== bootstrap complete ${TS} ==="
    exit 0
fi

###############################################################################
# 步骤 [4/6] kaniko 构建数据镜像(OSRM×3 + Valhalla)                            #
# 构建上下文打包为 tar.gz 上传 S3,kaniko Job 从 S3 URL 拉取构建上下文。          #
# kaniko 须作为独立 Pod/Job 运行,不能嵌入子进程(需改写自身 rootfs)。            #
###############################################################################
log "[4/6] 打包构建上下文 → S3 → 创建 kaniko Jobs"

S3_ENDPOINT="${S3_ENDPOINT:-http://minio:9000}"
S3_BUCKET="${S3_BUCKET:-places}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"

# 配置 mc(MinIO Client),所有 kaniko 构建共用
mc alias set pipeline "${S3_ENDPOINT}" "${S3_ACCESS_KEY}" "${S3_SECRET_KEY}" --quiet

# build_and_launch_kaniko_job PROFILE DATA_SRC_DIR IMAGE_DEST
# PROFILE      : osrm-car | osrm-moto | osrm-tuktuk | valhalla
# DATA_SRC_DIR : 含 Dockerfile 和数据的本地目录路径(临时目录,打完包后删除)
# IMAGE_DEST   : 目标镜像全名(不含 tag)
build_and_launch_kaniko_job() {
    local PROFILE="$1"
    local CTX_DIR="$2"
    local IMAGE_DEST="$3"
    local JOB_NAME="kaniko-${PROFILE}-${TS}"

    log "  [4/6] 打包 ${PROFILE} 构建上下文"
    local CTX_TAR="${WORKDIR}/ctx-${PROFILE}.tar.gz"
    tar -czf "${CTX_TAR}" -C "${CTX_DIR}" .
    local CTX_SIZE
    CTX_SIZE=$(du -h "${CTX_TAR}" | cut -f1)
    log "    context size: ${CTX_SIZE}"

    local S3_KEY="build-context/${PROFILE}-${TS}.tar.gz"
    log "    上传 → s3://${S3_BUCKET}/${S3_KEY}"
    mc cp "${CTX_TAR}" "pipeline/${S3_BUCKET}/${S3_KEY}"

    # 生成 presigned URL(MinIO: mc share download; 有效期 2h)
    local CTX_URL
    # shellcheck disable=SC2312
    CTX_URL=$(mc share download --expire=2h \
        "pipeline/${S3_BUCKET}/${S3_KEY}" 2>/dev/null \
        | grep -E '^Share:|^URL:' | awk '{print $NF}' | head -1 || true)
    if [ -z "${CTX_URL}" ]; then
        # mc 版本差异备用:presign 子命令
        CTX_URL=$(mc presign "pipeline/${S3_BUCKET}/${S3_KEY}" 7200 2>/dev/null || true)
    fi
    if [ -z "${CTX_URL}" ]; then
        # 最终后备:直接路径(需 S3 bucket policy 允许匿名读或 kaniko --registry-insecure)
        CTX_URL="${S3_ENDPOINT}/${S3_BUCKET}/${S3_KEY}"
        log "    WARN: presign 失败,回退直接 URL — 需 S3 bucket policy 或 IAM"
    fi
    log "    context URL: ${CTX_URL}"

    log "  [4/6] 创建 kaniko Job: ${JOB_NAME}"
    kubectl -n "${NAMESPACE}" apply -f - <<JOBEOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${JOB_NAME}
  namespace: ${NAMESPACE}
  labels:
    app: kaniko-build
    pipeline-ts: "${TS}"
    profile: "${PROFILE}"
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 3600
  template:
    spec:
      restartPolicy: Never
      # kaniko 无需 privileged; executor 以 overlay fs 构建 rootfs
      # 若集群 PSS=Restricted 需在 namespace 放开 seccompProfile (spec §10 假设①)
      containers:
        - name: kaniko
          image: ${KANIKO_IMAGE}
          args:
            - "--dockerfile=Dockerfile"
            - "--context=${CTX_URL}"
            - "--destination=${IMAGE_DEST}:${TS}"
            - "--cache=false"
            - "--single-snapshot"
            # k3d 本地 registry 为 HTTP;EDIT-ME 真实集群改用 HTTPS 去掉 --insecure
            - "--insecure"
            - "--skip-tls-verify"
          volumeMounts:
            - name: registry-cred
              mountPath: /kaniko/.docker
              readOnly: true
          resources:
            requests: {memory: 512Mi, cpu: "500m"}
            limits:   {memory: 2Gi,   cpu: "2"}
      volumes:
        - name: registry-cred
          secret:
            secretName: registry-cred
            items:
              # registry-cred 为 kubernetes.io/dockerconfigjson 类型
              # k3d registry 无认证(EDIT-ME: 真实 registry 需填 auth)
              - key: .dockerconfigjson
                path: config.json
JOBEOF
}

# ── 4a: 准备各 OSRM profile 构建上下文并启动 kaniko Job ─────────────────────
# 预期:osrm-extract 已在 bootstrap 或上次 PVC 中生成 /data/osrm-{car,moto,tuktuk}/
for PROFILE in car moto tuktuk; do
    CTX_DIR="${WORKDIR}/ctx-osrm-${PROFILE}"
    mkdir -p "${CTX_DIR}/data"
    cp /app/deploy/docker/osrm-data.Dockerfile "${CTX_DIR}/Dockerfile"
    if [ -d "/data/osrm-${PROFILE}" ]; then
        cp -r "/data/osrm-${PROFILE}/." "${CTX_DIR}/data/"
    else
        log "  WARN: /data/osrm-${PROFILE} 不存在 — 构建上下文无数据(CI dry-run 场景)"
    fi
    build_and_launch_kaniko_job "osrm-${PROFILE}" "${CTX_DIR}" "${REGISTRY}/osrm-data-${PROFILE}"
done

# ── 4b: 准备 Valhalla 构建上下文并启动 kaniko Job ───────────────────────────
CTX_DIR="${WORKDIR}/ctx-valhalla"
mkdir -p "${CTX_DIR}/custom_files"
cp /app/deploy/docker/valhalla-data.Dockerfile "${CTX_DIR}/Dockerfile"
if [ -d "/data/valhalla" ]; then
    cp -r "/data/valhalla/." "${CTX_DIR}/custom_files/"
else
    log "  WARN: /data/valhalla 不存在 — 构建上下文无数据(CI dry-run 场景)"
fi
build_and_launch_kaniko_job "valhalla" "${CTX_DIR}" "${REGISTRY}/valhalla-data"

# ── 4c: 等待所有 kaniko Job 完成 ────────────────────────────────────────────
log "[4/6] 等待 4 个 kaniko Jobs 完成 (timeout 60 min)"
KANIKO_JOBS="kaniko-osrm-car-${TS} kaniko-osrm-moto-${TS} kaniko-osrm-tuktuk-${TS} kaniko-valhalla-${TS}"
KANIKO_DEADLINE=$(($(date +%s) + 3600))

for JOB in ${KANIKO_JOBS}; do
    log "  等待 ${JOB}..."
    while true; do
        COMPLETE=$(kubectl -n "${NAMESPACE}" get job "${JOB}" \
            -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null || true)
        FAILED=$(kubectl -n "${NAMESPACE}" get job "${JOB}" \
            -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null || true)
        if [ "${COMPLETE}" = "True" ]; then
            log "  ${JOB}: SUCCEEDED"
            break
        fi
        if [ "${FAILED}" = "True" ]; then
            log "  ERROR: ${JOB} FAILED"
            kubectl -n "${NAMESPACE}" logs "job/${JOB}" --tail=50 >&2 || true
            exit 1
        fi
        if [ "$(date +%s)" -ge "${KANIKO_DEADLINE}" ]; then
            log "  ERROR: ${JOB} timeout (60 min)"
            exit 1
        fi
        sleep 15
    done
done

# Job 对象清理(Pod 日志由 kubectl 保留直到 TTL,Job 对象可清)
for JOB in ${KANIKO_JOBS}; do
    kubectl -n "${NAMESPACE}" delete job "${JOB}" --ignore-not-found=true
done

###############################################################################
# 步骤 [5/6] kubectl set image — 四个 Deployment 滚动发布                       #
# maxUnavailable=0:新 Pod readiness 后才终止旧 Pod,更新期间零中断。              #
# SIGSEGV 结构性消除:新镜像 tag 唯一标识不可变制品,无在线进程持有旧数据 mmap。   #
###############################################################################
log "[5/6] kubectl set image → 四个 Deployment (tag=${TS})"

kubectl -n "${NAMESPACE}" set image deployment/osrm-car    osrm="${REGISTRY}/osrm-data-car:${TS}"
kubectl -n "${NAMESPACE}" set image deployment/osrm-moto   osrm="${REGISTRY}/osrm-data-moto:${TS}"
kubectl -n "${NAMESPACE}" set image deployment/osrm-tuktuk osrm="${REGISTRY}/osrm-data-tuktuk:${TS}"
kubectl -n "${NAMESPACE}" set image deployment/valhalla    valhalla="${REGISTRY}/valhalla-data:${TS}"

log "  等待四个 Deployment rollout 完成 (timeout 20 min)"
for DEPLOY in osrm-car osrm-moto osrm-tuktuk valhalla; do
    log "  rollout status: ${DEPLOY}"
    kubectl -n "${NAMESPACE}" rollout status "deployment/${DEPLOY}" --timeout=20m
done

###############################################################################
# 步骤 [6/6] Golden smoke Job — 失败则 rollout undo 四个 Deployment + exit 1    #
###############################################################################
log "[6/6] Golden smoke Job"

GOLDEN_JOB="golden-weekly-${TS}"
# EDIT-ME: 将 builder:dev 替换为固定 digest/tag(与 10-golden.yaml 保持一致)
kubectl -n "${NAMESPACE}" apply -f - <<GOLDENEOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${GOLDEN_JOB}
  namespace: ${NAMESPACE}
  labels:
    app: golden-smoke
    pipeline-ts: "${TS}"
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 300
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: golden
          image: ${REGISTRY}/builder:dev
          command: ["python", "/app/golden/run_golden.py",
                    "--base-url", "http://gateway:8080",
                    "--api-key", "\$(GOLDEN_API_KEY)"]
          env:
            - name: GOLDEN_API_KEY
              valueFrom:
                secretKeyRef:
                  name: golden-api-key
                  key: API_KEY
          volumeMounts:
            - name: golden-runner
              mountPath: /app/golden/run_golden.py
              subPath: run_golden.py
          resources:
            requests: {memory: "128Mi", cpu: "100m"}
            limits:   {memory: "256Mi"}
      volumes:
        - name: golden-runner
          configMap:
            name: golden-runner
GOLDENEOF

log "  等待 golden Job: ${GOLDEN_JOB} (timeout 6 min)"
GOLDEN_OK=0
GOLDEN_DEADLINE=$(($(date +%s) + 360))
while true; do
    COMPLETE=$(kubectl -n "${NAMESPACE}" get job "${GOLDEN_JOB}" \
        -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null || true)
    FAILED=$(kubectl -n "${NAMESPACE}" get job "${GOLDEN_JOB}" \
        -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null || true)
    if [ "${COMPLETE}" = "True" ]; then
        log "  golden PASSED"
        GOLDEN_OK=1
        break
    fi
    if [ "${FAILED}" = "True" ]; then
        log "  golden FAILED"
        break
    fi
    if [ "$(date +%s)" -ge "${GOLDEN_DEADLINE}" ]; then
        log "  ERROR: golden timeout after 6 min"
        break
    fi
    sleep 10
done

# 打印 golden 日志(无论成败,便于 CronJob 失败排查)
kubectl -n "${NAMESPACE}" logs "job/${GOLDEN_JOB}" --tail=100 >&2 || true

if [ "${GOLDEN_OK}" -ne 1 ]; then
    log "ROLLBACK: golden smoke 失败 → rollout undo 四个 Deployment"
    kubectl -n "${NAMESPACE}" rollout undo deployment/osrm-car
    kubectl -n "${NAMESPACE}" rollout undo deployment/osrm-moto
    kubectl -n "${NAMESPACE}" rollout undo deployment/osrm-tuktuk
    kubectl -n "${NAMESPACE}" rollout undo deployment/valhalla
    log "  等待 rollback 稳定..."
    for DEPLOY in osrm-car osrm-moto osrm-tuktuk valhalla; do
        kubectl -n "${NAMESPACE}" rollout status "deployment/${DEPLOY}" --timeout=10m || true
    done
    log "ROLLBACK COMPLETE"
    exit 1
fi

# 清理 golden Job 对象
kubectl -n "${NAMESPACE}" delete job "${GOLDEN_JOB}" --ignore-not-found=true

###############################################################################
# 完成                                                                          #
###############################################################################
log "=== weekly pipeline DONE  ts=${TS} ==="
