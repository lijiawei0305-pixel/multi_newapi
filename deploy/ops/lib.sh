#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# lib.sh — deploy/ops 公共参数 + 助手函数（被 backup/restore/rollback/healthcheck
#          source 引用）。**在服务器上运行**（Debian 12 + Docker compose v2）。
#
# 用法：在脚本顶部 `source "$(dirname "$0")/lib.sh"`。
# 所有参数都可被环境变量覆盖（`STACK=xxx ./backup.sh`），默认值对齐测试栈事实。
#
# 安全红线：本套脚本只操作隔离的测试栈 `newapi_test`，
#           **绝不触碰现网 `newapi_YFNf`（api.wedreamhub.com → :3000）**。
#           guard_not_prod 会在每个入口强制校验。
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# ── 栈与路径（部署事实，见 RETRO「部署与环境」/ doc/tasks/phase2.md）──────────────
STACK="${STACK:-newapi_test}"                                   # compose 项目名（-p 前缀隔离）
SERVER_REPO="${SERVER_REPO:-/root/newapi-test}"                 # 服务器仓库根
COMPOSE_FILE="${COMPOSE_FILE:-$SERVER_REPO/deploy/docker-compose.test.yml}"
ENV_FILE="${ENV_FILE:-$SERVER_REPO/.env}"                       # 上游 Key/密钥仅存此处（600）

# ── 服务名 / 容器内组件（compose service 名）─────────────────────────────────────
APP_SVC="${APP_SVC:-app}"
AUTH_SVC="${AUTH_SVC:-auth-service}"
MYSQL_SVC="${MYSQL_SVC:-mysql}"
REDIS_SVC="${REDIS_SVC:-redis}"

# ── 回环端口（仅 127.0.0.1 暴露，公网经宿主 nginx 反代）──────────────────────────
APP_PORT="${APP_PORT:-3100}"        # app  127.0.0.1:3100 → 容器 3000
AUTH_PORT="${AUTH_PORT:-8180}"      # auth 127.0.0.1:8180 → 容器 8080
HOST_HEADER="${HOST_HEADER:-tokendream.wedreamhub.com}"  # 多租户 Host 识别用

# ── 数据库（root/testpass123，库名含连字符需引用）────────────────────────────────
DB_NAME="${DB_NAME:-new-api-test}"
DB_USER="${DB_USER:-root}"
DB_PASS="${DB_PASS:-testpass123}"

# ── 备份 / 保留 ─────────────────────────────────────────────────────────────────
BACKUP_DIR="${BACKUP_DIR:-/root/backups}"
KEEP="${KEEP:-7}"                   # 各类备份各保留最近 N 份

# ── 宿主 nginx vhost（宝塔）+ 证书目录（备份/上线手册引用）────────────────────────
NGINX_VHOST="${NGINX_VHOST:-/www/server/panel/vhost/nginx/wildcard.wedreamhub.com.conf}"   # 通配上线 vhost（tokendream 专属已让位 .bak）
NGINX_CERT_DIR="${NGINX_CERT_DIR:-/www/server/panel/vhost/cert/wildcard.wedreamhub.com}"   # 通配 CF Origin CA 证书目录

# ── 告警 webhook（可选；非空则 healthcheck 失败时 POST，占位）──────────────────────
ALERT_WEBHOOK="${ALERT_WEBHOOK:-}"

# ── 现网项目名（红线：脚本绝不可操作它）──────────────────────────────────────────
PROD_STACK="${PROD_STACK:-newapi_YFNf}"

# ── 助手 ─────────────────────────────────────────────────────────────────────────
log()  { printf '\033[1;34m[ops]\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m[ ok]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[wrn]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[err]\033[0m %s\n' "$*" >&2; exit 1; }

# guard_not_prod：拒绝任何指向现网 newapi_YFNf 的配置（违反即中止）。
guard_not_prod() {
  [ "$STACK" != "$PROD_STACK" ] || die "拒绝操作现网栈 $PROD_STACK（红线）。"
  case "$COMPOSE_FILE$ENV_FILE$SERVER_REPO" in
    *"$PROD_STACK"*) die "路径指向现网 $PROD_STACK，已中止。" ;;
  esac
}

# dc：固化 compose 调用（-p 项目名 + --env-file + 绝对 -f，cwd 无关）。
# 绝对 -f 时 build context（compose 里 `..`）解析为 compose 文件目录的上级 = $SERVER_REPO。
dc() { docker compose -p "$STACK" --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }

# confirm <提示>：危险操作二次确认；需键入 yes。可用 ASSUME_YES=1 跳过（供 cron/自动化）。
confirm() {
  if [ "${ASSUME_YES:-0}" = "1" ]; then return 0; fi
  printf '\033[1;33m%s\033[0m ' "$1 [键入 yes 继续]:" >&2
  local ans; read -r ans
  [ "$ans" = "yes" ] || die "已取消。"
}

# require：确认依赖命令存在。
require() { command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"; }

# 预创建备份目录。
ensure_backup_dir() { mkdir -p "$BACKUP_DIR"; }

# prune_keep <glob>：按时间保留最近 $KEEP 份，删除更旧的。
prune_keep() {
  local pattern="$1" f
  # shellcheck disable=SC2012
  ls -1t $pattern 2>/dev/null | tail -n +"$((KEEP + 1))" | while IFS= read -r f; do
    [ -n "$f" ] && { rm -f -- "$f"; log "清理旧备份：$f"; }
  done || true
}
