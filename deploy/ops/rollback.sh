#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# rollback.sh — 回滚测试栈到上一个版本（**服务器上运行，危险操作**）。
#
# 两种模式：
#   (默认) 镜像回滚：把部署前保存的 :prev 镜像重新打回 :latest，force-recreate 重起，
#                    **不重建**（秒级）。前提：deploy.sh 部署前已 `docker tag … :prev`
#                    （deploy.sh 自动做；手动部署须先存：见下「前提」）。
#   --git <ref>     git 回滚：checkout 指定 tag/commit 后 `up -d --build` 重建。
#                    用于源码层回退（如 :prev 镜像已被覆盖/丢失）。
#
# 前提（镜像回滚可用的条件）——部署前须存过上一版镜像：
#   docker tag ${STACK}-${APP_SVC}:latest  ${STACK}-${APP_SVC}:prev
#   docker tag ${STACK}-${AUTH_SVC}:latest ${STACK}-${AUTH_SVC}:prev
#   （deploy.sh 在重建前自动执行；故正常流程下随时可 ./rollback.sh）
#
# 用法：
#   ./rollback.sh                       # 镜像回滚到 :prev
#   ./rollback.sh --git deploy-20260629-1200   # git 回滚到某 tag 后重建
#   ASSUME_YES=1 ./rollback.sh          # 跳过确认（deploy.sh 失败自动回滚时用）
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
guard_not_prod
require docker

APP_IMG="${STACK}-${APP_SVC}"
AUTH_IMG="${STACK}-${AUTH_SVC}"

rollback_git() {
  local ref="$1"
  [ -n "$ref" ] || die "--git 需指定 tag/commit"
  require git
  log "git 回滚：$SERVER_REPO → ${ref}（将重建镜像）"
  confirm "确认 checkout $ref 并重建 $STACK ?"
  git -C "$SERVER_REPO" fetch --tags --quiet || warn "git fetch 失败（离线？继续用本地引用）"
  git -C "$SERVER_REPO" checkout "$ref" || die "git checkout $ref 失败"
  log "重建并重起…"
  dc up -d --build
  ok "git 回滚完成（${ref}）。运行 ./healthcheck.sh 验证。"
}

rollback_image() {
  # 校验 :prev 镜像存在（前提）。
  docker image inspect "$APP_IMG:prev" >/dev/null 2>&1 \
    || die "找不到 $APP_IMG:prev —— 部署前未存上一版镜像，无法镜像回滚。改用 ./rollback.sh --git <tag>。"
  log "镜像回滚：$APP_IMG:prev → :latest（不重建，force-recreate）"
  confirm "确认把 $STACK 回滚到 :prev 镜像 ?"
  docker tag "$APP_IMG:prev"  "$APP_IMG:latest"
  if docker image inspect "$AUTH_IMG:prev" >/dev/null 2>&1; then
    docker tag "$AUTH_IMG:prev" "$AUTH_IMG:latest"
  else
    warn "$AUTH_IMG:prev 不存在，仅回滚 app（auth-service 保持现镜像）。"
  fi
  # force-recreate 确保容器拾取被重新打标的 :latest（配置未变 up -d 不会自动重建容器）。
  dc up -d --force-recreate --no-build
  ok "镜像回滚完成。运行 ./healthcheck.sh 验证；如仍异常，考虑 ./rollback.sh --git <稳定 tag>。"
}

case "${1:-}" in
  --git) rollback_git "${2:-}" ;;
  "")    rollback_image ;;
  *)     die "未知参数：$1（用法见脚本头）" ;;
esac
