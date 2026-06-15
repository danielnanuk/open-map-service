#!/usr/bin/env bash
# scripts/k8s_bootstrap_config.sh — 运行时 ConfigMap/Secret 引导脚本
#
# 幂等创建以下资源(re-run 安全):
#
#   ConfigMap  migrations       → deploy/k8s/09-jobs.yaml Job/migrate
#                                  volumeMounts: /migrations/*.sql
#   ConfigMap  etl-mappings     → deploy/k8s/09-jobs.yaml Job/bootstrap-data
#                                  volumeMounts: subPath: mappings.json
#                                  → deploy/k8s/11-etl-cronjob.yaml (同名 volume)
#   ConfigMap  golden-runner    → deploy/k8s/10-golden.yaml Job/golden-smoke
#                                  volumeMounts: subPath: run_golden.py
#   Secret     golden-api-key   → deploy/k8s/10-golden.yaml Job/golden-smoke
#                                  env: secretKeyRef name=golden-api-key key=API_KEY
#                                  同时写入 PG api_keys 表(ON CONFLICT DO NOTHING)
#
# 用法: bash scripts/k8s_bootstrap_config.sh
#   脚本须在 repo 根目录下执行(通过 SCRIPT_DIR 自动解析)
#   kubectl context 须已指向目标集群,namespace places 须存在
#
# 依赖: kubectl、openssl、bash >= 4

set -euo pipefail

command -v kubectl >/dev/null 2>&1 || {
  echo "ERROR: kubectl 未找到。安装示例:" >&2
  echo "  curl -fsSL -o ~/.local/bin/kubectl https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl && chmod +x ~/.local/bin/kubectl" >&2
  exit 1
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
NAMESPACE="places"

echo "=== [1/4] ConfigMap: migrations (消费者: Job/migrate in 09-jobs.yaml) ==="
MIGRATIONS_DIR="${REPO_ROOT}/db/migrations"
if [[ ! -d "${MIGRATIONS_DIR}" ]]; then
  echo "ERROR: ${MIGRATIONS_DIR} 不存在" >&2; exit 1
fi
# --from-file dir → 每个文件名成为一个 key
kubectl -n "${NAMESPACE}" create configmap migrations \
  --from-file="${MIGRATIONS_DIR}" \
  --dry-run=client -o yaml | kubectl apply -f -
echo "ConfigMap/migrations: OK"

echo ""
echo "=== [2/4] ConfigMap: etl-mappings (消费者: Job/bootstrap-data in 09-jobs.yaml, 11-etl-cronjob.yaml) ==="
MAPPINGS_FILE="${REPO_ROOT}/etl/etl/mappings.json"
if [[ ! -f "${MAPPINGS_FILE}" ]]; then
  echo "ERROR: ${MAPPINGS_FILE} 不存在" >&2; exit 1
fi
# key 必须为 mappings.json 以匹配 subPath: mappings.json
kubectl -n "${NAMESPACE}" create configmap etl-mappings \
  --from-file=mappings.json="${MAPPINGS_FILE}" \
  --dry-run=client -o yaml | kubectl apply -f -
echo "ConfigMap/etl-mappings: OK"

echo ""
echo "=== [3/4] ConfigMap: golden-runner (消费者: Job/golden-smoke in 10-golden.yaml) ==="
GOLDEN_RUNNER="${REPO_ROOT}/golden/run_golden.py"
if [[ ! -f "${GOLDEN_RUNNER}" ]]; then
  echo "ERROR: ${GOLDEN_RUNNER} 不存在" >&2; exit 1
fi
# key 必须为 run_golden.py 以匹配 subPath: run_golden.py
kubectl -n "${NAMESPACE}" create configmap golden-runner \
  --from-file=run_golden.py="${GOLDEN_RUNNER}" \
  --dry-run=client -o yaml | kubectl apply -f -
echo "ConfigMap/golden-runner: OK"

echo ""
echo "=== [4/4] Secret: golden-api-key (消费者: Job/golden-smoke in 10-golden.yaml) ==="
# 检查 Secret 是否已存在(幂等:已存在则跳过生成步骤,不重复覆盖)
if kubectl -n "${NAMESPACE}" get secret golden-api-key >/dev/null 2>&1; then
  echo "Secret/golden-api-key: 已存在,跳过生成(不重复打印 key)"
else
  GOLDEN_KEY="$(openssl rand -hex 24)"
  kubectl -n "${NAMESPACE}" create secret generic golden-api-key \
    --from-literal=API_KEY="${GOLDEN_KEY}" \
    --dry-run=client -o yaml | kubectl apply -f -
  echo "Secret/golden-api-key: 已创建"

  # 写入 PG api_keys 表(ON CONFLICT DO NOTHING — 幂等)
  # 镜像 Makefile gen-api-key 的 SQL 模式
  echo "  → 将 golden-smoke key 写入 PG api_keys..."
  kubectl -n "${NAMESPACE}" exec -i postgis-0 -- \
    psql -U places -d places -c \
    "INSERT INTO api_keys (key, name, rpm_limit) VALUES ('${GOLDEN_KEY}', 'golden-smoke', 300) ON CONFLICT DO NOTHING;"
  echo ""
  echo "======================================================"
  echo "  GOLDEN_API_KEY: ${GOLDEN_KEY}"
  echo "  (已写入 PG api_keys; 请妥善保存,脚本不重复打印)"
  echo "======================================================"
fi

echo ""
echo "=== 完成:所有运行时 ConfigMap/Secret 已就绪 ==="
echo "下一步:apply 09-jobs.yaml 并等待 Job/migrate 完成,再运行 bootstrap-data Job"
