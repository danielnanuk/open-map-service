#!/usr/bin/env bash
# scripts/k3d_up.sh — 本地验证集群(单节点 + 内置 registry + 80 端口映射)
set -euo pipefail

# 确保 kubectl 可用 — k3d 不安装 kubectl
export PATH="$HOME/.local/bin:$PATH"

k3d registry create places-reg --port 5500 2>/dev/null || true
k3d cluster create places \
  --registry-use k3d-places-reg:5500 \
  -p "8088:80@loadbalancer" \
  --agents 0 2>/dev/null || true

kubectl cluster-info
echo "registry(宿主推送): localhost:5500 ;(集群内引用): k3d-places-reg:5500"
