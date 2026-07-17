#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# deploy.sh — 一键部署测试栈 newapi_test（**从 Mac 仓库根运行**，SSH 到服务器执行）。
#
#   固化全流程：预检 → 打 git tag → 存 :prev 镜像 → 备份 → tar 上传 → 后台重建
#               → 轮询健康 → 失败自动回滚。对齐 RETRO 部署惯例：
#               COPYFILE_DISABLE=1 tar-over-ssh、--env-file、后台 up -d --build。
#
#   运行位置：Mac 本仓库（W4：Mac 只编辑/调试，构建/部署在服务器；本脚本负责编排）。
#   护栏：确认部署目标确为期望栈 newapi_test（2026-07-03 单栈收敛后它是唯一现网/生产；
#         原 stock 栈 newapi_YFNf 已删除）。正向白名单挡拼写/误配，非旧的"拒绝 prod"。
#
# 用法：
#   ./deploy/ops/deploy.sh                 # 全流程部署
#   SKIP_PREFLIGHT=1 ./deploy/ops/deploy.sh    # 跳过本地预检（已手动跑过）
#   SKIP_BACKUP=1   ./deploy/ops/deploy.sh     # 跳过部署前远端备份
#   NO_ROLLBACK=1   ./deploy/ops/deploy.sh     # 健康失败时不自动回滚（仅告警）
#
# 前提：`ssh newapi628` 免密可用；服务器 $SERVER_REPO 已存在 .env（600，含上游 Key）。
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# locale 硬化：macOS 自带 bash 3.2 在 UTF-8 locale 下会把「$VAR 后紧跟的中文多字节字符」
# 首字节并入变量名 → set -u 误报 unbound（如 line 106 的 "$SERVER_REPO（并归档…"）。
# 强制 C locale：bash 按单字节处理、高位字节非标识符字符 → 变量名正确终止；中文 log 为
# 字节透传，显示不受影响。en_US.UTF-8 亦无法绕过（bash 3.2 多字节解析本身有 bug）。
export LC_ALL=C LANG=C

# ── 参数（可环境变量覆盖）────────────────────────────────────────────────────────
SSH_HOST="${SSH_HOST:-newapi628}"
LOCAL_REPO="${LOCAL_REPO:-$(cd "$(dirname "$0")/../.." && pwd)}"   # Mac 仓库根
SERVER_REPO="${SERVER_REPO:-/root/newapi-test}"
STACK="${STACK:-newapi_test}"
EXPECTED_STACK="${EXPECTED_STACK:-newapi_test}"   # 正向白名单：部署目标须确为它（原 PROD_STACK 守着已删除的 YFNf，恒放行，已弃）
COMPOSE_FILE="${COMPOSE_FILE:-$SERVER_REPO/deploy/docker-compose.test.yml}"
ENV_FILE="${ENV_FILE:-$SERVER_REPO/.env}"
APP_SVC="${APP_SVC:-app}"; AUTH_SVC="${AUTH_SVC:-auth-service}"
APP_PORT="${APP_PORT:-3100}"
HOST_HEADER="${HOST_HEADER:-tokendream.wedreamhub.com}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-600}"   # 轮询健康最长等待秒数（含构建）
APP_IMG="${STACK}-${APP_SVC}"; AUTH_IMG="${STACK}-${AUTH_SVC}"

log()  { printf '\033[1;34m[deploy]\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m[ ok ]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[fail]\033[0m %s\n' "$*" >&2; exit 1; }
remote() { ssh "$SSH_HOST" "$@"; }
# 服务器侧 compose 串（与 lib.sh dc() 等价）。
DC="docker compose -p $STACK --env-file $ENV_FILE -f $COMPOSE_FILE"

# 护栏：正向白名单——确认部署目标确为期望栈（挡拼写/误配）。
[ "$STACK" = "${EXPECTED_STACK}" ] || die "目标栈 '$STACK' ≠ 期望栈 '${EXPECTED_STACK}'（防误配/拼写），拒绝部署。"

TS="$(date +%Y%m%d-%H%M%S)"
TAG="deploy-$TS"

