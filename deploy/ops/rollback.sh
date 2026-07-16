#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# rollback.sh — 回滚测试栈到历史版本（**服务器上运行，危险操作**）。三种模式：
#
#   (默认)       镜像回滚：把部署前保存的 :prev 镜像重新打回 :latest，force-recreate 重起，
#                         **不重建**（秒级，回滚深度=1）。前提：deploy.sh 部署前已 `docker tag … :prev`。
#   --to <ts>    归档回滚：从 $ARCHIVE_DIR/src-<ts>.tgz 解包并重建（源码层回退，**多版本**）。
#                         用于 :prev 已被覆盖、或需回退到更早某次部署时。无需 .git。
#   --list       列出可回滚点：:prev 镜像 + 全部源码归档（含各自版本串）。
#
# 为什么没有 `--git`（#12）：服务器是「非 git 的 rsync 副本」——deploy.sh 用 `--exclude='./.git'`
#   上传，$SERVER_REPO 下**永远没有 .git** → 任何 `git checkout` 必败。旧脚本却在镜像回滚失败时
#   推荐 `--git <tag>`，指向一条不可能成功的路。源码层回退现由 deploy.sh 落下的归档 tar 承担。
#
# 前提（deploy.sh 每次部署自动满足）：
#   ① 重建前 `docker tag ${STACK}-${APP_SVC}:latest :prev`（供秒级镜像回滚）
#   ② 源码归档 $ARCHIVE_DIR/src-<ts>.tgz + sidecar .version/.manifest.json（供 --to 重建，恢复真实版本身份）
#
# 用法：
#   ./rollback.sh                          # 镜像回滚到 :prev（秒级）
#   ./rollback.sh --to 20260716-150816     # 从归档重建到该时间戳版本
#   ./rollback.sh --list                   # 列出可回滚点
#   ASSUME_YES=1 ./rollback.sh             # 跳过确认（deploy.sh 健康失败自动回滚时用）
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
guard_not_prod
require docker

APP_IMG="${STACK}-${APP_SVC}"
AUTH_IMG="${STACK}-${AUTH_SVC}"
ARCHIVE_DIR="${ARCHIVE_DIR:-/root/deploy-archives}"

# ── --list：列出所有可回滚点（镜像 :prev + 源码归档）──────────────────────────────
list_points() {
  log "可回滚点："
  if docker image inspect "$APP_IMG:prev" >/dev/null 2>&1; then
    echo "  [镜像 :prev] 可用 → ./rollback.sh（秒级，不重建，深度=1）"
  else
    echo "  [镜像 :prev] 无（尚未产生上一版镜像；首次部署或已丢失）"
  fi
  echo "  [源码归档] $ARCHIVE_DIR → ./rollback.sh --to <ts>（重建，无需 .git）："
  if ls "$ARCHIVE_DIR"/src-*.tgz >/dev/null 2>&1; then
    local f ts ver
    for f in $(ls -1t "$ARCHIVE_DIR"/src-*.tgz); do
      ts="$(basename "$f" .tgz)"; ts="${ts#src-}"
      ver="$(cat "$ARCHIVE_DIR/src-${ts}.version" 2>/dev/null || echo '?')"
      printf '    --to %-18s  version=%s\n' "$ts" "$ver"
    done
  else
    echo "    （无归档——本机 deploy.sh 尚未产出，或已被清理）"
  fi
}

# ── --to <ts>：从源码归档重建（多版本回退，无需 .git）──────────────────────────────
rollback_tarball() {
  local ts="$1"
  [ -n "$ts" ] || die "--to 需指定归档时间戳（见 ./rollback.sh --list）。"
  local arc="$ARCHIVE_DIR/src-${ts}.tgz"
  [ -f "$arc" ] || die "找不到归档 $arc（见 ./rollback.sh --list）。"
  local ver; ver="$(cat "$ARCHIVE_DIR/src-${ts}.version" 2>/dev/null || echo "$ts")"
  log "归档回滚：$arc（version=$ver）→ $SERVER_REPO（将重建镜像）"
  confirm "确认从归档 $ts 重建 $STACK ?"
  # 防孤儿：与 deploy.sh 同构，先清前端源码树再解包（移动/删除的文件不留旧孤儿）。
  rm -rf "$SERVER_REPO/web/default/src" "$SERVER_REPO/web/classic/src"
  tar xzf "$arc" -C "$SERVER_REPO"
  # 归档 tar 里的 VERSION/manifest 是部署时的 Mac 占位；用 sidecar 恢复真实版本身份，
  # 令重建后 /api/status 与 /deploy-manifest.json 仍如实反映「跑的是哪份代码」。
  [ -f "$ARCHIVE_DIR/src-${ts}.version" ]       && cp -f "$ARCHIVE_DIR/src-${ts}.version"       "$SERVER_REPO/VERSION"
  [ -f "$ARCHIVE_DIR/src-${ts}.manifest.json" ] && cp -f "$ARCHIVE_DIR/src-${ts}.manifest.json" "$SERVER_REPO/deploy-manifest.json"
  log "重建并重起…"
  dc up -d --build
  ok "归档回滚完成（$ts / version=$ver）。运行 ./healthcheck.sh 验证。"
}

# ── (默认)：镜像回滚到 :prev（秒级，深度=1）────────────────────────────────────────
rollback_image() {
  docker image inspect "$APP_IMG:prev" >/dev/null 2>&1 \
    || die "找不到 $APP_IMG:prev —— 无上一版镜像，无法秒级回滚。改用 ./rollback.sh --to <ts> 从源码归档重建（见 ./rollback.sh --list）。"
  log "镜像回滚：$APP_IMG:prev → :latest（不重建，force-recreate，深度=1）"
  confirm "确认把 $STACK 回滚到 :prev 镜像 ?"
  docker tag "$APP_IMG:prev" "$APP_IMG:latest"
  if docker image inspect "$AUTH_IMG:prev" >/dev/null 2>&1; then
    docker tag "$AUTH_IMG:prev" "$AUTH_IMG:latest"
  else
    warn "$AUTH_IMG:prev 不存在，仅回滚 app（auth-service 保持现镜像）。"
  fi
  # force-recreate 确保容器拾取被重新打标的 :latest（配置未变 up -d 不会自动重建容器）。
  dc up -d --force-recreate --no-build
  ok "镜像回滚完成。运行 ./healthcheck.sh 验证；如需回退更早版本，用 ./rollback.sh --to <ts>（见 ./rollback.sh --list）。"
}

case "${1:-}" in
  --to)   rollback_tarball "${2:-}" ;;
  --list) list_points ;;
  --git)  die "--git 已废弃（#12）：服务器是非 git 副本、无 .git，git 回滚必败。改用 ./rollback.sh --to <ts>（从源码归档重建），或 ./rollback.sh --list 查看可回滚点。" ;;
  "")     rollback_image ;;
  *)      die "未知参数：$1（用法见脚本头；或 ./rollback.sh --list）。" ;;
esac
