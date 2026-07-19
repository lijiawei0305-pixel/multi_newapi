#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# lib.sh — deploy/ops 公共参数 + 助手函数（被 backup/restore/rollback/healthcheck
#          source 引用）。**在服务器上运行**（Debian 12 + Docker compose v2）。
#
# 用法：在脚本顶部 `source "$(dirname "$0")/lib.sh"`。
# 所有参数都可被环境变量覆盖（`STACK=xxx ./backup.sh`），默认值对齐当前生产栈事实。
#
# 安全红线（2026-07-03 单栈收敛后已重定义，勿再按旧语义理解）：
#   `newapi_test` 已是【唯一现网 / 生产栈】——原 stock 栈 `newapi_YFNf`（:3000）已删除。
#   脚本的职责本就是操作它，故护栏不再是"拒绝 prod"(已无意义)，而是 guard_target：
#   确认目标确为期望栈 `newapi_test`，挡拼写/误配。破坏性【不可逆】操作（restore 整库覆盖）
#   另用 confirm_typed 强确认（须键入栈名、不受 ASSUME_YES 影响）+ 强制可恢复 pre-backup。
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail
export LC_ALL=C LANG=C

# ── 栈与路径（部署事实，见 RETRO「部署与环境」/ doc/tasks/phase2.md）──────────────
STACK="${STACK:-newapi_test}"                                   # compose 项目名（-p 前缀隔离）
SERVER_REPO="${SERVER_REPO:-/root/newapi-test}"                 # 服务器仓库根
COMPOSE_FILE="${COMPOSE_FILE:-$SERVER_REPO/deploy/docker-compose.test.yml}"
ENV_FILE="${ENV_FILE:-$SERVER_REPO/.env}"                       # 上游 Key/密钥仅存此处（600）

# ── 服务名 / 容器内组件（compose service 名）─────────────────────────────────────
APP_SVC="${APP_SVC:-app}"
MYSQL_SVC="${MYSQL_SVC:-mysql}"
REDIS_SVC="${REDIS_SVC:-redis}"

# ── 回环端口（仅 127.0.0.1 暴露，公网经宿主 nginx 反代）──────────────────────────
APP_PORT="${APP_PORT:-3100}"        # app  127.0.0.1:3100 → 容器 3000
HOST_HEADER="${HOST_HEADER:-tokendream.wedreamhub.com}"  # 多租户 Host 识别用

# ── 数据库（库名含连字符需引用；root 密码不入 git——C1，2026-07-17 修复）────────────
# DB_PASS 解析顺序：环境变量显式覆盖 > 服务器 .env 的 MYSQL_ROOT_PASSWORD > 缺失则留空。
#   过去此处内联低熵默认值（与 compose 两处同值、已进 git）——与 SESSION_SECRET 同款 C1 违例，已移除。
#   .env 行形如 MYSQL_ROOT_PASSWORD=xxx（取最后一条生效行，容忍两侧单/双引号）。
#   **本段只解析、绝不中止**：缺口令的 fail-closed 判定收进 require_db_pass()（见下），由真正
#   用到 DB 口令的脚本（backup/restore/reconcile）在 source 后显式调用。若在此顶层 exit，会
#   击穿每一个 source 本库、却根本不用 DB 口令的脚本——尤其 rollback.sh（deploy.sh 步骤⑧的
#   自动回滚执行体、唯一紧急回退手段）与 healthcheck.sh（*/5 cron 唯一自动巡检/告警触发点）：
#   任何缺 MYSQL_ROOT_PASSWORD 的旧 .env（从旧 config-*.tar.gz 恢复 / 新机重建）会令「最需要
#   回滚与巡检时」二者同时失效，正撞 C6「回滚兜底路径必须真实可达」（audit F3，High）。
DB_NAME="${DB_NAME:-new-api-test}"
DB_USER="${DB_USER:-root}"
if [ -z "${DB_PASS:-}" ] && [ -f "$ENV_FILE" ]; then
  DB_PASS="$(sed -n 's/^MYSQL_ROOT_PASSWORD=//p' "$ENV_FILE" | tail -n 1 | sed "s/^['\"]//;s/['\"]\$//")"
fi

# Redis 应用 ACL 凭据。与 DB root 运维凭据不同，Redis 的运维命令使用与 app
# 相同的受认证 ACL 用户；密码只经 stdin 送入容器内 redis-cli 环境，不进 argv。
REDIS_USER="${REDIS_USER:-}"
REDIS_PASS="${REDIS_PASS:-}"
if [ -f "$ENV_FILE" ]; then
  if [ -z "$REDIS_USER" ]; then
    REDIS_USER="$(sed -n 's/^REDIS_APP_USER=//p' "$ENV_FILE" | tail -n 1 | sed "s/^['\"]//;s/['\"]\$//")"
  fi
  if [ -z "$REDIS_PASS" ]; then
    REDIS_PASS="$(sed -n 's/^REDIS_PASSWORD=//p' "$ENV_FILE" | tail -n 1 | sed "s/^['\"]//;s/['\"]\$//")"
  fi
