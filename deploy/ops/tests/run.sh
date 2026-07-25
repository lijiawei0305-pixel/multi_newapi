#!/usr/bin/env bash
# 无外部框架的 ops 回归测试；所有“Docker/SSH/下载”均为临时假命令。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
TESTS=0

fail() {
  printf 'not ok - %s\n' "$*" >&2
  exit 1
}

run_test() {
  local name="$1"
  shift
  TESTS=$((TESTS + 1))
  if "$@"; then
    printf 'ok %d - %s\n' "$TESTS" "$name"
  else
    printf 'not ok %d - %s\n' "$TESTS" "$name" >&2
    exit 1
  fi
}

test_manifest_checksum_and_clean_extract() (
  set -euo pipefail
  local tmp manifest db redis config src stage
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  db="$tmp/db-20260719-120000.sql.gz"
  redis="$tmp/redis-20260719-120000.rdb"
  config="$tmp/config-20260719-120000.tar.gz"
  printf 'db-payload\n' > "$db"
  printf 'redis-payload\n' > "$redis"
  printf 'config-payload\n' > "$config"
  manifest="$tmp/backup-20260719-120000.manifest"
  {
    printf 'format=newapi-backup-v1\n'
    printf 'timestamp=20260719-120000\n'
    printf 'stack=newapi_test\n'
    printf 'db_name=new-api-test\n'
    printf 'consistency=writers-stopped\n'
    printf 'app_version=abc123-20260719-120000\n'
    printf 'redis_keys=0\n'
    printf 'db_file=%s\n' "$(basename "$db")"
    printf 'db_sha256=%s\n' "$(sha256sum "$db" | awk '{print $1}')"
    printf 'redis_file=%s\n' "$(basename "$redis")"
    printf 'redis_sha256=%s\n' "$(sha256sum "$redis" | awk '{print $1}')"
    printf 'config_file=%s\n' "$(basename "$config")"
    printf 'config_sha256=%s\n' "$(sha256sum "$config" | awk '{print $1}')"
  } > "$manifest"

  STACK=newapi_test DB_NAME=new-api-test \
    bash -c '. "$1"; verify_backup_manifest "$2"' _ "$ROOT/deploy/ops/lib.sh" "$manifest"
  printf 'tampered\n' >> "$redis"
  if STACK=newapi_test DB_NAME=new-api-test \
    bash -c '. "$1"; verify_backup_manifest "$2"' _ "$ROOT/deploy/ops/lib.sh" "$manifest" \
      >"$tmp/tamper.out" 2>&1; then
    fail "tampered Redis payload was accepted"
  fi

  src="$tmp/source"
  mkdir -p "$src/deploy/ops"
  printf 'FROM scratch\n' > "$src/Dockerfile"
  printf 'services: {}\n' > "$src/deploy/docker-compose.test.yml"
  printf '#!/usr/bin/env bash\n' > "$src/deploy/ops/rollback.sh"
  chmod +x "$src/deploy/ops/rollback.sh"
  tar czf "$tmp/release.tgz" -C "$src" .
  stage="$tmp/stage"
  bash -c '. "$1"; extract_release_tree "$2" "$3"' _ \
    "$ROOT/deploy/ops/lib.sh" "$tmp/release.tgz" "$stage"
  [ -f "$stage/Dockerfile" ] || fail "clean staging extraction did not produce release"

  mkdir "$tmp/nonempty"
  printf 'keep\n' > "$tmp/nonempty/sentinel"
  if bash -c '. "$1"; extract_release_tree "$2" "$3"' _ \
    "$ROOT/deploy/ops/lib.sh" "$tmp/release.tgz" "$tmp/nonempty" \
      >"$tmp/overlay.out" 2>&1; then
    fail "extract_release_tree overlaid an existing directory"
  fi
  [ "$(cat "$tmp/nonempty/sentinel")" = keep ] || fail "existing staging was modified"

  cp "$tmp/release.tgz" "$tmp/src-20260719-120001.tgz"
  printf 'release-v1\n' > "$tmp/src-20260719-120001.version"
  printf '{"version":"release-v1"}\n' > "$tmp/src-20260719-120001.manifest.json"
  {
    printf 'format=newapi-release-v1\n'
    printf 'timestamp=20260719-120001\n'
    printf 'archive_file=src-20260719-120001.tgz\n'
    printf 'archive_sha256=%s\n' "$(sha256sum "$tmp/src-20260719-120001.tgz" | awk '{print $1}')"
    printf 'version_file=src-20260719-120001.version\n'
    printf 'version_sha256=%s\n' "$(sha256sum "$tmp/src-20260719-120001.version" | awk '{print $1}')"
    printf 'deploy_manifest_file=src-20260719-120001.manifest.json\n'
    printf 'deploy_manifest_sha256=%s\n' "$(sha256sum "$tmp/src-20260719-120001.manifest.json" | awk '{print $1}')"
    printf 'version=release-v1\n'
  } > "$tmp/src-20260719-120001.release"
  bash -c '. "$1"; verify_release_manifest "$2"' _ \
    "$ROOT/deploy/ops/lib.sh" "$tmp/src-20260719-120001.release"
  printf 'archive-corruption\n' >> "$tmp/src-20260719-120001.tgz"
  if bash -c '. "$1"; verify_release_manifest "$2"' _ \
    "$ROOT/deploy/ops/lib.sh" "$tmp/src-20260719-120001.release" \
      >"$tmp/release-tamper.out" 2>&1; then
    fail "tampered release archive was accepted"
  fi
)

test_ops_lock_rejects_unrelated_owner() (
  set -euo pipefail
  local tmp lock
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  lock="$tmp/ops.lock"
  mkdir "$lock"
  printf 'restore-owner\n' > "$lock/owner"
  if OPS_LOCK_DIR="$lock" OPS_LOCK_TOKEN=deploy-owner \
    bash -c '. "$1"; acquire_ops_lock deploy' _ "$ROOT/deploy/ops/lib.sh" \
      >"$tmp/lock.out" 2>&1; then
    fail "unrelated operation bypassed the shared ops lock"
  fi
  OPS_LOCK_DIR="$lock" OPS_LOCK_TOKEN=restore-owner \
    bash -c '. "$1"; acquire_ops_lock nested-backup' _ "$ROOT/deploy/ops/lib.sh"
)

test_redis_backup_failure_is_fatal() (
  set -euo pipefail
  local tmp fake state output calls
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  state="$tmp/app-running"
  output="$tmp/backup.out"
  calls="$tmp/docker.log"
  mkdir -p "$fake" "$tmp/backups" "$tmp/server"
  printf 'true\n' > "$state"
  printf 'test-version\n' > "$tmp/server/VERSION"
  cat > "$fake/docker" <<'FAKE_DOCKER'
#!/usr/bin/env bash
set -u
args="$*"
printf '%s\n' "$args" >> "$DOCKER_CALLS"
if [[ "$args" == compose\ *" exec -T mysql sh -c "*" newapi-mysql-client mysqldump "* ]]; then
  cat >/dev/null
  i=0
  while [ "$i" -lt 300 ]; do printf 'INSERT INTO t VALUES (%s, %s);\n' "$i" "$((i * 7919))"; i=$((i + 1)); done
  exit 0
fi
if [[ "$args" == compose\ *" exec -T redis sh -c "*" newapi-redis-client newapi "* ]]; then
  cat >/dev/null
  case "$args" in
    *" newapi-redis-client newapi --raw PING") printf 'PONG\n' ;;
    *" newapi-redis-client newapi SAVE") exit 0 ;;
    *" newapi-redis-client newapi --raw INFO keyspace") printf '# Keyspace\ndb0:keys=1,expires=0,avg_ttl=0\n' ;;
    *) printf 'unexpected authenticated Redis call: %s\n' "$args" >&2; exit 97 ;;
  esac
  exit 0
fi
case "$args" in
  "compose "*" ps -a -q app") printf 'app-cid\n' ;;
  "compose "*" ps -a -q redis") printf 'redis-cid\n' ;;
  "inspect -f {{.Image}} app-cid") printf 'app-image-id\n' ;;
  "inspect -f {{.Image}} redis-cid") printf 'redis-image-id\n' ;;
  "inspect -f {{.State.Running}} app-cid") cat "$DOCKER_STATE" ;;
  "run --rm app-image-id --version") printf 'test-version\n' ;;
  "compose "*" stop app") printf 'false\n' > "$DOCKER_STATE" ;;
  "compose "*" start app") printf 'true\n' > "$DOCKER_STATE" ;;
  "compose "*" exec -T mysql sh -c "*) exit 0 ;;
  "compose "*" exec -T redis redis-check-rdb /data/dump.rdb") exit 0 ;;
  "compose "*" cp redis:/data/dump.rdb "*) exit 23 ;;
  *) printf 'unexpected fake docker call: %s\n' "$args" >&2; exit 97 ;;
esac
FAKE_DOCKER
  chmod +x "$fake/docker"

  if PATH="$fake:$PATH" DOCKER_STATE="$state" DOCKER_CALLS="$calls" DB_PASS=test-password \
    REDIS_PASS=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa BACKUP_OFFSITE_REQUIRED=0 \
    BACKUP_DIR="$tmp/backups" SERVER_REPO="$tmp/server" \
    OPS_LOCK_DIR="$tmp/ops.lock" \
    COMPOSE_FILE="$tmp/server/compose.yml" ENV_FILE="$tmp/server/.env" \
    NGINX_VHOST="$tmp/nginx.conf" NGINX_CERT_DIR="$tmp/cert" \
    bash "$ROOT/deploy/ops/backup.sh" >"$output" 2>&1; then
    cat "$output" >&2
    cat "$calls" >&2
    fail "backup succeeded after Redis copy failure"
  fi
  [ "$(cat "$state")" = true ] || fail "app running state was not restored after failed backup"
  if find "$tmp/backups" -maxdepth 1 -type f \( -name 'backup-*' -o -name 'db-*' -o -name 'redis-*' -o -name 'config-*' \) | grep -q .; then
    fail "failed backup left a partial recovery set"
  fi
)

test_deploy_aborts_on_backup_failure() (
  set -euo pipefail
  local tmp fake output ssh_log
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  output="$tmp/deploy.out"
  ssh_log="$tmp/ssh.log"
  mkdir -p "$fake" "$tmp/local"
  cat > "$fake/git" <<'FAKE_GIT'
#!/usr/bin/env bash
case "$*" in
  "rev-parse --short HEAD") printf 'abc1234\n' ;;
  "rev-parse --abbrev-ref HEAD") printf 'main\n' ;;
  "status --porcelain --untracked-files=all") exit 0 ;;
  "tag -f "*) exit 0 ;;
  *) exit 1 ;;
esac
FAKE_GIT
  cat > "$fake/ssh" <<'FAKE_SSH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SSH_LOG"
case "$*" in
  *"test -x "*"/deploy/ops/backup.sh"*) exit 0 ;;
  *"/deploy/ops/backup.sh"*) exit 42 ;;
  *) exit 0 ;;
esac
FAKE_SSH
  chmod +x "$fake/git" "$fake/ssh"

  if PATH="$fake:$PATH" SSH_LOG="$ssh_log" SKIP_PREFLIGHT=1 \
    LOCAL_REPO="$tmp/local" SSH_HOST=fake SERVER_REPO=/srv/newapi \
    COMPOSE_FILE=/srv/newapi/deploy/docker-compose.test.yml ENV_FILE=/srv/newapi/.env \
    bash "$ROOT/deploy/ops/deploy.sh" >"$output" 2>&1; then
    fail "deploy continued after backup failure"
  fi
  grep -Fq '发布前备份失败' "$output" || fail "deploy did not explain backup abort"
  if grep -Eq 'docker tag|src-[0-9].*tgz|stage-' "$ssh_log"; then
    fail "deploy mutated image/release after backup failure"
  fi
)