# ── 版本身份（C6/#12）：算真实、可溯源的版本串，盖进制品并随源码归档 ──────────────────
# 背景：生产曾无版本身份——VERSION 是 0 字节 → /api/status version=""；服务器无 .git → --git 回滚必败。
# 修法：用「git 短 SHA + 脏树标记 + 时间戳」构造真实版本串；部署时写进服务器 VERSION 与 deploy-manifest.json
#       （Dockerfile 烤进 Go 二进制 common.Version 与前端），并把整棵源码 tar 归档到 $ARCHIVE_DIR，
#       令任一近期部署无需 .git 即可重建（rollback.sh --to <ts>）。
ARCHIVE_DIR="${ARCHIVE_DIR:-/root/deploy-archives}"
ARCHIVE_KEEP="${ARCHIVE_KEEP:-7}"
GIT_SHA="$(cd "$LOCAL_REPO" && git rev-parse --short HEAD 2>/dev/null || echo nogit)"
GIT_BRANCH="$(cd "$LOCAL_REPO" && git rev-parse --abbrev-ref HEAD 2>/dev/null || echo nogit)"
GIT_DIRTY=false; DIRTY_SUFFIX=""
if [ -n "$(cd "$LOCAL_REPO" && git status --porcelain 2>/dev/null)" ]; then GIT_DIRTY=true; DIRTY_SUFFIX="-dirty"; fi
DEPLOYER="$(cd "$LOCAL_REPO" && git config user.name 2>/dev/null || whoami)"; DEPLOYER="${DEPLOYER//\'/}"
APP_VERSION="${GIT_SHA}${DIRTY_SUFFIX}-${TS}"     # 例：cd7d30d-dirty-20260716-150816
MANIFEST_JSON="{\"version\":\"$APP_VERSION\",\"git_sha\":\"$GIT_SHA\",\"dirty\":$GIT_DIRTY,\"branch\":\"$GIT_BRANCH\",\"deploy_tag\":\"$TAG\",\"deployed_at\":\"$TS\",\"deployed_by\":\"$DEPLOYER\"}"

# ── 1) 本地预检（复用 scripts/preflight.sh：gofmt/build/vet/test/前端构建）──────────
if [ "${SKIP_PREFLIGHT:-0}" != "1" ]; then
  log "1/8 本地预检 scripts/preflight.sh …"
  ( cd "$LOCAL_REPO" && bash scripts/preflight.sh ) || die "预检未过，已中止上传。"
else
  log "1/8 跳过预检（SKIP_PREFLIGHT=1）"
fi

# ── 2) 打 git tag（HEAD 标记；仅供参考，非权威版本）──────────────────────────────
# 诚实性（#12）：tag 打在 HEAD 上，而部署打包的是工作树。若工作树脏（dirty），tag 指向的代码
#   与实际上线的代码不一致——权威的「可重建版本」是 $ARCHIVE_DIR/src-$TS.tgz + 盖进制品的 $APP_VERSION，
#   不是这个 tag。故 dirty 时明确告警，避免把 tag 当成真实部署版本。
log "2/8 打 git tag ${TAG}（HEAD 标记，仅参考）"
( cd "$LOCAL_REPO" && git tag -f "${TAG}" >/dev/null 2>&1 ) \
  && ok "已打 tag ${TAG}（→ HEAD=${GIT_SHA}）" || log "（git tag 跳过：非 git 环境或无变更）"
[ "$GIT_DIRTY" = "true" ] && log "⚠ 工作树为 dirty：tag ${TAG} 指向 HEAD≠实际部署树。权威版本=${APP_VERSION}（见归档 src-${TS}.tgz）。"

# ── 3) 存 :prev 镜像（重建前；供 rollback.sh 秒级回滚）────────────────────────────
log "3/8 服务器保存当前镜像为 :prev（回滚用）"
remote "
  set -e
  if docker image inspect $APP_IMG:latest >/dev/null 2>&1; then
    docker tag $APP_IMG:latest $APP_IMG:prev && echo '  saved $APP_IMG:prev'
  else echo '  （首次部署，无 $APP_IMG:latest，跳过 :prev）'; fi
  if docker image inspect $AUTH_IMG:latest >/dev/null 2>&1; then
    docker tag $AUTH_IMG:latest $AUTH_IMG:prev && echo '  saved $AUTH_IMG:prev'
  fi
"

# ── 4) 部署前远端备份（保险；可 SKIP_BACKUP=1 跳过）──────────────────────────────
if [ "${SKIP_BACKUP:-0}" != "1" ]; then
  log "4/8 部署前远端备份（backup.sh）"
  remote "test -x $SERVER_REPO/deploy/ops/backup.sh && $SERVER_REPO/deploy/ops/backup.sh || echo '  （backup.sh 尚未上传，跳过本次；下次部署后即可用）'"
