# syntax=docker/dockerfile:1
# 前端只在 BUILDPLATFORM 上构建一次，避免多架构并行 npm ci 拖垮网络/超时；Go 在宿主编译器上按 TARGET* 交叉编译。

FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm config set fetch-retries 5 && npm config set fetch-retry-mintimeout 20000 && npm config set fetch-retry-maxtimeout 120000
RUN npm ci
COPY web/ ./
RUN npm run build:ci

FROM --platform=$BUILDPLATFORM golang:1.26.2-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
WORKDIR /src/main
# 依赖已 vendor 入库（main/vendor），无需 go mod download，也无需网络
COPY main/ ./
COPY --from=web /build/main/web ./web
RUN set -eux; \
  export CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}"; \
  if [ "${TARGETARCH}" = "arm" ] && [ -n "${TARGETVARIANT:-}" ]; then \
    export GOARM="${TARGETVARIANT#v}"; \
  fi; \
  go build -mod=vendor -buildvcs=false -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o /dnsplane .

FROM --platform=$TARGETPLATFORM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /dnsplane /app/dnsplane
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/dnsplane"]
