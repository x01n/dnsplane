# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS web
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build:ci

FROM golang:1.25-bookworm AS builder
WORKDIR /src/main
COPY main/go.mod main/go.sum ./
RUN go mod download
COPY main/ ./
COPY --from=web /build/main/web ./web
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /dnsplane .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /dnsplane /app/dnsplane
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/dnsplane"]
