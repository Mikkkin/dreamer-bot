# syntax=docker/dockerfile:1
#
# Multi-arch image (linux/amd64, linux/arm64). Both build stages run on the
# build host's native platform: the Mini App bundle is architecture-neutral
# and Go cross-compiles, so no QEMU emulation is needed.

ARG BUN_IMAGE=oven/bun:1.4.2-alpine@sha256:d888c0ae6c86d7866ff10c5aafdd9077b36aee6455b33dd270fb93c0dd5cef6f
ARG GO_IMAGE=golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

# ---- Mini App (React + Vite) ------------------------------------------------
FROM --platform=$BUILDPLATFORM ${BUN_IMAGE} AS web
WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# ---- Go binary --------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/dist ./internal/webapp/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w -buildid= -X main.version=${VERSION}" -o /out/dreamer ./cmd/dreamer \
 && mkdir -p /out/data

# ---- Runtime: no shell, no package manager, non-root ------------------------
FROM ${RUNTIME_IMAGE}
LABEL org.opencontainers.image.title="dreamer-bot" \
      org.opencontainers.image.description="Private Telegram wishlist and recipes bot with a Mini App" \
      org.opencontainers.image.source="https://github.com/Mikkkin/dreamer-bot"
COPY --from=build /out/dreamer /app/dreamer
# A named volume mounted here inherits this ownership on first use.
COPY --from=build --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data HTTP_ADDR=:8080
USER 65532:65532
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/app/dreamer", "healthcheck"]
ENTRYPOINT ["/app/dreamer"]