fi
REDIS_USER="${REDIS_USER:-newapi}"

# ── 备份 / 保留 ─────────────────────────────────────────────────────────────────
BACKUP_DIR="${BACKUP_DIR:-/root/backups}"
KEEP="${KEEP:-7}"                   # 各类备份各保留最近 N 份
BACKUP_FORMAT="${BACKUP_FORMAT:-newapi-backup-v1}"

# ── 运行验收 ─────────────────────────────────────────────────────────────────────
READINESS_TIMEOUT="${READINESS_TIMEOUT:-120}"
READINESS_INTERVAL="${READINESS_INTERVAL:-3}"

# 所有会停服/换树/动数据的 ops 入口共用同一个原子目录锁。
# deploy.sh 在 Mac 上运行、通过多次 SSH 跨连接持锁，因此不用仅在单一
# 进程 FD 生命周期内有效的 flock，而用 mkdir + 随机 token。异常崩溃时锁故意
# fail-closed 保留，操作员必须先核对线上状态再人工清锁。
OPS_LOCK_DIR="${OPS_LOCK_DIR:-/run/lock/newapi-ops.lock.d}"
OPS_LOCK_TOKEN="${OPS_LOCK_TOKEN:-}"
OPS_LOCK_OWNED=0

# ── 宿主 nginx vhost（宝塔）+ 证书目录（备份/上线手册引用）────────────────────────
NGINX_VHOST="${NGINX_VHOST:-/www/server/panel/vhost/nginx/wildcard.wedreamhub.com.conf}"   # 通配上线 vhost（tokendream 专属已让位 .bak）
NGINX_CERT_DIR="${NGINX_CERT_DIR:-/www/server/panel/vhost/cert/wildcard.wedreamhub.com}"   # 通配 CF Origin CA 证书目录

# ── 告警 webhook（可选；非空则 healthcheck 失败时 POST，占位）──────────────────────
ALERT_WEBHOOK="${ALERT_WEBHOOK:-}"

# ── 期望栈（正向白名单）：护栏确认操作目标确为它，挡拼写/误配 ──────────────────────
# 注：2026-07-03 单栈收敛后 newapi_test 即唯一现网/生产；原 newapi_YFNf 已删除，
#     不再作为"禁区"存在——旧的"拒绝 prod"护栏对现网恒放行，是纯粹的虚假安全感，已弃。
EXPECTED_STACK="${EXPECTED_STACK:-newapi_test}"

# ── 助手 ─────────────────────────────────────────────────────────────────────────
log()  { printf '\033[1;34m[ops]\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m[ ok]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[wrn]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[err]\033[0m %s\n' "$*" >&2; exit 1; }

# guard_target：正向白名单——确认操作目标确为期望栈 $EXPECTED_STACK（挡拼写/误配）。
# 自 2026-07-03 单栈收敛：newapi_test 即唯一现网，脚本本就该操作它；旧"拒绝 prod"语义已失效
#   （守着已删除的 YFNf → 对真生产恒放行）。此处不做路径 negative 检查：被守的栈已不存在，
#   而 stack 名(下划线 newapi_test)与仓库路径(连字符 newapi-test)本就不同形，positive 路径
#   匹配反而脆弱——栈名一致性足以挡住绝大多数误配。
guard_target() {
  [ "$STACK" = "$EXPECTED_STACK" ] \
    || die "目标栈 '$STACK' ≠ 期望栈 '$EXPECTED_STACK'（防误配/拼写），已中止。"
  validate_managed_root "$SERVER_REPO"
}
# 向后兼容别名：保留旧名 guard_not_prod（语义已更新为 guard_target），backup/healthcheck/
# reconcile/demo 等调用点无需改动。旧名字面意思已不准确，仅为兼容保留；新脚本请用 guard_target。
guard_not_prod() { guard_target; }

# dc：固化 compose 调用（-p 项目名 + --env-file + 绝对 -f，cwd 无关）。
# 绝对 -f 时 build context（compose 里 `..`）解析为 compose 文件目录的上级 = ${SERVER_REPO}。
dc() { docker compose -p "$STACK" --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }

# confirm <提示>：危险操作二次确认；需键入 yes。可用 ASSUME_YES=1 跳过（供 cron/自动化）。
confirm() {
  if [ "${ASSUME_YES:-0}" = "1" ]; then return 0; fi
  printf '\033[1;33m%s\033[0m ' "$1 [键入 yes 继续]:" >&2
  local ans; read -r ans
  [ "$ans" = "yes" ] || die "已取消。"
}

