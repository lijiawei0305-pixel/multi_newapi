#!/usr/bin/env bash
# ────────────────────────────────────────────────────────────────────────────
# backup.sh —— 生产栈 newapi_test 配对恢复集（服务器上运行）。
#
# 每个 backup-<ts>.manifest 是唯一权威恢复点，它成对引用：
#   db-<ts>.sql.gz / redis-<ts>.rdb / config-<ts>.tar.gz，并记录版本与 SHA-256。
#
# 为保证 MySQL/Redis 在同一逻辑时间线，本脚本会暂停 app（唯一写者）后再快照。
# 普通备份结束后恢复原 app 状态；restore.sh 以 KEEP_APP_STOPPED=1 调用，
# 使恢复全程一直停写。Redis 保存无 DB 后备的 Trial 权威键，故其备份失败
# 必须让整个备份失败，不存在“DB 为权威、跳过 Redis”模式。
#
# 用法：
#   ./backup.sh
#   KEEP=14 BACKUP_DIR=/data/bak ./backup.sh
# ─────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
umask 077
guard_target
require docker
require gzip
require sha256sum
require tar
require_db_pass
ensure_backup_dir
acquire_ops_lock backup

TS="$(date +%Y%m%d-%H%M%S)"
DB_OUT="$BACKUP_DIR/db-$TS.sql.gz"
REDIS_OUT="$BACKUP_DIR/redis-$TS.rdb"
CFG_OUT="$BACKUP_DIR/config-$TS.tar.gz"
MANIFEST_OUT="$BACKUP_DIR/backup-$TS.manifest"
MANIFEST_TMP="$BACKUP_DIR/.backup-$TS.manifest.tmp"
for output in "$DB_OUT" "$REDIS_OUT" "$CFG_OUT" "$MANIFEST_OUT" "$MANIFEST_TMP"; do
  if [ -e "$output" ]; then
    release_ops_lock || true
    die "同秒备份产物已存在，拒绝覆盖：$output"
  fi
done
TMPD=""
APP_CID=""
APP_WAS_RUNNING=0
BACKUP_COMPLETE=0

cleanup() {
  local rc=$?
  trap - EXIT
  set +e
  # 先恢复最重要的 app 运行状态；临时文件清理失败不得跳过这一步。
  if [ "$APP_WAS_RUNNING" = "1" ] && [ "${KEEP_APP_STOPPED:-0}" != "1" ]; then
    if dc start "$APP_SVC" >/dev/null 2>&1; then
      ok "app 已恢复到备份前的 running 状态"
    else
      warn "备份已产生，但 app 恢复启动失败；必须人工处理"
      rc=1
    fi
  fi
  [ -z "$TMPD" ] || rm -rf -- "$TMPD" || rc=1
  rm -f -- "$MANIFEST_TMP" || rc=1
  if [ "$BACKUP_COMPLETE" != "1" ]; then
    rm -f -- "$DB_OUT" "$REDIS_OUT" "$CFG_OUT" "$MANIFEST_OUT" || rc=1
  fi
  release_ops_lock || rc=1
  exit "$rc"
}
trap cleanup EXIT
TMPD="$(mktemp -d "$BACKUP_DIR/.backup-config-$TS.XXXXXX")"

# ── 0) 停写：app 是定时对账/支付回调/计费的唯一进程写者 ───────────────────────
APP_CID="$(service_container_id "$APP_SVC")"
[ -n "$APP_CID" ] || die "找不到 $APP_SVC 容器，无法证明写者已停止"
APP_IMAGE_ID="$(docker inspect -f '{{.Image}}' "$APP_CID")"
[ -n "$APP_IMAGE_ID" ] || die "无法读取 $APP_SVC 容器的不可变镜像 ID"
[ -s "$SERVER_REPO/VERSION" ] || die "缺少非空 VERSION，拒绝写入伪造的 unknown 备份身份"
APP_VERSION="$(tr -d '\r\n' < "$SERVER_REPO/VERSION")"
printf '%s\n' "$APP_VERSION" | grep -Eq '^[A-Za-z0-9._-]+$' || die "VERSION 含非法字符：$APP_VERSION"
APP_IMAGE_VERSION="$(docker run --rm "$APP_IMAGE_ID" --version 2>/dev/null | tr -d '\r\n')" \
  || die "无法从运行容器的不可变镜像读取版本"
[ "$APP_IMAGE_VERSION" = "$APP_VERSION" ] \
  || die "源码 VERSION=$APP_VERSION 与运行制品=$APP_IMAGE_VERSION 不一致，拒绝生成误导恢复集"
if container_running "$APP_CID"; then
  APP_WAS_RUNNING=1
  log "0/4 暂停 $APP_SVC，建立 MySQL/Redis 同一停写时间线…"
  dc stop "$APP_SVC" >/dev/null
fi
if container_running "$APP_CID"; then
  die "$APP_SVC 仍在运行，拒绝生成伪“配对”备份"
fi
db_ready || die "MySQL 未就绪，无法备份"
redis_ready || die "Redis 未就绪，无法备份"

# ── 1) MySQL：停写窗口内的一致事务快照 ─────────────────────────────────────────
log "1/4 mysqldump $DB_NAME → $DB_OUT"
mysql_with_secret mysqldump -u"$DB_USER" \
    --single-transaction --quick --routines --triggers --events \
    --databases "$DB_NAME" \
  | gzip > "$DB_OUT"
