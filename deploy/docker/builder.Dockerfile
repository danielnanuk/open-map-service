# deploy/docker/builder.Dockerfile — 周更管道编排镜像(创建 kaniko Job,不内嵌 executor)
FROM python:3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      curl ca-certificates git zstd postgresql-client \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /usr/local/bin/kubectl \
      "https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl" \
    && chmod +x /usr/local/bin/kubectl
# 与 kubectl 同纪律:版本钉死
RUN curl -fsSL -o /usr/local/bin/mc \
      "https://dl.min.io/client/mc/release/linux-amd64/archive/mc.RELEASE.2025-08-13T08-35-41Z" \
    && chmod +x /usr/local/bin/mc
COPY etl/ /app/etl/
COPY golden/ /app/golden/
COPY db/migrations/ /app/migrations/
# k8s_pipeline.sh: 完整实现的周更管道编排脚本(ETL → index → kaniko → set image → golden)
COPY scripts/k8s_pipeline.sh /app/
# 生产镜像:非可编辑安装,不含 dev/pytest
RUN pip install --no-cache-dir '/app/etl'
WORKDIR /app
