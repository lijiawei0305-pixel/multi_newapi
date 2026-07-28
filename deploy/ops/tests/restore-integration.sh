#!/usr/bin/env bash
# Real, disposable paired-restore drill.
#
# Unlike run.sh, this script never substitutes docker/curl. It creates an
# isolated Compose project with the production app, MySQL, and Redis images,
# exercises backup.sh + restore.sh end to end, and destroys the project/volumes
# on exit. It must never be pointed at the production project or its paths.
#
# Local developer machines without a running Docker daemon report SKIP. CI sets
# REQUIRE_DOCKER=1 so a missing daemon is a hard failure, making the Linux run
# authoritative instead of silently green.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
OPS_DIR="$ROOT/deploy/ops"

fail() {
  printf '[restore-drill][FAIL] %s\n' "$*" >&2
  exit 1
}

if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
  if [ "${REQUIRE_DOCKER:-0}" = "1" ]; then
    fail "Docker Engine + Compose are required by the authoritative CI drill"
  fi
  printf '[restore-drill][SKIP] Docker Engine + Compose are not installed; no real drill ran.\n'
  exit 0
fi
if ! docker info >/dev/null 2>&1; then
  if [ "${REQUIRE_DOCKER:-0}" = "1" ]; then
    fail "Docker daemon is unavailable in the authoritative CI drill"
  fi
  printf '[restore-drill][SKIP] Docker daemon is unavailable; no real drill ran.\n'
  exit 0
fi

require_host_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing host command: $1"
}
for command in curl gzip python3 sha256sum tar; do
  require_host_command "$command"
done

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/newapi-restore-drill.XXXXXX")"
run_suffix="${GITHUB_RUN_ID:-local}_$$_${RANDOM:-0}"
run_suffix="$(printf '%s' "$run_suffix" | tr -cd 'a-zA-Z0-9_-')"
STACK="newapi_restore_drill_${run_suffix}"
[ "$STACK" != "newapi_test" ] || fail "disposable stack unexpectedly equals production"

export STACK EXPECTED_STACK="$STACK"
export SERVER_REPO="$ROOT"
export COMPOSE_FILE="$ROOT/deploy/docker-compose.test.yml"
export ENV_FILE="$TMP_ROOT/drill.env"
export BACKUP_DIR="$TMP_ROOT/backups"
export OPS_LOCK_DIR="$TMP_ROOT/ops.lock"
export NGINX_VHOST="$TMP_ROOT/nginx/drill.conf"
export NGINX_CERT_DIR="$TMP_ROOT/nginx/cert"
export APP_PORT="${RESTORE_DRILL_APP_PORT:-$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')}"
printf '%s\n' "$APP_PORT" | grep -Eq '^[1-9][0-9]*$' || fail "could not select a non-zero loopback app port"
export HOST_HEADER=localhost
export DB_NAME=new-api-test DB_USER=root
export READINESS_TIMEOUT=420 READINESS_INTERVAL=2 REDIS_AOF_TIMEOUT=180
export KEEP=20 SKIP_PRUNE=1
export DOCKER_BUILDKIT=1

mkdir -p "$BACKUP_DIR" "$NGINX_CERT_DIR"
chmod 700 "$BACKUP_DIR" "$NGINX_CERT_DIR"

secret_seed="$(printf '%s:%s:%s' "$STACK" "$(date +%s)" "${RANDOM:-0}" | sha256sum | awk '{print $1}')"
export DB_PASS="${secret_seed:0:32}"
session_secret="$(printf 'session:%s' "$secret_seed" | sha256sum | awk '{print $1}')"
crypto_secret="$(printf 'crypto:%s' "$secret_seed" | sha256sum | awk '{print $1}')"
internal_secret="$(printf 'internal:%s' "$secret_seed" | sha256sum | awk '{print $1}')"
mysql_app_password="$(printf 'mysql-app:%s' "$secret_seed" | sha256sum | awk '{print $1}')"
redis_password="$(printf 'redis:%s' "$secret_seed" | sha256sum | awk '{print $1}')"
{
  printf 'MYSQL_ROOT_PASSWORD=%s\n' "$DB_PASS"
  printf 'MYSQL_APP_USER=newapi\n'
  printf 'MYSQL_APP_PASSWORD=%s\n' "$mysql_app_password"
  printf 'REDIS_APP_USER=newapi\n'
  printf 'REDIS_PASSWORD=%s\n' "$redis_password"
  printf 'BACKUP_OFFSITE_REQUIRED=0\n'
  printf 'SESSION_SECRET=%s\n' "$session_secret"
  printf 'CRYPTO_SECRET=%s\n' "$crypto_secret"
  printf 'MT_INTERNAL_SECRET=%s\n' "$internal_secret"
  printf 'MT_PAY_NOTIFY_BASE=http://127.0.0.1\n'
  printf 'UPSTREAM_BASE_URL=\n'
  printf 'UPSTREAM_API_KEY=\n'
} > "$ENV_FILE"
chmod 600 "$ENV_FILE"
printf 'server { listen 443 ssl; }\n' > "$NGINX_VHOST"
printf 'disposable restore drill certificate fixture\n' > "$NGINX_CERT_DIR/origin.pem"

