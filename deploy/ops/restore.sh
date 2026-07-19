#!/usr/bin/env bash
# restore.sh —— 从 backup.sh 配对 manifest 精确恢复 MySQL + Redis。
#
# 用法：
#   # 先在 Nginx/CF 将新请求与回调维护停流
#   MAINTENANCE_CONFIRMED=1 ./restore.sh /root/backups/backup-<ts>.manifest
#
# 任一步失败都保持 app stopped，保留已验证的隐藏恢复副本，
# 且绝不自动撤掉外部维护模式。
source "$(dirname "$0")/lib.sh"
umask 077
guard_target
require docker
require curl
require gzip
require sha256sum
require tar
require_db_pass
require_redis_pass
printf '%s\n' "$DB_NAME" | grep -Eq '^[A-Za-z0-9_-]+$' \
  || die "DB_NAME 含非法标识符字符：$DB_NAME"

acquire_ops_lock restore

SAFE_DIR=""
REDIS_BOOTSTRAP=""
RESTORE_MUTATED=0
RESTORE_COMPLETE=0

cleanup() {
  local rc=$?
  trap - EXIT
  set +e
  if [ -n "$REDIS_BOOTSTRAP" ]; then
    docker rm -f "$REDIS_BOOTSTRAP" >/dev/null 2>&1 || rc=1
  fi
  if [ "$RESTORE_MUTATED" = "1" ] && [ "$RESTORE_COMPLETE" != "1" ]; then
    if ! dc stop "$APP_SVC" >/dev/null 2>&1; then rc=1; fi
    warn "恢复未通过全部闸门；app 已尝试保持 stopped，外部维护模式不得撤销。"
    if [ -n "$SAFE_DIR" ] && [ -d "$SAFE_DIR" ]; then
      warn "已校验的恢复副本保留在：$SAFE_DIR（人工处置完再删）"
      SAFE_DIR=""
    fi
    warn "为防其他部署/备份在部分恢复状态下重启 app，ops lock 已 fail-closed 保留：$OPS_LOCK_DIR"
    warn "完成人工恢复与对账后，核对 owner=$OPS_LOCK_TOKEN 再清锁。"
    OPS_LOCK_OWNED=0
  fi
  if [ -n "$SAFE_DIR" ]; then rm -rf -- "$SAFE_DIR" || rc=1; fi
  release_ops_lock || rc=1
  exit "$rc"
}
trap cleanup EXIT

MANIFEST="${1:-}"
[ -n "$MANIFEST" ] || die "用法：MAINTENANCE_CONFIRMED=1 $0 <backup-*.manifest>"
case "$MANIFEST" in *.manifest) : ;; *) die "只接受 backup.sh 产生的 .manifest：$MANIFEST" ;; esac

# 锁已从首次校验前持有，防止 cron backup/prune 在人工确认期间换文件。
verify_backup_manifest "$MANIFEST"
gzip -t "$MANIFEST_DB_FILE" || die "DB gzip 完整性校验失败：$MANIFEST_DB_FILE"
tar tzf "$MANIFEST_CONFIG_FILE" >/dev/null || die "config tar 完整性校验失败：$MANIFEST_CONFIG_FILE"
SELECTED_MANIFEST="$(cd "$(dirname "$MANIFEST")" && pwd)/$(basename "$MANIFEST")"
SELECTED_VERSION="$MANIFEST_APP_VERSION"
SELECTED_TS="$MANIFEST_TS"
SELECTED_CONFIG_FILE="$MANIFEST_CONFIG_FILE"

APP_CID="$(service_container_id "$APP_SVC")"
REDIS_CID="$(service_container_id "$REDIS_SVC")"
[ -n "$APP_CID" ] || die "找不到 $APP_SVC 容器，无法安全恢复"
[ -n "$REDIS_CID" ] || die "找不到 $REDIS_SVC 容器，无法安全恢复"
REDIS_IMAGE_ID="$(docker inspect -f '{{.Image}}' "$REDIS_CID")"
REDIS_VOLUME="$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$REDIS_CID")"
[ -n "$REDIS_IMAGE_ID" ] || die "无法解析 Redis 容器镜像"
[ -n "$REDIS_VOLUME" ] || die "Redis /data 不是 named volume，拒绝猜测恢复路径"

docker run --rm \
  -v "$MANIFEST_DIR:/backup:ro" \
  --entrypoint redis-check-rdb "$REDIS_IMAGE_ID" "/backup/$(basename "$MANIFEST_REDIS_FILE")" >/dev/null \
  || die "Redis RDB 完整性校验失败：$MANIFEST_REDIS_FILE"

[ "${MAINTENANCE_CONFIRMED:-0}" = "1" ] \
  || die "尚未确认外部维护停流。先让 Nginx/CF 拒绝新交易与回调，再显式设 MAINTENANCE_CONFIRMED=1。"

log "目标栈      ：$STACK（DB=$DB_NAME）"
log "恢复 manifest：$SELECTED_MANIFEST"
log "恢复点版本  ：$SELECTED_VERSION @ $SELECTED_TS（Redis keys=$MANIFEST_REDIS_KEYS）"
warn "将配对覆盖生产 MySQL + Redis；恢复点后的支付/扣费/Trial 状态会被替换。"
confirm_typed "$STACK" "确认外部已停流，并整组覆盖生产栈 $STACK ?"

