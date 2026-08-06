#!/usr/bin/env bash
# healthcheck.sh —— newapi_test 服务器巡检。
#
#   1. /health/live：只检查 HTTP 进程存活。
#   2. /health/ready：每次直连 DB，且 Redis 已配置时必检 Redis。
#   3. app/mysql/redis 三个现役 compose 服务的容器状态。
#   4. 磁盘/内存阈值。
#   5. app 挂掉且无 ops 锁时自动拉起（防备份恢复失败后全站 502 挂死数小时）。
#
# 退役 auth-service 不在 compose 中，也不得再成为生产健康依赖。
set -euo pipefail
source "$(dirname "$0")/lib.sh"
guard_target
require docker
require curl

DISK_MAX="${DISK_MAX:-90}"
MEM_MAX="${MEM_MAX:-90}"
# AUTO_HEAL=0 可关自动拉起（演练/人工维护时）；默认开启。
AUTO_HEAL="${AUTO_HEAL:-1}"
FAILS=0
REPORT=""

fail() { FAILS=$((FAILS + 1)); REPORT+="✗ $*"$'\n'; warn "$*"; }
pass() { REPORT+="✓ $*"$'\n'; ok "$*"; }

if app_live; then
  pass "app /health/live 正常（:$APP_PORT, Host=$HOST_HEADER）"
elif [ "$AUTO_HEAL" = "1" ] && ops_lock_held; then
  fail "app /health/live 异常（:$APP_PORT）；ops lock 持有中，跳过自动拉起"
elif [ "$AUTO_HEAL" = "1" ]; then
  warn "app /health/live 异常，尝试自动拉起…"
  if ensure_app_running; then
    pass "app 已自动拉起并恢复 /health/live（:$APP_PORT）"
  else
    fail "app /health/live 异常且自动拉起失败（:$APP_PORT）"
  fi
else
  fail "app /health/live 异常（:$APP_PORT；AUTO_HEAL=0）"
fi

if app_ready; then
  pass "app /health/ready 正常（直连 DB + 已配置 Redis）"
else
  fail "app /health/ready 异常（DB/Redis/app 任一未就绪）"
fi

for svc in "$APP_SVC" "$MYSQL_SVC" "$REDIS_SVC"; do
  # -a：包含已停止容器，避免 compose 默认只列 running 时误报「无容器」。
  cid="$(dc ps -a -q "$svc" 2>/dev/null | head -n1 || true)"
  if [ -z "$cid" ]; then
    fail "服务 $svc 无容器（未创建）"
    continue
  fi
  state="$(docker inspect -f '{{.State.Status}}' "$cid" 2>/dev/null || echo unknown)"
  health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}n/a{{end}}' "$cid" 2>/dev/null || echo n/a)"
  if [ "$state" = running ] && [ "$health" != unhealthy ]; then
    pass "容器 $svc running（health=$health）"
  else
    fail "容器 $svc 状态=$state health=$health"
  fi
done

disk="$(df -P / | awk 'NR==2{gsub("%","",$5); print $5}')"
if [ "${disk:-100}" -lt "$DISK_MAX" ]; then
  pass "磁盘使用率 ${disk}% < ${DISK_MAX}%"
else
  fail "磁盘使用率 ${disk}% ≥ ${DISK_MAX}%（清理日志/旧备份/镜像）"
fi

mem="$(free | awk '/^Mem:/{printf "%d", $3*100/$2}')"
if [ "${mem:-100}" -lt "$MEM_MAX" ]; then
  pass "内存使用率 ${mem}% < ${MEM_MAX}%"
else
  fail "内存使用率 ${mem}% ≥ ${MEM_MAX}%"
fi

echo "---------- healthcheck @ $(date '+%F %T') stack=$STACK ----------"
printf '%s' "$REPORT"
if [ "$FAILS" -gt 0 ]; then
  warn "巡检发现 $FAILS 项异常。"
  if [ -n "$ALERT_WEBHOOK" ]; then
    curl -fsS --max-time 10 -X POST -H 'Content-Type: application/json' \
      -d "{\"text\":\"[newapi_test] healthcheck 失败 ${FAILS} 项 @ $(hostname)\"}" \
      "$ALERT_WEBHOOK" >/dev/null 2>&1 || warn "webhook 告警发送失败"
  fi
  exit "$FAILS"
fi
ok "全部健康（栈=$STACK）"