# shellcheck source=../lib.sh
source "$OPS_DIR/lib.sh"

WRITER_PID=""
RESTORE_PID=""
COMPOSE_STARTED=0
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  set +e
  if [ -n "$WRITER_PID" ]; then
    kill "$WRITER_PID" >/dev/null 2>&1 || true
    wait "$WRITER_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "$RESTORE_PID" ]; then
    kill "$RESTORE_PID" >/dev/null 2>&1 || true
    wait "$RESTORE_PID" >/dev/null 2>&1 || true
  fi
  if [ "$COMPOSE_STARTED" = "1" ]; then
    if [ "$rc" -ne 0 ]; then
      dc ps >&2 || true
      dc logs --no-color --tail=200 >&2 || true
    fi
    dc down -v --remove-orphans --rmi local >/dev/null 2>&1 || true
  fi
  if [ "${KEEP_RESTORE_DRILL_ARTIFACTS:-0}" = "1" ]; then
    printf '[restore-drill] retained diagnostics: %s\n' "$TMP_ROOT" >&2
  else
    rm -rf -- "$TMP_ROOT"
  fi
  exit "$rc"
}
trap cleanup EXIT INT TERM

mysql_scalar() {
  local sql="$1"
  mysql_with_secret mysql -u"$DB_USER" -Nse \
    "USE \`$DB_NAME\`; $sql" 2>/dev/null | tr -d '\r'
}

redis_scalar() {
  redis_cli_service --raw "$@" 2>/dev/null | tr -d '\r'
}

printf '[restore-drill] starting real isolated stack %s on %s\n' "$STACK" "$(uname -s)"
COMPOSE_STARTED=1
dc --progress quiet up -d --build --quiet-pull
printf '[restore-drill] app published at 127.0.0.1:%s\n' "$APP_PORT"

expected_version="$(tr -d '\r\n' < "$ROOT/VERSION")"
[ -n "$expected_version" ] || fail "VERSION is empty"
wait_runtime_ready "$expected_version" "$READINESS_TIMEOUT" \
  || fail "fresh disposable application did not become ready as $expected_version"

setup_response="$(curl -fsS --max-time 15 \
  -H "Host: $HOST_HEADER" -H 'Content-Type: application/json' \
  --data '{"username":"drillroot","password":"RestoreDrill!2026","confirmPassword":"RestoreDrill!2026","SelfUseModeEnabled":false,"DemoSiteEnabled":false}' \
  "http://127.0.0.1:$APP_PORT/api/setup")"
printf '%s' "$setup_response" | grep -Eq '"success"[[:space:]]*:[[:space:]]*true' \
  || fail "application setup failed: $setup_response"

mysql_scalar "CREATE TABLE ops_restore_drill_marker (id BIGINT PRIMARY KEY, marker VARCHAR(128) NOT NULL);"
mysql_scalar "INSERT INTO ops_restore_drill_marker(id, marker) VALUES (1, 'paired-restore-point');"
# Seed every withdrawal state with the current money-flow invariant. This makes
# the post-restore reconcile gate prove that pending + approved remain frozen
# while only paid withdrawals have left the wallet.
mysql_scalar "INSERT INTO agent_profiles(tenant_id) VALUES (4242);"
mysql_scalar "INSERT INTO agent_wallets(tenant_id, withdrawable_balance, withdrawable_balance_units, frozen_withdraw_amount, frozen_withdraw_amount_units, total_earned, total_earned_units) VALUES (4242, 20, 2000000000, 30, 3000000000, 100, 10000000000);"
mysql_scalar "INSERT INTO agent_earning_logs(tenant_id, source_type, source_id, idem_key, idem_key_hash, amount, amount_units) VALUES (4242, 'manual_adjustment', 'restore-drill', LOWER(SHA2(CONCAT('4242',CHAR(0),'manual_adjustment',CHAR(0),'restore-drill'),256)), LOWER(SHA2(CONCAT('4242',CHAR(0),'manual_adjustment',CHAR(0),'restore-drill'),256)), 100, 10000000000);"
mysql_scalar "INSERT INTO agent_withdrawals(tenant_id, amount, amount_units, status, payout_ref, payout_ref_hash, paid_at) VALUES (4242, 10, 1000000000, 'pending', '', NULL, NULL), (4242, 20, 2000000000, 'approved', '', NULL, NULL), (4242, 50, 5000000000, 'paid', 'restore-paid-4242', LOWER(SHA2('restore-paid-4242',256)), NOW()), (4242, 5, 500000000, 'rejected', '', NULL, NULL);"
mysql_scalar "INSERT INTO agent_payout_ref_claims_v3(payout_ref_hash, payout_ref, withdrawal_id, created_at) SELECT payout_ref_hash, payout_ref, id, NOW() FROM agent_withdrawals WHERE tenant_id=4242 AND status='paid';"
snapshot_login_count="$(mysql_scalar 'SELECT COUNT(*) FROM logs WHERE type=7;')"
printf '%s\n' "$snapshot_login_count" | grep -Eq '^[0-9]+$' || fail "could not read login-log baseline"