# confirm_typed <token> <提示>：破坏性【不可逆】操作的强确认——必须原样键入 <token>
#   （通常是栈名/库名）。**故意不理会 ASSUME_YES**：此类操作（如 restore 整库覆盖生产库）
#   无自动化调用方，必须人在环——杜绝"读到旧注释‘只操作隔离测试栈’就 ASSUME_YES 一把梭、
#   把生产库整库覆盖"的误伤路径。非交互（无输入）即中止（fail-closed）。
confirm_typed() {
  local token="$1" prompt="$2" ans
  printf '\033[1;31m%s\033[0m ' "$prompt [键入 '$token' 以确认]:" >&2
  read -r ans || die "无输入（非交互环境），已取消。"
  [ "$ans" = "$token" ] || die "确认串不匹配（需键入 '$token'），已取消。"
}

# require：确认依赖命令存在。
require() { command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"; }

# validate_managed_root <path>：只允许可预测的绝对 release 路径，拒绝
# /root、/var 等顶层目录和 .. / shell 元字符。这是换树/删失败树前的底线。
validate_managed_root() {
  local path="$1"
  printf '%s\n' "$path" | grep -Eq '^/[A-Za-z0-9._/-]+$' \
    || die "SERVER_REPO 必须是不含 shell 元字符的绝对路径：$path"
  case "$path" in
    /|/root|/home|/usr|/var|/etc|/opt|/srv|/tmp|*/)
      die "SERVER_REPO 过于宽泛或含路径穿越，拒绝：$path"
      ;;
  esac
  case "/${path#/}/" in *'/../'*|*'/./'*|*'//'*) die "SERVER_REPO 含路径穿越，拒绝：$path" ;; esac
}

valid_release_relative_path() {
  local path="$1"
  [ -n "$path" ] || return 1
  printf '%s\n' "$path" | grep -Eq '^[A-Za-z0-9._/-]+$' || return 1
  case "/$path/" in *'/../'*|*'/./'*|*'//'*) return 1 ;; esac
}

# require_db_pass：需要 DB 口令的脚本显式调用（backup/restore/reconcile）。
# 不在 lib.sh 顶层校验——否则 source 期的 exit 会击穿 rollback.sh / healthcheck.sh 等
# 根本不用 DB 口令的脚本，令「最需要回滚/巡检时」反而因无关凭据失效（C6，audit F3）。
# die 定义在本函数之前（上方），函数体在调用时（source 完成后）才求值，故引用安全。
require_db_pass() {
  [ -n "${DB_PASS:-}" ] || die "DB_PASS 未设置且 $ENV_FILE 缺 MYSQL_ROOT_PASSWORD——DB 密码已按 C1 移出 git，禁止内联默认；请先在服务器 .env(600) 写入。"
  case "$DB_PASS" in
    *$'\r'*|*$'\n'*) die "DB_PASS 不得包含换行符；请使用 openssl rand -hex 32 生成单行密码。" ;;
  esac
}

require_redis_pass() {
  [ -n "${REDIS_PASS:-}" ] || die "REDIS_PASS 未设置且 $ENV_FILE 缺 REDIS_PASSWORD——生产 Redis 强制 ACL 认证，禁止匿名运维。"
  printf '%s\n' "$REDIS_USER" | grep -Eq '^[A-Za-z][A-Za-z0-9_-]{0,31}$' \
    || die "REDIS_USER 格式非法：$REDIS_USER"
  printf '%s\n' "$REDIS_PASS" | grep -Eq '^[0-9a-fA-F]{64,128}$' \
    || die "REDIS_PASSWORD 必须是 64–128 位十六进制随机值（建议 openssl rand -hex 32）"
}

# mysql_secret_container_exec：在 MySQL 容器内从 stdin 读取第一行密码，
# 写入短命 0600 defaults-extra-file 后执行 mysql/mysqldump。密码不进入
# 宿主或容器的 argv；成功、失败和中断都由 EXIT trap 删除临时文件。
mysql_secret_container_exec() {
  local tool="$1"
  shift
  case "$tool" in mysql|mysqldump) ;;
    *) die "不允许的 MySQL 客户端：$tool" ;;
  esac

  dc exec -T "$MYSQL_SVC" sh -c '
    set -eu
    tool=$1
    shift
    IFS= read -r password
    umask 077
    defaults=$(mktemp /tmp/newapi-mysql-client.XXXXXX.cnf)
    trap '\''rc=$?; trap - EXIT; rm -f -- "$defaults"; exit "$rc"'\'' EXIT
    trap '\''exit 129'\'' HUP
    trap '\''exit 130'\'' INT
    trap '\''exit 143'\'' TERM
    escaped=$(printf "%s" "$password" | sed '\''s/\\/\\\\/g; s/"/\\"/g'\'')
    unset password
    printf "[client]\npassword=\"%s\"\n" "$escaped" > "$defaults"
    unset escaped
    chmod 600 "$defaults"
    "$tool" --defaults-extra-file="$defaults" "$@"
  ' newapi-mysql-client "$tool" "$@"
}