else
  log "4/8 跳过部署前备份（SKIP_BACKUP=1）"
fi

# ── 5) tar-over-ssh 上传（COPYFILE_DISABLE=1，排除 .git/node_modules/dist/垃圾）──────
# 归档（C6/#12）：源码 tar 先落到 $ARCHIVE_DIR/src-$TS.tgz，再从该归档解包——
#   于是「实际上线的那棵树」被原样留存，任一近期部署无需 .git 即可 rollback.sh --to <ts> 重建。
log "5/8 上传源码 → $SSH_HOST:$SERVER_REPO（并归档 src-$TS.tgz）"
remote "mkdir -p $SERVER_REPO $ARCHIVE_DIR"
# delete-sync 前端源码树（防孤儿）：tar 只覆盖不删除，路由/组件被移动或删除后
# 服务器会留旧文件孤儿 → 前端构建挂（旧文件 import 已重命名/删除的符号）或 false-success。
# web/default/src、web/classic/src 全为仓库文件（.env/node_modules/dist 均在其外），
# 部署前清空可保证上传后与本机完全一致、零孤儿。（Go 侧孤儿由步骤 8 镜像 ID 校验兜底。）
log "    delete-sync：清理前端源码树（防移动/删除文件残留孤儿）"
remote "rm -rf $SERVER_REPO/web/default/src $SERVER_REPO/web/classic/src"
COPYFILE_DISABLE=1 tar czf - \
  --exclude='./.git' \
  --exclude='node_modules' \
  --exclude='./web/default/dist' \
  --exclude='./web/classic/dist' \
  --exclude='.DS_Store' \
  --exclude='._*' \
  --exclude='./bulb-orbit' \
  -C "$LOCAL_REPO" . \
  | remote "cat > $ARCHIVE_DIR/src-$TS.tgz && tar xzf $ARCHIVE_DIR/src-$TS.tgz -C $SERVER_REPO"
ok "上传完成 + 归档 $ARCHIVE_DIR/src-$TS.tgz（服务器 Dockerfile 内构建前端 dist + go embed）"

# ── 5.5) 盖版本身份（C6/#12）：服务器 VERSION + deploy-manifest.json（解包之后、构建之前）──
# tar 内的 VERSION/manifest 是 Mac 占位；必须在解包后覆盖为真实版本串，Dockerfile 的
# `$(cat VERSION)` 与 `COPY deploy-manifest.json` 才能把真实身份烤进制品。
# 同步为归档写 sidecar（.version/.manifest.json），令 rollback.sh --to 重建时也能恢复真实身份。
log "5.5/8 盖版本身份 → VERSION=$APP_VERSION + deploy-manifest.json"
remote "printf '%s' '$APP_VERSION' > $SERVER_REPO/VERSION"
remote "printf '%s' '$MANIFEST_JSON' > $SERVER_REPO/deploy-manifest.json"
remote "cp -f $SERVER_REPO/VERSION $ARCHIVE_DIR/src-$TS.version && cp -f $SERVER_REPO/deploy-manifest.json $ARCHIVE_DIR/src-$TS.manifest.json"
# 归档只保留最近 $ARCHIVE_KEEP 份（含其 sidecar），防无限增长。
remote "cd $ARCHIVE_DIR && ls -1t src-*.tgz 2>/dev/null | tail -n +$((ARCHIVE_KEEP+1)) | while read -r f; do b=\${f%.tgz}; rm -f \"\$f\" \"\$b.version\" \"\$b.manifest.json\"; done; echo '  归档保留最近 $ARCHIVE_KEEP 份'"

# ── 6) 后台重建（nohup，避免长构建 ssh 断流误判，见 RETRO）──────────────────────
log "6/8 后台重建 up -d --build（日志 $SERVER_REPO/deploy-build.log）"
remote "cd $SERVER_REPO && nohup $DC up -d --build > $SERVER_REPO/deploy-build.log 2>&1 & echo '  构建已在后台启动'"

