#!/usr/bin/env bash
# rollback.sh —— 服务器端经验证、整棵 release 回滚。
#
#   ./rollback.sh              使用 deploy.sh 与 :prev 镜像成对保存的 prev release
#   ./rollback.sh --to <ts>    验证 src-<ts>.release 后从空 staging 重建
#   ./rollback.sh --list       列出已通过 SHA-256/版本绑定校验的恢复点
#
# 默认模式不是“只换镜像”：它同时换回与 :prev 成对的源码/compose，
# 防止出现“旧二进制 + 新 compose/新运维树”的混合 release。
source "$(dirname "$0")/lib.sh"
guard_target
require docker
require curl
require tar
require sha256sum

APP_IMG="${STACK}-${APP_SVC}"
ARCHIVE_DIR="${ARCHIVE_DIR:-/root/deploy-archives}"
ROLLBACK_STAGE=""

cleanup() {
  local rc=$?
  trap - EXIT
  set +e
  [ -z "$ROLLBACK_STAGE" ] || rm -rf -- "$ROLLBACK_STAGE"
  release_ops_lock || rc=1
  exit "$rc"
}

image_version() {
  local image="$1" version
  version="$(docker run --rm "$image" --version 2>/dev/null | tr -d '\r\n')" \
    || die "无法从镜像读取版本：$image"
  printf '%s\n' "$version" | grep -Eq '^[A-Za-z0-9._-]+$' \
    || die "镜像版本非法或为空：$image -> '$version'"
  printf '%s\n' "$version"
}

previous_release_manifest() {
  local pointer="$ARCHIVE_DIR/prev.current" name
  [ -s "$pointer" ] || die "找不到 $pointer；本机尚无与 :prev 成对的源码 release。"
  name="$(tr -d '\r\n' < "$pointer")"
  valid_manifest_file "$name" || die "prev.current 内文件名非法：$name"
  case "$name" in prev-[0-9]*.release) : ;; *) die "prev.current 不是 prev release：$name" ;; esac
  printf '%s/%s\n' "$ARCHIVE_DIR" "$name"
}

list_points() {
  local manifest file ts version prev_manifest
  log "可回滚点（只列出能完整自证的 release）："
  if docker image inspect "$APP_IMG:prev" >/dev/null 2>&1 \
    && prev_manifest="$(previous_release_manifest 2>/dev/null)" \
    && (verify_release_manifest "$prev_manifest") >/dev/null 2>&1; then
    version="$(manifest_value "$prev_manifest" version)"
    if [ "$(image_version "$APP_IMG:prev")" = "$version" ]; then
      printf '  [成对 :prev] version=%s release=%s\n' "$version" "$(basename "$prev_manifest")"
    else
      echo "  [成对 :prev] 无效（镜像与 release 版本不一致）"
    fi
  else
    echo "  [成对 :prev] 无"
  fi
  echo "  [源码归档] $ARCHIVE_DIR"
  for file in "$ARCHIVE_DIR"/src-*.release; do
    [ -e "$file" ] || continue
    if (verify_release_manifest "$file") >/dev/null 2>&1; then
      manifest="$file"
      ts="$(manifest_value "$manifest" timestamp)"
      version="$(manifest_value "$manifest" version)"
      printf '    --to %-18s version=%s\n' "$ts" "$version"
    else
      printf '    [invalid] %s\n' "$(basename "$file")"
    fi
  done
}

