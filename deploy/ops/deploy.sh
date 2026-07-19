#!/usr/bin/env bash
# deploy.sh —— 从 Mac 仓库根将 newapi_test 发布到服务器。
#
# 不变量：
#   1. 旧版数据配对备份失败即中止，不存在“带病继续发布”。
#   2. 只允许干净 Git checkout，并从 HEAD 对象生成归档；忽略文件/runner 残留
#      无法混入发布。归档只解包到全新 staging，再原子换树。
#   3. 构建成功、MySQL/Redis/app 就绪且线上版本精确匹配后才删旧树。
#   4. 失败后只有回滚版本/readiness 验收通过，才报告“已回滚”。
#
# 用法：
#   ./deploy/ops/deploy.sh
#   SKIP_PREFLIGHT=1 ./deploy/ops/deploy.sh
#   NO_ROLLBACK=1 ./deploy/ops/deploy.sh
#
# 首次空环境若无 mysql 容器，可显式设 ALLOW_INITIAL_NO_BACKUP=1。
# SKIP_BACKUP 被禁用，防止误把备份失败当成可忽略警告。
set -euo pipefail

export LC_ALL=C LANG=C

SSH_HOST="${SSH_HOST:-newapi628}"
LOCAL_REPO="${LOCAL_REPO:-$(cd "$(dirname "$0")/../.." && pwd)}"
SERVER_REPO="${SERVER_REPO:-/root/newapi-test}"
STACK="${STACK:-newapi_test}"
EXPECTED_STACK="${EXPECTED_STACK:-newapi_test}"
COMPOSE_FILE="${COMPOSE_FILE:-$SERVER_REPO/deploy/docker-compose.test.yml}"
ENV_FILE="${ENV_FILE:-$SERVER_REPO/.env}"
APP_SVC="${APP_SVC:-app}"
MYSQL_SVC="${MYSQL_SVC:-mysql}"
APP_PORT="${APP_PORT:-3100}"
HOST_HEADER="${HOST_HEADER:-tokendream.wedreamhub.com}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-600}"
ARCHIVE_DIR="${ARCHIVE_DIR:-/root/deploy-archives}"
ARCHIVE_KEEP="${ARCHIVE_KEEP:-7}"
APP_IMG="${STACK}-${APP_SVC}"
DC="docker compose -p $STACK --env-file $ENV_FILE -f $COMPOSE_FILE"

log() { printf '\033[1;34m[deploy]\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m[ ok ]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[fail]\033[0m %s\n' "$*" >&2; exit 1; }
remote() { ssh "$SSH_HOST" "$@"; }

[ "$STACK" = "$EXPECTED_STACK" ] \
  || die "目标栈 '$STACK' ≠ 期望栈 '$EXPECTED_STACK'，拒绝部署。"
[ "${SKIP_BACKUP:-0}" != "1" ] \
  || die "SKIP_BACKUP 已禁用；发布前备份不得绕过。首次空环境仅可用 ALLOW_INITIAL_NO_BACKUP=1。"

TS="$(date +%Y%m%d-%H%M%S)"
TAG="deploy-$TS"
ARCHIVE="$ARCHIVE_DIR/src-$TS.tgz"
VERSION_SIDECAR="$ARCHIVE_DIR/src-$TS.version"
MANIFEST_SIDECAR="$ARCHIVE_DIR/src-$TS.manifest.json"
RELEASE_SIDECAR="$ARCHIVE_DIR/src-$TS.release"
REMOTE_STAGE="${SERVER_REPO}.stage-${TS}"
OLD_TREE="${SERVER_REPO}.pre-${TS}"
OPS_LOCK_DIR="${OPS_LOCK_DIR:-/run/lock/newapi-ops.lock.d}"
OPS_LOCK_TOKEN="deploy-$TS-$$-${RANDOM:-0}"
DEPLOY_LOCK_HELD=0
KEEP_DEPLOY_LOCK=0

valid_remote_path() {
  local path="$1"
  printf '%s\n' "$path" | grep -Eq '^/[A-Za-z0-9._/-]+$' || return 1
  case "$path" in /|/root|/home|/usr|/var|/etc|/opt|/srv|/tmp|*/) return 1 ;; esac
  case "/${path#/}/" in *'/../'*|*'/./'*|*'//'*) return 1 ;; esac
}

valid_remote_file() {
  local path="$1"
  printf '%s\n' "$path" | grep -Eq '^/[A-Za-z0-9._/-]+$' || return 1
  case "$path" in */) return 1 ;; esac
  case "/${path#/}/" in *'/../'*|*'/./'*|*'//'*) return 1 ;; esac
}

