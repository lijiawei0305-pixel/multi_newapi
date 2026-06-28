# syntax=docker/dockerfile:1
#
# 多阶段构建：前端(web/dist) + Go 服务器(cmd/server) -> 单一静态镜像。
# 产物：/server（静态二进制）+ /web/dist（SPA 静态资源），由 server 同源服务。

# ---- stage 1: 构建前端，产出 web/dist ----
FROM node:20-alpine AS web
WORKDIR /web
# 用 corepack 提供 pnpm；pin 到 9.x 以兼容 pnpm-lock.yaml（不匹配则下方回退非 frozen 安装）。
RUN corepack enable && corepack prepare pnpm@9 --activate
# 先装依赖（利用层缓存）：仅 manifest 变化才重装。
COPY web/package.json web/pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install
# 再拷源码并构建（web/node_modules、web/dist 已被 .dockerignore 排除）。
COPY web/ ./
RUN pnpm build

# ---- stage 2: 编译 Go 服务器 ----
FROM golang:1.26 AS gobuild
WORKDIR /src
# 先拉依赖（利用层缓存）。
COPY go.mod go.sum ./
RUN go mod download
# 再拷其余源码并静态编译（CGO 关，便于 distroless static 运行）。
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# ---- final: 最小运行镜像 ----
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=gobuild /server /server
COPY --from=web /web/dist /web/dist
ENV PORT=3000 \
    WEB_DIST=/web/dist \
    GIN_MODE=release
EXPOSE 3000
USER nonroot:nonroot
CMD ["/server"]