# mysql_with_secret 用于 -e 查询和 mysqldump；mysql_with_secret_input 还会
# 在密码行后转发调用方 stdin，供数据库导入使用。
mysql_with_secret() {
  local tool="$1"
  shift
  printf '%s\n' "$DB_PASS" | mysql_secret_container_exec "$tool" "$@"
}

mysql_with_secret_input() {
  local tool="$1"
  shift
  { printf '%s\n' "$DB_PASS"; cat; } | mysql_secret_container_exec "$tool" "$@"
}

# redis_cli_service <args...>：密码经 stdin 注入容器内 REDISCLI_AUTH，绝不
# 作为 docker/redis-cli 参数出现。--no-auth-warning 避免运维日志产生噪声。
redis_cli_service() {
  printf '%s\n' "$REDIS_PASS" | dc exec -T "$REDIS_SVC" sh -c '
    set -eu
    user=$1
    shift
    IFS= read -r REDISCLI_AUTH
    export REDISCLI_AUTH
    exec redis-cli --user "$user" --no-auth-warning "$@"
  ' newapi-redis-client "$REDIS_USER" "$@"
}

acquire_ops_lock() {
  local operation="${1:-ops}" owner parent
  parent="$(dirname "$OPS_LOCK_DIR")"
  mkdir -p "$parent"

  # restore -> backup 或 deploy -> backup/rollback 的子操作继承 token；只有
  # 与锁内 owner 精确匹配才能复用，不是一个可任意设置的 bypass 开关。
  if [ -n "$OPS_LOCK_TOKEN" ] \
    && [ -f "$OPS_LOCK_DIR/owner" ] \
    && [ "$(cat "$OPS_LOCK_DIR/owner" 2>/dev/null || true)" = "$OPS_LOCK_TOKEN" ]; then
    export OPS_LOCK_DIR OPS_LOCK_TOKEN
    return 0
  fi

  OPS_LOCK_TOKEN="${operation}-$(date +%Y%m%d-%H%M%S)-$$-${RANDOM:-0}"
  if ! mkdir "$OPS_LOCK_DIR" 2>/dev/null; then
    owner="$(cat "$OPS_LOCK_DIR/owner" 2>/dev/null || echo unknown)"
    die "另一个运维操作正持有 $OPS_LOCK_DIR（owner=$owner）。若为崩溃残留，先核对容器/维护状态，再人工清理；禁止并发继续。"
  fi
  printf '%s\n' "$OPS_LOCK_TOKEN" > "$OPS_LOCK_DIR/owner"
  chmod 700 "$OPS_LOCK_DIR"
  chmod 600 "$OPS_LOCK_DIR/owner"
  OPS_LOCK_OWNED=1
  export OPS_LOCK_DIR OPS_LOCK_TOKEN
}

release_ops_lock() {
  local owner
  [ "$OPS_LOCK_OWNED" = "1" ] || return 0
  owner="$(cat "$OPS_LOCK_DIR/owner" 2>/dev/null || true)"
  [ "$owner" = "$OPS_LOCK_TOKEN" ] || {
    warn "ops lock owner 已变化，拒绝删除：$OPS_LOCK_DIR"
    return 1
  }
  rm -f -- "$OPS_LOCK_DIR/owner"
  if ! rmdir "$OPS_LOCK_DIR"; then
    warn "ops lock 目录含未知内容，已 fail-closed 保留：$OPS_LOCK_DIR"
    return 1
  fi
  OPS_LOCK_OWNED=0
}

# 预创建备份目录。
ensure_backup_dir() {
  mkdir -p "$BACKUP_DIR"
  chmod 700 "$BACKUP_DIR"
}

# file_size <path>：同时兼容 GNU/BSD stat（脚本主要在 Debian 服务器运行）。
file_size() {
  stat -c%s "$1" 2>/dev/null || stat -f%z "$1"
}

# sha256_file <path>：备份 manifest 一律使用 SHA-256，不接受弱摘要。
sha256_file() {
  sha256sum "$1" | awk '{print $1}'
}

# manifest_value <manifest> <key>：严格读取单一 key=value，不 source 外部文件。
# restore 会把 manifest 当不可信输入；若 source，被篡改的备份可直接变成 root shell。
manifest_value() {
  local manifest="$1" key="$2" count
  count="$(grep -c "^${key}=" "$manifest" 2>/dev/null || true)"
  [ "$count" = "1" ] || return 1
  sed -n "s/^${key}=//p" "$manifest"
}

valid_manifest_file() {
  local name="$1"
  [ -n "$name" ] || return 1
  [ "$name" = "$(basename "$name")" ] || return 1
  case "$name" in
    *[!A-Za-z0-9._-]*) return 1 ;;
  esac
}

