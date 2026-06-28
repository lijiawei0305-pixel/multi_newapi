#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# deploy.sh — 一键部署测试栈 newapi_test（**从 Mac 仓库根运行**，SSH 到服务器执行）。
#
#   固化全流程：预检 → 打 git tag → 存 :prev 镜像 → 备份 → tar 上传 → 后台重建
#               → 轮询健康 → 失败自动回滚。对齐 RETRO 部署惯例：
#               COPYFILE_DISABLE=1 tar-over-ssh、--env-file、后台 up -d --build。
#
#   运行位置：Mac 本仓库（W4：Mac 只编辑/调试，构建/部署在服务器；本脚本负责编排）。
#   红线：只动隔离栈 newapi_test；绝不触碰现网 newapi_YFNf（api.wedreamhub.com）。
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

# ── 参数（可环境变量覆盖）────────────────────────────────────────────────────────
SSH_HOST="${SSH_HOST:-newapi628}"
LOCAL_REPO="${LOCAL_REPO:-$(cd "$(dirname "$0")/../.." && pwd)}"   # Mac 仓库根
SERVER_REPO="${SERVER_REPO:-/root/newapi-test}"
STACK="${STACK:-newapi_test}"
PROD_STACK="${PROD_STACK:-newapi_YFNf}"
COMPOSE_FILE="${COMPOSE_FILE:-$SERVER_REPO/deploy/docker-compose.test.yml}"
ENV_FILE="${ENV_FILE:-$SERVER_REPO/.env}"
APP_SVC="${APP_SVC:-app}"; AUTH_SVC="${AUTH_SVC:-auth-service}"
APP_PORT="${APP_PORT:-3100}"
HOST_HEADER="${HOST_HEADER:-tokendream.wedreamhub.com}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-600}"   # 轮询健康最长等待秒数（含构建）
APP_IMG="${STACK}-${APP_SVC}"; AUTH_IMG="${STACK}-${AUTH_SVC}"

log()  { printf '\033[1;34m[deploy]\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m[ ok ]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[fail]\033[0m %s\n' "$*" >&2; exit 1; }
remote() { ssh "$SSH_HOST" "$@"; }
# 服务器侧 compose 串（与 lib.sh dc() 等价）。
DC="docker compose -p $STACK --env-file $ENV_FILE -f $COMPOSE_FILE"

# 红线校验。
[ "$STACK" != "$PROD_STACK" ] || die "拒绝部署现网栈 $PROD_STACK。"
case "$COMPOSE_FILE$ENV_FILE$SERVER_REPO" in *"$PROD_STACK"*) die "路径指向现网 $PROD_STACK。";; esac

TS="$(date +%Y%m%d-%H%M%S)"
TAG="deploy-$TS"

# ── 1) 本地预检（复用 scripts/preflight.sh：gofmt/build/vet/test/前端构建）──────────
if [ "${SKIP_PREFLIGHT:-0}" != "1" ]; then
  log "1/8 本地预检 scripts/preflight.sh …"
  ( cd "$LOCAL_REPO" && bash scripts/preflight.sh ) || die "预检未过，已中止上传。"
else
  log "1/8 跳过预检（SKIP_PREFLIGHT=1）"
fi

# ── 2) 打 git tag（回滚标记；本地轻量 tag，不 push）────────────────────────────────
log "2/8 打 git tag $TAG（回滚标记）"
( cd "$LOCAL_REPO" && git tag -f "$TAG" >/dev/null 2>&1 ) \
  && ok "已打 tag $TAG" || log "（git tag 跳过：非 git 环境或无变更）"

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
log "5/8 上传源码 → $SSH_HOST:$SERVER_REPO"
remote "mkdir -p $SERVER_REPO"
COPYFILE_DISABLE=1 tar czf - \
  --exclude='./.git' \
  --exclude='node_modules' \
  --exclude='./web/default/dist' \
  --exclude='./web/classic/dist' \
  --exclude='.DS_Store' \
  --exclude='._*' \
  -C "$LOCAL_REPO" . \
  | remote "tar xzf - -C $SERVER_REPO"
ok "上传完成（服务器 Dockerfile 内构建前端 dist + go embed）"

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
  ok "8/8 健康通过：app /api/status success（$STACK）"
  remote "$SERVER_REPO/deploy/ops/healthcheck.sh || true"   # 打印完整巡检（不阻断）
  ok "部署成功 ✅ tag=$TAG。回滚命令：ssh $SSH_HOST '$SERVER_REPO/deploy/ops/rollback.sh'"
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
