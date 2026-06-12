# deploy/docker/valhalla-data.Dockerfile — 数据即镜像:包含预构建的瓦片与配置
# 基镜像 digest 与 docker-compose.yml valhalla 服务保持一致
# compose 挂载 ./data/valhalla 到 /custom_files,此处原样 COPY
FROM ghcr.io/valhalla/valhalla-scripted@sha256:bb3e89c0e8d3fb35e9278e23a1f7fdbcbf76d29c1edbf7effad829e1ee26b6ed
# compose 实际使用: serve_tiles=True — 仅服务已有瓦片,不重建
# use_tiles_ignore_pbf 不在 compose ENV 中,故此处不设置以与 compose 完全对齐
ENV serve_tiles=True \
    build_admins=True \
    build_time_zones=True
COPY custom_files/ /custom_files/
EXPOSE 8002