valid_sha256() {
  printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{64}$'
}

# verify_backup_manifest <manifest>：验证格式/目标/文件名/三个内容摘要，并导出
# MANIFEST_{DIR,TS,APP_VERSION,REDIS_KEYS,DB_FILE,REDIS_FILE,CONFIG_FILE} 供 restore 使用。
verify_backup_manifest() {
  local manifest="$1" format stack db_name consistency
  local db_file redis_file config_file db_sha redis_sha config_sha
  [ -f "$manifest" ] || die "备份 manifest 不存在：$manifest"

  format="$(manifest_value "$manifest" format)" || die "manifest 缺失/重复 format：$manifest"
  stack="$(manifest_value "$manifest" stack)" || die "manifest 缺失/重复 stack：$manifest"
  db_name="$(manifest_value "$manifest" db_name)" || die "manifest 缺失/重复 db_name：$manifest"
  consistency="$(manifest_value "$manifest" consistency)" || die "manifest 缺失/重复 consistency：$manifest"
  MANIFEST_TS="$(manifest_value "$manifest" timestamp)" || die "manifest 缺失/重复 timestamp：$manifest"
  MANIFEST_APP_VERSION="$(manifest_value "$manifest" app_version)" || die "manifest 缺失/重复 app_version：$manifest"
  MANIFEST_REDIS_KEYS="$(manifest_value "$manifest" redis_keys)" || die "manifest 缺失/重复 redis_keys：$manifest"
  db_file="$(manifest_value "$manifest" db_file)" || die "manifest 缺失/重复 db_file：$manifest"
  redis_file="$(manifest_value "$manifest" redis_file)" || die "manifest 缺失/重复 redis_file：$manifest"
  config_file="$(manifest_value "$manifest" config_file)" || die "manifest 缺失/重复 config_file：$manifest"
  db_sha="$(manifest_value "$manifest" db_sha256)" || die "manifest 缺失/重复 db_sha256：$manifest"
  redis_sha="$(manifest_value "$manifest" redis_sha256)" || die "manifest 缺失/重复 redis_sha256：$manifest"
  config_sha="$(manifest_value "$manifest" config_sha256)" || die "manifest 缺失/重复 config_sha256：$manifest"

  [ "$format" = "$BACKUP_FORMAT" ] || die "不支持的 manifest format：$format"
  [ "$stack" = "$STACK" ] || die "manifest 属于栈 '$stack'，当前目标为 '$STACK'"
  [ "$db_name" = "$DB_NAME" ] || die "manifest 属于库 '$db_name'，当前目标为 '$DB_NAME'"
  [ "$consistency" = "writers-stopped" ] || die "manifest 不是停写配对快照（consistency=$consistency），拒绝生产恢复"
  printf '%s\n' "$MANIFEST_TS" | grep -Eq '^[0-9]{8}-[0-9]{6}$' || die "manifest timestamp 非法：$MANIFEST_TS"
  printf '%s\n' "$MANIFEST_APP_VERSION" | grep -Eq '^[A-Za-z0-9._-]+$' || die "manifest app_version 非法：$MANIFEST_APP_VERSION"
  printf '%s\n' "$MANIFEST_REDIS_KEYS" | grep -Eq '^[0-9]+$' || die "manifest redis_keys 非法：$MANIFEST_REDIS_KEYS"
  valid_manifest_file "$db_file" || die "manifest db_file 非法：$db_file"
  valid_manifest_file "$redis_file" || die "manifest redis_file 非法：$redis_file"
  valid_manifest_file "$config_file" || die "manifest config_file 非法：$config_file"
  valid_sha256 "$db_sha" || die "manifest db_sha256 非法"
  valid_sha256 "$redis_sha" || die "manifest redis_sha256 非法"
  valid_sha256 "$config_sha" || die "manifest config_sha256 非法"

  MANIFEST_DIR="$(cd "$(dirname "$manifest")" && pwd)"
  MANIFEST_DB_FILE="$MANIFEST_DIR/$db_file"
  MANIFEST_REDIS_FILE="$MANIFEST_DIR/$redis_file"
  MANIFEST_CONFIG_FILE="$MANIFEST_DIR/$config_file"
  [ -f "$MANIFEST_DB_FILE" ] || die "manifest 引用的 DB 备份不存在：$MANIFEST_DB_FILE"
  [ -f "$MANIFEST_REDIS_FILE" ] || die "manifest 引用的 Redis 备份不存在：$MANIFEST_REDIS_FILE"
  [ -f "$MANIFEST_CONFIG_FILE" ] || die "manifest 引用的 config 备份不存在：$MANIFEST_CONFIG_FILE"
  [ "$(sha256_file "$MANIFEST_DB_FILE")" = "$db_sha" ] || die "DB 备份 SHA-256 不匹配：$MANIFEST_DB_FILE"
  [ "$(sha256_file "$MANIFEST_REDIS_FILE")" = "$redis_sha" ] || die "Redis 备份 SHA-256 不匹配：$MANIFEST_REDIS_FILE"
  [ "$(sha256_file "$MANIFEST_CONFIG_FILE")" = "$config_sha" ] || die "config 备份 SHA-256 不匹配：$MANIFEST_CONFIG_FILE"
}