# 把整组复制到不匹配 prune glob 的隐藏目录，然后对副本再做一次
# manifest SHA/gzip/tar/RDB 校验，关闭“校验 -> 等人确认 -> 使用” TOCTOU。
ensure_backup_dir
SAFE_DIR="$(mktemp -d "$BACKUP_DIR/.restore-$SELECTED_TS.XXXXXX")"
SAFE_MANIFEST="$SAFE_DIR/$(basename "$SELECTED_MANIFEST")"
cp -a "$MANIFEST_DB_FILE" "$SAFE_DIR/$(basename "$MANIFEST_DB_FILE")"
cp -a "$MANIFEST_REDIS_FILE" "$SAFE_DIR/$(basename "$MANIFEST_REDIS_FILE")"
cp -a "$MANIFEST_CONFIG_FILE" "$SAFE_DIR/$(basename "$MANIFEST_CONFIG_FILE")"
cp -a "$SELECTED_MANIFEST" "$SAFE_MANIFEST"
verify_backup_manifest "$SAFE_MANIFEST"
SAFE_DB="$MANIFEST_DB_FILE"
SAFE_REDIS="$MANIFEST_REDIS_FILE"
SAFE_CONFIG="$MANIFEST_CONFIG_FILE"
gzip -t "$SAFE_DB" || die "安全副本 DB gzip 校验失败"
tar tzf "$SAFE_CONFIG" >/dev/null || die "安全副本 config tar 校验失败"
docker run --rm \
  -v "$SAFE_DIR:/backup:ro" \
  --entrypoint redis-check-rdb "$REDIS_IMAGE_ID" "/backup/$(basename "$SAFE_REDIS")" >/dev/null \
  || die "安全副本 Redis RDB 校验失败"

# 从这里开始会改动线上状态；任何退出都会重停 app 并保留 SAFE_DIR。
RESTORE_MUTATED=1

[ "$(service_container_id "$APP_SVC")" = "$APP_CID" ] \
  || die "等待确认期间 app 容器已被替换，拒绝基于过期拓扑恢复"
[ "$(service_container_id "$REDIS_SVC")" = "$REDIS_CID" ] \
  || die "等待确认期间 Redis 容器已被替换，拒绝基于过期持久卷恢复"

log "1/5 停止 $APP_SVC 并验证无写者…"
dc stop "$APP_SVC" >/dev/null
[ "$(service_container_id "$APP_SVC")" = "$APP_CID" ] || die "$APP_SVC 停服期间容器被替换"
container_running "$APP_CID" && die "$APP_SVC 仍在运行，拒绝恢复"
log "恢复前强制备份当前 MySQL+Redis 时间线…"
KEEP_APP_STOPPED=1 SKIP_PRUNE=1 "$(dirname "$0")/backup.sh" \
  || die "恢复前配对备份失败，已中止"

# mysqldump --databases 会建库，但只导入现存库会留下恢复点后新增表。
# 因此经 pre-backup 后先 DROP 整库，再导入，才是精确的整库时间线。
log "2/5 精确恢复 MySQL（删除当前库后导入 $SAFE_DB）…"
mysql_with_secret mysql -u"$DB_USER" \
  -e "DROP DATABASE IF EXISTS \`$DB_NAME\`;"
gunzip -c "$SAFE_DB" | mysql_with_secret_input mysql -u"$DB_USER"
DB_RESTORED="$(mysql_with_secret mysql -u"$DB_USER" -Nse \
  "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME='$DB_NAME';" | tr -d '\r')"
[ "$DB_RESTORED" = "$DB_NAME" ] || die "MySQL 导入后目标库不存在"
db_ready || die "MySQL 导入后未就绪"

# Redis 7 存在 multi-part AOF；仅删 AOF 后用 appendonly=yes 重启可能忽略
# dump.rdb 并以空库创建新 AOF。正确转换是：先 appendonly=no 加载 RDB，
# 在活进程 CONFIG SET appendonly yes，等 AOF rewrite 成功，再用正式 compose 重启并核对 key 总数。
log "3/5 恢复 Redis named volume $REDIS_VOLUME，并安全将 RDB 转为 Redis 7 AOF…"
dc stop "$REDIS_SVC" >/dev/null
[ "$(service_container_id "$REDIS_SVC")" = "$REDIS_CID" ] || die "$REDIS_SVC 停服期间容器被替换"
container_running "$REDIS_CID" && die "$REDIS_SVC 仍在运行，拒绝修改其持久卷"
docker run --rm --user 0 \
  -e RESTORE_RDB="$(basename "$SAFE_REDIS")" \
  -v "$REDIS_VOLUME:/data" \
  -v "$SAFE_DIR:/backup:ro" \
  --entrypoint sh "$REDIS_IMAGE_ID" -ec '
    if [ -e /data/dump.rdb ]; then owner="$(stat -c "%u:%g" /data/dump.rdb)";
    elif [ -d /data/appendonlydir ]; then owner="$(stat -c "%u:%g" /data/appendonlydir)";
    else owner="$(stat -c "%u:%g" /data)"; fi
    rm -rf /data/appendonlydir
    for f in /data/appendonly.aof*; do [ ! -e "$f" ] || rm -f -- "$f"; done
    rm -f /data/dump.rdb
    cp "/backup/$RESTORE_RDB" /data/dump.rdb
    chown "$owner" /data/dump.rdb
    chmod 600 /data/dump.rdb
  '