valid_remote_path "$SERVER_REPO" || die "SERVER_REPO 不是安全的受管 release 路径：$SERVER_REPO"
valid_remote_path "$ARCHIVE_DIR" || die "ARCHIVE_DIR 不是安全路径：$ARCHIVE_DIR"
valid_remote_path "$OPS_LOCK_DIR" || die "OPS_LOCK_DIR 不是安全路径：$OPS_LOCK_DIR"
valid_remote_file "$COMPOSE_FILE" || die "COMPOSE_FILE 路径非法：$COMPOSE_FILE"
valid_remote_file "$ENV_FILE" || die "ENV_FILE 路径非法：$ENV_FILE"
for safe_name in "$STACK" "$EXPECTED_STACK" "$APP_SVC" "$MYSQL_SVC"; do
  printf '%s\n' "$safe_name" | grep -Eq '^[A-Za-z0-9_-]+$' || die "stack/service 参数含非法字符：$safe_name"
done
printf '%s\n' "$HOST_HEADER" | grep -Eq '^[A-Za-z0-9.-]+$' || die "HOST_HEADER 非法：$HOST_HEADER"
printf '%s\n' "$SSH_HOST" | grep -Eq '^[A-Za-z0-9._@:-]+$' || die "SSH_HOST 非法：$SSH_HOST"
for numeric_value in "$APP_PORT" "$HEALTH_TIMEOUT" "$ARCHIVE_KEEP"; do
  printf '%s\n' "$numeric_value" | grep -Eq '^[0-9]+$' || die "数值参数非法：$numeric_value"
done

GIT_SHA="$(cd "$LOCAL_REPO" && git rev-parse --short HEAD 2>/dev/null || echo nogit)"
GIT_BRANCH="$(cd "$LOCAL_REPO" && git rev-parse --abbrev-ref HEAD 2>/dev/null || echo nogit)"
GIT_DIRTY=false
[ "$GIT_SHA" != nogit ] && [ "$GIT_BRANCH" != nogit ] \
  || die "LOCAL_REPO 必须是可验证 Git checkout，拒绝从临时目录拼装发布。"
if [ -n "$(cd "$LOCAL_REPO" && git status --porcelain --untracked-files=all)" ]; then
  die "工作树或 index 非干净状态；请先形成可审查提交，再从 HEAD 发布。"
fi
APP_VERSION="${GIT_SHA}-${TS}"

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '%s' "$value"
}

MANIFEST_JSON="$(printf \
  '{\"version\":\"%s\",\"git_sha\":\"%s\",\"dirty\":%s,\"branch\":\"%s\",\"deploy_tag\":\"%s\",\"deployed_at\":\"%s\"}' \
  "$(json_escape "$APP_VERSION")" "$(json_escape "$GIT_SHA")" "$GIT_DIRTY" \
  "$(json_escape "$GIT_BRANCH")" "$(json_escape "$TAG")" "$(json_escape "$TS")")"

