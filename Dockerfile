# syntax=docker/dockerfile:1

# ---- 构建阶段 ----
# 在构建机原生架构上交叉编译目标架构，避免 Go 编译走 QEMU 模拟
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG QUAD_VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
# BuildKit cache mount：跨构建复用 Go module 缓存，不进入最终镜像
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -buildvcs=false \
    -ldflags="-s -w -X quad/internal/cli.Version=${QUAD_VERSION}" \
    -o /src/quad .

# ---- 运行阶段 ----
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S quad \
    && adduser -u 10001 -S -G quad -h /home/quad quad \
    && mkdir -p /data \
    && chown quad:quad /data

COPY --from=build /src/quad /usr/local/bin/quad

ENV QUAD_HOME=/data
WORKDIR /data
USER quad

EXPOSE 8787
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8787/healthz || exit 1

ENTRYPOINT ["quad"]
CMD ["serve", "--addr", "0.0.0.0:8787"]
