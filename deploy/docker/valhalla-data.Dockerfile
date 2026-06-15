# deploy/docker/valhalla-data.Dockerfile — 数据即镜像:包含预构建的瓦片与配置
# 基镜像 digest 与 docker-compose.yml valhalla 服务保持一致
# compose 挂载 ./data/valhalla 到 /custom_files,此处原样 COPY
FROM ghcr.io/valhalla/valhalla-scripted@sha256:bb3e89c0e8d3fb35e9278e23a1f7fdbcbf76d29c1edbf7effad829e1ee26b6ed
# compose 实际使用: serve_tiles=True — 仅服务已有瓦片,不重建
# use_tiles_ignore_pbf=True: 镜像内永远无 PBF,显式跳过瓦片构建
# (compose 场景通过运行时挂载 custom_files;此处直接 COPY,不依赖 entrypoint hash 检测)
ENV serve_tiles=True \
    build_admins=True \
    build_time_zones=True \
    use_tiles_ignore_pbf=True
COPY custom_files/ /custom_files/
EXPOSE 8002
# 基镜像 CMD 为 ["build_tiles"]; 显式继承确保行为可审查
# entrypoint(/valhalla/scripts/docker-entrypoint.sh) 读取上方 ENV 决定行为:
#   serve_tiles=True + use_tiles_ignore_pbf=True → 直接服务已有瓦片,不构建
CMD ["build_tiles"]