# verify_release_manifest <src/prev-<ts>.release>：将归档、VERSION 与烤入镜像的
# deploy-manifest.json 绑定在同一个 SHA-256 恢复点中。
verify_release_manifest() {
  local manifest="$1" base stem ts format archive_file archive_sha
  local version_file version_sha deploy_file deploy_sha version json_version
  [ -f "$manifest" ] || die "release manifest 不存在：$manifest"
  base="$(basename "$manifest")"
  case "$base" in
    src-[0-9]*.release|prev-[0-9]*.release) : ;;
    *) die "release manifest 文件名非法：$base" ;;
  esac
  stem="${base%.release}"
  ts="${stem#*-}"
  printf '%s\n' "$ts" | grep -Eq '^[0-9]{8}-[0-9]{6}$' || die "release timestamp 非法：$ts"

  format="$(manifest_value "$manifest" format)" || die "release manifest 缺失/重复 format"
  [ "$format" = newapi-release-v1 ] || die "release manifest format 不支持：$format"
  [ "$(manifest_value "$manifest" timestamp)" = "$ts" ] || die "release manifest timestamp 与文件名不一致"
  archive_file="$(manifest_value "$manifest" archive_file)" || die "release manifest 缺 archive_file"
  archive_sha="$(manifest_value "$manifest" archive_sha256)" || die "release manifest 缺 archive_sha256"
  version_file="$(manifest_value "$manifest" version_file)" || die "release manifest 缺 version_file"
  version_sha="$(manifest_value "$manifest" version_sha256)" || die "release manifest 缺 version_sha256"
  deploy_file="$(manifest_value "$manifest" deploy_manifest_file)" || die "release manifest 缺 deploy_manifest_file"
  deploy_sha="$(manifest_value "$manifest" deploy_manifest_sha256)" || die "release manifest 缺 deploy_manifest_sha256"
  version="$(manifest_value "$manifest" version)" || die "release manifest 缺 version"

  [ "$archive_file" = "$stem.tgz" ] || die "release archive_file 与恢复点不一致"
  [ "$version_file" = "$stem.version" ] || die "release version_file 与恢复点不一致"
  [ "$deploy_file" = "$stem.manifest.json" ] || die "release deploy_manifest_file 与恢复点不一致"
  valid_sha256 "$archive_sha" && valid_sha256 "$version_sha" && valid_sha256 "$deploy_sha" \
    || die "release manifest 含非法 SHA-256"
  printf '%s\n' "$version" | grep -Eq '^[A-Za-z0-9._-]+$' || die "release version 非法：$version"

  RELEASE_DIR="$(cd "$(dirname "$manifest")" && pwd)"
  RELEASE_ARCHIVE="$RELEASE_DIR/$archive_file"
  RELEASE_VERSION_FILE="$RELEASE_DIR/$version_file"
  RELEASE_DEPLOY_MANIFEST="$RELEASE_DIR/$deploy_file"
  [ -f "$RELEASE_ARCHIVE" ] && [ -f "$RELEASE_VERSION_FILE" ] && [ -f "$RELEASE_DEPLOY_MANIFEST" ] \
    || die "release manifest 引用文件不完整"
  [ "$(sha256_file "$RELEASE_ARCHIVE")" = "$archive_sha" ] || die "release 归档 SHA-256 不匹配"
  [ "$(sha256_file "$RELEASE_VERSION_FILE")" = "$version_sha" ] || die "release VERSION SHA-256 不匹配"
  [ "$(sha256_file "$RELEASE_DEPLOY_MANIFEST")" = "$deploy_sha" ] || die "release deploy manifest SHA-256 不匹配"
  [ "$(tr -d '\r\n' < "$RELEASE_VERSION_FILE")" = "$version" ] || die "release VERSION 内容不匹配"
  json_version="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$RELEASE_DEPLOY_MANIFEST" | head -n1)"
  [ "$json_version" = "$version" ] || die "deploy-manifest.json version '$json_version' ≠ release '$version'"
  RELEASE_TS="$ts"
  RELEASE_VERSION="$version"
}

service_container_id() {
  dc ps -a -q "$1" 2>/dev/null | head -n1
}

container_running() {
  [ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null || echo false)" = "true" ]
}

app_reported_version() {
  curl -fsS --max-time 8 -H "Host: $HOST_HEADER" "http://127.0.0.1:$APP_PORT/api/status" 2>/dev/null \
    | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
}

