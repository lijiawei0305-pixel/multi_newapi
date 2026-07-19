#!/usr/bin/env bash
# custom-domain-lib.sh —— 自定义域名证书签发的共享配置与函数（被 issue-cert.sh / domain-cert-loop.sh source）。
#
# 服务器侧（64.90.4.114，Debian 12 + 宝塔）。所有路径与"宝塔接管 Nginx、手写 vhost 共存"约定对齐：
# 自定义域名各写一个 vhost 文件到宝塔 vhost 目录（与 wildcard.wedreamhub.com.conf 同处），
# 不抢 default_server，不改宝塔已有站点 —— 故与宝塔零冲突（见 doc/domains-ssl.md / RETRO）。

# ── 可被环境变量覆盖的配置 ──────────────────────────────────────────────────────
ACME_BIN="${ACME_BIN:-/root/.acme.sh/acme.sh}"
ACME_EMAIL="${ACME_EMAIL:-admin@wedreamhub.com}"
WEBROOT="${WEBROOT:-/www/wwwroot/acme-challenge}"          # HTTP-01 challenge 共享 webroot
VHOST_DIR="${VHOST_DIR:-/www/server/panel/vhost/nginx}"    # 宝塔 nginx vhost 目录（主 nginx.conf include *.conf）
CERT_ROOT="${CERT_ROOT:-/www/server/panel/vhost/cert/custom}"  # 各自定义域名证书目录：<CERT_ROOT>/<domain>/
UPSTREAM="${UPSTREAM:-127.0.0.1:3100}"                     # 唯一现网多租户栈 app（回环口）
ENV_FILE="${ENV_FILE:-/root/newapi-test/.env}"            # 取 MT_INTERNAL_SECRET
API_BASE="${API_BASE:-http://127.0.0.1:3100/api/internal/domain}"
STATE_DIR="${STATE_DIR:-/var/lib/newapi-cert}"             # 签发退避状态
LOG_FILE="${LOG_FILE:-/var/log/newapi-cert.log}"

log() { printf '%s [cert] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$LOG_FILE" >&2; }

# internal_secret 读取主站内网共享密钥（与 app 容器同一 MT_INTERNAL_SECRET）。
internal_secret() {
  grep -E '^MT_INTERNAL_SECRET=' "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2-
}

# valid_domain 防注入：仅允许 DNS 主机名字符（小写字母/数字/点/连字符），且至少一个点。
valid_domain() {
  local d="$1"
  [[ "$d" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]]
}

# conf_path 返回某域名的 vhost 文件路径（custom_ 前缀便于辨识与清理）。
conf_path() { printf '%s/custom_%s.conf' "$VHOST_DIR" "$1"; }

# write_http_conf 写 HTTP-01 阶段 vhost（仅 80：放行 well-known，其余统一 308 HTTPS）。签发前必须先就位并 reload，
# 否则 LE 取 challenge 取不到。
write_http_conf() {
  local domain="$1"
  cat > "$(conf_path "$domain")" <<NGINX
# managed by newapi issue-cert.sh —— 自定义域名 ${domain}（HTTP-01 阶段，仅 80）
server {
    listen 80;
    server_name ${domain};
    client_max_body_size 200m;
    location ^~ /.well-known/acme-challenge/ {
        root ${WEBROOT};
        default_type "text/plain";
        allow all;
    }
    location / { return 308 https://\$host\$request_uri; }
}
NGINX
}

# write_full_conf 写最终 vhost（80→308 跳 https + 保留 well-known 供续期；443 ssl 反代 3100，Host 透传按 Host 解析租户）。
write_full_conf() {
  local domain="$1"
  cat > "$(conf_path "$domain")" <<NGINX
# managed by newapi issue-cert.sh —— 自定义域名 ${domain}（已签发证书）
server {
    listen 80;
    server_name ${domain};
    client_max_body_size 200m;
    location ^~ /.well-known/acme-challenge/ {
        root ${WEBROOT};
        default_type "text/plain";
        allow all;
    }
    location / { return 308 https://\$host\$request_uri; }
}
server {
    listen 443 ssl;
    server_name ${domain};
    client_max_body_size 200m;
    ssl_certificate     ${CERT_ROOT}/${domain}/fullchain.pem;
    ssl_certificate_key ${CERT_ROOT}/${domain}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    location ^~ /api/internal/ { return 404; }
    location / {
        proxy_pass http://${UPSTREAM};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
NGINX
}

# nginx_reload 校验后热加载（校验失败不 reload，避免打挂全站）。
nginx_reload() {
  if nginx -t >>"$LOG_FILE" 2>&1; then
    nginx -s reload >>"$LOG_FILE" 2>&1
    return 0
  fi
  log "nginx -t failed; NOT reloading"
  return 1
}

# internal_api_curl <secret> <curl args...>：通过 stdin FD（-H @-）传入密钥
# header，curl argv 不出现密钥本身，也不产生可遗留的临时文件。
internal_api_curl() (
  set -euo pipefail
  local secret="$1"
  shift
  [ -n "$secret" ] || return 1
  case "$secret" in
    *$'\r'*|*$'\n'*) log "MT_INTERNAL_SECRET contains a forbidden newline"; return 1 ;;
  esac

  printf 'X-Internal-Secret: %s\n' "$secret" | curl -H @- "$@"
)

# callback_cert_issued 回写主站内网端点：转 active + 失效缓存。
callback_cert_issued() {
  local domain="$1" expires="$2" secret
  secret="$(internal_secret)"
  if [[ -z "$secret" ]]; then log "no MT_INTERNAL_SECRET; skip callback for ${domain}"; return 1; fi
  internal_api_curl "$secret" -fsS -X POST "${API_BASE}/cert-issued" \
    -H 'Content-Type: application/json' \
    -d "{\"domain\":\"${domain}\",\"cert_status\":\"issued\",\"expires_at\":\"${expires}\"}" \
    >>"$LOG_FILE" 2>&1
}

# cert_expiry_iso 从已安装的 fullchain 读取证书到期时间，转 RFC3339 UTC（失败回退 +89 天）。
cert_expiry_iso() {
  local domain="$1" end
  end="$(openssl x509 -enddate -noout -in "${CERT_ROOT}/${domain}/fullchain.pem" 2>/dev/null | cut -d= -f2-)"
  if [[ -n "$end" ]]; then
    date -u -d "$end" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null && return 0
  fi
  date -u -d "+89 days" +%Y-%m-%dT%H:%M:%SZ
}