test_deploy_bootstraps_current_head_offsite_before_release_mutation() (
  set -euo pipefail
  local tmp fake output ssh_log git_log backup_state backup_line offsite_line
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  output="$tmp/deploy.out"
  ssh_log="$tmp/ssh.log"
  git_log="$tmp/git.log"
  backup_state="$tmp/backup.done"
  mkdir -p "$fake" "$tmp/local"

  cat > "$fake/git" <<'FAKE_OFFSITE_GIT'
#!/usr/bin/env bash
set -u
printf '%s\n' "$*" >> "$GIT_LOG"
case "$*" in
  "rev-parse --short HEAD") printf 'abc1234\n' ;;
  "rev-parse --abbrev-ref HEAD") printf 'main\n' ;;
  "status --porcelain --untracked-files=all") exit 0 ;;
  "tag -f "*) exit 0 ;;
  "archive --format=tar HEAD -- deploy/ops/lib.sh deploy/ops/offsite-copy.sh")
    printf 'audited-helper-from-head\n'
    ;;
  *) exit 97 ;;
esac
FAKE_OFFSITE_GIT
  cat > "$fake/ssh" <<'FAKE_OFFSITE_SSH'
#!/usr/bin/env bash
set -u
shift
cmd="$*"
printf '%s\n' "$cmd" >> "$SSH_LOG"
cat >/dev/null || true
case "$cmd" in
  *"find '/srv/backups' -maxdepth 1 -type f -name 'backup-*.manifest'"*)
    [ ! -f "$BACKUP_STATE" ] || printf 'backup-20260719-120099.manifest\n'
    ;;
  *"test -x '/srv/newapi/deploy/ops/backup.sh'"*) exit 0 ;;
  *"BACKUP_DIR='/srv/backups' '/srv/newapi/deploy/ops/backup.sh'"*)
    touch "$BACKUP_STATE"
    ;;
  *"manifest_sha="*"encrypted_file="*) printf 'missing\n' ;;
  *"bash '/srv/newapi.offsite-stage-"*"/deploy/ops/offsite-copy.sh' '/srv/backups/backup-20260719-120099.manifest'"*)
    [[ "$cmd" == *"cat '/srv/ops.lock/owner'"* ]] || exit 91
    [[ "$cmd" == *"OPS_LOCK_DIR='/srv/ops.lock' OPS_LOCK_TOKEN='deploy-"* ]] || exit 92
    exit 23
    ;;
esac
exit 0
FAKE_OFFSITE_SSH
  chmod +x "$fake/git" "$fake/ssh"

  if PATH="$fake:$PATH" GIT_LOG="$git_log" SSH_LOG="$ssh_log" BACKUP_STATE="$backup_state" \
    SKIP_PREFLIGHT=1 LOCAL_REPO="$tmp/local" SSH_HOST=fake SERVER_REPO=/srv/newapi \
    COMPOSE_FILE=/srv/newapi/deploy/docker-compose.test.yml ENV_FILE=/srv/newapi/.env \
    BACKUP_DIR=/srv/backups ARCHIVE_DIR=/srv/archive OPS_LOCK_DIR=/srv/ops.lock \
    bash "$ROOT/deploy/ops/deploy.sh" >"$output" 2>&1; then
    fail "deploy continued after the bootstrapped offsite verification failed"
  fi

  grep -Fq '本次发布前备份未完成异地加密上传' "$output" \
    || { cat "$output" >&2; fail "deploy did not explain the offsite fail-closed abort"; }
  grep -Fxq 'archive --format=tar HEAD -- deploy/ops/lib.sh deploy/ops/offsite-copy.sh' "$git_log" \
    || fail "deploy did not source the bootstrap helper exclusively from git archive HEAD"
  grep -Fq "BACKUP_DIR='/srv/backups' '/srv/newapi/deploy/ops/backup.sh'" "$ssh_log" \
    || fail "deploy did not pass the selected BACKUP_DIR to the old backup script"
  grep -Fq "'/srv/backups/backup-20260719-120099.manifest'" "$ssh_log" \
    || fail "deploy did not bind offsite verification to the one newly-created manifest"

  backup_line="$(grep -nF "BACKUP_DIR='/srv/backups' '/srv/newapi/deploy/ops/backup.sh'" "$ssh_log" | head -n1 | cut -d: -f1)"
  offsite_line="$(grep -nF "/deploy/ops/offsite-copy.sh' '/srv/backups/backup-20260719-120099.manifest'" "$ssh_log" | tail -n1 | cut -d: -f1)"
  [ -n "$backup_line" ] && [ -n "$offsite_line" ] && [ "$backup_line" -lt "$offsite_line" ] \
    || fail "offsite verification did not happen after the new paired backup"
  if grep -Eq "docker image inspect '/?newapi_test-app:latest|/srv/archive/src-[0-9]{8}-[0-9]{6}|/srv/newapi\.stage-[0-9]{8}-[0-9]{6}|deploy-build\.status" "$ssh_log"; then
    fail "deploy reached :prev/release archive/tree swap/build after offsite failure"
  fi
  if grep -Eq "(cp|install|mv)[[:space:]].*'/srv/newapi/deploy/ops/(lib|offsite-copy)\.sh'" "$ssh_log"; then
    fail "bootstrap helper overlaid the currently running release tree"
  fi
)

test_deploy_accepts_bound_receipt_without_duplicate_offsite_copy() (
  set -euo pipefail
  local tmp fake output ssh_log git_log backup_state
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  output="$tmp/deploy.out"
  ssh_log="$tmp/ssh.log"
  git_log="$tmp/git.log"
  backup_state="$tmp/backup.done"
  mkdir -p "$fake" "$tmp/local"

  cat > "$fake/git" <<'FAKE_RECEIPT_GIT'
#!/usr/bin/env bash
set -u
printf '%s\n' "$*" >> "$GIT_LOG"
case "$*" in
  "rev-parse --short HEAD") printf 'abc1234\n' ;;
  "rev-parse --abbrev-ref HEAD") printf 'main\n' ;;
  "status --porcelain --untracked-files=all") exit 0 ;;
  "tag -f "*) exit 0 ;;
  *) exit 97 ;;
esac
FAKE_RECEIPT_GIT
  cat > "$fake/ssh" <<'FAKE_RECEIPT_SSH'
#!/usr/bin/env bash
set -u
shift
cmd="$*"
printf '%s\n' "$cmd" >> "$SSH_LOG"
cat >/dev/null || true
case "$cmd" in
  *"find '/srv/backups' -maxdepth 1 -type f -name 'backup-*.manifest'"*)
    [ ! -f "$BACKUP_STATE" ] || printf 'backup-20260719-120100.manifest\n'
    ;;
  *"test -x '/srv/newapi/deploy/ops/backup.sh'"*) exit 0 ;;
  *"BACKUP_DIR='/srv/backups' '/srv/newapi/deploy/ops/backup.sh'"*)
    touch "$BACKUP_STATE"
    ;;
  *"manifest_sha="*"encrypted_file="*)
    [[ "$cmd" == *"source_manifest=backup-20260719-120100.manifest"* ]] || exit 91
    [[ "$cmd" == *"source_manifest_sha256=\$manifest_sha"* ]] || exit 92
    [[ "$cmd" == *"verification=full-download-sha256"* ]] || exit 93
    printf 'valid\n'
    ;;
  *"docker image inspect 'newapi_test-app:latest'"*) exit 61 ;;
esac
exit 0
FAKE_RECEIPT_SSH
  chmod +x "$fake/git" "$fake/ssh"

  if PATH="$fake:$PATH" GIT_LOG="$git_log" SSH_LOG="$ssh_log" BACKUP_STATE="$backup_state" \
    SKIP_PREFLIGHT=1 LOCAL_REPO="$tmp/local" SSH_HOST=fake SERVER_REPO=/srv/newapi \
    COMPOSE_FILE=/srv/newapi/deploy/docker-compose.test.yml ENV_FILE=/srv/newapi/.env \
    BACKUP_DIR=/srv/backups ARCHIVE_DIR=/srv/archive OPS_LOCK_DIR=/srv/ops.lock \
    bash "$ROOT/deploy/ops/deploy.sh" >"$output" 2>&1; then
    fail "receipt fixture unexpectedly reached a complete deploy"
  fi

  grep -Fq '已由服务器 backup.sh 完成异地回读验真' "$output" \
    || { cat "$output" >&2; fail "deploy rejected a strictly-bound existing receipt"; }
  if grep -Fq 'archive --format=tar HEAD -- deploy/ops/lib.sh deploy/ops/offsite-copy.sh' "$git_log"; then
    fail "deploy uploaded the bootstrap helper even though the new backup already had a valid receipt"
  fi
  if grep -Fq '/deploy/ops/offsite-copy.sh' "$ssh_log"; then
    fail "deploy repeated offsite-copy for a manifest with a valid bound receipt"
  fi
  grep -Fq 'source_manifest_sha256=' "$ssh_log" \
    || fail "deploy accepted an existing receipt without binding it to the actual manifest SHA-256"
)

test_deploy_rejects_dirty_checkout_before_remote_access() (
  set -euo pipefail
  local tmp fake output ssh_log
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  output="$tmp/deploy.out"
  ssh_log="$tmp/ssh.log"
  mkdir -p "$fake" "$tmp/local"
  cat > "$fake/git" <<'FAKE_DIRTY_GIT'
#!/usr/bin/env bash
case "$*" in
  "rev-parse --short HEAD") printf 'abc1234\n' ;;
  "rev-parse --abbrev-ref HEAD") printf 'main\n' ;;
  "status --porcelain --untracked-files=all") printf '?? runner-only-file\n' ;;
  *) exit 1 ;;
esac
FAKE_DIRTY_GIT
  cat > "$fake/ssh" <<'FAKE_DIRTY_SSH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SSH_LOG"
exit 99
FAKE_DIRTY_SSH
  chmod +x "$fake/git" "$fake/ssh"

  if PATH="$fake:$PATH" SSH_LOG="$ssh_log" SKIP_PREFLIGHT=1 \
    LOCAL_REPO="$tmp/local" SSH_HOST=fake SERVER_REPO=/srv/newapi \
    COMPOSE_FILE=/srv/newapi/deploy/docker-compose.test.yml ENV_FILE=/srv/newapi/.env \
    bash "$ROOT/deploy/ops/deploy.sh" > "$output" 2>&1; then
    fail "deploy accepted a dirty checkout"
  fi
  grep -Fq '工作树或 index 非干净状态' "$output" \
    || fail "dirty checkout refusal did not explain the reproducibility boundary"
  [ ! -s "$ssh_log" ] || fail "dirty checkout reached the server before refusal"
)

test_all_deploy_remote_programs_parse() (
  set -euo pipefail
  local tmp fake output
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  output="$tmp/deploy.out"
  mkdir -p "$fake" "$tmp/local"
  printf 'placeholder\n' > "$tmp/local/README"
  cat > "$fake/git" <<'FAKE_PARSE_GIT'
#!/usr/bin/env bash
case "$*" in
  "rev-parse --short HEAD") printf 'abc1234\n' ;;
  "rev-parse --abbrev-ref HEAD") printf 'main\n' ;;
  "status --porcelain --untracked-files=all") exit 0 ;;
  "archive --format=tar HEAD -- "*) /usr/bin/tar -cf - . ;;
  "tag -f "*) exit 0 ;;
  *) exit 1 ;;
esac
FAKE_PARSE_GIT
  cat > "$fake/ssh" <<'FAKE_PARSE_SSH'
#!/usr/bin/env bash
set -u
shift
cmd="$*"
cat >/dev/null || true
bash -n -c "$cmd" || exit 90
case "$cmd" in
  *"find '"*"/backups' -maxdepth 1 -type f -name 'backup-*.manifest'"*)
    [ ! -f "$SSH_BACKUP_STATE" ] || printf 'backup-20260719-120099.manifest\n'
    ;;
  *"/deploy/ops/backup.sh'"*) touch "$SSH_BACKUP_STATE" ;;
  *"manifest_sha="*"encrypted_file="*) printf 'missing\n' ;;
  *"cat '"*"/deploy-build.status' 2>/dev/null"*) printf '0\n' ;;
  *"docker image inspect -f"*":latest"*) printf 'sha256:new\n' ;;
  *"docker image inspect -f"*":prev"*) printf 'sha256:old\n' ;;
