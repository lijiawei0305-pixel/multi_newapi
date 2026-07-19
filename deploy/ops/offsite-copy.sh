#!/usr/bin/env bash
# Encrypt one already-verified paired backup set and copy it to an off-site
# rclone destination. The encrypted object is downloaded again and checked
# byte-for-byte before a local receipt is committed.
#
# Configuration is read without sourcing .env (no eval / shell execution):
#   BACKUP_OFFSITE_REQUIRED=1
#   BACKUP_OFFSITE_REMOTE=s3-worm:newapi-production/backups
#   BACKUP_OFFSITE_AGE_RECIPIENT=age1...
#
# The remote account/bucket must independently enforce object lock/immutability.
set -euo pipefail

source "$(dirname "$0")/lib.sh"
umask 077

env_file_value() {
  local key="$1" count value
  [ -f "$ENV_FILE" ] || return 1
  count="$(grep -Ec "^${key}=" "$ENV_FILE" 2>/dev/null || true)"
  [ "$count" = 1 ] || return 1
  value="$(sed -n "s/^${key}=//p" "$ENV_FILE")"
  case "$value" in
    \"*\") value="${value#\"}"; value="${value%\"}" ;;
    \'*\') value="${value#\'}"; value="${value%\'}" ;;
  esac
  printf '%s' "$value"
}

required="${BACKUP_OFFSITE_REQUIRED:-$(env_file_value BACKUP_OFFSITE_REQUIRED 2>/dev/null || true)}"
remote="${BACKUP_OFFSITE_REMOTE:-$(env_file_value BACKUP_OFFSITE_REMOTE 2>/dev/null || true)}"
recipient="${BACKUP_OFFSITE_AGE_RECIPIENT:-$(env_file_value BACKUP_OFFSITE_AGE_RECIPIENT 2>/dev/null || true)}"
if [ -z "$required" ]; then
  # newapi_test 是仓库唯一生产栈；生产默认必须异地成功。一次性恢复演练使用
  # 随机隔离 STACK，因此在没有外部凭据时可保持 0。
  if [ "$STACK" = newapi_test ]; then required=1; else required=0; fi
fi

case "$required" in 0|1) ;; *) die "BACKUP_OFFSITE_REQUIRED 只能是 0 或 1" ;; esac
[ "$#" = 1 ] || die "用法：offsite-copy.sh /path/to/backup-<timestamp>.manifest"

if [ -z "$remote" ]; then
  [ "$required" = 0 ] || die "已要求异地备份，但 BACKUP_OFFSITE_REMOTE 未配置"
  warn "未配置 BACKUP_OFFSITE_REMOTE；本次仅保留同机恢复点"
  exit 0
fi
[ -n "$recipient" ] || die "已配置异地目标，但缺少 BACKUP_OFFSITE_AGE_RECIPIENT"
case "$remote$recipient" in *$'\r'*|*$'\n'*) die "异地目标/age recipient 不得包含换行" ;; esac
printf '%s\n' "$remote" | grep -Eq '^[A-Za-z0-9_.-]+:[^[:cntrl:]]+$' \
  || die "BACKUP_OFFSITE_REMOTE 必须是明确的 rclone remote:path，禁止本机路径/选项"
printf '%s\n' "$recipient" | grep -Eq '^(age1[0-9a-z]{20,}|age1[a-z0-9-]+|ssh-(rsa|ed25519)[[:space:]][A-Za-z0-9+/=]+)$' \
  || die "BACKUP_OFFSITE_AGE_RECIPIENT 格式非法"

guard_target
require age
require rclone
require sha256sum
require tar
verify_backup_manifest "$1"
[ "$(basename "$1")" = "backup-${MANIFEST_TS}.manifest" ] \
  || die "manifest 文件名与其 timestamp 不一致"

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/newapi-offsite-${MANIFEST_TS}.XXXXXX")"
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  rm -rf -- "$tmp_dir"
  exit "$rc"
}
trap cleanup EXIT INT TERM

encrypted="$tmp_dir/encrypted.age"
downloaded="$tmp_dir/downloaded.age"

log "加密已验证恢复集：$MANIFEST_TS"
tar -C "$MANIFEST_DIR" -czf - \
  "$(basename "$1")" \
  "$(basename "$MANIFEST_DB_FILE")" \
  "$(basename "$MANIFEST_REDIS_FILE")" \
  "$(basename "$MANIFEST_CONFIG_FILE")" \
  | age --recipient "$recipient" --output "$encrypted"
[ "$(file_size "$encrypted")" -gt 0 ] || die "age 加密产物为空"
encrypted_sha="$(sha256_file "$encrypted")"
object_name="newapi-${STACK}-${MANIFEST_TS}-${encrypted_sha:0:16}.tar.gz.age"
metadata="$tmp_dir/$object_name.metadata"
remote_base="${remote%/}/${STACK}/${MANIFEST_TS}"
remote_object="$remote_base/$object_name"
remote_metadata="$remote_base/$object_name.metadata"

{
  printf 'format=newapi-offsite-v1\n'
  printf 'timestamp=%s\n' "$MANIFEST_TS"
  printf 'stack=%s\n' "$STACK"
  printf 'app_version=%s\n' "$MANIFEST_APP_VERSION"
  printf 'source_manifest=%s\n' "$(basename "$1")"
  printf 'source_manifest_sha256=%s\n' "$(sha256_file "$1")"
  printf 'encrypted_file=%s\n' "$object_name"
  printf 'encrypted_sha256=%s\n' "$encrypted_sha"
} > "$metadata"

log "复制加密恢复集到异地目标（目标内容不会写入日志）"
rclone copyto --immutable "$encrypted" "$remote_object"
rclone copyto --immutable "$metadata" "$remote_metadata"

log "从异地重新下载并校验 SHA-256"
rclone copyto "$remote_object" "$downloaded"
[ "$(sha256_file "$downloaded")" = "$encrypted_sha" ] \
  || die "异地对象下载复验 SHA-256 不匹配"

receipt="$BACKUP_DIR/offsite-${MANIFEST_TS}.receipt"
receipt_tmp="$BACKUP_DIR/.offsite-${MANIFEST_TS}.receipt.tmp"
[ ! -e "$receipt" ] || die "异地复制 receipt 已存在，拒绝覆盖：$receipt"
{
  cat "$metadata"
  printf 'verified_at_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'verification=full-download-sha256\n'
} > "$receipt_tmp"
mv "$receipt_tmp" "$receipt"
chmod 600 "$receipt"
ok "异地加密副本已上传并完成回读校验：$receipt"