trial_key='risk:trial:user:424242'
[ "$(redis_scalar SET "$trial_key" 424242)" = OK ] || fail "could not seed permanent Trial key"
[ "$(redis_scalar GET "$trial_key")" = 424242 ] || fail "Trial key value was not seeded"
[ "$(redis_scalar TTL "$trial_key")" = -1 ] || fail "Trial key is not permanent before backup"

printf '[restore-drill] creating real paired MySQL/Redis/config backup\n'
bash "$OPS_DIR/backup.sh"
selected_manifest="$(find "$BACKUP_DIR" -maxdepth 1 -type f -name 'backup-*.manifest' -print | sort | head -n1)"
[ -n "$selected_manifest" ] || fail "backup.sh produced no manifest"
verify_backup_manifest "$selected_manifest"
[ "$(manifest_value "$selected_manifest" consistency)" = writers-stopped ] \
  || fail "manifest does not certify a stopped-writer consistency point"
selected_manifest_sha="$(sha256_file "$selected_manifest")"
wait_runtime_ready "$expected_version" 120 \
  || fail "application did not regain readiness after the selected backup"

# Destroy both sides after the selected recovery point. The actual application
# login loop below creates additional DB writes until restore stops app; it exits
# on the first transport failure and therefore cannot resume after app restart.
mysql_scalar "UPDATE ops_restore_drill_marker SET marker='corrupted-after-backup' WHERE id=1;"
mysql_scalar "INSERT INTO ops_restore_drill_marker(id, marker) VALUES (2, 'must-disappear');"
[ "$(redis_scalar SET "$trial_key" corrupted EX 600)" = OK ] || fail "could not corrupt Trial key"
[ "$(redis_scalar SET 'risk:purchase:99:user:424242' 9)" = OK ] || fail "could not seed Redis contamination"

writer_success="$TMP_ROOT/writer.success"
writer_stopped="$TMP_ROOT/writer.stopped"
writer_loop() {
  local response
  while :; do
    if ! response="$(curl -sS --connect-timeout 1 --max-time 3 \
      -H "Host: $HOST_HEADER" -H 'Content-Type: application/json' \
      --data '{"username":"drillroot","password":"RestoreDrill!2026"}' \
      "http://127.0.0.1:$APP_PORT/api/user/login")"; then
      printf 'application transport stopped\n' > "$writer_stopped"
      return 0
    fi
    if printf '%s' "$response" | grep -Eq '"success"[[:space:]]*:[[:space:]]*true'; then
      printf 'login write committed\n' >> "$writer_success"
    fi
    sleep 1
  done
}
writer_loop &
WRITER_PID=$!

writer_deadline=$(( $(date +%s) + 30 ))
while [ ! -s "$writer_success" ] && [ "$(date +%s)" -lt "$writer_deadline" ]; do sleep 1; done
[ -s "$writer_success" ] || fail "the real application writer never committed a post-backup login"
post_backup_login_count="$(mysql_scalar 'SELECT COUNT(*) FROM logs WHERE type=7;')"
[ "$post_backup_login_count" -gt "$snapshot_login_count" ] \
  || fail "post-backup login did not create an observable DB write"

# Avoid colliding with backup.sh's second-resolution output identity when
# restore.sh creates its mandatory pre-restore paired backup.
sleep 2
restore_log="$TMP_ROOT/restore.log"
(
  printf '%s\n' "$STACK" \
    | MAINTENANCE_CONFIRMED=1 bash "$OPS_DIR/restore.sh" "$selected_manifest"
) > "$restore_log" 2>&1 &
RESTORE_PID=$!

