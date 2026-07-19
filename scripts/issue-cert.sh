#!/usr/bin/env bash
# issue-cert.sh <domain> —— 为单个已 DNS 验证（dns_verified）的自定义域名签发 Let's Encrypt 证书并上线。
#
# 流程：先写 HTTP-only vhost + reload（让 LE HTTP-01 可达）→ acme.sh --issue --webroot（EC-256）
#   → install-cert（含 --reloadcmd 续期自动 reload）→ 写 80→https + 443 ssl 完整 vhost + reload
#   → 回写主站内网端点 /api/internal/domain/cert-issued（转 active + 失效缓存）。
#
# 幂等：可重复执行（acme.sh 已有有效证书会跳过签发，仍刷新 vhost + 回写）。
# 红线：只反代到唯一现网多租户栈 127.0.0.1:3100；拒绝 /api/internal/；
# 不声明 default_server（与宝塔已有站点共存）。
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=custom-domain-lib.sh
source "$HERE/custom-domain-lib.sh"

DOMAIN="${1:-}"
if [[ -z "$DOMAIN" ]]; then echo "usage: $0 <domain>" >&2; exit 2; fi
DOMAIN="$(echo "$DOMAIN" | tr '[:upper:]' '[:lower:]')"
if ! valid_domain "$DOMAIN"; then log "invalid domain: ${DOMAIN}"; exit 2; fi
if [[ ! -x "$ACME_BIN" ]]; then log "acme.sh not installed at ${ACME_BIN}; run install-acme.sh"; exit 3; fi

mkdir -p "$WEBROOT/.well-known/acme-challenge" "$CERT_ROOT/$DOMAIN" "$STATE_DIR"
log "issuing cert for ${DOMAIN}"

# 阶段 1：HTTP-only vhost 就位（供 HTTP-01 challenge）。
write_http_conf "$DOMAIN"
nginx_reload || { log "stage1 reload failed for ${DOMAIN}"; exit 4; }

# 阶段 2：签发（webroot HTTP-01，EC-256）。已存在有效证书时 acme.sh 返回非 0(2) 但属正常跳过。
"$ACME_BIN" --issue -d "$DOMAIN" --webroot "$WEBROOT" --keylength ec-256 --server letsencrypt >>"$LOG_FILE" 2>&1
rc=$?
if [[ $rc -ne 0 && $rc -ne 2 ]]; then
  log "acme issue failed for ${DOMAIN} (rc=${rc})"
  exit 5
fi

# 阶段 3：安装证书到固定路径，登记续期自动 reload。
if ! "$ACME_BIN" --install-cert -d "$DOMAIN" --ecc \
      --key-file "$CERT_ROOT/$DOMAIN/privkey.pem" \
      --fullchain-file "$CERT_ROOT/$DOMAIN/fullchain.pem" \
      --reloadcmd "nginx -s reload" >>"$LOG_FILE" 2>&1; then
  log "install-cert failed for ${DOMAIN}"
  exit 6
fi

# 阶段 4：完整 vhost（443 ssl + 80 跳转）+ reload。
write_full_conf "$DOMAIN"
nginx_reload || { log "stage4 reload failed for ${DOMAIN}"; exit 7; }

# 阶段 5：回写主站（转 active）。
EXP="$(cert_expiry_iso "$DOMAIN")"
if callback_cert_issued "$DOMAIN" "$EXP"; then
  log "cert issued + active for ${DOMAIN} (expires ${EXP})"
  rm -f "$STATE_DIR/${DOMAIN}.state"   # 成功清退避状态
else
  log "callback failed for ${DOMAIN} (cert installed; will retry callback next loop)"
  exit 8
fi