release_deploy_lock() {
  local rc=$?
  trap - EXIT
  if [ "$DEPLOY_LOCK_HELD" = 1 ]; then
    if [ "$KEEP_DEPLOY_LOCK" = 1 ]; then
      log "远端状态未能安全收敛；为防并发操作，故意保留 ops lock：$OPS_LOCK_DIR"
      log "核对容器/release/构建进程后，再按 owner token 人工清理。"
    elif ! remote "
      set -e
      [ \"\$(cat '$OPS_LOCK_DIR/owner' 2>/dev/null)\" = '$OPS_LOCK_TOKEN' ]
      rm -f '$OPS_LOCK_DIR/owner'
      rmdir '$OPS_LOCK_DIR'
    "; then
      log "部署已收敛，但 ops lock 释放失败：$OPS_LOCK_DIR"
      rc=1
    fi
  fi
  exit "$rc"
}

die_keep_lock() {
  KEEP_DEPLOY_LOCK=1
  die "$*"
}

cancel_build() {
  if ! remote "
    [ ! -s '$SERVER_REPO/deploy-build.status' ] || exit 0
    [ -s '$SERVER_REPO/deploy-build.pid' ] || exit 1
    pid=\$(cat '$SERVER_REPO/deploy-build.pid')
    printf '%s\n' \"\$pid\" | grep -Eq '^[0-9]+$' || exit 1
    if kill -0 \"\$pid\" 2>/dev/null; then
      cmdline=\$(tr '\000' ' ' < \"/proc/\$pid/cmdline\" 2>/dev/null || true)
      case \"\$cmdline\" in *'$SERVER_REPO/deploy-build.status'*) : ;; *) exit 1 ;; esac
      kill -TERM -- \"-\$pid\" 2>/dev/null || kill -TERM \"\$pid\" 2>/dev/null || exit 1
      sleep 2
      kill -KILL -- \"-\$pid\" 2>/dev/null || kill -KILL \"\$pid\" 2>/dev/null || true
      sleep 1
      kill -0 -- \"-\$pid\" 2>/dev/null && exit 1
    elif kill -0 -- \"-\$pid\" 2>/dev/null; then
      # leader 已消失但进程组仍在：无法再用 cmdline 证明归属，不冒险回滚。
      exit 1
    fi
    true
  "; then
    log "未能确认后台构建进程已终止；后续回滚不得视为已验证。"
    return 1
  fi
}

rollback_and_die() {
  local reason="$1" rollback_version
  log "部署失败：$reason"
  remote "tail -n 60 '$SERVER_REPO/deploy-build.log' 2>/dev/null || true" || true
  if [ "${NO_ROLLBACK:-0}" = "1" ]; then
    die_keep_lock "$reason。NO_ROLLBACK=1，未自动回滚；旧源码保留在 $OLD_TREE。"
  fi
  cancel_build || die_keep_lock "$reason；构建进程状态不确定，未执行可能竞态的自动回滚。"

  log "尝试回滚 :prev，并严格验证依赖/版本…"
  if ! remote "ASSUME_YES=1 OPS_LOCK_DIR='$OPS_LOCK_DIR' OPS_LOCK_TOKEN='$OPS_LOCK_TOKEN' STACK='$STACK' EXPECTED_STACK='$EXPECTED_STACK' SERVER_REPO='$SERVER_REPO' COMPOSE_FILE='$COMPOSE_FILE' ENV_FILE='$ENV_FILE' HOST_HEADER='$HOST_HEADER' APP_PORT='$APP_PORT' READINESS_TIMEOUT='$HEALTH_TIMEOUT' '$SERVER_REPO/deploy/ops/rollback.sh'"; then
    die_keep_lock "$reason；自动回滚失败或验收未通过。不能宣称已回滚；保留 $OLD_TREE 供人工恢复。"
  fi

  if ! rollback_version="$(remote "docker run --rm '$APP_IMG:latest' --version" 2>/dev/null | tr -d '\r\n')"; then
    die_keep_lock "$reason；:prev 运行验收过，但无法读取回滚镜像版本，不报告完整回滚。"
  fi
  printf '%s\n' "$rollback_version" | grep -Eq '^[A-Za-z0-9._-]+$' \
    || die_keep_lock "$reason；:prev 运行验收过，但无法取得可验证版本，不报告完整回滚。"

  # rollback.sh 已将与 :prev 成对的归档整树换回并验收。
  # deploy 换树时留下的 OLD_TREE 只是同一旧 release 的额外保险副本。
  remote "rm -rf -- '$OLD_TREE'" \
    || die_keep_lock "$reason；回滚已验收，但无法清理旧 release 副本，锁已保留供人工核对。"

  remote "rm -f '$ARCHIVE' '$VERSION_SIDECAR' '$MANIFEST_SIDECAR' '$RELEASE_SIDECAR'" \
    || log "回滚已验收，但未能清理失败发布归档：$ARCHIVE"
  die "$reason；自动回滚已通过 MySQL/Redis/app 与版本 $rollback_version 验收。"
}

if [ "${SKIP_PREFLIGHT:-0}" != "1" ]; then
  log "1/8 本地预检…"
  (cd "$LOCAL_REPO" && bash scripts/preflight.sh) || die "预检未过，已中止。"
else
  log "1/8 跳过预检（SKIP_PREFLIGHT=1）"
fi

# 新 production Compose 会在 app 启动前创建 schema-scoped MySQL 用户并启用
# Redis ACL。先验证服务器 .env 已完成无损迁移所需的外部密钥准备，避免换树/构建
# 后才发现缺凭据。只检查格式和权限，不输出任何 secret。
log "验证服务器数据服务最小权限迁移凭据…"
remote "
  set -eu
  [ -f '$ENV_FILE' ] || { echo 'missing runtime env: $ENV_FILE' >&2; exit 1; }
  [ ! -L '$ENV_FILE' ] || { echo 'runtime env must not be a symlink' >&2; exit 1; }
  command -v age >/dev/null || { echo 'age is required for production offsite backup' >&2; exit 1; }
  command -v rclone >/dev/null || { echo 'rclone is required for production offsite backup' >&2; exit 1; }
  mode=\$(stat -c %a '$ENV_FILE' 2>/dev/null || stat -f %Lp '$ENV_FILE')
  [ \"\$mode\" = 600 ] || { echo 'runtime env must have mode 600' >&2; exit 1; }
  value_of() {
    key=\$1
    count=\$(grep -Ec \"^\${key}=\" '$ENV_FILE' 2>/dev/null || true)
    [ \"\$count\" = 1 ] || { echo \"runtime env must contain exactly one \${key}\" >&2; exit 1; }
    sed -n \"s/^\${key}=//p\" '$ENV_FILE' | sed \"s/^['\\\"]//;s/['\\\"]\$//\"
  }
  optional_value_of() {
    key=\$1
    count=\$(grep -Ec \"^\${key}=\" '$ENV_FILE' 2>/dev/null || true)
    [ \"\$count\" -le 1 ] || { echo \"runtime env contains duplicate \${key}\" >&2; exit 1; }
    [ \"\$count\" = 1 ] || return 0
    sed -n \"s/^\${key}=//p\" '$ENV_FILE' | sed \"s/^['\\\"]//;s/['\\\"]\$//\"
  }
  mysql_user=\$(value_of MYSQL_APP_USER)
  redis_user=\$(value_of REDIS_APP_USER)
  mysql_pass=\$(value_of MYSQL_APP_PASSWORD)
  redis_pass=\$(value_of REDIS_PASSWORD)
  offsite_required=\$(value_of BACKUP_OFFSITE_REQUIRED)
  offsite_remote=\$(value_of BACKUP_OFFSITE_REMOTE)
  offsite_recipient=\$(value_of BACKUP_OFFSITE_AGE_RECIPIENT)
  printf '%s\\n' \"\$mysql_user\" | grep -Eq '^[A-Za-z][A-Za-z0-9_]{0,31}$'
  printf '%s\\n' \"\$redis_user\" | grep -Eq '^[A-Za-z][A-Za-z0-9_-]{0,31}$'
  printf '%s\\n' \"\$mysql_pass\" | grep -Eq '^[0-9a-fA-F]{64,128}$'
  printf '%s\\n' \"\$redis_pass\" | grep -Eq '^[0-9a-fA-F]{64,128}$'
  [ \"\$mysql_pass\" != \"\$redis_pass\" ] || { echo 'MySQL and Redis passwords must differ' >&2; exit 1; }
  [ \"\$offsite_required\" = 1 ] || { echo 'production requires BACKUP_OFFSITE_REQUIRED=1' >&2; exit 1; }
  printf '%s\\n' \"\$offsite_remote\" | grep -Eq '^[A-Za-z0-9_.-]+:[^[:cntrl:]]+$'
  printf '%s\\n' \"\$offsite_recipient\" | grep -Eq '^(age1[0-9a-z]{20,}|age1[a-z0-9-]+|ssh-(rsa|ed25519)[[:space:]][A-Za-z0-9+/=]+)$'
  offsite_remote_name=\${offsite_remote%%:*}:
  rclone listremotes | grep -Fx \"\$offsite_remote_name\" >/dev/null || {
    echo 'configured BACKUP_OFFSITE_REMOTE is absent from rclone config' >&2
    exit 1
  }
  for image_key in MYSQL_IMAGE REDIS_IMAGE; do
    image=\$(optional_value_of \"\$image_key\")
    [ -z \"\$image\" ] || printf '%s\\n' \"\$image\" | grep -Eq '^[A-Za-z0-9._/-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$' || {
      echo \"\$image_key override must pin a tag and sha256 digest\" >&2
      exit 1
    }
  done
" || die "服务器 .env 尚未准备最小权限凭据与强制异地备份配置；未触碰线上状态。"

log "2/8 标记本次 HEAD：$TAG"
(cd "$LOCAL_REPO" && git tag -f "$TAG" >/dev/null 2>&1) \
  && ok "git tag $TAG -> $GIT_SHA" \
  || log "git tag 未写入（非 git 环境或权限不足）"

log "获取远端独占 ops lock（覆盖 backup/restore/deploy/rollback）"
if ! remote "
  set -e
  mkdir -p '$(dirname "$OPS_LOCK_DIR")'
  if ! mkdir '$OPS_LOCK_DIR' 2>/dev/null; then
    echo \"ops lock busy: \$(cat '$OPS_LOCK_DIR/owner' 2>/dev/null || echo unknown)\" >&2
    exit 75
  fi
  printf '%s\n' '$OPS_LOCK_TOKEN' > '$OPS_LOCK_DIR/owner'
  chmod 700 '$OPS_LOCK_DIR'
  chmod 600 '$OPS_LOCK_DIR/owner'
"; then
  die "无法获取远端 ops lock；另一个备份/恢复/部署/回滚可能正在进行。"
fi
DEPLOY_LOCK_HELD=1
trap release_deploy_lock EXIT

log "3/8 生成发布前配对备份"
if remote "test -x '$SERVER_REPO/deploy/ops/backup.sh'"; then
  backup_rc=0
  remote "OPS_LOCK_DIR='$OPS_LOCK_DIR' OPS_LOCK_TOKEN='$OPS_LOCK_TOKEN' STACK='$STACK' EXPECTED_STACK='$EXPECTED_STACK' SERVER_REPO='$SERVER_REPO' COMPOSE_FILE='$COMPOSE_FILE' ENV_FILE='$ENV_FILE' '$SERVER_REPO/deploy/ops/backup.sh'" || backup_rc=$?
  if [ "$backup_rc" != 0 ]; then
    [ "$backup_rc" != 255 ] || die_keep_lock "发布前备份期间 SSH 中断，无法证明远端备份进程已退出；已保留 ops lock。"
    die "发布前备份失败；已在上传/换树之前中止。"
  fi
else
  [ "${ALLOW_INITIAL_NO_BACKUP:-0}" = "1" ] \
    || die "找不到可执行 backup.sh，且未显式允许首次空环境。"
  if ! MYSQL_CID="$(remote "$DC ps -a -q '$MYSQL_SVC'")"; then
    die "无法查询服务器 mysql 状态；不得将 SSH/compose 失败当成‘空环境’。"
  fi
  [ -z "$MYSQL_CID" ] \
    || die "已存在 mysql 容器 $MYSQL_CID；禁止以‘首次部署’跳过备份。"
  log "显式允许首次空环境无备份（已成功查询且未发现 mysql 容器）"
fi

log "4/8 备份通过后，成对保存当前 :prev 镜像 + 干净源码 release"
if ! remote "
  set -e
  if docker image inspect '$APP_IMG:latest' >/dev/null 2>&1; then
    stem='prev-$TS'
    archive='$ARCHIVE_DIR/prev-$TS.tgz'
    version_file='$ARCHIVE_DIR/prev-$TS.version'
    deploy_file='$ARCHIVE_DIR/prev-$TS.manifest.json'
    release_file='$ARCHIVE_DIR/prev-$TS.release'
    pointer_tmp='$ARCHIVE_DIR/.prev.current.$TS.tmp'
    archive_tmp=\"\$archive.tmp\"
    committed=0
    created=0
    tag_changed=0
    had_old_prev=0
    cleanup_prev() {
      rc=\$?
      if [ \"\$tag_changed\" = 1 ] \
        && [ \"\$(cat '$ARCHIVE_DIR/prev.current' 2>/dev/null || true)\" = \"\$(basename \"\$release_file\")\" ]; then
        committed=1
      fi
      if [ \"\$committed\" != 1 ] && [ \"\$created\" = 1 ]; then
        rm -f -- \"\$archive_tmp\" \"\$archive\" \"\$version_file\" \"\$deploy_file\" \"\$release_file\" \"\$release_file.tmp\" \"\$pointer_tmp\"
        if [ \"\$tag_changed\" = 1 ]; then
          if [ \"\$had_old_prev\" = 1 ]; then docker tag '$APP_IMG:prev-before-update' '$APP_IMG:prev' || true
          else docker image rm '$APP_IMG:prev' >/dev/null 2>&1 || true; fi
        fi
      fi
      if [ \"\$had_old_prev\" = 1 ]; then docker image rm '$APP_IMG:prev-before-update' >/dev/null 2>&1 || true; fi
      exit \"\$rc\"
    }
    trap cleanup_prev EXIT
    mkdir -p '$ARCHIVE_DIR'
    for path in \"\$archive\" \"\$version_file\" \"\$deploy_file\" \"\$release_file\" \"\$archive_tmp\" \"\$release_file.tmp\" \"\$pointer_tmp\"; do [ ! -e \"\$path\" ]; done
    created=1
    test -d '$SERVER_REPO'
    test ! -L '$SERVER_REPO'
    test -f '$SERVER_REPO/Dockerfile'
    test -f '$SERVER_REPO/deploy/docker-compose.test.yml'
    test -x '$SERVER_REPO/deploy/ops/rollback.sh'
    test -s '$SERVER_REPO/VERSION'
    test -s '$SERVER_REPO/deploy-manifest.json'
    source_version=\$(tr -d '\\r\\n' < '$SERVER_REPO/VERSION')
    image_version=\$(docker run --rm '$APP_IMG:latest' --version | tr -d '\\r\\n')
    json_version=\$(sed -n 's/.*\"version\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p' '$SERVER_REPO/deploy-manifest.json' | head -n1)
    printf '%s\\n' \"\$source_version\" | grep -Eq '^[A-Za-z0-9._-]+$'
    [ \"\$source_version\" = \"\$image_version\" ]
    [ \"\$source_version\" = \"\$json_version\" ]
    tar --exclude='./.env' --exclude='./.env.*' --exclude='./deploy-build.log' \
      --exclude='./deploy-build.pid' --exclude='./deploy-build.status' --exclude='./deploy-build.status.tmp' \
      -czf \"\$archive_tmp\" -C '$SERVER_REPO' .
    gzip -t \"\$archive_tmp\"
    mv \"\$archive_tmp\" \"\$archive\"
    cp '$SERVER_REPO/VERSION' \"\$version_file\"
    cp '$SERVER_REPO/deploy-manifest.json' \"\$deploy_file\"
    {
      printf 'format=newapi-release-v1\\n'
      printf 'timestamp=%s\\n' '$TS'
      printf 'archive_file=%s\\n' \"\$(basename \"\$archive\")\"
      printf 'archive_sha256=%s\\n' \"\$(sha256sum \"\$archive\" | cut -d ' ' -f1)\"
      printf 'version_file=%s\\n' \"\$(basename \"\$version_file\")\"
      printf 'version_sha256=%s\\n' \"\$(sha256sum \"\$version_file\" | cut -d ' ' -f1)\"
      printf 'deploy_manifest_file=%s\\n' \"\$(basename \"\$deploy_file\")\"
      printf 'deploy_manifest_sha256=%s\\n' \"\$(sha256sum \"\$deploy_file\" | cut -d ' ' -f1)\"
      printf 'version=%s\\n' \"\$source_version\"
    } > \"\$release_file.tmp\"
    mv \"\$release_file.tmp\" \"\$release_file\"
    chmod 600 \"\$archive\" \"\$version_file\" \"\$deploy_file\" \"\$release_file\"
    if docker image inspect '$APP_IMG:prev' >/dev/null 2>&1; then
      docker tag '$APP_IMG:prev' '$APP_IMG:prev-before-update'
      had_old_prev=1
    fi
    docker tag '$APP_IMG:latest' '$APP_IMG:prev'
    tag_changed=1
    printf '%s\\n' \"\$(basename \"\$release_file\")\" > \"\$pointer_tmp\"
    mv \"\$pointer_tmp\" '$ARCHIVE_DIR/prev.current'
    committed=1
    trap - EXIT
    if [ \"\$had_old_prev\" = 1 ]; then docker image rm '$APP_IMG:prev-before-update' >/dev/null 2>&1 || true; fi
  else
    [ ! -e '$ARCHIVE_DIR/prev.current' ]
    if docker image inspect '$APP_IMG:prev' >/dev/null 2>&1; then exit 1; fi
    echo '  首次部署：当前 app 镜像不存在'
  fi
"; then
  die_keep_lock "无法以交易方式成对保存 :prev 镜像与源码 release；已保留 ops lock 供核对。"
fi

log "5/8 从干净 HEAD 上传可重建归档（不读取工作树忽略文件）"
remote "
  set -e
  mkdir -p '$ARCHIVE_DIR'
  for path in '$REMOTE_STAGE' '$OLD_TREE' '$ARCHIVE' '$VERSION_SIDECAR' '$MANIFEST_SIDECAR' '$RELEASE_SIDECAR'; do
    [ ! -e \"\$path\" ]
  done
"
(cd "$LOCAL_REPO" && git archive --format=tar HEAD -- . ':(exclude)bulb-orbit') \
  | gzip \
  | remote "
      set -e
      tmp='$ARCHIVE.tmp'
      trap 'rm -f \"\$tmp\"' EXIT
      cat > \"\$tmp\"
      gzip -t \"\$tmp\"
      mv \"\$tmp\" '$ARCHIVE'
      trap - EXIT
    "
printf '%s' "$APP_VERSION" | remote "umask 077; cat > '$VERSION_SIDECAR.tmp'; mv '$VERSION_SIDECAR.tmp' '$VERSION_SIDECAR'"
printf '%s' "$MANIFEST_JSON" | remote "umask 077; cat > '$MANIFEST_SIDECAR.tmp'; mv '$MANIFEST_SIDECAR.tmp' '$MANIFEST_SIDECAR'"
remote "
  set -e
  archive_sha=\$(sha256sum '$ARCHIVE' | cut -d ' ' -f1)
  version_sha=\$(sha256sum '$VERSION_SIDECAR' | cut -d ' ' -f1)
  deploy_sha=\$(sha256sum '$MANIFEST_SIDECAR' | cut -d ' ' -f1)
  json_version=\$(sed -n 's/.*\"version\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p' '$MANIFEST_SIDECAR' | head -n1)
  [ \"\$json_version\" = '$APP_VERSION' ]
  {
    printf 'format=newapi-release-v1\\n'
    printf 'timestamp=%s\\n' '$TS'
    printf 'archive_file=%s\\n' 'src-$TS.tgz'
    printf 'archive_sha256=%s\\n' \"\$archive_sha\"
    printf 'version_file=%s\\n' 'src-$TS.version'
    printf 'version_sha256=%s\\n' \"\$version_sha\"
    printf 'deploy_manifest_file=%s\\n' 'src-$TS.manifest.json'
    printf 'deploy_manifest_sha256=%s\\n' \"\$deploy_sha\"
    printf 'version=%s\\n' '$APP_VERSION'
  } > '$RELEASE_SIDECAR.tmp'
  mv '$RELEASE_SIDECAR.tmp' '$RELEASE_SIDECAR'
  chmod 600 '$ARCHIVE' '$VERSION_SIDECAR' '$MANIFEST_SIDECAR' '$RELEASE_SIDECAR'
"

ENV_REL=""
case "$ENV_FILE" in
  "$SERVER_REPO"/*) ENV_REL="${ENV_FILE#"$SERVER_REPO"/}" ;;
esac
if [ -n "$ENV_REL" ]; then
  [ "$ENV_REL" = .env ] || die "ENV_FILE 位于 release 树内时必须是根目录 .env，拒绝可穿越的自定义相对路径：$ENV_REL"
fi

log "5.5/8 解包到空 staging，验证后原子换树"
if ! remote "
  set -e
  stage='$REMOTE_STAGE'
  old='$OLD_TREE'
  test ! -L '$SERVER_REPO'
  archive_sha=\$(sed -n 's/^archive_sha256=//p' '$RELEASE_SIDECAR')
  version_sha=\$(sed -n 's/^version_sha256=//p' '$RELEASE_SIDECAR')
  deploy_sha=\$(sed -n 's/^deploy_manifest_sha256=//p' '$RELEASE_SIDECAR')
  [ \"\$(sha256sum '$ARCHIVE' | cut -d ' ' -f1)\" = \"\$archive_sha\" ]
  [ \"\$(sha256sum '$VERSION_SIDECAR' | cut -d ' ' -f1)\" = \"\$version_sha\" ]
  [ \"\$(sha256sum '$MANIFEST_SIDECAR' | cut -d ' ' -f1)\" = \"\$deploy_sha\" ]
  [ \"\$(tr -d '\\r\\n' < '$VERSION_SIDECAR')\" = '$APP_VERSION' ]
  mkdir -m 700 \"\$stage\"
  moved_old=0
  cleanup_stage() {
    rc=\$?
    if [ \"\$rc\" -ne 0 ]; then
      if [ \"\$moved_old\" = 1 ] && [ ! -e '$SERVER_REPO' ] && [ -d \"\$old\" ]; then mv \"\$old\" '$SERVER_REPO' || true; fi
      rm -rf -- \"\$stage\"
    fi
    exit \"\$rc\"
  }
  trap cleanup_stage EXIT
  tar --no-same-owner -xzf '$ARCHIVE' -C \"\$stage\"
  [ \"\$(sha256sum '$ARCHIVE' | cut -d ' ' -f1)\" = \"\$archive_sha\" ]
  test -f \"\$stage/Dockerfile\"
  test -f \"\$stage/deploy/docker-compose.test.yml\"
  test -x \"\$stage/deploy/ops/rollback.sh\"
  if [ -n '$ENV_REL' ]; then
    test -f '$ENV_FILE'
    test ! -L '$ENV_FILE'
    repo_real=\$(readlink -f '$SERVER_REPO')
    env_real=\$(readlink -f '$ENV_FILE')
    case \"\$env_real\" in \"\$repo_real\"/*) : ;; *) exit 1 ;; esac
    mkdir -p \"\$stage/\$(dirname '$ENV_REL')\"
    install -m 600 '$ENV_FILE' \"\$stage/$ENV_REL\"
  fi
  cp '$VERSION_SIDECAR' \"\$stage/VERSION\"
  cp '$MANIFEST_SIDECAR' \"\$stage/deploy-manifest.json\"
  chmod 644 \"\$stage/VERSION\" \"\$stage/deploy-manifest.json\"
  test -d '$SERVER_REPO'
  mv '$SERVER_REPO' \"\$old\"
  moved_old=1
  if ! mv \"\$stage\" '$SERVER_REPO'; then
    mv \"\$old\" '$SERVER_REPO' || true
    exit 1
  fi
  trap - EXIT
"; then
  die_keep_lock "staging 验证/换树失败或 SSH 结果不确定；已保留 ops lock 供人工核对。"
fi

log "6/8 后台构建/启动，并记录可轮询的真实退出状态"
remote "
  set -e
  command -v setsid >/dev/null
  rm -f '$SERVER_REPO/deploy-build.status' '$SERVER_REPO/deploy-build.status.tmp' '$SERVER_REPO/deploy-build.pid'
  cd '$SERVER_REPO'
  nohup setsid sh -c '$DC up -d --build; rc=\$?; printf \"%s\\n\" \"\$rc\" > \"$SERVER_REPO/deploy-build.status.tmp\"; mv \"$SERVER_REPO/deploy-build.status.tmp\" \"$SERVER_REPO/deploy-build.status\"; exit \"\$rc\"' \
    > '$SERVER_REPO/deploy-build.log' 2>&1 </dev/null &
  printf '%s\n' \$! > '$SERVER_REPO/deploy-build.pid'
" || rollback_and_die "无法启动可跟踪的后台构建"

log "7/8 等待构建完成（总时限 ${HEALTH_TIMEOUT}s）"
deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
build_status=""
while [ "$(date +%s)" -lt "$deadline" ]; do
  if ! build_status="$(remote "cat '$SERVER_REPO/deploy-build.status' 2>/dev/null || true" | tr -d '\r\n')"; then
    die_keep_lock "轮询构建状态时 SSH 失败；远端结果不确定，未冒险发起竞态回滚。旧树保留在 $OLD_TREE。"
  fi
  [ -z "$build_status" ] || break
  sleep 5
done
[ "$build_status" = "0" ] || rollback_and_die "构建失败或未在 ${HEALTH_TIMEOUT}s 内完成（status=${build_status:-timeout}）"

remaining=$(( deadline - $(date +%s) ))
[ "$remaining" -gt 0 ] || rollback_and_die "构建完成时已用尽部署时限"
log "7.5/8 验证 MySQL/Redis/app 就绪且版本精确为 $APP_VERSION"
if ! remote "
  STACK='$STACK' EXPECTED_STACK='$EXPECTED_STACK' SERVER_REPO='$SERVER_REPO' \
    COMPOSE_FILE='$COMPOSE_FILE' ENV_FILE='$ENV_FILE' HOST_HEADER='$HOST_HEADER' \
    APP_PORT='$APP_PORT' READINESS_INTERVAL=3 \
    bash -c '. \"\$SERVER_REPO/deploy/ops/lib.sh\"; wait_runtime_ready \"$APP_VERSION\" \"$remaining\"'
"; then
  rollback_and_die "新 release 的依赖/readiness/版本验收失败"
fi

NEW_ID="$(remote "docker image inspect -f '{{.Id}}' '$APP_IMG:latest'")"
PREV_ID="$(remote "docker image inspect -f '{{.Id}}' '$APP_IMG:prev' 2>/dev/null || true")"
if [ -n "$PREV_ID" ] && [ "$NEW_ID" = "$PREV_ID" ]; then
  rollback_and_die "线上版本虽匹配，但 latest 与 :prev 镜像 ID 相同，无法证明产出新制品"
fi

log "8/8 验收通过，清理旧 release 与超额归档"
remote "rm -rf -- '$OLD_TREE'" || die_keep_lock "新 release 已验收，但旧树清理失败；保留 ops lock 供人工核对。"
if ! remote "
  set -e
  cd '$ARCHIVE_DIR'
  current_prev=\$(cat prev.current 2>/dev/null || true)
  for release in prev-*.release; do
    [ -e \"\$release\" ] || continue
    [ \"\$release\" = \"\$current_prev\" ] && continue
    stem=\${release%.release}
    rm -f -- \"\$stem.tgz\" \"\$stem.version\" \"\$stem.manifest.json\" \"\$release\"
  done
  ls -1t src-*.release 2>/dev/null | tail -n +$((ARCHIVE_KEEP + 1)) | while IFS= read -r release; do
    [ -n \"\$release\" ] || continue
    stem=\${release%.release}
    rm -f -- \"\$stem.tgz\" \"\$stem.version\" \"\$stem.manifest.json\" \"\$release\"
  done
"; then
  log "部署验收已通过，但旧归档清理失败；不影响当前 release。"
fi
ok "部署成功：版本 $APP_VERSION，可重建归档 $ARCHIVE"
log "回滚：$SERVER_REPO/deploy/ops/rollback.sh 或 --to $TS"