REDIS_BOOTSTRAP="newapi-restore-$SELECTED_TS-$$"
docker run -d --rm --name "$REDIS_BOOTSTRAP" \
  -v "$REDIS_VOLUME:/data" "$REDIS_IMAGE_ID" \
  redis-server --appendonly no >/dev/null
redis_deadline=$(( $(date +%s) + 60 ))
until [ "$(docker exec "$REDIS_BOOTSTRAP" redis-cli --raw PING 2>/dev/null | tr -d '\r')" = PONG ]; do
  if [ "$(date +%s)" -ge "$redis_deadline" ]; then
    docker logs "$REDIS_BOOTSTRAP" >&2 || true
    die "Redis 未能以 appendonly=no 从目标 RDB 启动"
  fi
  sleep 2
done
LOADED_KEYS="$(redis_key_count_container "$REDIS_BOOTSTRAP")"
[ "$LOADED_KEYS" = "$MANIFEST_REDIS_KEYS" ] \
  || die "RDB 加载 key 数 $LOADED_KEYS ≠ manifest $MANIFEST_REDIS_KEYS，拒绝转 AOF"
[ "$(docker exec "$REDIS_BOOTSTRAP" redis-cli --raw CONFIG SET appendonly yes | tr -d '\r')" = OK ] \
  || die "Redis CONFIG SET appendonly yes 失败"

aof_deadline=$(( $(date +%s) + ${REDIS_AOF_TIMEOUT:-300} ))
while :; do
  if [ "$(redis_persistence_value_container "$REDIS_BOOTSTRAP" aof_enabled)" = 1 ] \
    && [ "$(redis_persistence_value_container "$REDIS_BOOTSTRAP" aof_rewrite_in_progress)" = 0 ] \
    && [ "$(redis_persistence_value_container "$REDIS_BOOTSTRAP" aof_rewrite_scheduled)" = 0 ] \
    && [ "$(redis_persistence_value_container "$REDIS_BOOTSTRAP" aof_last_bgrewrite_status)" = ok ] \
    && [ "$(redis_persistence_value_container "$REDIS_BOOTSTRAP" aof_last_write_status)" = ok ] \
    && docker exec "$REDIS_BOOTSTRAP" sh -c 'test -s /data/appendonlydir/appendonly.aof.manifest'; then
    break
  fi
  [ "$(date +%s)" -lt "$aof_deadline" ] || die "Redis AOF rewrite 未在时限内完成或状态非 ok"
  sleep 2
done
[ "$(redis_key_count_container "$REDIS_BOOTSTRAP")" = "$LOADED_KEYS" ] \
  || die "RDB -> AOF 转换期间 Redis key 数变化"
docker stop -t 30 "$REDIS_BOOTSTRAP" >/dev/null
REDIS_BOOTSTRAP=""

dc start "$REDIS_SVC" >/dev/null
redis_deadline=$(( $(date +%s) + 60 ))
until redis_ready; do
  [ "$(date +%s)" -lt "$redis_deadline" ] || die "正式 Redis 从新 AOF 重启后 60s 仍未就绪"
  sleep 2
done
[ "$(redis_key_count_service)" = "$LOADED_KEYS" ] \
  || die "正式 Redis 从 AOF 重启后 key 数与转换前不一致"
[ "$(redis_persistence_value_service aof_enabled)" = 1 ] \
  && [ "$(redis_persistence_value_service aof_last_write_status)" = ok ] \
  || die "正式 Redis 未以健康 AOF 模式运行"

EXPECTED_RUNTIME_VERSION="$(tr -d '\r\n' < "$SERVER_REPO/VERSION")"
printf '%s\n' "$EXPECTED_RUNTIME_VERSION" | grep -Eq '^[A-Za-z0-9._-]+$' \
  || die "当前 VERSION 非法，无法验证恢复后制品身份"
log "4/5 启动 $APP_SVC，等待依赖与版本 $EXPECTED_RUNTIME_VERSION 就绪…"
dc start "$APP_SVC" >/dev/null
wait_runtime_ready "$EXPECTED_RUNTIME_VERSION" "$READINESS_TIMEOUT" \
  || die "恢复后 readiness/版本验收失败"

log "5/5 运行只读对账…"
"$(dirname "$0")/reconcile.sh" \
  || die "恢复后 reconcile 失败；app 将重新停止，不得开流"

RESTORE_COMPLETE=1
ok "配对恢复全部闸门通过：$SELECTED_MANIFEST"
warn "外部维护模式仍未由脚本撤销。人工核对支付渠道流水与 $SELECTED_CONFIG_FILE 上下文后，再开流。"
