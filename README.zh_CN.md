# New API

新一代大模型网关与 AI 资产管理系统。一次部署即可通过统一 API 接入多家模型服务，并提供用户管理、用量记账和管理后台。

仅用于合法、已获授权的网关与私有部署。请合法取得上游 API 密钥和模型权限，并遵守上游条款与适用法律。

英文说明见 [README.md](./README.md)。

## 安装

需要 Docker 与 Docker Compose；改动 Web 时还需要 Go 1.26.8（以 `go.mod` 为准）和 [Bun](https://bun.sh)。

```bash
git clone https://github.com/QuantumNous/new-api.git
cd new-api
cp .env.example .env
openssl rand -hex 32   # 写入 POSTGRES_PASSWORD
openssl rand -hex 32   # 写入另一个值到 REDIS_PASSWORD
```

`.env.example` 故意把密钥留空。在本地填写 `.env`，不要提交该文件。

## 运行

`docker-compose.yml` 是本地 HTTP 快速启动模板，监听 `http://localhost:3000`，并显式进入本地开发模式。不要把该模式暴露到公网。

```bash
docker compose up -d
```

同样的本地模式也可以单容器运行，数据放在 `./data`（SQLite）：

```bash
docker run --name new-api -d --restart always \
  -p 3000:3000 \
  -e DEPLOYMENT_ENV=development \
  -e SESSION_COOKIE_SECURE=false \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  calciumion/new-api:latest
```

## 基本配置

| 变量 | 说明 |
| --- | --- |
| `DEPLOYMENT_ENV` | 安全模式。未设置或无法识别的值按 `production` 处理。本地源码或 HTTP 开发必须显式设为 `development`。 |
| `SESSION_COOKIE_SECURE` | 会话 Cookie 仅通过 HTTPS 发送。生产环境必须为 `true`。仅当同时显式设置本地 `DEPLOYMENT_ENV=development` 时才使用 `false`。 |
| `SESSION_SECRET` | 会话签名密钥。生产环境必填，用 `openssl rand -hex 32` 生成。 |
| `CRYPTO_SECRET` | 加密与 HMAC 密钥。生产环境必填，且必须与 `SESSION_SECRET` 不同。 |
| `SQL_DSN` | 不使用 Compose 托管数据库时的连接串。 |
| `REDIS_CONN_STRING` | 不使用 Compose 托管 Redis 时的连接串。 |

省略 `DEPLOYMENT_ENV` 不会退回不安全的开发模式。通过 HTTP 访问的本地源码安装必须显式设置 `DEPLOYMENT_ENV=development`。面向公网的部署必须保持生产模式、终止 TLS、设置 `SESSION_COOKIE_SECURE=true`，并提供两把不同的随机 `SESSION_SECRET` 与 `CRYPTO_SECRET`。不要把这些密钥提交进仓库。

其余本地模板见 `.env.example`。

## 开发与测试

在仓库根目录执行：

```bash
bash scripts/preflight.sh
bash deploy/ops/tests/run.sh
bash scripts/check-docker-context-secrets.sh
```

`scripts/preflight.sh` 是本地、CI 与发布共用的门禁。`PREFLIGHT_SCOPE` 可以是 `all`（默认）、`backend`、`default`、`classic`、`orbit` 或 `electron`。Docker 构建上下文检查也包含在该脚本的 backend 范围里。

默认前端：

```bash
cd web/default
bun install --frozen-lockfile
bun run dev
```

## 许可证

本项目采用 [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE)。AGPLv3 第 7 条的附加条款见 [NOTICE](./NOTICE)。

修改版本必须在适当法律声明，以及用户界面中显著的关于、法律、页脚或署名位置，保留作者归属声明 `Frontend design and development by New API contributors.`。

提供用户界面的修改版本还必须保留指向原项目的可见链接：<https://github.com/QuantumNous/new-api>。

第三方依赖声明见 [THIRD-PARTY-LICENSES.md](./THIRD-PARTY-LICENSES.md)。

本程序基于 [One API](https://github.com/songquanpeng/one-api)（MIT License）开发。

Copyright (c) QuantumNous and contributors.