rollback_release() {
  local release_manifest="$1" mode="$2" target_version current_version current_cid current_image_id old_tree
  local target_ok=0 image_restored=0 source_restored=0
  verify_release_manifest "$release_manifest"
  target_version="$RELEASE_VERSION"
  docker image inspect "$APP_IMG:latest" >/dev/null 2>&1 \
    || die "找不到 $APP_IMG:latest，无法建立失败恢复点。"
  current_cid="$(service_container_id "$APP_SVC")"
  [ -n "$current_cid" ] || die "找不到当前 $APP_SVC 容器，无法建立精确恢复点。"
  current_image_id="$(docker inspect -f '{{.Image}}' "$current_cid")"
  [ -n "$current_image_id" ] || die "无法读取当前 app 容器的不可变镜像 ID。"
  current_version="$(image_version "$current_image_id")"
  if [ "$mode" = prev ]; then
    docker image inspect "$APP_IMG:prev" >/dev/null 2>&1 \
      || die "找不到 $APP_IMG:prev。"
    [ "$(image_version "$APP_IMG:prev")" = "$target_version" ] \
      || die ":prev 镜像版本与成对 release 不一致，拒绝混合回滚。"
  fi

  ROLLBACK_STAGE="${SERVER_REPO}.rollback-stage-${RELEASE_TS}-$$"
  old_tree="${SERVER_REPO}.rollback-old-${RELEASE_TS}-$$"
  log "整棵 release 回滚：$current_version -> $target_version（mode=$mode）"
  confirm "确认将 $STACK 回滚到 $target_version ?"

  extract_release_tree "$RELEASE_ARCHIVE" "$ROLLBACK_STAGE"
  # 解包后再校验一次原始恢复集；若在验证/解包间被替换，不换树。
  verify_release_manifest "$release_manifest"
  cp "$RELEASE_VERSION_FILE" "$ROLLBACK_STAGE/VERSION"
  cp "$RELEASE_DEPLOY_MANIFEST" "$ROLLBACK_STAGE/deploy-manifest.json"
  chmod 644 "$ROLLBACK_STAGE/VERSION" "$ROLLBACK_STAGE/deploy-manifest.json"

  # 先建立回滚失败时的镜像恢复点，再换树。tag 失败时源码零变更。
  docker tag "$current_image_id" "$APP_IMG:pre-rollback"
  swap_release_tree "$ROLLBACK_STAGE" "$old_tree"
  ROLLBACK_STAGE=""

  if [ "$mode" = prev ]; then
    if docker tag "$APP_IMG:prev" "$APP_IMG:latest" \
      && dc up -d --no-build \
      && wait_runtime_ready "$target_version" "$READINESS_TIMEOUT"; then
      target_ok=1
    fi
  elif dc up -d --build \
    && wait_runtime_ready "$target_version" "$READINESS_TIMEOUT"; then
    target_ok=1
  fi

  if [ "$target_ok" = 1 ]; then
    rm -rf -- "$old_tree" || warn "回滚已验收，但旧 release 副本清理失败：$old_tree"
    ok "release 回滚已验收：MySQL/Redis/app 就绪，version=$target_version"
    return 0
  fi

  warn "目标 release 构建/启动/验收失败，尝试同时恢复原镜像与原源码树…"
  if docker tag "$APP_IMG:pre-rollback" "$APP_IMG:latest"; then image_restored=1; fi
  if restore_release_tree "$old_tree"; then source_restored=1; fi
  if [ "$image_restored" = 1 ] && [ "$source_restored" = 1 ] \
    && dc up -d --no-build \
    && wait_runtime_ready "$current_version" "$READINESS_TIMEOUT"; then
    die "目标 release 回滚失败；原 release $current_version 已完整恢复并通过验收。"
  fi
  die "目标 release 回滚与原 release 恢复均未通过验收；不能宣称已回滚。"
}

case "${1:-}" in
  --list)
    list_points
    exit 0
    ;;
  --git)
    die "--git 已废弃：服务器是无 .git 归档树；请用 --to <ts>。"
    ;;
  --to)
    TS_ARG="${2:-}"
    printf '%s\n' "$TS_ARG" | grep -Eq '^[0-9]{8}-[0-9]{6}$' || die "--to 时间戳非法：$TS_ARG"
    acquire_ops_lock rollback
    trap cleanup EXIT
    rollback_release "$ARCHIVE_DIR/src-$TS_ARG.release" archive
    ;;
  "")
    acquire_ops_lock rollback
    trap cleanup EXIT
    PREV_RELEASE="$(previous_release_manifest)"
    rollback_release "$PREV_RELEASE" prev
    ;;
  *) die "未知参数：$1" ;;
esac
