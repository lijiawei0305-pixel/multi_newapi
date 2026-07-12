#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# backup.sh — 测试栈 newapi_test 一致性备份（**服务器上运行**）。
#
#   产物（带时间戳，落 ${BACKUP_DIR}，默认 /root/backups）：
#     db-<ts>.sql.gz       mysqldump(new-api-test) gzip，--single-transaction 一致快照
#     redis-<ts>.rdb       redis SAVE 后拷出的 dump.rdb
#     config-<ts>.tar.gz   .env + compose 文件 + nginx vhost（恢复部署上下文）
#   保留最近 $KEEP 份（默认 7），自动清理更旧的。
#
# 用法：
#   ./backup.sh                       # 用默认参数备份测试栈
#   KEEP=14 ./backup.sh               # 保留 14 份
#   BACKUP_DIR=/data/bak ./backup.sh  # 改备份目录
#
# cron（每日 02:30）：
#   30 2 * * * /root/newapi-test/deploy/ops/backup.sh >> /var/log/newapi-backup.log 2>&1
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
guard_not_prod
require docker
ensure_backup_dir

TS="$(date +%Y%m%d-%H%M%S)"
DB_OUT="$BACKUP_DIR/db-$TS.sql.gz"
REDIS_OUT="$BACKUP_DIR/redis-$TS.rdb"
CFG_OUT="$BACKUP_DIR/config-$TS.tar.gz"

# ── 1) MySQL：容器内 mysqldump → 宿主 gzip ────────────────────────────────────
# --single-transaction：InnoDB 一致快照，不锁表（边备份边服务）。
# --databases：dump 内含 CREATE DATABASE IF NOT EXISTS + USE，restore 自给自足。
# -T：禁用 TTY，便于管道/cron。库名含连字符，--databases 后单参数安全。
log "1/3 mysqldump $DB_NAME → $DB_OUT"
dc exec -T "$MYSQL_SVC" \
  mysqldump -u"$DB_USER" -p"$DB_PASS" \
    --single-transaction --quick --routines --triggers --events \
    --databases "$DB_NAME" \
  | gzip > "$DB_OUT"
# 体积健全性：dump 不应为空（gzip 空流也有几十字节，<100B 视为失败）。
[ "$(stat -f%z "$DB_OUT" 2>/dev/null || stat -c%s "$DB_OUT")" -gt 100 ] \
  || die "mysqldump 产物异常小，疑似失败：$DB_OUT"
ok "DB 备份完成（$(du -h "$DB_OUT" | cut -f1)）"

# ── 2) Redis：SAVE 落盘后拷出 dump.rdb ────────────────────────────────────────
# 测试栈 redis 无密码；SAVE 同步落盘（数据量小可接受阻塞），再 cp 出容器。
log "2/3 redis SAVE → $REDIS_OUT"
dc exec -T "$REDIS_SVC" redis-cli SAVE >/dev/null
if dc cp "$REDIS_SVC:/data/dump.rdb" "$REDIS_OUT" 2>/dev/null; then
  ok "Redis 备份完成（$(du -h "$REDIS_OUT" | cut -f1)）"
else
  warn "redis dump.rdb 拷出失败（可能未持久化/无 /data 卷）；跳过，DB 为权威源。"
  rm -f "$REDIS_OUT"
fi

# ── 3) 配置：.env + compose + nginx vhost 打包 ────────────────────────────────
log "3/3 打包配置 → $CFG_OUT"
TMPD="$(mktemp -d)"; trap 'rm -rf "$TMPD"' EXIT
cp -a "$ENV_FILE"      "$TMPD/env"            2>/dev/null || warn "未找到 $ENV_FILE"
cp -a "$COMPOSE_FILE"  "$TMPD/compose.yml"    2>/dev/null || warn "未找到 $COMPOSE_FILE"
cp -a "$NGINX_VHOST"   "$TMPD/nginx.vhost.conf" 2>/dev/null || warn "未找到 ${NGINX_VHOST}（宝塔 nginx vhost）"
cp -a "$NGINX_CERT_DIR" "$TMPD/nginx.cert"      2>/dev/null || warn "未找到 ${NGINX_CERT_DIR}（通配 CF Origin CA 证书目录）"
tar czf "$CFG_OUT" -C "$TMPD" .
ok "配置备份完成（$(du -h "$CFG_OUT" | cut -f1)）—— 含通配 vhost + CF Origin CA 证书"
warn "config 备份内含证书私钥与 .env，仅存服务器 root 目录、勿入库/外传"

# ── 4) 保留策略：各类各留最近 $KEEP 份 ────────────────────────────────────────
prune_keep "$BACKUP_DIR/db-*.sql.gz"
prune_keep "$BACKUP_DIR/redis-*.rdb"
prune_keep "$BACKUP_DIR/config-*.tar.gz"

ok "全部备份完成 @ $BACKUP_DIR （保留最近 $KEEP 份）"
ls -lh "$BACKUP_DIR" | grep -- "-$TS" || true
