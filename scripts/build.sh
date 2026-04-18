#!/usr/bin/env bash
# 一键本地编译：前端 Next.js → main/web/，后端 Go 交叉编译 → dist/
# 用法：
#   scripts/build.sh                    # 默认本机架构
#   scripts/build.sh amd64              # 指定单架构
#   scripts/build.sh amd64 arm64        # 多架构一次性出
#   SKIP_FRONTEND=1 scripts/build.sh    # 跳过前端（仅改后端时用）
#
# 前置条件：
#   - Go 1.26.2+（本机）
#   - Node 22+ 或 Bun（如 SKIP_FRONTEND=1 可不装）
#   - 仓库已 vendor（直接使用 -mod=vendor）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ARCHS=("$@")
if [ "${#ARCHS[@]}" -eq 0 ]; then
  # 本机默认架构：go env GOARCH 已反映 amd64/arm64
  ARCHS=("$(go env GOARCH)")
fi

# 1) 前端
if [ "${SKIP_FRONTEND:-0}" != "1" ]; then
  echo "==> building frontend"
  pushd web >/dev/null
    if command -v bun >/dev/null 2>&1 && [ -f bun.lock ]; then
      bun install --frozen-lockfile
      bun run build:ci
    else
      npm ci
      npm run build:ci
    fi
  popd >/dev/null
else
  echo "==> SKIP_FRONTEND=1, reusing existing main/web/"
fi

# 2) 后端交叉编译
mkdir -p dist
# 版本号优先级：环境变量 VERSION > 仓库根 VERSION 文件 > git describe > dev
if [ -z "${VERSION:-}" ]; then
  if [ -f VERSION ]; then
    VERSION="$(tr -d '[:space:]' < VERSION)"
  else
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
  fi
fi
LDFLAGS="-s -w -X main.Version=${VERSION}"

for ARCH in "${ARCHS[@]}"; do
  OUT="dist/dnsplane-linux-${ARCH}"
  echo "==> building backend linux/${ARCH} -> ${OUT}"
  (
    cd main
    CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" \
      go build -mod=vendor -trimpath -buildvcs=false \
      -ldflags="${LDFLAGS}" \
      -o "../${OUT}" .
  )
done

# 3) 校验和
pushd dist >/dev/null
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum dnsplane-linux-* > SHA256SUMS
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 dnsplane-linux-* > SHA256SUMS
  fi
  ls -lh
popd >/dev/null

echo "==> done. artifacts in dist/"
