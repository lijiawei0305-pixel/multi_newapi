# deploy/ops — 运维脚本

> 现网栈 `newapi_test` 的运维脚本集：备份 / 恢复 / 部署 / 回滚 / 健康巡检 + 迁移说明。
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
| app 回环端口 | `127.0.0.1:3100` |
| MySQL | app 使用 schema 专用用户 `MYSQL_APP_USER`；root 只供一次性账号 bootstrap 与备份/恢复，密码均只存服务器 `.env` |
| Redis | default 用户关闭；app/ops 使用 `REDIS_APP_USER` + `REDIS_PASSWORD` ACL；凭据不进命令行 |
| Host 头 `HOST_HEADER` | `tokendream.wedreamhub.com` |
| 备份目录 `BACKUP_DIR` / 保留 `KEEP` | `/root/backups` / `7` |
| app 镜像名 | `newapi_test-app` |
| SSH 别名（deploy.sh） | `newapi628` |

> 所有参数都可覆盖，例如：`KEEP=14 BACKUP_DIR=/data/bak ./backup.sh`。

## 各脚本职责

- **`deploy.sh`**（Mac）— 预检 → 强制发布前配对备份 → 只接受本次刚新增的唯一 manifest；若新版服务器 `backup.sh` 已留下与 manifest 名称、实际 SHA-256、完整回读证明严格绑定的 receipt，则直接复用；若首轮迁移的旧脚本没有 receipt，才从当前本地 `HEAD` 抽取 `lib.sh` + `offsite-copy.sh` 到服务器隔离 sibling，补做 age/rclone 异地上传与完整回读验真 → 保存 `:prev` → 上传可重建归档 → 只解包到全新 staging 并换树 → 记录后台构建真实退出码 → 严格验证 MySQL/Redis/app 与唯一版本。已有但绑定无效的 receipt 会 fail-closed；helper 不覆盖现役源码树，且全程复用 deploy 持有的 ops lock。只有回滚的版本/readiness 也通过才报“已回滚”。
- **`backup.sh`**（服务器）— 短暂停 app 建立停写点，生成一个 `backup-<ts>.manifest` 权威恢复集：MySQL gzip + Redis RDB + `.env`/compose/nginx/证书/版本上下文，三个产物都带 SHA-256。manifest 同时记录 Redis 快照总 key 数和永久 key 数；Redis 失败会废弃整组；配置异地目标后自动调用 `offsite-copy.sh` 加密上传并完整回读校验。
- **`offsite-copy.sh <manifest>`**（服务器）— 只接受已通过 manifest/SHA-256 校验的配对集，用 age recipient 加密后经 rclone 复制到异地，重新下载比对 SHA-256 后才原子写本地 receipt；不 source `.env`、不使用 `eval`。
- **`restore.sh <backup-*.manifest>`**（服务器，**危险·成对覆盖生产状态**）— 先校验 manifest/三个 SHA-256/gzip/tar/RDB，要求外部维护停流确认与**键入栈名**，强制 pre-backup 后配对恢复 MySQL + Redis。MySQL 会先删整库再导入，不留恢复点后新表；Redis 先以 `appendonly=no` 加载 RDB，再在活进程开 AOF、等 rewrite 成功，最后用正式 compose 重启。RDB 使用绝对过期时间，因此延迟恢复时允许 TTL key 自然减少，但永久 key 必须与新 manifest 精确守恒，转换/重启过程也不得增加总 key 或改变永久 key。任一 readiness/版本/reconcile 闸门失败都保持 app stopped、保留已验证恢复副本与 ops lock，且不自动撤维护模式。
- **`rollback.sh`**（服务器，危险）— 默认同时换回与 `:prev` 成对的镜像 + 源码/compose release，不会留下新旧混合树；`--to <YYYYMMDD-HHMMSS>` 从已绑定 SHA-256 的服务器归档解包到空 staging 再重建。服务器 release 无 `.git`，因此不支持 `--git`。两种模式都只在版本/readiness 精确通过后成功返回。
- **`healthcheck.sh`**（服务器）— app `/health/live` 进程存活 + `/health/ready` 直连 DB/已配置 Redis + app/mysql/redis 三容器 + 磁盘/内存阈值。退役 auth-service 不再是健康依赖。

## 典型操作

```bash
# ── 部署（Mac 仓库根）──────────────────────────────────────────
./deploy/ops/deploy.sh
#   只可跳过已单独跑过的本地预检：SKIP_PREFLIGHT=1 ./deploy/ops/deploy.sh
#   SKIP_BACKUP 已禁用；首次且成功查明无 mysql 容器时，可显式用 ALLOW_INITIAL_NO_BACKUP=1

# ── 以下在服务器（ssh newapi628）────────────────────────────────
cd /root/newapi-test/deploy/ops

# 手动备份
./backup.sh

# 必须异地成功才允许 backup/deploy 成功（生产必须写入 .env）
# BACKUP_OFFSITE_REQUIRED=1
# BACKUP_OFFSITE_REMOTE=s3-worm:newapi-production/backups
# BACKUP_OFFSITE_AGE_RECIPIENT=age1...

# 健康巡检（接 cron 看退出码）
./healthcheck.sh

# 回滚（回上一版成对镜像 + release 树）
./rollback.sh
#   查看/使用无 git 归档：./rollback.sh --list
#   ./rollback.sh --to 20260719-120000

# 配对恢复 DB + Redis（危险：先在 Nginx/CF 维护停流）
MAINTENANCE_CONFIRMED=1 ./restore.sh /root/backups/backup-20260719-120000.manifest
```