app_live() {
  curl -fsS --max-time 8 -H "Host: $HOST_HEADER" "http://127.0.0.1:$APP_PORT/health/live" 2>/dev/null \
    | grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"'
}

app_ready() {
  curl -fsS --max-time 8 -H "Host: $HOST_HEADER" "http://127.0.0.1:$APP_PORT/health/ready" 2>/dev/null \
    | grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"'
}

db_ready() {
  dc exec -T "$MYSQL_SVC" sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqladmin ping -h 127.0.0.1 -uroot --silent' >/dev/null 2>&1
}

redis_ready() {
  [ "$(redis_cli_service --raw PING 2>/dev/null | tr -d '\r')" = "PONG" ]
}

redis_key_count_service() {
  redis_cli_service --raw INFO keyspace 2>/dev/null \
    | tr -d '\r' | awk -F'[=,]' '/^db[0-9]+:keys=/{sum += $2} END{print sum + 0}'
}

redis_key_count_container() {
  docker exec "$1" redis-cli --raw INFO keyspace 2>/dev/null \
    | tr -d '\r' | awk -F'[=,]' '/^db[0-9]+:keys=/{sum += $2} END{print sum + 0}'
}

redis_persistence_value_container() {
  local container="$1" key="$2"
  docker exec "$container" redis-cli --raw INFO persistence 2>/dev/null \
    | tr -d '\r' | awk -F: -v wanted="$key" '$1 == wanted {print $2; exit}'
}

redis_persistence_value_service() {
  local key="$1"
  redis_cli_service --raw INFO persistence 2>/dev/null \
    | tr -d '\r' | awk -F: -v wanted="$key" '$1 == wanted {print $2; exit}'
}

