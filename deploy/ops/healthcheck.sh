#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# healthcheck.sh — 测试栈 newapi_test 健康巡检（**服务器上运行**）。
#
#   检查项（任一失败 → 退出码 = 失败数，可接 cron/告警）：
#     1) app   /api/status   （带 Host 头，经回环 127.0.0.1:$APP_PORT）→ "success":true
#     2) auth  /auth/healthz （回环 127.0.0.1:$AUTH_PORT）            → "success":true
#     3) 各 compose 服务容器 running（app/auth/mysql/redis）
#     4) 磁盘使用率 < $DISK_MAX%（默认 90）
#     5) 内存使用率 < $MEM_MAX%（默认 90）
#   可选：设 ALERT_WEBHOOK 时，失败会 POST 一条 JSON 告警（占位，按需对接）。
#
# 用法：
#   ./healthcheck.sh                 # 巡检，打印结果；全绿退出 0
#   DISK_MAX=85 MEM_MAX=85 ./healthcheck.sh
#   ALERT_WEBHOOK=https://hooks... ./healthcheck.sh
#
# cron（每 5 分钟）：
#   */5 * * * * /root/newapi-test/deploy/ops/healthcheck.sh >> /var/log/newapi-health.log 2>&1
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
guard_not_prod
require docker; require curl

DISK_MAX="${DISK_MAX:-90}"
MEM_MAX="${MEM_MAX:-90}"
FAILS=0
REPORT=""

fail() { FAILS=$((FAILS + 1)); REPORT+="✗ $*"$'\n'; warn "$*"; }
pass() { REPORT+="✓ $*"$'\n'; ok "$*"; }

# ── 1) app /api/status（经 Host 头，模拟多租户入口）──────────────────────────────
if curl -fsS --max-time 10 -H "Host: $HOST_HEADER" \
      "http://127.0.0.1:$APP_PORT/api/status" 2>/dev/null | grep -q '"success":true'; then
  pass "app /api/status 正常（:$APP_PORT, Host=$HOST_HEADER）"
else
  fail "app /api/status 异常（:$APP_PORT）"
fi

# ── 2) auth-service /auth/healthz ────────────────────────────────────────────────
if curl -fsS --max-time 10 \
      "http://127.0.0.1:$AUTH_PORT/auth/healthz" 2>/dev/null | grep -q '"success":true'; then
  pass "auth /auth/healthz 正常（:$AUTH_PORT）"
else
  fail "auth /auth/healthz 异常（:$AUTH_PORT）"
fi

# ── 3) 容器 running 状态 ─────────────────────────────────────────────────────────
for svc in "$APP_SVC" "$AUTH_SVC" "$MYSQL_SVC" "$REDIS_SVC"; do
  cid="$(dc ps -q "$svc" 2>/dev/null || true)"
  if [ -z "$cid" ]; then
    fail "服务 $svc 无容器（未启动）"
    continue
  fi
  state="$(docker inspect -f '{{.State.Status}}' "$cid" 2>/dev/null || echo unknown)"
  health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}n/a{{end}}' "$cid" 2>/dev/null || echo n/a)"
  if [ "$state" = "running" ] && [ "$health" != "unhealthy" ]; then
    pass "容器 $svc running（health=$health）"
  else
    fail "容器 $svc 状态=$state health=$health"
  fi
done

# ── 4) 磁盘使用率（根分区，docker/备份所在）──────────────────────────────────────
disk="$(df -P / | awk 'NR==2{gsub("%","",$5); print $5}')"
if [ "${disk:-100}" -lt "$DISK_MAX" ]; then
  pass "磁盘使用率 ${disk}% < ${DISK_MAX}%"
else
  fail "磁盘使用率 ${disk}% ≥ ${DISK_MAX}%（清理日志/旧备份/镜像）"
fi

# ── 5) 内存使用率 ────────────────────────────────────────────────────────────────
mem="$(free | awk '/^Mem:/{printf "%d", $3*100/$2}')"
if [ "${mem:-100}" -lt "$MEM_MAX" ]; then
  pass "内存使用率 ${mem}% < ${MEM_MAX}%"
else
  fail "内存使用率 ${mem}% ≥ ${MEM_MAX}%"
fi

# ── 汇总 + 可选 webhook 告警 ─────────────────────────────────────────────────────
echo "────────── healthcheck @ $(date '+%F %T') 栈=$STACK ──────────"
printf '%s' "$REPORT"
if [ "$FAILS" -gt 0 ]; then
  warn "巡检发现 $FAILS 项异常。"
  if [ -n "$ALERT_WEBHOOK" ]; then
    # 占位：按目标平台（飞书/钉钉/Slack/企业微信）调整 payload 字段。
    curl -fsS --max-time 10 -X POST -H 'Content-Type: application/json' \
      -d "{\"text\":\"[newapi_test] healthcheck 失败 ${FAILS} 项 @ $(hostname)\"}" \
      "$ALERT_WEBHOOK" >/dev/null 2>&1 || warn "webhook 告警发送失败"
  fi
  exit "$FAILS"
fi
ok "全部健康（栈=$STACK）"
