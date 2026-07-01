#!/usr/bin/env bash
# domain-cert-loop.sh —— 周期扫"待发证"（dns_verified）域名并逐个签发，含 Let's Encrypt 限流指数退避。
#
# 由 systemd timer（newapi-cert.timer，默认每 2 分钟）触发一次；非常驻。
# 取待发证列表走主站内网端点 GET /api/internal/domain/pending-cert（共享密钥）。
# 退避：每域名记 fail_count + last_attempt；下次允许时刻 = last_attempt + min(2^fail_count, 60) 分钟，
# 避免 LE 限流封禁（5 失败/小时/域名）。成功后由 issue-cert.sh 清状态。
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=custom-domain-lib.sh
source "$HERE/custom-domain-lib.sh"

mkdir -p "$STATE_DIR"
SECRET="$(internal_secret)"
if [[ -z "$SECRET" ]]; then log "no MT_INTERNAL_SECRET; abort loop"; exit 1; fi

resp="$(curl -fsS "${API_BASE}/pending-cert" -H "X-Internal-Secret: ${SECRET}" 2>>"$LOG_FILE")" || { log "pending-cert fetch failed"; exit 0; }

# 解析 data.domains[]（优先 jq，回退 python3，再回退 grep）。
parse_domains() {
  if command -v jq >/dev/null 2>&1; then
    echo "$1" | jq -r '.data.domains[]?' 2>/dev/null
  elif command -v python3 >/dev/null 2>&1; then
    echo "$1" | python3 -c 'import sys,json; print("\n".join(json.load(sys.stdin).get("data",{}).get("domains",[]) or []))' 2>/dev/null
  else
    echo "$1" | grep -oE '"domains":\[[^]]*\]' | grep -oE '[a-z0-9][a-z0-9.-]*\.[a-z]+'
  fi
}

# backoff_minutes 给定失败次数返回退避分钟数（封顶 60）。
backoff_minutes() {
  local n="$1" m=$((1 << n))
  (( m > 60 )) && m=60
  echo "$m"
}

now=$(date +%s)
mapfile -t domains < <(parse_domains "$resp")
if [[ ${#domains[@]} -eq 0 ]]; then exit 0; fi
log "pending-cert: ${#domains[@]} domain(s)"

for d in "${domains[@]}"; do
  [[ -z "$d" ]] && continue
  state="$STATE_DIR/${d}.state"
  fail=0; last=0
  if [[ -f "$state" ]]; then
    # 文件格式：fail_count last_attempt_epoch
    read -r fail last < "$state" 2>/dev/null || { fail=0; last=0; }
  fi
  if (( fail > 0 )); then
    wait_min=$(backoff_minutes "$fail")
    next=$(( last + wait_min * 60 ))
    if (( now < next )); then
      log "skip ${d}: backoff (fail=${fail}, retry in $(( (next-now)/60 ))m)"
      continue
    fi
  fi
  if "$HERE/issue-cert.sh" "$d"; then
    : # issue-cert.sh 成功时已删状态文件
  else
    fail=$(( fail + 1 ))
    printf '%s %s\n' "$fail" "$now" > "$state"
    log "issue failed for ${d} (fail=${fail}); backing off"
  fi
done