lock_deadline=$(( $(date +%s) + 30 ))
while [ ! -s "$OPS_LOCK_DIR/owner" ] && [ "$(date +%s)" -lt "$lock_deadline" ]; do
  kill -0 "$RESTORE_PID" 2>/dev/null || break
  sleep 1
done
[ -s "$OPS_LOCK_DIR/owner" ] || {
  cat "$restore_log" >&2 || true
  fail "restore did not acquire the shared ops lock"
}
grep -Eq '^restore-' "$OPS_LOCK_DIR/owner" || fail "unexpected ops lock owner during restore"

competing_log="$TMP_ROOT/competing-backup.log"
if bash "$OPS_DIR/backup.sh" > "$competing_log" 2>&1; then
  fail "a concurrent backup bypassed the restore ops lock"
fi
grep -Fq '另一个运维操作正持有' "$competing_log" \
  || fail "concurrent backup failed for a reason other than the shared ops lock"

if ! wait "$RESTORE_PID"; then
  RESTORE_PID=""
  cat "$restore_log" >&2 || true
  fail "real paired restore failed"
fi
RESTORE_PID=""
if [ -n "$WRITER_PID" ]; then
  writer_stop_deadline=$(( $(date +%s) + 10 ))
  while kill -0 "$WRITER_PID" 2>/dev/null \
    && [ ! -s "$writer_stopped" ] \
    && [ "$(date +%s)" -lt "$writer_stop_deadline" ]; do
    sleep 1
  done
  if [ ! -s "$writer_stopped" ]; then
    kill "$WRITER_PID" >/dev/null 2>&1 || true
    wait "$WRITER_PID" >/dev/null 2>&1 || true
    WRITER_PID=""
    fail "application writer never observed the enforced stop boundary"
  fi
  wait "$WRITER_PID" || true
  WRITER_PID=""
fi
[ -s "$writer_stopped" ] || fail "application writer never observed the enforced stop boundary"

grep -Fq '配对恢复全部闸门通过' "$restore_log" \
  || fail "restore did not report all readiness/reconcile gates passed"
grep -Eq '对账完成：.*全部 PASS' "$restore_log" \
  || fail "restore log contains no successful real reconcile result"

[ "$(mysql_scalar "SELECT marker FROM ops_restore_drill_marker WHERE id=1;")" = paired-restore-point ] \
  || fail "MySQL marker did not return to the selected recovery point"
[ "$(mysql_scalar 'SELECT COUNT(*) FROM ops_restore_drill_marker;')" = 1 ] \
  || fail "post-backup MySQL contamination survived restore"
[ "$(mysql_scalar 'SELECT COUNT(*) FROM logs WHERE type=7;')" = "$snapshot_login_count" ] \
  || fail "actual application writes after the recovery point contaminated restored MySQL"
[ "$(redis_scalar GET "$trial_key")" = 424242 ] \
  || fail "permanent Trial key value did not return to the selected recovery point"
[ "$(redis_scalar TTL "$trial_key")" = -1 ] \
  || fail "permanent Trial key TTL did not restore to -1"
[ "$(redis_scalar EXISTS 'risk:purchase:99:user:424242')" = 0 ] \
  || fail "post-backup Redis contamination survived restore"

[ "$(sha256_file "$selected_manifest")" = "$selected_manifest_sha" ] \
  || fail "selected manifest changed during restore"
verify_backup_manifest "$selected_manifest"
manifest_count="$(find "$BACKUP_DIR" -maxdepth 1 -type f -name 'backup-*.manifest' | wc -l | tr -d ' ')"
[ "$manifest_count" -ge 2 ] || fail "restore did not create the mandatory pre-restore paired backup"
while IFS= read -r manifest; do
  verify_backup_manifest "$manifest"
done < <(find "$BACKUP_DIR" -maxdepth 1 -type f -name 'backup-*.manifest' -print | sort)

wait_runtime_ready "$expected_version" 30 || fail "application lost readiness after restore"
bash "$OPS_DIR/reconcile.sh" > "$TMP_ROOT/final-reconcile.log"
grep -Eq '对账完成：.*全部 PASS' "$TMP_ROOT/final-reconcile.log" \
  || fail "explicit post-restore reconcile did not pass"

printf '[restore-drill][PASS] real MySQL + Redis paired restore verified (%s manifests); app writer stopped and post-point writes removed.\n' "$manifest_count"
