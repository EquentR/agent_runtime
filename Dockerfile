# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /src

RUN corepack enable && corepack prepare pnpm@10.26.0 --activate
COPY webapp/package.json webapp/pnpm-lock.yaml ./webapp/
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm config set store-dir /pnpm/store && \
    pnpm --dir webapp install --frozen-lockfile
COPY webapp ./webapp
RUN pnpm --dir webapp build

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/webapp/dist ./app/router/embedded_web

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG GIT_COMMIT=none

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
      -tags embed_web \
      -trimpath \
      -ldflags "-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT}" \
      -o /out/ice_art \
      ./cmd/ice_art

RUN mkdir -p /out/runtime/data /out/runtime/logs && \
    go run ./scripts/releasepack -source /src -dest /out/runtime

FROM debian:bookworm
WORKDIR /app

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/*

USER root

COPY --from=build /out/ice_art /app/ice_art
COPY --from=build /out/runtime/ /app/

ENV GIN_MODE=release \
    TZ=Asia/Shanghai
EXPOSE 18080
VOLUME ["/app/data", "/app/logs", "/app/workspace"]

ENTRYPOINT ["/app/ice_art", "-config", "/app/conf/app.yaml"]