esac
FAKE_PARSE_SSH
  chmod +x "$fake/git" "$fake/ssh"

  PATH="$fake:$PATH" SSH_BACKUP_STATE="$tmp/backup.done" SKIP_PREFLIGHT=1 LOCAL_REPO="$tmp/local" SSH_HOST=fake \
    SERVER_REPO="$tmp/server" COMPOSE_FILE="$tmp/server/deploy/docker-compose.test.yml" \
    ENV_FILE="$tmp/server/.env" ARCHIVE_DIR="$tmp/archive" OPS_LOCK_DIR="$tmp/ops.lock" \
    HEALTH_TIMEOUT=30 ARCHIVE_KEEP=2 \
    bash "$ROOT/deploy/ops/deploy.sh" >"$output" 2>&1 \
    || { cat "$output" >&2; fail "a remote deploy command is not valid shell"; }
  grep -Fq '部署成功' "$output" || fail "full fake deploy did not reach verified success"
)

test_acme_checksum_failure_writes_nothing() (
  set -euo pipefail
  local tmp fake output
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  output="$tmp/acme.out"
  mkdir -p "$fake"
  cat > "$fake/curl" <<'FAKE_CURL'
#!/usr/bin/env bash
set -u
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then shift; out="$1"; fi
  shift
done
[ -n "$out" ] || exit 2
printf 'not-the-pinned-archive\n' > "$out"
FAKE_CURL
  cat > "$fake/systemctl" <<'FAKE_SYSTEMCTL'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
FAKE_SYSTEMCTL
  chmod +x "$fake/curl" "$fake/systemctl"

  if PATH="$fake:$PATH" SYSTEMCTL=systemctl SYSTEMCTL_LOG="$tmp/systemctl.log" \
    TMPDIR="$tmp" ACME_BIN="$tmp/acme-home/acme.sh" \
    WEBROOT="$tmp/webroot" CERT_ROOT="$tmp/certs" STATE_DIR="$tmp/state" \
    LOG_FILE="$tmp/cert.log" SYSTEMD_DIR="$tmp/systemd" \
    bash "$ROOT/scripts/install-acme.sh" >"$output" 2>&1; then
    fail "ACME installer accepted a checksum mismatch"
  fi
  [ ! -e "$tmp/acme-home" ] || fail "ACME code executed before checksum verification"
  [ ! -e "$tmp/systemd" ] || fail "systemd units were written before checksum verification"
  [ ! -e "$tmp/webroot" ] || fail "runtime directories were written before checksum verification"
  [ ! -e "$tmp/systemctl.log" ] || fail "systemctl ran before checksum verification"
)

test_restore_requires_maintenance_before_mutation() (
  set -euo pipefail
  local tmp fake db redis config manifest docker_log
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  db="$tmp/db-20260719-120002.sql.gz"
  redis="$tmp/redis-20260719-120002.rdb"
  config="$tmp/config-20260719-120002.tar.gz"
  manifest="$tmp/backup-20260719-120002.manifest"
  docker_log="$tmp/docker.log"
  mkdir -p "$fake" "$tmp/config-src" "$tmp/server"
  printf 'CREATE DATABASE `new-api-test`;\n' | gzip > "$db"
  printf 'fake-rdb\n' > "$redis"
  printf 'config\n' > "$tmp/config-src/value"
  tar czf "$config" -C "$tmp/config-src" .
  {
    printf 'format=newapi-backup-v1\n'
    printf 'timestamp=20260719-120002\n'
    printf 'stack=newapi_test\n'
    printf 'db_name=new-api-test\n'
    printf 'consistency=writers-stopped\n'
    printf 'app_version=release-v1\n'
    printf 'redis_keys=1\n'
    printf 'redis_persistent_keys=1\n'
    printf 'db_file=%s\n' "$(basename "$db")"
    printf 'db_sha256=%s\n' "$(sha256sum "$db" | awk '{print $1}')"
    printf 'redis_file=%s\n' "$(basename "$redis")"
    printf 'redis_sha256=%s\n' "$(sha256sum "$redis" | awk '{print $1}')"
    printf 'config_file=%s\n' "$(basename "$config")"
    printf 'config_sha256=%s\n' "$(sha256sum "$config" | awk '{print $1}')"
  } > "$manifest"
  cat > "$fake/docker" <<'FAKE_RESTORE_DOCKER'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$*" in
  "compose "*" ps -a -q app") printf 'app-cid\n' ;;
  "compose "*" ps -a -q redis") printf 'redis-cid\n' ;;
  "inspect -f {{.Image}} redis-cid") printf 'redis-image-id\n' ;;
  "inspect -f {{range .Mounts}}{{if eq .Destination /data}}{{.Name}}{{end}}{{end}} redis-cid") printf 'redis-volume\n' ;;
  "run --rm "*) exit 0 ;;
  *) printf 'unexpected restore docker call: %s\n' "$*" >&2; exit 91 ;;
esac
FAKE_RESTORE_DOCKER
  chmod +x "$fake/docker"
  if PATH="$fake:$PATH" DOCKER_LOG="$docker_log" DB_PASS=test-password \
    REDIS_PASS=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
    SERVER_REPO="$tmp/server" COMPOSE_FILE="$tmp/server/compose.yml" ENV_FILE="$tmp/server/.env" \
    BACKUP_DIR="$tmp/backups" OPS_LOCK_DIR="$tmp/ops.lock" \
    bash "$ROOT/deploy/ops/restore.sh" "$manifest" >"$tmp/restore.out" 2>&1; then
    fail "restore proceeded without MAINTENANCE_CONFIRMED=1"
  fi
  if grep -Eq ' compose .* (stop|start|up) ' "$docker_log"; then
    fail "restore mutated services before maintenance confirmation"
  fi
  [ ! -e "$tmp/ops.lock" ] || fail "restore did not release ops lock after pre-mutation refusal"
)

test_restore_orchestration_contract_converts_rdb_to_aof_and_reconciles() (
  set -euo pipefail
  local tmp fake selected db redis config manifest docker_log app_state redis_state
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  selected="$tmp/selected"
  docker_log="$tmp/docker.log"
  app_state="$tmp/app-running"
  redis_state="$tmp/redis-running"
  mkdir -p "$fake" "$selected/config-src" "$tmp/server/deploy" "$tmp/cert" "$tmp/backups"
  printf 'true\n' > "$app_state"
  printf 'true\n' > "$redis_state"
  printf 'current-v1\n' > "$tmp/server/VERSION"
  printf '{"version":"current-v1"}\n' > "$tmp/server/deploy-manifest.json"
  printf 'services: {}\n' > "$tmp/server/deploy/docker-compose.test.yml"
  {
    printf 'MYSQL_ROOT_PASSWORD=test-password\n'
    printf 'REDIS_APP_USER=newapi\n'
    printf 'REDIS_PASSWORD=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
  } > "$tmp/server/.env"
  printf 'nginx\n' > "$tmp/nginx.conf"
  printf 'cert\n' > "$tmp/cert/fullchain.pem"

  db="$selected/db-20260719-120003.sql.gz"
  redis="$selected/redis-20260719-120003.rdb"
  config="$selected/config-20260719-120003.tar.gz"
  manifest="$selected/backup-20260719-120003.manifest"
  printf 'CREATE DATABASE `new-api-test`;\n' | gzip > "$db"
  printf 'restored-rdb-with-one-key\n' > "$redis"
  printf 'context\n' > "$selected/config-src/value"
  tar czf "$config" -C "$selected/config-src" .
  {
    printf 'format=newapi-backup-v1\n'
    printf 'timestamp=20260719-120003\n'
    printf 'stack=newapi_test\n'
    printf 'db_name=new-api-test\n'
    printf 'consistency=writers-stopped\n'
    printf 'app_version=old-v1\n'
    # The snapshot held two TTL keys that expired before restore plus one
    # permanent key. Loading one key is therefore the correct delayed result.
    printf 'redis_keys=3\n'
    printf 'redis_persistent_keys=1\n'
    printf 'db_file=%s\n' "$(basename "$db")"
    printf 'db_sha256=%s\n' "$(sha256sum "$db" | awk '{print $1}')"
    printf 'redis_file=%s\n' "$(basename "$redis")"
    printf 'redis_sha256=%s\n' "$(sha256sum "$redis" | awk '{print $1}')"
    printf 'config_file=%s\n' "$(basename "$config")"
    printf 'config_sha256=%s\n' "$(sha256sum "$config" | awk '{print $1}')"
  } > "$manifest"

  cat > "$fake/curl" <<'FAKE_RESTORE_CURL'
#!/usr/bin/env bash
case "${*: -1}" in
  */health/live|*/health/ready) printf '{"status":"ok"}\n' ;;
  */api/status) printf '{"success":true,"data":{"version":"current-v1"}}\n' ;;
  *) exit 22 ;;
esac
FAKE_RESTORE_CURL
  cat > "$fake/docker" <<'FAKE_FULL_RESTORE_DOCKER'
#!/usr/bin/env bash
set -u
args="$*"
printf '%s\n' "$args" >> "$DOCKER_LOG"
persistence() {
  printf 'aof_enabled:1\naof_rewrite_in_progress:0\naof_rewrite_scheduled:0\naof_last_bgrewrite_status:ok\naof_last_write_status:ok\n'
}
if [[ "$args" == compose\ *" exec -T mysql sh -c "*" newapi-mysql-client mysqldump "* ]]; then
  cat >/dev/null
  i=0
  while [ "$i" -lt 300 ]; do printf 'INSERT INTO t VALUES (%s, %s);\n' "$i" "$((i * 7919))"; i=$((i + 1)); done
  exit 0
fi
if [[ "$args" == compose\ *" exec -T mysql sh -c "*" newapi-mysql-client mysql "* ]]; then
  cat >/dev/null
  if [[ "$args" == *"SELECT SCHEMA_NAME"* ]]; then printf 'new-api-test\n'; fi
  exit 0
fi
if [[ "$args" == compose\ *" exec -T redis sh -c "*" newapi-redis-client newapi "* ]]; then
  cat >/dev/null
  case "$args" in
    *" newapi-redis-client newapi SAVE") exit 0 ;;
    *" newapi-redis-client newapi --raw PING") printf 'PONG\n' ;;
    *" newapi-redis-client newapi --raw INFO keyspace") printf '# Keyspace\ndb0:keys=1,expires=0,avg_ttl=0\n' ;;
    *" newapi-redis-client newapi --raw INFO persistence") persistence ;;
    *) printf 'unexpected authenticated Redis call: %s\n' "$args" >&2; exit 96 ;;
  esac
  exit 0
fi
case "$args" in
  "compose "*" ps -a -q app") printf 'app-cid\n' ;;
  "compose "*" ps -a -q redis") printf 'redis-cid\n' ;;
  "inspect -f {{.Image}} app-cid") printf 'app-image-id\n' ;;
  "inspect -f {{.Image}} redis-cid") printf 'redis-image-id\n' ;;
  "inspect -f {{range .Mounts}}"*"redis-cid") printf 'redis-volume\n' ;;
  "inspect -f {{.State.Running}} app-cid") cat "$APP_STATE" ;;
  "inspect -f {{.State.Running}} redis-cid") cat "$REDIS_STATE" ;;
  "run --rm app-image-id --version") printf 'current-v1\n' ;;
  "compose "*" stop app") printf 'false\n' > "$APP_STATE" ;;
  "compose "*" start app") printf 'true\n' > "$APP_STATE" ;;
  "compose "*" stop redis") printf 'false\n' > "$REDIS_STATE" ;;
  "compose "*" start redis") printf 'true\n' > "$REDIS_STATE" ;;
  "compose "*" exec -T mysql sh -c "*) exit 0 ;;
  "compose "*" exec -T redis redis-check-rdb /data/dump.rdb") exit 0 ;;
  "compose "*" cp redis:/data/dump.rdb "*)
    destination="${!#}"
    printf 'pre-restore-rdb\n' > "$destination"
    ;;
  "run --rm -v "*"--entrypoint redis-check-rdb "*) exit 0 ;;
  "run --rm --user 0 "*) exit 0 ;;
  "run -d --rm --name "*) printf 'bootstrap-id\n' ;;
  "exec newapi-restore-"*" redis-cli --raw PING") printf 'PONG\n' ;;
  "exec newapi-restore-"*" redis-cli --raw INFO keyspace") printf '# Keyspace\ndb0:keys=1,expires=0,avg_ttl=0\n' ;;
  "exec newapi-restore-"*" redis-cli --raw CONFIG SET appendonly yes") printf 'OK\n' ;;
  "exec newapi-restore-"*" redis-cli --raw INFO persistence") persistence ;;
  "exec newapi-restore-"*" sh -c test -s /data/appendonlydir/appendonly.aof.manifest") exit 0 ;;
  "stop -t 30 newapi-restore-"*) exit 0 ;;
  "ps --filter label=com.docker.compose.project=newapi_test --filter label=com.docker.compose.service=mysql -q") printf 'mysql-cid\n' ;;
  *) printf 'unexpected full restore docker call: %s\n' "$args" >&2; exit 96 ;;
