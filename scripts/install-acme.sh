#!/usr/bin/env bash
# install-acme.sh —— 安装 acme.sh 并设默认 CA 为 Let's Encrypt + 准备 webroot/状态目录/systemd timer。
# 幂等：已安装则跳过安装，仅补默认 CA 与目录/单元。
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=custom-domain-lib.sh
source "$HERE/custom-domain-lib.sh"

mkdir -p "$WEBROOT/.well-known/acme-challenge" "$CERT_ROOT" "$STATE_DIR"
touch "$LOG_FILE" 2>/dev/null || true

if [[ ! -x "$ACME_BIN" ]]; then
  echo "[install-acme] installing acme.sh ..."
  curl -fsS https://get.acme.sh | sh -s "email=${ACME_EMAIL}"
fi
"$ACME_BIN" --set-default-ca --server letsencrypt >/dev/null 2>&1 || true
echo "[install-acme] acme.sh: $("$ACME_BIN" --version 2>/dev/null | tr '\n' ' ')"

# systemd timer：每 2 分钟跑一次签发循环。
SVC=/etc/systemd/system/newapi-cert.service
TMR=/etc/systemd/system/newapi-cert.timer
cat > "$SVC" <<UNIT
[Unit]
Description=newapi custom-domain cert issuance loop
After=network-online.target docker.service

[Service]
Type=oneshot
ExecStart=${HERE}/domain-cert-loop.sh
UNIT
cat > "$TMR" <<UNIT
[Unit]
Description=run newapi custom-domain cert loop every 2 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=2min
Unit=newapi-cert.service

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now newapi-cert.timer >/dev/null 2>&1 || true
echo "[install-acme] timer: $(systemctl is-enabled newapi-cert.timer 2>/dev/null) / $(systemctl is-active newapi-cert.timer 2>/dev/null)"
echo "[install-acme] done. webroot=${WEBROOT} cert_root=${CERT_ROOT}"