### 从异地副本恢复到隔离 staging（不直接覆盖生产）

把 `offsite-<ts>.receipt` 与 receipt 中的 `encrypted_file` 从对象存储下载到
一次性隔离主机，先按 `encrypted_sha256` 校验，再用**不存放在生产服务器**的 age
identity 解密到空目录：

```bash
sha256sum encrypted-file.tar.gz.age
mkdir -m 700 recovered
age --decrypt --identity /secure/offline/identity.txt encrypted-file.tar.gz.age \
  | tar --no-same-owner -xzf - -C recovered
```

随后在隔离栈用 `verify_backup_manifest`、`restore.sh`、readiness 与 `reconcile.sh`
完成整套验收并记录 RTO/RPO。不要把 age 私钥复制到生产主机，也不要把解密流
直接 pipe 到生产 restore；远端副本首次真实演练仍是外部验收项。

## crontab（服务器；示例见 go-live.md §5）

```cron
30 2 * * *  /root/newapi-test/deploy/ops/backup.sh      >> /var/log/newapi-backup.log 2>&1
*/5 * * * *  /root/newapi-test/deploy/ops/healthcheck.sh >> /var/log/newapi-health.log 2>&1
```

## 约定与注意

- 所有脚本 `set -euo pipefail`。`rollback` 二次确认可 `ASSUME_YES=1` 跳过（供 `deploy.sh` 健康失败时自动回滚）；**`restore`（整库覆盖生产）例外——必须【键入栈名】强确认，`ASSUME_YES` 无法跳过，且强制恢复前 pre-backup（失败即中止）。**
- compose 调用固化 `-p $STACK --env-file $ENV_FILE -f $COMPOSE_FILE`（绝对 `-f`，cwd 无关；对齐 RETRO「未加载 .env」教训）。
- `backup`/`restore`/`deploy`/`rollback` 共用 `/run/lock/newapi-ops.lock.d`。运维进程崩溃或部分恢复时锁会故意 fail-closed 保留；先核对 app 仍 stopped、外部维护仍在、数据/对账已收敛，再按 `owner` token 人工清锁，不得为了“让脚本能跑”直接删。
- MySQL 密码只通过 stdin 送入容器内短命 `0600` client option file；成功、失败或中断都自动删除。禁止恢复 `mysql -p<password>` / `mysqldump -p<password>` 写法，否则密码会出现在 `ps` 和 `/proc/*/cmdline`。
- `rollback.sh` 默认模式前提：`deploy.sh` 在备份通过后成对保存 `:prev` + `prev-<ts>.*` 并原子更新 `prev.current`。多版本回退使用 `/root/deploy-archives/src-<ts>.tgz` + `.version` + `.manifest.json` + `.release`；`.release` 绑定三件 SHA-256 与版本，不依赖 `.git`。
- 部署只接受干净 Git checkout，并由 `git archive HEAD` 生成上传内容；工作树 ignored/untracked 文件不会进入 release。服务器 `.env` 是换树时唯一明确保留的 release 外状态。
- config tar 用于审计/人工灾备上下文，`restore.sh` 不会自动覆盖当前 `.env`/Nginx/证书。先核对后手工恢复，避免在数据恢复脚本中意外换密钥或改入口。
- `/root/backups` 的 7 份默认保留只是**同机恢复点**。生产应设置 `BACKUP_OFFSITE_REQUIRED=1`；脚本能证明 age 加密、异地上传与完整回读摘要，但对象锁/跨账号不可变保留仍须存储端策略证明。未从远端副本实际演练恢复前，不得宣称已覆盖整机/磁盘丢失场景。
- MySQL 专用用户、Redis ACL、当前镜像 digest、8.4 LTS 隔离升级与回滚步骤见 [`data-services-hardening.md`](data-services-hardening.md)。仓库迁移路径不等于生产已执行，必须归档环境侧验收证据。

## 脚本回归测试与真实恢复演练

快速契约测试（不需要 Docker daemon）：

```bash
./deploy/ops/tests/run.sh
```

这些单元/编排契约测试只使用临时目录和替身 `docker`/`ssh`/`curl`/`systemctl`，覆盖 backup/release manifest 篡改、共享 ops lock、Redis 备份失败、备份失败阻断发布、干净 staging、RDB→AOF 调用顺序、成对 image/source 回滚与失败复原、健康端点/HTTP 308 契约、Electron 文档命令漂移，以及 ACME 校验和失败时零执行/零 unit 写入。它们不等价于真实恢复，也不会连接或改写现网服务器。

真正的成对恢复集成演练：

```bash
REQUIRE_DOCKER=1 ./deploy/ops/tests/restore-integration.sh
```

该脚本不替换 `docker` 或 `curl`，而是创建唯一 Compose project 和一次性 MySQL/Redis volumes，构建并启动真实 app，写入 MySQL marker 与 `TTL=-1` 的永久 Trial 键，执行配对 backup，破坏两边数据后再跑 `restore.sh`。演练还用真实登录请求制造恢复点后的应用写入，验证停 app 后写者被截断、恢复点后写入不残留；同时验证并发 backup 被共享 ops lock 拒绝、所有 manifest/SHA-256 可复验、readiness 与只读 reconcile 通过。退出时只删除该唯一演练 project/volumes/network 与本地构建的演练 app image，绝不使用生产栈或 VPS 路径。

`.github/workflows/restore-drill.yml` 在 Linux GitHub runner 上以 `REQUIRE_DOCKER=1` 权威执行；daemon 缺失会失败。本地未启动 Docker daemon 时脚本只明确输出 `SKIP`，不得把该结果表述成“恢复已实跑”。
