FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder

WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
RUN bun install --frozen-lockfile
COPY ./web/default ./default
COPY ./VERSION /build/VERSION
# 版本身份（C6/#12）：VERSION 曾为 0 字节 → 注入空串。此处若为空则回落到确定的占位串，
# 绝不让空串覆盖版本身份。正常部署由 deploy.sh 在服务器把 VERSION 盖成真实版本串。
RUN VER="$(cat /build/VERSION)"; [ -n "$VER" ] || VER="v0.0.0-unknown"; \
    cd default && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$VER" bun run build

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder-classic

WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
RUN bun install --filter ./classic --frozen-lockfile
COPY ./web/classic ./classic
COPY ./VERSION /build/VERSION
RUN VER="$(cat /build/VERSION)"; [ -n "$VER" ] || VER="v0.0.0-unknown"; \
    cd classic && VITE_REACT_APP_VERSION="$VER" bun run build

FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder2
ENV GO111MODULE=on CGO_ENABLED=0

ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

ADD go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=builder /build/web/default/dist ./web/default/dist
COPY --from=builder-classic /build/web/classic/dist ./web/classic/dist
# 版本身份（C6/#12）：空 VERSION 不得把 common.Version 覆盖成空串（否则 /api/status version=""）。
RUN VER="$(cat VERSION)"; [ -n "$VER" ] || VER="v0.0.0-unknown"; \
    go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=${VER}'" -o new-api

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
COPY LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/
# 版本身份（C6/#12）：把部署清单烤进镜像，令「跑的是哪份代码」可 `docker exec <app> cat /deploy-manifest.json` 直接回答，
# 不依赖服务器 .git（服务器是非 git 的 rsync 副本）。仓库根有占位版；deploy.sh 在构建前把它盖成真实清单。
COPY deploy-manifest.json /deploy-manifest.json
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