[ "$(file_size "$DB_OUT")" -gt 100 ] || die "mysqldump 产物异常小：$DB_OUT"
gzip -t "$DB_OUT" || die "mysqldump gzip 完整性校验失败：$DB_OUT"
ok "DB 备份完成（$(du -h "$DB_OUT" | cut -f1)）"

# ── 2) Redis：SAVE 后拷出可独立恢复的 RDB ───────────────────────────────────────────
log "2/4 redis SAVE → $REDIS_OUT"
REDIS_CID="$(service_container_id "$REDIS_SVC")"
[ -n "$REDIS_CID" ] || die "找不到 $REDIS_SVC 容器"
REDIS_IMAGE_ID="$(docker inspect -f '{{.Image}}' "$REDIS_CID")"
[ -n "$REDIS_IMAGE_ID" ] || die "无法读取 Redis 不可变镜像 ID"
dc exec -T "$REDIS_SVC" redis-cli SAVE >/dev/null
dc exec -T "$REDIS_SVC" redis-check-rdb /data/dump.rdb >/dev/null \
  || die "Redis 容器内 dump.rdb 校验失败"
REDIS_KEYS="$(redis_key_count_service)"
printf '%s\n' "$REDIS_KEYS" | grep -Eq '^[0-9]+$' || die "无法读取 Redis key 总数"
dc cp "$REDIS_SVC:/data/dump.rdb" "$REDIS_OUT" \
  || die "Redis dump.rdb 拷出失败；Trial 权威键无 DB 后备，整个备份已中止"
[ "$(file_size "$REDIS_OUT")" -gt 0 ] || die "Redis 备份为空：$REDIS_OUT"
docker run --rm \
  -v "$BACKUP_DIR:/backup:ro" \
  --entrypoint redis-check-rdb "$REDIS_IMAGE_ID" "/backup/$(basename "$REDIS_OUT")" >/dev/null \
  || die "Redis RDB 拷出后校验失败：$REDIS_OUT"
ok "Redis 备份完成（$(du -h "$REDIS_OUT" | cut -f1)）"

# ── 3) 配置与版本上下文 ───────────────────────────────────────────────────────────────────
log "3/4 打包配置/版本 → $CFG_OUT"
[ -f "$ENV_FILE" ] || die "缺少生产 env：$ENV_FILE"
[ -f "$COMPOSE_FILE" ] || die "缺少 production compose：$COMPOSE_FILE"
[ -f "$NGINX_VHOST" ] || die "缺少 nginx vhost：$NGINX_VHOST"
[ -d "$NGINX_CERT_DIR" ] || die "缺少 nginx 证书目录：$NGINX_CERT_DIR"
install -m 600 "$ENV_FILE" "$TMPD/env"
install -m 600 "$COMPOSE_FILE" "$TMPD/compose.yml"
install -m 600 "$NGINX_VHOST" "$TMPD/nginx.vhost.conf"
cp -a "$NGINX_CERT_DIR" "$TMPD/nginx.cert"
install -m 600 "$SERVER_REPO/VERSION" "$TMPD/VERSION"
if [ -s "$SERVER_REPO/deploy-manifest.json" ]; then
  install -m 600 "$SERVER_REPO/deploy-manifest.json" "$TMPD/deploy-manifest.json"
fi
tar czf "$CFG_OUT" -C "$TMPD" .
[ "$(file_size "$CFG_OUT")" -gt 0 ] || die "config 备份为空：$CFG_OUT"
tar tzf "$CFG_OUT" >/dev/null || die "config tar 完整性校验失败：$CFG_OUT"
ok "配置备份完成（$(du -h "$CFG_OUT" | cut -f1)）"

# ── 4) manifest 最后原子就位；没有 manifest 就不是可恢复集 ─────────────────────────────
log "4/4 生成配对 manifest + SHA-256 → $MANIFEST_OUT"
{
  printf 'format=%s\n' "$BACKUP_FORMAT"
  printf 'timestamp=%s\n' "$TS"
  printf 'created_at_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'stack=%s\n' "$STACK"
  printf 'db_name=%s\n' "$DB_NAME"
  printf 'consistency=writers-stopped\n'
  printf 'app_version=%s\n' "$APP_VERSION"
  printf 'redis_keys=%s\n' "$REDIS_KEYS"
  printf 'db_file=%s\n' "$(basename "$DB_OUT")"
  printf 'db_sha256=%s\n' "$(sha256_file "$DB_OUT")"
  printf 'redis_file=%s\n' "$(basename "$REDIS_OUT")"
  printf 'redis_sha256=%s\n' "$(sha256_file "$REDIS_OUT")"
  printf 'config_file=%s\n' "$(basename "$CFG_OUT")"
  printf 'config_sha256=%s\n' "$(sha256_file "$CFG_OUT")"
} > "$MANIFEST_TMP"
mv "$MANIFEST_TMP" "$MANIFEST_OUT"
chmod 600 "$DB_OUT" "$REDIS_OUT" "$CFG_OUT" "$MANIFEST_OUT"
verify_backup_manifest "$MANIFEST_OUT"
BACKUP_COMPLETE=1
if [ "${SKIP_PRUNE:-0}" != "1" ]; then prune_backup_sets; fi

ok "配对备份集完成：$MANIFEST_OUT（保留最近 $KEEP 组）"
warn "config 备份含 .env/证书私钥；产物已设 600，仍须仅存 root 可访问且定期异地加密备份"
