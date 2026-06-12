# deploy/docker/builder.Dockerfile — 周更管道编排镜像(创建 kaniko Job,不内嵌 executor)
FROM python:3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      curl ca-certificates git zstd postgresql-client \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /usr/local/bin/kubectl \
      "https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl" \
    && chmod +x /usr/local/bin/kubectl
RUN curl -fsSL -o /usr/local/bin/mc \
      "https://dl.min.io/client/mc/release/linux-amd64/mc" \
    && chmod +x /usr/local/bin/mc
COPY etl/ /app/etl/
COPY golden/ /app/golden/
COPY db/migrations/ /app/migrations/
# scripts/k8s_pipeline.sh 在 M6-T5 实现;此处为占位 stub
COPY scripts/k8s_pipeline.sh /app/
# Makefile py-setup: pip install -e 'etl[dev]';builder 镜像用同等可编辑安装
RUN pip install --no-cache-dir -e '/app/etl[dev]'
WORKDIR /app