esac
FAKE_FULL_RESTORE_DOCKER
  chmod +x "$fake/curl" "$fake/docker"

  printf 'newapi_test\n' | PATH="$fake:$PATH" DOCKER_LOG="$docker_log" \
    APP_STATE="$app_state" REDIS_STATE="$redis_state" DB_PASS=test-password \
    REDIS_PASS=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa BACKUP_OFFSITE_REQUIRED=0 \
    MAINTENANCE_CONFIRMED=1 SERVER_REPO="$tmp/server" \
    COMPOSE_FILE="$tmp/server/deploy/docker-compose.test.yml" ENV_FILE="$tmp/server/.env" \
    BACKUP_DIR="$tmp/backups" OPS_LOCK_DIR="$tmp/ops.lock" \
    NGINX_VHOST="$tmp/nginx.conf" NGINX_CERT_DIR="$tmp/cert" READINESS_TIMEOUT=5 REDIS_AOF_TIMEOUT=5 \
    bash "$ROOT/deploy/ops/restore.sh" "$manifest" >"$tmp/restore-full.out" 2>&1 \
    || { cat "$tmp/restore-full.out" >&2; cat "$docker_log" >&2; fail "restore orchestration contract did not complete"; }

  grep -Fq 'redis-server --appendonly no' "$docker_log" || fail "restore did not load RDB with AOF disabled"
  grep -Fq 'CONFIG SET appendonly yes' "$docker_log" || fail "restore did not convert the live RDB dataset to AOF"
  grep -Fq 'DROP DATABASE IF EXISTS' "$docker_log" || fail "restore did not drop the target database before import"
  grep -Fq '配对恢复全部闸门通过' "$tmp/restore-full.out" || fail "restore reported no verified completion"
  [ "$(cat "$app_state")" = true ] || fail "app was not running after verified restore"
  [ "$(cat "$redis_state")" = true ] || fail "Redis was not running after verified restore"
  [ ! -e "$tmp/ops.lock" ] || fail "successful restore did not release the shared lock"
)

test_full_paired_release_rollback() (
  set -euo pipefail
  local tmp fake server target archive stem runtime docker_log
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  server="$tmp/server"
  target="$tmp/target"
  archive="$tmp/archive"
  stem="prev-20260719-120004"
  runtime="$tmp/runtime-version"
  pre_runtime="$tmp/pre-runtime-version"
  docker_log="$tmp/docker.log"
  mkdir -p "$fake" "$server/deploy/ops" "$target/deploy/ops" "$archive"

  printf 'FROM scratch\n' > "$server/Dockerfile"
  printf 'services: {}\n' > "$server/deploy/docker-compose.test.yml"
  printf '#!/usr/bin/env bash\n' > "$server/deploy/ops/rollback.sh"
  chmod +x "$server/deploy/ops/rollback.sh"
  printf 'current-v1\n' > "$server/VERSION"
  printf '{"version":"current-v1"}\n' > "$server/deploy-manifest.json"
  printf 'secret-runtime-env\n' > "$server/.env"
  printf 'must-disappear\n' > "$server/stale-only"

  printf 'FROM scratch\n' > "$target/Dockerfile"
  printf 'services: {}\n' > "$target/deploy/docker-compose.test.yml"
  printf '#!/usr/bin/env bash\n' > "$target/deploy/ops/rollback.sh"
  chmod +x "$target/deploy/ops/rollback.sh"
  printf 'from-prev-release\n' > "$target/target-only"
  tar czf "$archive/$stem.tgz" -C "$target" .
  printf 'prev-v1\n' > "$archive/$stem.version"
  printf '{"version":"prev-v1"}\n' > "$archive/$stem.manifest.json"
  {
    printf 'format=newapi-release-v1\n'
    printf 'timestamp=20260719-120004\n'
    printf 'archive_file=%s.tgz\n' "$stem"
    printf 'archive_sha256=%s\n' "$(sha256sum "$archive/$stem.tgz" | awk '{print $1}')"
    printf 'version_file=%s.version\n' "$stem"
    printf 'version_sha256=%s\n' "$(sha256sum "$archive/$stem.version" | awk '{print $1}')"
    printf 'deploy_manifest_file=%s.manifest.json\n' "$stem"
    printf 'deploy_manifest_sha256=%s\n' "$(sha256sum "$archive/$stem.manifest.json" | awk '{print $1}')"
    printf 'version=prev-v1\n'
  } > "$archive/$stem.release"
  printf '%s.release\n' "$stem" > "$archive/prev.current"
  printf 'current-v1\n' > "$runtime"

  cat > "$fake/curl" <<'FAKE_ROLLBACK_CURL'
#!/usr/bin/env bash
case "${*: -1}" in
  */health/live) printf '{"status":"ok"}\n' ;;
  */health/ready)
    if [ "${FAIL_BAD_RELEASE:-0}" = 1 ] && [ "$(cat "$RUNTIME_VERSION")" = bad-v1 ]; then exit 22; fi
    printf '{"status":"ok"}\n'
    ;;
  */api/status) printf '{"success":true,"data":{"version":"%s"}}\n' "$(cat "$RUNTIME_VERSION")" ;;
  *) exit 22 ;;
esac
FAKE_ROLLBACK_CURL
  cat > "$fake/docker" <<'FAKE_ROLLBACK_DOCKER'
#!/usr/bin/env bash
set -u
args="$*"
printf '%s\n' "$args" >> "$DOCKER_LOG"
case "$args" in
  "image inspect newapi_test-app:latest"|"image inspect newapi_test-app:prev") exit 0 ;;
  "compose "*" ps -a -q app") printf 'app-cid\n' ;;
  "inspect -f {{.Image}} app-cid") printf 'current-image-id\n' ;;
  "run --rm current-image-id --version") cat "$RUNTIME_VERSION" ;;
  "run --rm newapi_test-app:prev --version") printf 'prev-v1\n' ;;
  "tag current-image-id newapi_test-app:pre-rollback") cp "$RUNTIME_VERSION" "$PRE_RUNTIME_VERSION" ;;
  "tag newapi_test-app:prev newapi_test-app:latest") printf 'prev-v1\n' > "$RUNTIME_VERSION" ;;
  "tag newapi_test-app:pre-rollback newapi_test-app:latest") cp "$PRE_RUNTIME_VERSION" "$RUNTIME_VERSION" ;;
  "compose "*" up -d --build") printf 'bad-v1\n' > "$RUNTIME_VERSION" ;;
  "compose "*" up -d --no-build") exit 0 ;;
  *) printf 'unexpected rollback docker call: %s\n' "$args" >&2; exit 95 ;;
esac
FAKE_ROLLBACK_DOCKER
  chmod +x "$fake/curl" "$fake/docker"

  PATH="$fake:$PATH" RUNTIME_VERSION="$runtime" PRE_RUNTIME_VERSION="$pre_runtime" DOCKER_LOG="$docker_log" ASSUME_YES=1 \
    SERVER_REPO="$server" COMPOSE_FILE="$server/deploy/docker-compose.test.yml" ENV_FILE="$server/.env" \
    ARCHIVE_DIR="$archive" OPS_LOCK_DIR="$tmp/ops.lock" READINESS_TIMEOUT=3 READINESS_INTERVAL=1 \
    bash "$ROOT/deploy/ops/rollback.sh" >"$tmp/rollback.out" 2>&1 \
    || { cat "$tmp/rollback.out" >&2; cat "$docker_log" >&2; fail "paired release rollback failed"; }

  [ "$(tr -d '\r\n' < "$server/VERSION")" = prev-v1 ] || fail "rollback did not activate the target VERSION"
  [ -f "$server/target-only" ] || fail "rollback did not activate the target source tree"
  [ ! -e "$server/stale-only" ] || fail "rollback overlaid the target and retained a stale file"
  [ "$(cat "$server/.env")" = secret-runtime-env ] || fail "rollback did not preserve the runtime .env"
  [ "$(cat "$runtime")" = prev-v1 ] || fail "rollback did not activate the paired :prev image"
  [ ! -e "$tmp/ops.lock" ] || fail "successful rollback did not release the shared lock"
  grep -Fq 'release 回滚已验收' "$tmp/rollback.out" || fail "rollback reported no verified success"

  # 再演练一个目标 readiness 失败：必须同时恢复原镜像与原源码树。
  mkdir -p "$tmp/bad/deploy/ops"
  printf 'FROM scratch\n' > "$tmp/bad/Dockerfile"
  printf 'services: {}\n' > "$tmp/bad/deploy/docker-compose.test.yml"
  printf '#!/usr/bin/env bash\n' > "$tmp/bad/deploy/ops/rollback.sh"
  chmod +x "$tmp/bad/deploy/ops/rollback.sh"
  printf 'bad-release\n' > "$tmp/bad/bad-only"
  tar czf "$archive/src-20260719-120005.tgz" -C "$tmp/bad" .
  printf 'bad-v1\n' > "$archive/src-20260719-120005.version"
  printf '{"version":"bad-v1"}\n' > "$archive/src-20260719-120005.manifest.json"
  {
    printf 'format=newapi-release-v1\n'
    printf 'timestamp=20260719-120005\n'
    printf 'archive_file=src-20260719-120005.tgz\n'
    printf 'archive_sha256=%s\n' "$(sha256sum "$archive/src-20260719-120005.tgz" | awk '{print $1}')"
    printf 'version_file=src-20260719-120005.version\n'
    printf 'version_sha256=%s\n' "$(sha256sum "$archive/src-20260719-120005.version" | awk '{print $1}')"
    printf 'deploy_manifest_file=src-20260719-120005.manifest.json\n'
    printf 'deploy_manifest_sha256=%s\n' "$(sha256sum "$archive/src-20260719-120005.manifest.json" | awk '{print $1}')"
    printf 'version=bad-v1\n'
  } > "$archive/src-20260719-120005.release"

  if PATH="$fake:$PATH" RUNTIME_VERSION="$runtime" PRE_RUNTIME_VERSION="$pre_runtime" \
    DOCKER_LOG="$docker_log" FAIL_BAD_RELEASE=1 ASSUME_YES=1 \
    SERVER_REPO="$server" COMPOSE_FILE="$server/deploy/docker-compose.test.yml" ENV_FILE="$server/.env" \
    ARCHIVE_DIR="$archive" OPS_LOCK_DIR="$tmp/ops.lock" READINESS_TIMEOUT=2 READINESS_INTERVAL=1 \
    bash "$ROOT/deploy/ops/rollback.sh" --to 20260719-120005 >"$tmp/rollback-fail.out" 2>&1; then
    fail "rollback reported success for a target whose readiness failed"
  fi
  grep -Fq '原 release prev-v1 已完整恢复并通过验收' "$tmp/rollback-fail.out" \
    || { cat "$tmp/rollback-fail.out" >&2; fail "rollback did not verify recovery of the original release"; }
  [ "$(tr -d '\r\n' < "$server/VERSION")" = prev-v1 ] || fail "failed rollback did not restore original source VERSION"
  [ -f "$server/target-only" ] && [ ! -e "$server/bad-only" ] || fail "failed rollback left a mixed source tree"
  [ "$(cat "$runtime")" = prev-v1 ] || fail "failed rollback did not restore original image"
  [ ! -e "$tmp/ops.lock" ] || fail "verified rollback recovery did not release the shared lock"
)

