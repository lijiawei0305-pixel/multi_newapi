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

resp="$(internal_api_curl "$SECRET" -fsS "${API_BASE}/pending-cert" 2>>"$LOG_FILE")" || { log "pending-cert fetch failed"; exit 0; }

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

# ── 阶段 0(≤1 次/20h):回刷 active 域名的磁盘证书到期时间进 DB ──────────────────
# acme.sh --cron 自动续期只更新磁盘证书、不写 DB;不回刷则 cert_expires_at 停在首签时刻,
# 前端「SSL 到期提醒」续期后会变假警报(P3 #9)。复用 cert-issued 回写(幂等,重置 active 无副作用)。
REFRESH_STAMP="$STATE_DIR/.expiry-refresh"
if [[ ! -f "$REFRESH_STAMP" || -n "$(find "$REFRESH_STAMP" -mmin +1200 2>/dev/null)" ]]; then
  act="$(internal_api_curl "$SECRET" -fsS "${API_BASE}/active-cert" 2>>"$LOG_FILE")" || act=""
  if [[ -n "$act" ]]; then
    mapfile -t actives < <(parse_domains "$act")
    n=0
    for d in "${actives[@]}"; do
      [[ -z "$d" ]] && continue
      exp="$(cert_expiry_iso "$d")"
      callback_cert_issued "$d" "$exp" && n=$((n+1))
    done
    (( n > 0 )) && log "expiry refresh: ${n} active domain(s) synced"
    touch "$REFRESH_STAMP"
  fi
fi

now=$(date +%s)
mapfile -t raw < <(parse_domains "$resp")
# 过滤空行(python3 兜底对空数组会输出一个空行,曾被误计为 1 个域名)。
domains=()
for d in "${raw[@]}"; do [[ -n "$d" ]] && domains+=("$d"); done
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
