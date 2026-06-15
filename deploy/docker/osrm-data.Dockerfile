# deploy/docker/osrm-data.Dockerfile — 数据即镜像:构建上下文 = 本文件 + data/(某 profile 的 .osrm* 全套)
# 基镜像 digest 与 docker-compose.yml osrm-car/moto/tuktuk 服务保持一致
FROM ghcr.io/project-osrm/osrm-backend@sha256:7e2d775e5dd1f6752f679621e79dcff3b6bc37266733c771a360af9b3d652205
COPY data/ /data/
EXPOSE 5000
CMD ["osrm-routed", "--algorithm", "mld", "--max-table-size", "2000", "/data/cambodia-latest.osrm"]