test_health_and_https_contract() (
  set -euo pipefail
  local tmp file
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT

  grep -Fq '/health/live' "$ROOT/deploy/docker-compose.test.yml" || fail "compose has no liveness probe"
  grep -Fq '/health/live' "$ROOT/deploy/ops/healthcheck.sh" || fail "ops healthcheck has no liveness probe"
  grep -Fq '/health/ready' "$ROOT/deploy/ops/healthcheck.sh" || fail "ops healthcheck has no readiness probe"
  if grep -Eq 'AUTH_(SVC|PORT)|auth/healthz' "$ROOT/deploy/ops/healthcheck.sh" "$ROOT/deploy/ops/lib.sh"; then
    fail "retired auth-service is still a health dependency"
  fi

  for file in \
    "$ROOT/deploy/nginx/wildcard.wedreamhub.com.conf" \
    "$ROOT/deploy/nginx/tokendream.wedreamhub.com.conf" \
    "$ROOT/deploy/nginx/apex-redirect.wedreamhub.com.conf" \
    "$ROOT/deploy/nginx/api-443-to-origin.wedreamhub.com.conf"; do
    grep -Fq 'listen 80;' "$file" || fail "missing HTTP listener: $file"
    grep -Fq 'return 308 https://' "$file" || fail "HTTP listener is not a 308 HTTPS redirect: $file"
    if grep -Fq '127.0.0.1:8180' "$file"; then fail "retired auth upstream remains: $file"; fi
  done

  mkdir "$tmp/vhosts"
  VHOST_DIR="$tmp/vhosts" WEBROOT="$tmp/webroot" \
    bash -c '. "$1"; write_http_conf customer.example.com; write_full_conf active.example.com' _ \
      "$ROOT/scripts/custom-domain-lib.sh"
  grep -Fq 'location ^~ /.well-known/acme-challenge/' "$tmp/vhosts/custom_customer.example.com.conf" \
    || fail "HTTP-01 exception disappeared"
  grep -Fq 'return 308 https://$host$request_uri;' "$tmp/vhosts/custom_customer.example.com.conf" \
    || fail "pre-certificate custom domain does not redirect non-challenge HTTP"
  if grep -Fq 'proxy_pass' "$tmp/vhosts/custom_customer.example.com.conf"; then
    fail "pre-certificate HTTP vhost still proxies plaintext traffic"
  fi
  grep -Fq 'return 308 https://$host$request_uri;' "$tmp/vhosts/custom_active.example.com.conf" \
    || fail "active custom domain HTTP listener is not 308"
  grep -Fq 'location ^~ /api/internal/ { return 404; }' "$tmp/vhosts/custom_active.example.com.conf" \
    || fail "active custom domain exposes internal-only routes to the public internet"
)

test_secrets_stay_out_of_process_arguments() (
  set -euo pipefail
  local tmp fake db_secret redis_secret internal_secret path
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  mkdir -p "$fake" "$tmp/tmp"

  cat > "$fake/curl" <<'FAKE_SECRET_CURL'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$CURL_ARGV_LOG"
saw_stdin_header=0
while [ "$#" -gt 0 ]; do
  if [ "$1" = -H ] || [ "$1" = --header ]; then
    shift
    [ "${1:-}" = @- ] && saw_stdin_header=1
  fi
  shift || true
done
[ "$saw_stdin_header" = 1 ] || exit 91
cat >> "$CURL_HEADER_CAPTURE"
[ "${FAKE_CURL_FAIL:-0}" != 1 ] || exit 22
printf '{"success":true}\n'
FAKE_SECRET_CURL

  cat > "$fake/docker" <<'FAKE_SECRET_DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$DOCKER_ARGV_LOG"
while [ "$#" -gt 0 ] && [ "$1" != exec ]; do shift; done
[ "${1:-}" = exec ] || exit 94
shift
[ "${1:-}" = -T ] || exit 95
shift
[ "${1:-}" = mysql ] || [ "${1:-}" = redis ] || exit 96
shift
exec "$@"
FAKE_SECRET_DOCKER

  cat > "$fake/mysql" <<'FAKE_SECRET_MYSQL'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$MYSQL_ARGV_LOG"
defaults=""
for arg in "$@"; do
  case "$arg" in --defaults-extra-file=*) defaults="${arg#*=}" ;; esac
done
[ -n "$defaults" ] && [ -f "$defaults" ] || exit 97
mode="$(stat -c %a "$defaults" 2>/dev/null || stat -f %Lp "$defaults")"
[ "$mode" = 600 ] || exit 98
printf '%s\n' "$defaults" >> "$MYSQL_DEFAULT_PATHS"
cat "$defaults" >> "$MYSQL_DEFAULT_CAPTURE"
cat >> "$MYSQL_STDIN_CAPTURE"
[ "${FAKE_MYSQL_FAIL:-0}" != 1 ] || exit 23
FAKE_SECRET_MYSQL
  cat > "$fake/redis-cli" <<'FAKE_SECRET_REDIS'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$REDIS_ARGV_LOG"
printf '%s\n' "${REDISCLI_AUTH:-}" >> "$REDIS_AUTH_CAPTURE"
printf 'PONG\n'
FAKE_SECRET_REDIS
  chmod +x "$fake/curl" "$fake/docker" "$fake/mysql" "$fake/redis-cli"

  internal_secret='internal-secret-must-not-appear-in-argv'
  PATH="$fake:$PATH" CURL_ARGV_LOG="$tmp/curl.argv" \
    CURL_HEADER_CAPTURE="$tmp/curl.header" \
    bash -c '. "$1"; internal_api_curl "$2" -fsS "http://127.0.0.1/internal" >/dev/null' _ \
      "$ROOT/scripts/custom-domain-lib.sh" "$internal_secret"
  if grep -Fq "$internal_secret" "$tmp/curl.argv"; then
    fail "MT_INTERNAL_SECRET leaked into curl argv"
  fi
  grep -Fxq "X-Internal-Secret: $internal_secret" "$tmp/curl.header" \
    || fail "internal API helper did not transmit the secret header"
  if PATH="$fake:$PATH" FAKE_CURL_FAIL=1 CURL_ARGV_LOG="$tmp/curl.argv" \
    CURL_HEADER_CAPTURE="$tmp/curl.header" \
    bash -c '. "$1"; internal_api_curl "$2" -fsS "http://127.0.0.1/internal" >/dev/null' _ \
      "$ROOT/scripts/custom-domain-lib.sh" "$internal_secret"; then
    fail "internal API helper hid a curl failure"
  fi

  db_secret='db secret "quoted" \ path # ; $(not-executed)'
  PATH="$fake:$PATH" DB_PASS="$db_secret" DOCKER_ARGV_LOG="$tmp/docker.argv" \
    MYSQL_ARGV_LOG="$tmp/mysql.argv" MYSQL_DEFAULT_PATHS="$tmp/mysql.paths" \
    MYSQL_DEFAULT_CAPTURE="$tmp/mysql.defaults" MYSQL_STDIN_CAPTURE="$tmp/mysql.stdin" \
    SERVER_REPO="$tmp/server" COMPOSE_FILE="$tmp/compose.yml" ENV_FILE="$tmp/.env" \
    bash -c '. "$1"; require_db_pass; printf "SELECT 1;\n" | mysql_with_secret_input mysql -N' _ \
      "$ROOT/deploy/ops/lib.sh"
  if grep -Fq "$db_secret" "$tmp/docker.argv" "$tmp/mysql.argv"; then
    fail "DB_PASS leaked into docker/mysql argv"
  fi
  grep -Fxq 'password="db secret \"quoted\" \\ path # ; $(not-executed)"' "$tmp/mysql.defaults" \
    || fail "MySQL helper did not create the protected client option"
  grep -Fxq 'SELECT 1;' "$tmp/mysql.stdin" \
    || fail "MySQL import helper did not preserve caller stdin"
  if PATH="$fake:$PATH" DB_PASS="$db_secret" FAKE_MYSQL_FAIL=1 DOCKER_ARGV_LOG="$tmp/docker.argv" \
    MYSQL_ARGV_LOG="$tmp/mysql.argv" MYSQL_DEFAULT_PATHS="$tmp/mysql.paths" \
    MYSQL_DEFAULT_CAPTURE="$tmp/mysql.defaults" MYSQL_STDIN_CAPTURE="$tmp/mysql.stdin" \
    SERVER_REPO="$tmp/server" COMPOSE_FILE="$tmp/compose.yml" ENV_FILE="$tmp/.env" \
    bash -c '. "$1"; require_db_pass; mysql_with_secret mysql -Nse "SELECT 1"' _ \
      "$ROOT/deploy/ops/lib.sh"; then
    fail "MySQL secret wrapper hid a client failure"
  fi
  while IFS= read -r path; do
    [ ! -e "$path" ] || fail "MySQL temporary defaults file survived client exit: $path"
  done < "$tmp/mysql.paths"

  redis_secret='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  PATH="$fake:$PATH" REDIS_PASS="$redis_secret" REDIS_USER=newapi \
    DOCKER_ARGV_LOG="$tmp/docker.argv" REDIS_ARGV_LOG="$tmp/redis.argv" \
    REDIS_AUTH_CAPTURE="$tmp/redis.auth" SERVER_REPO="$tmp/server" \
    COMPOSE_FILE="$tmp/compose.yml" ENV_FILE="$tmp/.env" \
    bash -c '. "$1"; require_redis_pass; redis_cli_service --raw PING' _ \
      "$ROOT/deploy/ops/lib.sh" | grep -Fxq PONG
  if grep -Fq "$redis_secret" "$tmp/docker.argv" "$tmp/redis.argv"; then
    fail "REDIS_PASSWORD leaked into docker/redis-cli argv"
  fi
  grep -Fxq "$redis_secret" "$tmp/redis.auth" \
    || fail "Redis helper did not inject authentication through stdin/environment"

  if grep -R -nE --include='*.sh' -- '-p"?\$DB_PASS|-H "X-Internal-Secret: \$\{?[A-Za-z_]' \
      "$ROOT/deploy/ops" "$ROOT/scripts" >/dev/null; then
    fail "an executable ops script still places a production secret in process argv"
  fi
)

