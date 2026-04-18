#!/usr/bin/env bash
# 多架构 Docker 镜像一键构建与（可选）推送
# 用法：
#   scripts/docker-build.sh                      # 仅本地构建 linux/amd64,linux/arm64（不 push）
#   scripts/docker-build.sh --push v1.2.3        # 构建并打 tag 推送
#   IMAGE=docker.cnb.cool/neko_kernel/dnsplane \
#   scripts/docker-build.sh --push v1.2.3        # 指定镜像名
#
# 需要：
#   - Docker 20.10+ 且启用 buildx；首次会自动创建 buildx builder
#   - 若传 --push，调用方需已 docker login 到目标 registry
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

IMAGE="${IMAGE:-docker.cnb.cool/neko_kernel/dnsplane}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
BUILDER="${BUILDER:-dnsplane-multiarch}"

PUSH=0
TAG="dev"
while [ $# -gt 0 ]; do
  case "$1" in
    --push) PUSH=1; shift ;;
    --platform) PLATFORMS="$2"; shift 2 ;;
    -h|--help)
      sed -n '1,20p' "$0"; exit 0 ;;
    *)
      TAG="$1"; shift ;;
  esac
done

# 若未显式传 tag，从 git 推导
if [ "$TAG" = "dev" ]; then
  TAG="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi

# 确保 buildx builder 存在且已 bootstrap
if ! docker buildx inspect "${BUILDER}" >/dev/null 2>&1; then
  echo "==> creating buildx builder ${BUILDER}"
  docker buildx create --use --name "${BUILDER}" --driver docker-container
fi
docker buildx use "${BUILDER}"
docker buildx inspect --bootstrap >/dev/null

COMMON_ARGS=(
  --platform "${PLATFORMS}"
  --tag "${IMAGE}:${TAG}"
  --tag "${IMAGE}:latest"
  --label "org.opencontainers.image.source=https://cnb.cool/Neko_Kernel/DNSPlane"
  --label "org.opencontainers.image.version=${TAG}"
  --label "org.opencontainers.image.revision=$(git rev-parse HEAD 2>/dev/null || echo unknown)"
)

if [ "$PUSH" = "1" ]; then
  echo "==> building and pushing ${IMAGE}:${TAG} for ${PLATFORMS}"
  docker buildx build "${COMMON_ARGS[@]}" --push .
else
  # 多平台不能同时 --load（Docker 限制），仅 amd64 时 load，否则走 cache（无导出）
  if [ "${PLATFORMS}" = "linux/$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')" ]; then
    echo "==> building and loading ${IMAGE}:${TAG} (${PLATFORMS})"
    docker buildx build "${COMMON_ARGS[@]}" --load .
  else
    echo "==> building (no push, no load) ${IMAGE}:${TAG} for ${PLATFORMS}"
    echo "    多架构镜像默认无法本地 --load；如需 push 请加 --push <tag>"
    docker buildx build "${COMMON_ARGS[@]}" .
  fi
fi

echo "==> done."
