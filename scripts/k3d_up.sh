#!/usr/bin/env bash
# scripts/k3d_up.sh — 本地验证集群(单节点 + 内置 registry + 80 端口映射)
set -euo pipefail

# 确保 kubectl 可用 — k3d 不安装 kubectl
export PATH="$HOME/.local/bin:$PATH"
command -v kubectl >/dev/null || { echo "kubectl 未安装(本机曾装至 ~/.local/bin):curl -fsSL -o ~/.local/bin/kubectl https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl && chmod +x ~/.local/bin/kubectl" >&2; exit 1; }

k3d registry list 2>/dev/null | grep -q '^k3d-places-reg ' || k3d registry create places-reg --port 5500
k3d cluster list 2>/dev/null | grep -q '^places ' || k3d cluster create places \
  --registry-use k3d-places-reg:5500 \
  -p "8088:80@loadbalancer" \
  --agents 0

kubectl cluster-info
echo "registry(宿主推送): localhost:5500 ;(集群内引用): k3d-places-reg:5500"

# 预建 namespace,确保 99-minio-k3d.yaml 等 manifests 可直接 apply
kubectl create namespace places --dry-run=client -o yaml | kubectl apply -f -

# 下一步:在 apply secrets/manifests 之后、运行 migrate Job 之前,执行:
#   bash scripts/k8s_bootstrap_config.sh
# 该脚本幂等创建以下运行时 ConfigMap/Secret:
#   ConfigMap/migrations      (09-jobs.yaml Job/migrate)
#   ConfigMap/etl-mappings    (09-jobs.yaml Job/bootstrap-data, 11-etl-cronjob.yaml)
#   ConfigMap/golden-runner   (10-golden.yaml Job/golden-smoke)
#   Secret/golden-api-key     (10-golden.yaml Job/golden-smoke + PG api_keys)
