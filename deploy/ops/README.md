# deploy/ops — 运维脚本

> 测试栈 `newapi_test`（即将正式化）的运维脚本集：备份 / 恢复 / 部署 / 回滚 / 健康巡检 + 迁移说明。
> **护栏**（2026-07-03 单栈收敛后已重定义）：`newapi_test` 是【唯一现网 / 生产栈】——原 stock 栈 `newapi_YFNf`（:3000）已删除。每个入口 `guard_target`（旧名 `guard_not_prod` 保留为兼容别名）做**正向白名单**校验：确认目标确为 `newapi_test`，挡拼写/误配。破坏性操作（`restore` 整库覆盖）另需【键入栈名】强确认。
> 上线手册见 [`go-live.md`](go-live.md)；迁移机制见 [`migrate-note.md`](migrate-note.md)。

## 运行位置

| 脚本 | 运行位置 | 说明 |
| --- | --- | --- |
| `deploy.sh` | **Mac 仓库根** | 编排：预检在本机、构建/部署 SSH 到服务器（W4） |
| `backup.sh` `restore.sh` `rollback.sh` `healthcheck.sh` | **服务器** | 直接操作本地 Docker/MySQL/文件 |
| `lib.sh` | （被 source） | 公共参数 + 助手，服务器脚本共用 |

## 部署事实（脚本默认值，可用环境变量覆盖）

| 项 | 值 |
| --- | --- |
| compose 项目名 `STACK` | `newapi_test` |
| 服务器仓库 `SERVER_REPO` | `/root/newapi-test` |
| compose 文件 `COMPOSE_FILE` | `$SERVER_REPO/deploy/docker-compose.test.yml` |
| env 文件 `ENV_FILE` | `$SERVER_REPO/.env`（含上游 Key，600，不入库） |
| app / auth 回环端口 | `127.0.0.1:3100` / `127.0.0.1:8180` |
| MySQL | 库 `new-api-test`，root / `testpass123` |
| Host 头 `HOST_HEADER` | `tokendream.wedreamhub.com` |
| 备份目录 `BACKUP_DIR` / 保留 `KEEP` | `/root/backups` / `7` |
| 镜像名 | `newapi_test-app` / `newapi_test-auth-service` |
| SSH 别名（deploy.sh） | `newapi628` |

> 所有参数都可覆盖，例如：`KEEP=14 BACKUP_DIR=/data/bak ./backup.sh`。

## 各脚本职责

- **`deploy.sh`**（Mac）— 一键部署：①本地 `scripts/preflight.sh` 预检 → ②打 git tag `deploy-<ts>` → ③服务器存当前镜像为 `:prev`（回滚用）→ ④部署前 `backup.sh` → ⑤`COPYFILE_DISABLE=1` tar-over-ssh 上传 → ⑥后台 `up -d --build`（nohup，避免长构建 ssh 断流误判）→ ⑦轮询 `/api/status` 健康 → ⑧失败自动 `rollback.sh`。
- **`backup.sh`**（服务器）— `mysqldump --single-transaction` 一致快照(gzip) + redis `SAVE` 拷 `dump.rdb` + `.env`/compose/nginx vhost 打包；带时间戳落 `/root/backups`，各类保留最近 `KEEP` 份。
- **`restore.sh <db-*.sql.gz>`**（服务器，**危险·整库覆盖生产**）— 覆盖恢复【生产】库 `new-api-test`；**键入栈名**强确认（`ASSUME_YES` 不可跳过）+ 恢复前**强制** pre-backup（失败即中止，绝不无保险覆盖）。
- **`rollback.sh`**（服务器，危险）— 默认把 `:prev` 镜像打回 `:latest` 并 `force-recreate`（**不重建**，秒级）；`--git <tag>` 走源码回退后重建。
- **`healthcheck.sh`**（服务器）— app `/api/status`(带 Host) + auth `/auth/healthz` + 四容器 running + 磁盘/内存阈值；失败退出码=失败数（cron 友好），可选 `ALERT_WEBHOOK` 告警。

## 典型操作

```bash
# ── 部署（Mac 仓库根）──────────────────────────────────────────
./deploy/ops/deploy.sh
#   跳过预检/备份：SKIP_PREFLIGHT=1 SKIP_BACKUP=1 ./deploy/ops/deploy.sh

# ── 以下在服务器（ssh newapi628）────────────────────────────────
cd /root/newapi-test/deploy/ops

# 手动备份
./backup.sh

# 健康巡检（接 cron 看退出码）
./healthcheck.sh

# 回滚（秒级，回上一版镜像）
./rollback.sh
#   源码层回退：./rollback.sh --git deploy-20260629-1200

# 恢复 DB（危险，二次确认）
./restore.sh /root/backups/db-20260629-023000.sql.gz
```

## crontab（服务器；示例见 go-live.md §5）

```cron
30 2 * * *  /root/newapi-test/deploy/ops/backup.sh      >> /var/log/newapi-backup.log 2>&1
*/5 * * * *  /root/newapi-test/deploy/ops/healthcheck.sh >> /var/log/newapi-health.log 2>&1
```

## 约定与注意

- 所有脚本 `set -euo pipefail`。`rollback` 二次确认可 `ASSUME_YES=1` 跳过（供 `deploy.sh` 健康失败时自动回滚）；**`restore`（整库覆盖生产）例外——必须【键入栈名】强确认，`ASSUME_YES` 无法跳过，且强制恢复前 pre-backup（失败即中止）。**
- compose 调用固化 `-p $STACK --env-file $ENV_FILE -f $COMPOSE_FILE`（绝对 `-f`，cwd 无关；对齐 RETRO「未加载 .env」教训）。
- `rollback.sh` 镜像模式前提：部署前存过 `:prev`（`deploy.sh` 自动做）；`--git` 模式要求 `$SERVER_REPO` 是 git 工作副本。
- 上传默认排除 `.git`/`node_modules`/`web/*/dist`/`.DS_Store`/`._*`（dist 由服务器 Dockerfile 内 bun 构建）。