test_payment_topology_and_demo_safety_contract() (
  set -euo pipefail
  local tmp fake calls output compose env_example demo readme payment_runbook
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  calls="$tmp/external-calls.log"
  output="$tmp/demo.out"
  compose="$ROOT/deploy/docker-compose.test.yml"
  env_example="$ROOT/deploy/.env.test.example"
  demo="$ROOT/deploy/demo/demo.sh"
  readme="$ROOT/deploy/demo/README.md"
  payment_runbook="$ROOT/docs/vendor/payments/deploy-real-payments.md"

  grep -Fq 'MT_INTERNAL_SECRET: "${MT_INTERNAL_SECRET:?' "$compose" \
    || fail "Compose does not fail closed when MT_INTERNAL_SECRET is missing"
  grep -Fq 'MT_PAY_NOTIFY_BASE' "$compose" \
    || fail "Compose no longer passes the in-process payment callback base"
  if grep -Eq '^[[:space:]]{2}(auth-service|auth_service):|127\.0\.0\.1:8180|AUTH_(MOCK|WXPAY|ALIPAY)' "$compose"; then
    fail "production Compose resurrected the retired payment service"
  fi
  grep -Fq '系统设置 → 集成 → 支付' "$env_example" \
    || fail "environment template does not direct credentials to the in-process provider settings"
  if grep -Eq '(^|[^[:digit:]])8180([^[:digit:]]|$)|^AUTH_' "$env_example"; then
    fail "environment template still exposes a retired payment port or AUTH_* configuration"
  fi

  grep -Fq '/api/pay/wechat/notify' "$payment_runbook" \
    || fail "payment runbook does not document the current WeChat callback"
  grep -Fq '/api/pay/alipay/notify' "$payment_runbook" \
    || fail "payment runbook does not document the current Alipay callback"
  if grep -Eq '8180|AUTH_MOCK|AUTH_(WXPAY|ALIPAY)|/pay/wxpay/notify|/auth/alipay/notify' "$payment_runbook"; then
    fail "payment runbook still contains an executable retired-topology recipe"
  fi
  if grep -R -nE --include='*.md' \
    '8180|auth-service/config\.yaml|AUTH_(MOCK|WXPAY|ALIPAY)|/pay/wxpay/notify|/auth/alipay/notify|/api/payment/(wechat|alipay)/notify' \
    "$ROOT/docs/vendor/payments" >/dev/null; then
    fail "a vendor payment guide still points project operators at a retired topology or callback"
  fi
  if grep -nE \
    '8180|auth-service/config\.yaml|AUTH_(MOCK|WXPAY|ALIPAY)|/pay/wxpay/notify|/auth/alipay/notify|/api/payment/(wechat|alipay)/notify' \
    "$ROOT/doc/proposal.md" "$ROOT/doc/acceptance.md" "$ROOT/doc/api-contract.md" >/dev/null; then
    fail "an authoritative project document still presents the retired payment topology"
  fi
  if grep -R -nE --include='*.md' '凭据.*加密(落库|持久化)|从加密配置初始化' \
    "$ROOT/docs/vendor/payments" "$env_example" >/dev/null; then
    fail "payment documentation claims at-rest credential encryption that the options store does not implement"
  fi

  grep -Fq 'ALLOW_REAL_PAYMENT_DEMO=1' "$demo" \
    || fail "real-payment demo has no explicit opt-in gate"
  if grep -Eq 'confirm_pay|AUTH_(MOCK|PORT|SVC)|127\.0\.0\.1:8180' "$demo" "$readme"; then
    fail "demo still contains a mock callback or retired payment topology"
  fi
  if awk '/\.\/demo\.sh/ && $0 !~ /ALLOW_REAL_PAYMENT_DEMO=1/ { bad=1 } END { exit bad ? 0 : 1 }' "$demo" "$readme"; then
    fail "a documented demo command can bypass the explicit real-payment opt-in"
  fi

  mkdir -p "$fake"
  for command in curl docker; do
    cat > "$fake/$command" <<'FAKE_EXTERNAL'
#!/usr/bin/env bash
printf '%s\n' "$0 $*" >> "$EXTERNAL_CALLS"
exit 99
FAKE_EXTERNAL
    chmod +x "$fake/$command"
  done
  if PATH="$fake:$PATH" EXTERNAL_CALLS="$calls" DB_PASS=test-password \
    SERVER_REPO="$tmp/server" ALLOW_REAL_PAYMENT_DEMO=0 \
    bash "$demo" >"$output" 2>&1; then
    fail "real-payment demo ran without ALLOW_REAL_PAYMENT_DEMO=1"
  fi
  grep -Fq 'ALLOW_REAL_PAYMENT_DEMO=1' "$output" \
    || fail "default demo refusal does not explain the required real-payment opt-in"
  [ ! -s "$calls" ] || fail "default demo refusal invoked Docker or HTTP before the funds gate"

  if grep -nH --fixed-strings './rollback.sh --git' "$ROOT/deploy/ops"/*.md >/dev/null; then
    fail "an ops runbook still presents the retired --git rollback as executable"
  fi
)

test_session_security_deployment_docs_contract() (
  set -euo pipefail
  local quick="$ROOT/docker-compose.yml"
  local dev="$ROOT/docker-compose.dev.yml"
  local production="$ROOT/deploy/docker-compose.test.yml"
  local root_env_example="$ROOT/.env.example"
  local env_example="$ROOT/deploy/.env.test.example"
  local empty_secret redis_compose
  local readme

  for local_compose in "$quick" "$dev"; do
    grep -Fq 'DEPLOYMENT_ENV=development' "$local_compose" \
      || fail "local HTTP Compose does not explicitly opt into development mode: $local_compose"
    grep -Fq 'SESSION_COOKIE_SECURE=false' "$local_compose" \
      || fail "local HTTP Compose does not explicitly declare its insecure-cookie scope: $local_compose"
    grep -Fq '/health/ready' "$local_compose" \
      || fail "local Compose still lacks dependency-aware readiness: $local_compose"
    if grep -Fq '/api/status' "$local_compose"; then
      fail "local Compose still treats the metadata endpoint as health: $local_compose"
    fi
  done

  for redis_compose in "$quick" "$dev"; do
    grep -Fq 'image: redis:7.4.9-alpine' "$redis_compose" \
      || fail "Compose does not pin the verified Redis patch image: $redis_compose"
    grep -Fq -- '--appendonly' "$redis_compose" \
      || fail "Compose does not enable Redis AOF persistence: $redis_compose"
    if grep -Eq 'image:[[:space:]]+redis:(latest|7-alpine)([[:space:]#]|$)' "$redis_compose"; then
      fail "Compose still uses a mutable Redis tag: $redis_compose"
    fi
  done
  grep -Fq 'redis:8.8.0@sha256:234c902a2db49461a129e2d4aeff85b28cf20187ed274a67f6e50995fa713c7b' "$production" \
    || fail "production Redis is not pinned to the verified immutable digest"
  grep -Fq 'mysql:8.2@sha256:212fe73edca5df6ff14826d5eb975c914bfb91f82a2e923f9050568f99525da1' "$production" \
    || fail "production MySQL is not pinned to the verified immutable digest"
  grep -Fq -- '--appendonly yes --aclfile' "$production" \
    || fail "production Redis does not enable AOF together with an ACL file"
  grep -Fq 'redis_data:/data' "$quick" \
    || fail "Quick Start Redis has no persistent named volume"
  grep -Fq 'redis_data:' "$quick" \
    || fail "Quick Start does not declare its Redis named volume"
  grep -Fq 'redis-cli --no-auth-warning' "$quick" \
    || fail "Quick Start Redis has no authenticated healthcheck"
  [ "$(grep -Fc 'condition: service_healthy' "$quick")" -ge 2 ] \
    || fail "Quick Start app does not wait for healthy PostgreSQL and Redis"

  grep -Fq 'DEPLOYMENT_ENV: "production"' "$production" \
    || fail "production Compose does not explicitly select production security"
  grep -Fq 'SESSION_COOKIE_SECURE: "true"' "$production" \
    || fail "production Compose does not require Secure cookies"
  grep -Fq 'SESSION_SECRET: "${SESSION_SECRET:?' "$production" \
    || fail "production Compose does not fail closed on missing SESSION_SECRET"
  grep -Fq 'CRYPTO_SECRET: "${CRYPTO_SECRET:?' "$production" \
    || fail "production Compose does not fail closed on missing CRYPTO_SECRET"
  grep -Fq 'SQL_DSN: "${MYSQL_APP_USER:-newapi}:${MYSQL_APP_PASSWORD:?' "$production" \
    || fail "production app does not fail closed on a dedicated MySQL credential"
  if grep -Eq 'SQL_DSN:[[:space:]]*"?root:' "$production"; then
    fail "production app still connects to MySQL as root"
  fi
  grep -Fq 'REDIS_CONN_STRING: "redis://${REDIS_APP_USER:-newapi}:${REDIS_PASSWORD:?' "$production" \
    || fail "production app does not use an authenticated Redis ACL URI"
  grep -Fq 'user default off' "$production" \
    || fail "production Redis does not disable the anonymous default user"
  grep -Fq 'exec docker-entrypoint.sh redis-server' "$production" \
    || fail "production Redis bypasses the official non-root entrypoint"
  grep -Fq -- '-flushall -flushdb' "$production" \
    || fail "production Redis ACL still grants destructive database flush commands"
  grep -Fq 'mysql-access-bootstrap:' "$production" \
    || fail "production Compose has no existing-volume user bootstrap"
  grep -Fq 'BATCH_UPDATE_ENABLED: "false"' "$production" \
    || fail "production Compose re-enabled crash-lossy quota/stat batching"
  grep -Fq 'AGENT_HOOK_ASYNC_ENABLED: "false"' "$production" \
    || fail "production Compose re-enabled in-memory agent earning buffering"
  grep -Fq -- '--innodb-flush-log-at-trx-commit=1' "$production" \
    || fail "production MySQL does not durably flush every committed redo record"
  grep -Fq -- '--sync-binlog=1' "$production" \
    || fail "production MySQL does not durably flush every committed binlog record"
  if grep -Eq -- '--innodb-flush-log-at-trx-commit=(0|2)|--sync-binlog=0' "$production"; then
    fail "production MySQL still permits acknowledged financial commits to vanish on host failure"
  fi
  if grep -Eq 'Redis 为额度权威|可花余额以 Redis 为准|只会少计代理收益' "$production"; then
    fail "production Compose still documents a lossy cache/earning model as safe"
  fi

  for empty_secret in MT_INTERNAL_SECRET MYSQL_ROOT_PASSWORD MYSQL_APP_PASSWORD REDIS_PASSWORD SESSION_SECRET CRYPTO_SECRET UPSTREAM_API_KEY; do
    grep -Eq "^${empty_secret}=$" "$env_example" \
      || fail "environment example must leave $empty_secret empty so Compose cannot accept a known placeholder"
  done
  if grep -Eq 'change-me|replace-with|random_string' "$env_example"; then
    fail "environment example contains a known secret placeholder that could pass validation"
  fi

  for empty_secret in POSTGRES_PASSWORD REDIS_PASSWORD MYSQL_ROOT_PASSWORD CLICKHOUSE_PASSWORD SESSION_SECRET CRYPTO_SECRET; do
    grep -Eq "^${empty_secret}=$" "$root_env_example" \
      || fail "Quick Start environment example must leave $empty_secret empty"
  done
  if grep -nE '123456|sk-your-[[:alnum:]_-]+' \
    "$quick" "$dev" "$production" "$root_env_example" "$env_example" "$ROOT"/README*.md >/dev/null; then
    fail "deployment entry point contains a copyable literal password or API-key placeholder"
  fi

  for readme in "$ROOT/README.md" "$ROOT/README.zh_CN.md"; do
    grep -Fq '`DEPLOYMENT_ENV`' "$readme" \
      || fail "README omits the fail-closed deployment mode: $readme"
    grep -Fq '`SESSION_COOKIE_SECURE`' "$readme" \
      || fail "README omits the production Secure-cookie requirement: $readme"
  done
  for readme in "$ROOT"/README*.md; do
    grep -Fq 'DEPLOYMENT_ENV=development' "$readme" \
      || fail "README does not explain the explicit local HTTP opt-in: $readme"
    grep -Fq 'SESSION_COOKIE_SECURE=false' "$readme" \
      || fail "README Docker example does not scope insecure cookies to local HTTP: $readme"
  done
)

test_documented_commands_match_build_drivers() (
  set -euo pipefail
  local readme="$ROOT/electron/README.md"
  local build="$ROOT/electron/build.sh"
  local main_image="$ROOT/Dockerfile"
  local auth_image="$ROOT/Dockerfile.authservice"
  local dev_image="$ROOT/Dockerfile.dev"
  local go_manifest="$ROOT/go.mod"

  if grep -Eq '(^|[^[:alnum:]_])TODO([^[:alnum:]_]|$)|npm[[:space:]]+install|npm[[:space:]]+start' "$readme"; then
    fail "Electron README still documents a placeholder or nonexistent command"
  fi
  grep -Fq './electron/build.sh' "$readme" || fail "Electron README bypasses the repository build driver"
  grep -Fq 'npm ci' "$readme" || fail "Electron README does not follow its lockfile install path"
  grep -Fq 'npm run dev-app' "$readme" || fail "Electron README does not name the real development script"
  grep -Fq 'web/default' "$readme" && grep -Fq 'web/classic' "$readme" \
    || fail "Electron README does not describe both frontend themes"
  grep -Fq -- '--version' "$readme" || fail "Electron README omits backend version verification"

  grep -Fq '(cd default && VITE_REACT_APP_VERSION="$VERSION" bun run build)' "$build" \
    || fail "Electron build driver no longer builds Default with the release version"
  grep -Fq '(cd classic && VITE_REACT_APP_VERSION="$VERSION" bun run build)' "$build" \
    || fail "Electron build driver no longer builds Classic with the release version"
  grep -Fq 'verify_backend_version' "$build" || fail "Electron build driver no longer verifies the backend version"
  grep -Fq '"dev-app":' "$ROOT/electron/package.json" || fail "documented Electron development script is absent"
  if grep -Fq 'cd web && bun dev' "$ROOT/electron/main.js"; then
    fail "Electron runtime guidance still names a nonexistent workspace-root dev script"
  fi
  grep -Fq 'cd web/default && bun run dev' "$ROOT/electron/main.js" \
    || fail "Electron runtime guidance does not name the Default development server"
  if grep -Fq '"from": "../web/dist"' "$ROOT/electron/package.json"; then
    fail "Electron packaging still references the nonexistent workspace-root web/dist"
  fi

  grep -Fq 'Legacy/archive-only auth-service image' "$auth_image" \
    || fail "retired auth-service image is not marked legacy/archive-only"
  grep -Fq 'must not be added to production Compose or health/readiness' "$auth_image" \
    || fail "retired auth-service image lacks the production dependency warning"
  if grep -E '^FROM[[:space:]]+' "$auth_image" | grep -Ev '@sha256:[0-9a-f]{64}([[:space:]]|$)' >/dev/null; then
    fail "legacy/archive image contains a floating builder or runtime base"
  fi
  grep -Fq 'golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2' "$auth_image" \
    || fail "legacy image does not reuse the audited main Go builder digest"
  grep -Fq 'golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2' "$main_image" \
    || fail "main image does not pin the audited Go 1.26.5 builder digest"
  grep -Fq 'golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2' "$dev_image" \
    || fail "development image does not reuse the audited Go builder digest"
  grep -Fxq 'go 1.26.5' "$go_manifest" \
    || fail "Go manifest does not require the patched standard library toolchain"
  grep -Fq 'debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a' "$auth_image" \
    || fail "legacy image does not reuse the audited main runtime digest"
  if grep -E '^FROM[[:space:]]+' "$dev_image" | grep -Ev '@sha256:[0-9a-f]{64}([[:space:]]|$)' >/dev/null; then
    fail "local development image contains a floating builder or runtime base"
  fi
  grep -Fq 'github.com/QuantumNous/new-api/common.Version=${VER}' "$dev_image" \
    || fail "local development image does not inject the full version symbol"
  grep -Fq 'v0.0.0-dev' "$dev_image" \
    || fail "local development image can overwrite the version identity with an empty string"

  bash "$ROOT/scripts/lint-deploy-helpers.sh" "$ROOT/deploy/ops" \
    || fail "deploy helper visibility lint rejected the executable release scripts"
)

test_formal_electron_release_requires_verified_signatures() (
  local workflow="$ROOT/.github/workflows/electron-build.yml"

  grep -Fq 'environment: release-publish' "$workflow" \
    || fail "Electron build does not enter the protected release Environment"
  grep -Fq 'workflow_dispatch must select an existing version tag' "$workflow" \
    || fail "manual Electron releases can run from an unprotected branch ref"
  if grep -Fq 'VERSION="dev-' "$workflow"; then
    fail "formal Electron workflow still contains an unsigned development release path"
  fi
  grep -Fq 'WINDOWS_CSC_LINK' "$workflow" \
    || fail "Electron release does not require a Windows signing identity"
  grep -Fq 'WINDOWS_CSC_KEY_PASSWORD' "$workflow" \
    || fail "Electron release does not require the signing-key password"
  grep -Fq 'Get-AuthenticodeSignature' "$workflow" \
    || fail "Electron release does not verify Authenticode signatures"
  grep -Fq "signature.Status -ne 'Valid'" "$workflow" \
    || fail "Electron release does not fail on an invalid signature"
  grep -Fq 'signature.TimeStamperCertificate' "$workflow" \
    || fail "Electron release does not require a trusted timestamp"

  local verify_line checksum_line upload_line
  verify_line=$(grep -nF 'Verify Authenticode signatures' "$workflow" | cut -d: -f1)
  checksum_line=$(grep -nF 'Generate Electron checksums' "$workflow" | cut -d: -f1)
  upload_line=$(grep -nF 'Upload artifacts' "$workflow" | head -n 1 | cut -d: -f1)
  [ -n "$verify_line" ] && [ -n "$checksum_line" ] && [ -n "$upload_line" ] \
    || fail "Electron signing/checksum/upload steps are incomplete"
  [ "$verify_line" -lt "$checksum_line" ] && [ "$checksum_line" -lt "$upload_line" ] \
    || fail "Electron artifacts are checksummed or uploaded before signature verification"
)

test_gitee_sync_requires_governed_tag_source() (
  local workflow="$ROOT/.github/workflows/sync-to-gitee.yml"

  grep -Fq 'name: Verify governed release source' "$workflow" \
    || fail "Gitee sync has no independent source-verification job"
  grep -Fq '^v?[0-9]+\.[0-9]+\.[0-9]+$' "$workflow" \
    || fail "Gitee sync does not require a strict semantic-version tag"
  grep -Fq 'EXPECTED_REF="refs/tags/$TAG_NAME"' "$workflow" \
    || fail "Gitee sync does not bind its input to the triggering tag ref"
  grep -Fq 'test "$GITHUB_REF" = "$EXPECTED_REF"' "$workflow" \
    || fail "Gitee sync does not reject a mismatched triggering ref"
  grep -Fq 'TAG_COMMIT=$(git rev-parse "refs/tags/$TAG_NAME^{commit}")' "$workflow" \
    || fail "Gitee sync does not resolve the requested tag commit"
  grep -Fq 'test "$TAG_COMMIT" = "$GITHUB_SHA"' "$workflow" \
    || fail "Gitee sync does not bind the tag commit to the workflow source"
  grep -Fq 'git merge-base --is-ancestor "$TAG_COMMIT" refs/remotes/origin/main' "$workflow" \
    || fail "Gitee sync does not require main-branch ancestry"
  grep -Fq 'bash scripts/verify-github-environments.sh release-publish' "$workflow" \
    || fail "Gitee sync does not audit the strict release Environment"
  grep -Fq 'name: Publish governed release to Gitee' "$workflow" \
    || fail "Gitee sync has no distinct publishing job"
  if grep -Fq 'runs-on: sync' "$workflow"; then
    fail "Gitee publishing depends on an unregistered self-hosted runner"
  fi
  grep -Fq 'runs-on: ubuntu-latest' "$workflow" \
    || fail "Gitee publishing has no available hosted runner"
  grep -Fq 'environment: release-publish' "$workflow" \
    || fail "Gitee publishing does not enter the protected release Environment"
  grep -Fq 'ref: ${{ needs.verify.outputs.commit }}' "$workflow" \
    || fail "Gitee publishing does not check out the verified commit"
  grep -Fq 'GITEE_TARGET_COMMITISH: ${{ needs.verify.outputs.commit }}' "$workflow" \
    || fail "Gitee release target is not bound to the verified commit"
  if grep -Fq 'nICEnnnnnnnLee/action-gitee-release' "$workflow"; then
    fail "Gitee publishing still executes an external release action"
  fi
  grep -Fq 'GITEE_TOKEN: ${{ secrets.GITEE_TOKEN }}' "$workflow" \
    || fail "Gitee publishing does not pass its token through the step environment"
  grep -Fq 'run: python3 scripts/gitee_release.py' "$workflow" \
    || fail "Gitee publishing does not use the repository-owned release client"
  if grep -Eq 'run:.*(GITEE_TOKEN|secrets\.GITEE_TOKEN)|pip[[:space:]]+install' "$workflow"; then
    fail "Gitee publishing exposes its token in argv or installs runtime dependencies"
  fi
  grep -Fq -- '--json name,body,tagName,targetCommitish,assets' "$workflow" \
    || fail "Gitee sync does not read the authoritative GitHub asset manifest"
  grep -Fq 'python3 scripts/gitee_release.py verify-github-assets' "$workflow" \
    || fail "Gitee sync does not verify downloaded assets against the manifest"
  if grep -Fq 'gh release download "$TAG_NAME" --dir ./release_assets ||' "$workflow"; then
    fail "Gitee sync treats GitHub asset download failures as an empty release"
  fi
  grep -Fq "python3 -m unittest discover -s scripts/tests -p 'test_*.py'" "$ROOT/scripts/preflight.sh" \
    || fail "Gitee release client tests are absent from the shared preflight"
)

test_restore_drill_covers_production_and_lts_candidate() (
  local workflow="$ROOT/.github/workflows/restore-drill.yml"

  grep -Fq 'mysql_image: mysql:8.2@sha256:212fe73edca5df6ff14826d5eb975c914bfb91f82a2e923f9050568f99525da1' "$workflow" \
    || fail "restore drill does not cover the immutable production MySQL baseline"
  grep -Fq 'mysql_image: mysql:8.4.10@sha256:c592c15aaf4a1961e15d82eb31ea5987dda862d1c4b1e93424438c0e91dc1f8d' "$workflow" \
    || fail "restore drill does not cover the immutable MySQL 8.4 LTS candidate"
  grep -Fq 'MYSQL_IMAGE: ${{ matrix.mysql_image }}' "$workflow" \
    || fail "restore drill matrix does not pass its immutable MySQL image to the drill"
  grep -Fq "REQUIRE_DOCKER: '1'" "$workflow" \
    || fail "authoritative restore drill can silently skip without Docker"
  if grep -Eq 'mysql_image:[[:space:]]+mysql:(latest|8|8\.4)([[:space:]#]|$)' "$workflow"; then
    fail "restore drill contains a floating MySQL image"
  fi
)

test_database_compatibility_images_are_immutable() (
  local workflow="$ROOT/.github/workflows/ci.yml"

  grep -Fq 'image: mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b' "$workflow" \
    || fail "database compatibility CI does not pin its MySQL image"
  grep -Fq 'image: postgres:16@sha256:33f923b05f64ca54ac4401c01126a6b92afe839a0aa0a52bc5aeb5cc958e5f20' "$workflow" \
    || fail "database compatibility CI does not pin its PostgreSQL image"
  grep -Fq 'image: clickhouse/clickhouse-server:24.8@sha256:1ffa82edee000a42c09313bd9f1293d94c570aee74babc1b3ca9983a35fa597b' "$workflow" \
    || fail "database compatibility CI does not pin its ClickHouse image"
  if grep -A70 -F 'database-compatibility:' "$workflow" \
    | grep -E '^[[:space:]]+image:[[:space:]]+' \
    | grep -Ev '@sha256:[0-9a-f]{64}$' >/dev/null; then
    fail "database compatibility CI contains a floating service image"
  fi
)

test_backend_security_scanners_are_pinned() (
  local preflight="$ROOT/scripts/preflight.sh"
  local size_gate="$ROOT/scripts/check-go-file-size.sh"
  local package_gate="$ROOT/scripts/check-go-package-scope.sh"

  grep -Fq 'github.com/zricethezav/gitleaks/v8@v8.30.1' "$preflight" \
    || fail "backend preflight does not pin its repository secret scanner"
  grep -Fq 'golang.org/x/vuln/cmd/govulncheck@v1.6.0' "$preflight" \
    || fail "backend preflight does not pin its Go vulnerability scanner"
  grep -Fq 'honnef.co/go/tools/cmd/staticcheck@v0.7.0' "$preflight" \
    || fail "backend preflight does not pin its correctness analyzer"
  if grep -Eq 'go run [^[:space:]]+@latest' "$preflight"; then
    fail "backend preflight executes a floating Go tool version"
  fi
  if grep -Eq '(^|[^[:alnum:]_])rg([[:space:]]|$)' "$size_gate" "$package_gate"; then
    fail "backend source/package gates require optional ripgrep on clean runners"
  fi
  awk '
    /step "准备 go:embed 前端目录"/ {embed = NR}
    /step "Go first-party package 范围门禁"/ {scope = NR}
    END {exit !(embed > 0 && scope > 0 && embed < scope)}
  ' "$preflight" || fail "backend package analysis runs before go:embed placeholders exist"
)

test_offsite_copy_is_encrypted_verified_and_fail_closed() (
  set -euo pipefail
  local tmp fake store manifest db redis config recipient malicious_remote second_manifest encrypted_remote
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  store="$tmp/remote"
  mkdir -p "$fake" "$store" "$tmp/backups" "$tmp/server"
  recipient='age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq'

  make_offsite_fixture() {
    local ts="$1" target
    target="$tmp/backups/backup-$ts.manifest"
    db="$tmp/backups/db-$ts.sql.gz"
    redis="$tmp/backups/redis-$ts.rdb"
    config="$tmp/backups/config-$ts.tar.gz"
    printf 'database\n' > "$db"
    printf 'redis\n' > "$redis"
    printf 'config\n' > "$config"
    {
      printf 'format=newapi-backup-v1\n'
      printf 'timestamp=%s\n' "$ts"
      printf 'stack=newapi_test\n'
      printf 'db_name=new-api-test\n'
      printf 'consistency=writers-stopped\n'
      printf 'app_version=verified-v1\n'
      printf 'redis_keys=1\n'
      printf 'db_file=%s\n' "$(basename "$db")"
      printf 'db_sha256=%s\n' "$(sha256sum "$db" | awk '{print $1}')"
      printf 'redis_file=%s\n' "$(basename "$redis")"
      printf 'redis_sha256=%s\n' "$(sha256sum "$redis" | awk '{print $1}')"
      printf 'config_file=%s\n' "$(basename "$config")"
      printf 'config_sha256=%s\n' "$(sha256sum "$config" | awk '{print $1}')"
    } > "$target"
    printf '%s\n' "$target"
  }

  cat > "$fake/age" <<'FAKE_AGE'
#!/usr/bin/env bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output=$2; shift 2 ;;
    --recipient) shift 2 ;;
    *) exit 64 ;;
  esac
done
[ -n "$output" ]
{ printf 'AGE-ENCRYPTED-FIXTURE\n'; cat; } > "$output"
FAKE_AGE
  cat > "$fake/rclone" <<'FAKE_RCLONE'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = copyto ] || exit 64
shift
[ "${1:-}" != --immutable ] || shift
source=$1
destination=$2
if [[ "$source" == fake:* ]]; then
  cp "$RCLONE_STORE/$(basename "$source")" "$destination"
  [ "${RCLONE_TAMPER_DOWNLOAD:-0}" != 1 ] || printf 'tampered\n' >> "$destination"
else
  cp "$source" "$RCLONE_STORE/$(basename "$destination")"
fi
FAKE_RCLONE
  chmod +x "$fake/age" "$fake/rclone"

  manifest="$(make_offsite_fixture 20260719-120010)"
  malicious_remote="fake:bucket/\$(touch $tmp/pwned)"
  PATH="$fake:$PATH" RCLONE_STORE="$store" BACKUP_DIR="$tmp/backups" \
    SERVER_REPO="$tmp/server" ENV_FILE="$tmp/missing.env" \
    BACKUP_OFFSITE_REQUIRED=1 BACKUP_OFFSITE_REMOTE="$malicious_remote" \
    BACKUP_OFFSITE_AGE_RECIPIENT="$recipient" \
    bash "$ROOT/deploy/ops/offsite-copy.sh" "$manifest" >/dev/null
  [ ! -e "$tmp/pwned" ] || fail "offsite destination was evaluated as shell code"
  [ -s "$tmp/backups/offsite-20260719-120010.receipt" ] \
    || fail "verified offsite upload wrote no receipt"
  grep -Fq 'verification=full-download-sha256' "$tmp/backups/offsite-20260719-120010.receipt" \
    || fail "offsite receipt does not prove full-download verification"
  encrypted_remote="$(find "$store" -maxdepth 1 -type f -name 'newapi-newapi_test-20260719-120010-*.tar.gz.age' -print | head -n1)"
  [ -n "$encrypted_remote" ] && grep -Fq 'AGE-ENCRYPTED-FIXTURE' "$encrypted_remote" \
    || fail "offsite upload bypassed encryption"

  second_manifest="$(make_offsite_fixture 20260719-120011)"
  if PATH="$fake:$PATH" RCLONE_STORE="$store" RCLONE_TAMPER_DOWNLOAD=1 \
    BACKUP_DIR="$tmp/backups" SERVER_REPO="$tmp/server" ENV_FILE="$tmp/missing.env" \
    BACKUP_OFFSITE_REQUIRED=1 BACKUP_OFFSITE_REMOTE='fake:bucket/backups' \
    BACKUP_OFFSITE_AGE_RECIPIENT="$recipient" \
    bash "$ROOT/deploy/ops/offsite-copy.sh" "$second_manifest" >/dev/null 2>&1; then
    fail "offsite copy accepted a corrupted round-trip download"
  fi
  [ ! -e "$tmp/backups/offsite-20260719-120011.receipt" ] \
    || fail "failed offsite verification committed a success receipt"

  if PATH="$fake:$PATH" RCLONE_STORE="$store" BACKUP_DIR="$tmp/backups" \
    SERVER_REPO="$tmp/server" ENV_FILE="$tmp/missing.env" \
    BACKUP_OFFSITE_REQUIRED= BACKUP_OFFSITE_REMOTE= BACKUP_OFFSITE_AGE_RECIPIENT= \
    bash "$ROOT/deploy/ops/offsite-copy.sh" "$second_manifest" >/dev/null 2>&1; then
    fail "production default accepted missing offsite configuration"
  fi
  if grep -nE '^[[:space:]]*eval[[:space:]]' "$ROOT/deploy/ops/offsite-copy.sh" >/dev/null; then
    fail "offsite path executes configuration through eval"
  fi
)

test_github_environment_audit_is_read_only_and_fail_closed() (
  set -euo pipefail
  local tmp fake evidence audit_calls compatible_calls
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  fake="$tmp/bin"
  evidence="$tmp/evidence.json"
  mkdir -p "$fake"
  cat > "$fake/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$GH_CALLS"
[ "$1" = api ] || exit 64
case "$*" in
  *container-publish/deployment-branch-policies)
    if [ "${GH_BAD_RULES:-0}" = 1 ]; then
      printf '{"branch_policies":[{"name":"main","type":"branch"}]}\n'
    else
      printf '{"branch_policies":[{"name":"v*","type":"tag"},{"name":"nightly","type":"branch"},{"name":"[0-9]*","type":"tag"},{"name":"main","type":"branch"},{"name":"alpha","type":"branch"}]}\n'
    fi
    ;;
  *release-publish/deployment-branch-policies)
    printf '{"branch_policies":[{"name":"v*","type":"tag"},{"name":"[0-9]*","type":"tag"}]}\n'
    ;;
  *container-publish|*release-publish)
    if [ "${GH_ENTERPRISE_RULES:-0}" = 1 ]; then
      printf '{"can_admins_bypass":false,"protection_rules":[{"type":"required_reviewers","reviewers":[{"type":"User","reviewer":{"login":"reviewer"}}]},{"type":"wait_timer","wait_timer":5}],"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}\n'
    else
      printf '{"can_admins_bypass":true,"protection_rules":[],"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}\n'
    fi
    ;;
  *) exit 1 ;;
esac
FAKE_GH
  chmod +x "$fake/gh"

  PATH="$fake:$PATH" GH_CALLS="$tmp/gh.calls" \
    GITHUB_ENVIRONMENT_APPROVALS_REQUIRED=false \
    GITHUB_REPOSITORY=owner/repository GITHUB_ENVIRONMENT_EVIDENCE_OUT="$evidence" \
    bash "$ROOT/scripts/verify-github-environments.sh" >/dev/null \
    || fail "Environment audit rejected the approved private-repository policy fixture"
  [ "$(jq 'length' "$evidence")" = 2 ] || fail "Environment audit did not emit both sanitized records"
  jq -e 'all(.[]; .approval_protection_required == false and .branch_policy == true)' \
    "$evidence" >/dev/null || fail "Environment audit lost the active policy mode"
  [ "$(jq '[.[].deployment_policies | length] | add' "$evidence")" = 7 ] \
    || fail "Environment audit did not preserve the seven approved deployment policies"
  if PATH="$fake:$PATH" GH_CALLS="$tmp/gh.calls" GH_BAD_RULES=1 \
    GITHUB_ENVIRONMENT_APPROVALS_REQUIRED=false GITHUB_REPOSITORY=owner/repository \
    bash "$ROOT/scripts/verify-github-environments.sh" container-publish >/dev/null 2>&1; then
    fail "Environment audit accepted a deployment policy outside the approved baseline"
  fi
  if PATH="$fake:$PATH" GH_CALLS="$tmp/gh.calls" \
    GITHUB_REPOSITORY=owner/repository \
    bash "$ROOT/scripts/verify-github-environments.sh" container-publish >/dev/null 2>&1; then
    fail "Environment audit disabled approval protection without an explicit policy setting"
  fi
  PATH="$fake:$PATH" GH_CALLS="$tmp/gh.calls" GH_ENTERPRISE_RULES=1 \
    GITHUB_REPOSITORY=owner/repository \
    bash "$ROOT/scripts/verify-github-environments.sh" >/dev/null \
    || fail "Environment audit rejected the strict approval-protected fixture"
  audit_calls="$(grep -R -h -F 'run: bash scripts/verify-github-environments.sh' \
    "$ROOT/.github/workflows" | wc -l | tr -d '[:space:]')"
  compatible_calls="$(grep -R -h -F "GITHUB_ENVIRONMENT_APPROVALS_REQUIRED: 'false'" \
    "$ROOT/.github/workflows" | wc -l | tr -d '[:space:]')"
  [ "$audit_calls" -gt 0 ] || fail "no repository workflow verifies GitHub Environments"
  [ "$compatible_calls" = "$audit_calls" ] \
    || fail "every repository Environment audit must declare the private-repository approval policy"
  if grep -Eq '(^|[[:space:]])(put|post|patch|delete)([[:space:]]|$)' "$tmp/gh.calls"; then
    fail "Environment audit attempted a mutating GitHub API method"
  fi
)

test_clean_checkout_gate_rejects_runner_inputs() (
  set -euo pipefail
  local tmp sha
  tmp="$(mktemp -d)"
  trap 'rm -rf -- "$tmp"' EXIT
  git -C "$tmp" init -q
  git -C "$tmp" config user.name test
  git -C "$tmp" config user.email test@example.invalid
  printf 'tracked\n' > "$tmp/tracked.txt"
  git -C "$tmp" add tracked.txt
  git -C "$tmp" commit -qm initial
  sha="$(git -C "$tmp" rev-parse HEAD)"
  (cd "$tmp" && EXPECTED_CHECKOUT_SHA="$sha" bash "$ROOT/scripts/verify-clean-checkout.sh" >/dev/null)
  printf 'runner-only\n' > "$tmp/untracked.txt"
  if (cd "$tmp" && bash "$ROOT/scripts/verify-clean-checkout.sh" >/dev/null 2>&1); then
    fail "clean-checkout gate accepted an untracked runner input"
  fi
)

run_test "manifest tamper detection and clean release extraction" test_manifest_checksum_and_clean_extract
run_test "shared ops lock rejects a different operation owner" test_ops_lock_rejects_unrelated_owner
run_test "Redis backup failure aborts and removes the partial set" test_redis_backup_failure_is_fatal
run_test "deploy stops before mutation when backup fails" test_deploy_aborts_on_backup_failure
run_test "deploy bootstraps current-HEAD offsite verification before release mutation" test_deploy_bootstraps_current_head_offsite_before_release_mutation
run_test "deploy reuses a strictly-bound receipt without duplicate offsite copy" test_deploy_accepts_bound_receipt_without_duplicate_offsite_copy
run_test "deploy rejects a dirty checkout before server access" test_deploy_rejects_dirty_checkout_before_remote_access
run_test "every remote deploy program parses through the success path" test_all_deploy_remote_programs_parse
run_test "ACME checksum mismatch executes and writes nothing" test_acme_checksum_failure_writes_nothing
run_test "restore refuses to mutate before maintenance confirmation" test_restore_requires_maintenance_before_mutation
run_test "restore orchestration contract converts RDB to AOF, restarts, and reconciles" test_restore_orchestration_contract_converts_rdb_to_aof_and_reconciles
run_test "paired rollback swaps image and source as one verified release" test_full_paired_release_rollback
run_test "health probes and every HTTP vhost enforce the production contract" test_health_and_https_contract
run_test "internal and database secrets stay out of process arguments" test_secrets_stay_out_of_process_arguments
run_test "single-stack payment docs and demo fail closed before real funds" test_payment_topology_and_demo_safety_contract
run_test "local and production session-security deployment modes stay explicit" test_session_security_deployment_docs_contract
run_test "documented Electron and legacy-image commands match executable build topology" test_documented_commands_match_build_drivers
run_test "formal Electron artifacts require valid timestamped Authenticode signatures" test_formal_electron_release_requires_verified_signatures
run_test "Gitee sync requires a governed tag source and protected publishing job" test_gitee_sync_requires_governed_tag_source
run_test "restore drill covers immutable production and MySQL LTS images" test_restore_drill_covers_production_and_lts_candidate
run_test "database compatibility CI uses immutable service images" test_database_compatibility_images_are_immutable
run_test "backend security scanners are enabled and immutable" test_backend_security_scanners_are_pinned
run_test "offsite backup encrypts, round-trip verifies, and fails closed" test_offsite_copy_is_encrypted_verified_and_fail_closed
run_test "GitHub Environment audit is read-only and fail-closed" test_github_environment_audit_is_read_only_and_fail_closed
run_test "clean checkout gate rejects runner-supplied inputs" test_clean_checkout_gate_rejects_runner_inputs
printf '1..%d\n' "$TESTS"