# ── 7) 轮询健康（构建+启动可能数分钟；超时即判失败）──────────────────────────────
log "7/8 轮询健康（最长 ${HEALTH_TIMEOUT}s）…"
deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
healthy=0
while [ "$(date +%s)" -lt "$deadline" ]; do
  if remote "curl -fsS --max-time 8 -H 'Host: $HOST_HEADER' http://127.0.0.1:$APP_PORT/api/status 2>/dev/null | grep -q '\"success\":true'"; then
    healthy=1; break
  fi
  printf '.'; sleep 10
done
echo

# ── 8) 结果：成功收尾 / 失败自动回滚 ────────────────────────────────────────────
if [ "$healthy" = "1" ]; then
  # false-success 防护：构建失败时 compose 不重建容器、旧容器续跑 → /api/status 仍 success，
  # 健康轮询会「假通过」。校验 app 新镜像 ID 与部署前保存的 :prev 不同（步骤 3 已 tag :prev
  # = 构建前的 :latest）。相同 = 本次未产出新镜像 = 构建失败被旧容器掩盖 → 判失败并回滚。
  # 首次部署无 :prev（PREV_ID 为空）则跳过此校验。
  NEW_ID="$(remote "docker image inspect -f '{{.Id}}' $APP_IMG:latest 2>/dev/null || true")"
  PREV_ID="$(remote "docker image inspect -f '{{.Id}}' $APP_IMG:prev 2>/dev/null || true")"
  if [ -n "$PREV_ID" ] && [ "$NEW_ID" = "$PREV_ID" ]; then
    log "⚠ 健康虽通过，但 $APP_IMG:latest 镜像 ID 未变（==:prev）→ 构建未产出新镜像（false-success）"
    log "构建尾日志（排错用）："
    remote "tail -n 40 $SERVER_REPO/deploy-build.log 2>/dev/null || true"
    die "构建未产出新镜像（旧容器续跑致健康假通过）。改动未上线——排查上方构建日志后重试。"
  fi
  ok "8/8 健康通过：app /api/status success（${STACK}）｜镜像已更新（≠:prev）"
  # 版本身份端到端自检（C6/#12）：/api/status 报告的 version 必须等于本次盖入的 $APP_VERSION。
  # 不等 = 版本身份未正确烤入（VERSION 盖戳/Dockerfile ldflags 断裂）→ 告警但不阻断（健康已过）。
  REPORTED="$(remote "curl -fsS --max-time 8 -H 'Host: $HOST_HEADER' http://127.0.0.1:$APP_PORT/api/status 2>/dev/null" \
    | sed -n 's/.*"version" *: *"\([^"]*\)".*/\1/p' | head -n1)"
  if [ "$REPORTED" = "$APP_VERSION" ]; then
    ok "  版本身份自检通过：/api/status version=$REPORTED"
  else
    warn "⚠ 版本身份自检失败：/api/status 报告 '$REPORTED' ≠ 期望 '$APP_VERSION'。"
    warn "   → 版本串未正确烤入镜像，请排查 Dockerfile ldflags 与服务器 VERSION 盖戳（步骤 5.5）。"
  fi
  remote "$SERVER_REPO/deploy/ops/healthcheck.sh || true"   # 打印完整巡检（不阻断）
  ok "部署成功 ✅ 版本 ${APP_VERSION}（tag=${TAG}，归档 ${ARCHIVE_DIR}/src-${TS}.tgz）"
  log "  溯源：curl -s .../api/status | jq .data.version ｜ ssh $SSH_HOST 'docker exec \$(docker ps -qf name=${STACK}-${APP_SVC}) cat /deploy-manifest.json'"
  log "  回滚：./rollback.sh（:prev 秒级） ｜ ./rollback.sh --to ${TS}（从归档重建） ｜ ./rollback.sh --list"
  exit 0
fi

log "构建尾日志（排错用）："
remote "tail -n 40 $SERVER_REPO/deploy-build.log 2>/dev/null || true"
if [ "${NO_ROLLBACK:-0}" = "1" ]; then
  die "健康未通过（${HEALTH_TIMEOUT}s 超时）。NO_ROLLBACK=1，未自动回滚——请手动处理。"
fi
log "8/8 健康未通过 → 自动回滚到 :prev 镜像"
remote "ASSUME_YES=1 $SERVER_REPO/deploy/ops/rollback.sh || echo '  回滚失败（可能首次部署无 :prev），请人工介入'"
die "部署失败已回滚。排查构建日志后重试。"
