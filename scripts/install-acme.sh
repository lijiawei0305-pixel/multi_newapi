#!/usr/bin/env bash
# install-acme.sh —— 安装经 SHA-256 锁定的 acme.sh，再写入 systemd timer。
#
# 不使用 `curl | sh`：这个脚本通常以 root 运行，远程内容必须先完整
# 下载、验证固定 commit 的 SHA-256，才允许执行。验证/安装/默认 CA
# 任一失败都不写 unit，不启用 timer。
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=custom-domain-lib.sh
source "$HERE/custom-domain-lib.sh"

ACME_VERSION="3.1.4"
ACME_COMMIT="3661fd86b6304115e42f43910e6dd452ab9866d6"
ACME_ARCHIVE_URL="https://codeload.github.com/acmesh-official/acme.sh/tar.gz/$ACME_COMMIT"
ACME_ARCHIVE_SHA256="9af3ad3d775a5782246df4cdd4b4e7b9b3179deb63c509b10e3ba0433093a884"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SYSTEMCTL="${SYSTEMCTL:-systemctl}"

for command_name in curl sha256sum tar mktemp install "$SYSTEMCTL"; do
  command -v "$command_name" >/dev/null 2>&1 \
    || { echo "[install-acme] missing command: $command_name" >&2; exit 1; }
done

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/newapi-acme.XXXXXX")"
UNITS_TOUCHED=0
UNITS_COMMITTED=0
SERVICE_EXISTED=0
TIMER_EXISTED=0
TIMER_WAS_ENABLED=0
TIMER_WAS_ACTIVE=0
cleanup() {
  local rc=$?
  trap - EXIT
  set +e
  if [ "$UNITS_TOUCHED" = 1 ] && [ "$UNITS_COMMITTED" != 1 ]; then
    if [ "$SERVICE_EXISTED" = 1 ]; then
      install -m 644 "$WORK_DIR/original-newapi-cert.service" "$SYSTEMD_DIR/newapi-cert.service"
    else
      rm -f -- "$SYSTEMD_DIR/newapi-cert.service"
    fi
    if [ "$TIMER_EXISTED" = 1 ]; then
      install -m 644 "$WORK_DIR/original-newapi-cert.timer" "$SYSTEMD_DIR/newapi-cert.timer"
    else
      rm -f -- "$SYSTEMD_DIR/newapi-cert.timer"
    fi
    "$SYSTEMCTL" daemon-reload >/dev/null 2>&1 || true
    if [ "$TIMER_WAS_ENABLED" = 1 ]; then
      "$SYSTEMCTL" enable newapi-cert.timer >/dev/null 2>&1 || true
    else
      "$SYSTEMCTL" disable newapi-cert.timer >/dev/null 2>&1 || true
    fi
    if [ "$TIMER_WAS_ACTIVE" = 1 ]; then
      "$SYSTEMCTL" start newapi-cert.timer >/dev/null 2>&1 || true
    else
      "$SYSTEMCTL" stop newapi-cert.timer >/dev/null 2>&1 || true
    fi
    echo "[install-acme] unit update failed; previous unit files/state restored" >&2
  fi
  rm -rf -- "$WORK_DIR"
  exit "$rc"
}
trap cleanup EXIT

ARCHIVE="$WORK_DIR/acme.sh-$ACME_COMMIT.tar.gz"
SOURCE_DIR="$WORK_DIR/acme.sh-$ACME_COMMIT"

echo "[install-acme] downloading pinned acme.sh v$ACME_VERSION ($ACME_COMMIT) ..."
curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error \
  "$ACME_ARCHIVE_URL" -o "$ARCHIVE"
printf '%s  %s\n' "$ACME_ARCHIVE_SHA256" "$ARCHIVE" | sha256sum -c - >/dev/null
echo "[install-acme] SHA-256 verified: $ACME_ARCHIVE_SHA256"

tar xzf "$ARCHIVE" -C "$WORK_DIR"
[ -f "$SOURCE_DIR/acme.sh" ] \
  || { echo "[install-acme] verified archive has no acme.sh" >&2; exit 1; }

ACME_HOME="$(dirname "$ACME_BIN")"
(
  cd "$SOURCE_DIR"
  sh ./acme.sh --install \
    --home "$ACME_HOME" \
    --email "$ACME_EMAIL" \
    --no-cron \
    --no-profile
)
[ -x "$ACME_BIN" ] \
  || { echo "[install-acme] installer did not create executable: $ACME_BIN" >&2; exit 1; }

ACME_VERSION_OUTPUT="$("$ACME_BIN" --version 2>&1)"
printf '%s\n' "$ACME_VERSION_OUTPUT" | grep -Fxq "v$ACME_VERSION" \
  || { echo "[install-acme] installed version is not v$ACME_VERSION" >&2; exit 1; }
"$ACME_BIN" --set-default-ca --server letsencrypt >/dev/null
echo "[install-acme] installed verified acme.sh v$ACME_VERSION"

# 远程制品验证与本地安装全部成功后，才创建运行目录/unit。
mkdir -p "$WEBROOT/.well-known/acme-challenge" "$CERT_ROOT" "$STATE_DIR" "$SYSTEMD_DIR"
touch "$LOG_FILE"

SERVICE_TMP="$WORK_DIR/newapi-cert.service"
TIMER_TMP="$WORK_DIR/newapi-cert.timer"
cat > "$SERVICE_TMP" <<UNIT
[Unit]
Description=newapi custom-domain cert issuance loop
After=network-online.target docker.service

[Service]
Type=oneshot
ExecStart=${HERE}/domain-cert-loop.sh
UNIT
cat > "$TIMER_TMP" <<UNIT
[Unit]
Description=run newapi custom-domain cert loop every 2 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=2min
Unit=newapi-cert.service

[Install]
WantedBy=timers.target
UNIT

if [ -f "$SYSTEMD_DIR/newapi-cert.service" ]; then
  cp "$SYSTEMD_DIR/newapi-cert.service" "$WORK_DIR/original-newapi-cert.service"
  SERVICE_EXISTED=1
fi
if [ -f "$SYSTEMD_DIR/newapi-cert.timer" ]; then
  cp "$SYSTEMD_DIR/newapi-cert.timer" "$WORK_DIR/original-newapi-cert.timer"
  TIMER_EXISTED=1
fi
if "$SYSTEMCTL" is-enabled newapi-cert.timer >/dev/null 2>&1; then TIMER_WAS_ENABLED=1; fi
if "$SYSTEMCTL" is-active newapi-cert.timer >/dev/null 2>&1; then TIMER_WAS_ACTIVE=1; fi
UNITS_TOUCHED=1
install -m 644 "$SERVICE_TMP" "$SYSTEMD_DIR/newapi-cert.service"
install -m 644 "$TIMER_TMP" "$SYSTEMD_DIR/newapi-cert.timer"
"$SYSTEMCTL" daemon-reload
"$SYSTEMCTL" enable --now newapi-cert.timer >/dev/null
TIMER_ENABLED="$("$SYSTEMCTL" is-enabled newapi-cert.timer)"
TIMER_ACTIVE="$("$SYSTEMCTL" is-active newapi-cert.timer)"
UNITS_COMMITTED=1
echo "[install-acme] timer: $TIMER_ENABLED / $TIMER_ACTIVE"
echo "[install-acme] done. webroot=$WEBROOT cert_root=$CERT_ROOT"