# wait_runtime_ready [expected-version] [timeout]：/health/live 判进程，/health/ready
# 由 app 每次直连 DB 并在配置 Redis 时必检 Redis。/api/status 只用于制品
# 版本身份，不再充当健康信号。expected 为 - 时只要求非空版本。
wait_runtime_ready() {
  local expected="${1:--}" timeout="${2:-$READINESS_TIMEOUT}" deadline reported
  deadline=$(( $(date +%s) + timeout ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if app_live && app_ready; then
      reported="$(app_reported_version || true)"
      if [ -n "$reported" ] && { [ "$expected" = "-" ] || [ "$reported" = "$expected" ]; }; then
        return 0
      fi
    fi
    sleep "$READINESS_INTERVAL"
  done
  return 1
}

# extract_release_tree <archive> <empty-stage>：只向全新目录解包，禁止 overlay。
extract_release_tree() {
  local archive="$1" stage="$2"
  [ -f "$archive" ] || die "源码归档不存在：$archive"
  [ ! -e "$stage" ] || die "staging 已存在，拒绝覆盖：$stage"
  mkdir -p "$stage"
  if ! tar --no-same-owner -xzf "$archive" -C "$stage"; then
    rm -rf -- "$stage"
    die "源码归档解包失败：$archive"
  fi
  if [ ! -f "$stage/Dockerfile" ] \
    || [ ! -f "$stage/deploy/docker-compose.test.yml" ] \
    || [ ! -x "$stage/deploy/ops/rollback.sh" ]; then
    rm -rf -- "$stage"
    die "归档缺 Dockerfile/production compose/可执行 rollback.sh：$archive"
  fi
}

# preserve_runtime_env <old-root> <new-root>：当 ENV_FILE 在源码树内时只保留这一个
# 明确的外部状态。其余文件必须来自归档，否则会重新引入新旧混合树。
preserve_runtime_env() {
  local old_root="$1" new_root="$2" rel old_real env_real
  case "$ENV_FILE" in
    "$old_root"/*)
      rel="${ENV_FILE#"$old_root"/}"
      valid_release_relative_path "$rel" || die "ENV_FILE 相对路径非法：$rel"
      [ "$rel" = .env ] || die "ENV_FILE 位于 release 树内时必须是根目录 .env：$rel"
      [ -f "$ENV_FILE" ] || die "运行 env 不存在：$ENV_FILE"
      [ ! -L "$ENV_FILE" ] || die "ENV_FILE 不得是符号链接：$ENV_FILE"
      old_real="$(cd "$old_root" && pwd -P)"
      env_real="$(cd "$(dirname "$ENV_FILE")" && pwd -P)/$(basename "$ENV_FILE")"
      case "$env_real" in "$old_real"/*) : ;; *) die "ENV_FILE 真实路径逃出 release：$env_real" ;; esac
      mkdir -p "$new_root/$(dirname "$rel")"
      install -m 600 "$ENV_FILE" "$new_root/$rel"
      ;;
  esac
}

# swap_release_tree <stage> <old-tree>：stage 必须与 SERVER_REPO 同文件系统。先保留
# 旧树，新树就位失败时立即复原。old-tree 由调用方在验收通过后删除。
swap_release_tree() {
  local stage="$1" old_tree="$2"
  validate_managed_root "$SERVER_REPO"
  case "$stage" in "$SERVER_REPO".*) : ;; *) die "staging 必须是 SERVER_REPO 的受控 sibling：$stage" ;; esac
  case "$old_tree" in "$SERVER_REPO".*) : ;; *) die "old-tree 必须是 SERVER_REPO 的受控 sibling：$old_tree" ;; esac
  [ -d "$stage" ] || die "staging 不存在：$stage"
  [ -d "$SERVER_REPO" ] || die "SERVER_REPO 不存在：$SERVER_REPO"
  [ ! -L "$SERVER_REPO" ] || die "SERVER_REPO 不得是符号链接：$SERVER_REPO"
  [ -f "$stage/Dockerfile" ] && [ -f "$stage/deploy/docker-compose.test.yml" ] \
    || die "staging 不是有效 release 树：$stage"
  [ -f "$SERVER_REPO/Dockerfile" ] && [ -f "$SERVER_REPO/deploy/docker-compose.test.yml" ] \
    || die "当前 SERVER_REPO 缺 release marker，拒绝换树：$SERVER_REPO"
  [ ! -e "$old_tree" ] || die "旧树保留路径已存在：$old_tree"
  preserve_runtime_env "$SERVER_REPO" "$stage"
  mv "$SERVER_REPO" "$old_tree"
  if ! mv "$stage" "$SERVER_REPO"; then
    mv "$old_tree" "$SERVER_REPO" || true
    die "新 release 就位失败，已尝试复原旧树"
  fi
}

restore_release_tree() {
  local old_tree="$1" failed_tree="${SERVER_REPO}.restore-failed-$$"
  validate_managed_root "$SERVER_REPO"
  case "$old_tree" in "$SERVER_REPO".*) : ;; *) return 1 ;; esac
  [ -d "$old_tree" ] || return 1
  [ -f "$old_tree/Dockerfile" ] && [ -f "$old_tree/deploy/docker-compose.test.yml" ] || return 1
  [ -d "$SERVER_REPO" ] && [ ! -L "$SERVER_REPO" ] || return 1
  [ -f "$SERVER_REPO/Dockerfile" ] || return 1
  [ ! -e "$failed_tree" ] || return 1
  mv "$SERVER_REPO" "$failed_tree" || return 1
  if ! mv "$old_tree" "$SERVER_REPO"; then
    mv "$failed_tree" "$SERVER_REPO" || true
    return 1
  fi
  rm -rf -- "$failed_tree"
}

# prune_keep <glob>：按时间保留最近 $KEEP 份，删除更旧的。
prune_keep() {
  local pattern="$1" f
  # shellcheck disable=SC2012
  ls -1t $pattern 2>/dev/null | tail -n +"$((KEEP + 1))" | while IFS= read -r f; do
    [ -n "$f" ] && { rm -f -- "$f"; log "清理旧备份：$f"; }
  done || true
}

# prune_backup_sets：以 manifest 为备份集权威，连同其引用的三个产物一起删除。
# 旧版无 manifest 的单文件不自动删，避免升级脚本首次运行误删唯一灾备料。
prune_backup_sets() {
  local manifest base ts
  ls -1t "$BACKUP_DIR"/backup-*.manifest 2>/dev/null | tail -n +"$((KEEP + 1))" | while IFS= read -r manifest; do
    [ -n "$manifest" ] || continue
    base="$(basename "$manifest")"
    ts="${base#backup-}"; ts="${ts%.manifest}"
    if ! printf '%s\n' "$ts" | grep -Eq '^[0-9]{8}-[0-9]{6}$' \
      || ! (verify_backup_manifest "$manifest") >/dev/null 2>&1 \
      || [ "$(manifest_value "$manifest" timestamp 2>/dev/null || true)" != "$ts" ] \
      || [ "$(manifest_value "$manifest" db_file 2>/dev/null || true)" != "db-$ts.sql.gz" ] \
      || [ "$(manifest_value "$manifest" redis_file 2>/dev/null || true)" != "redis-$ts.rdb" ] \
      || [ "$(manifest_value "$manifest" config_file 2>/dev/null || true)" != "config-$ts.tar.gz" ]; then
      warn "旧备份 manifest 无法完整自证，为防跨集合误删已跳过：$manifest"
      continue
    fi
    rm -f -- "$BACKUP_DIR/db-$ts.sql.gz" "$BACKUP_DIR/redis-$ts.rdb" "$BACKUP_DIR/config-$ts.tar.gz"
    rm -f -- "$manifest"
    log "清理旧备份集：$manifest"
  done || true
}
