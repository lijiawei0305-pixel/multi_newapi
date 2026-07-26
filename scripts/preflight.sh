#!/usr/bin/env bash
# 本地部署、CI 与 Release 共用的质量门。
# PREFLIGHT_SCOPE 可取 all/backend/default/classic/orbit/electron；默认 all。
set -uo pipefail

cd "$(dirname "$0")/.."

scope=${PREFLIGHT_SCOPE:-all}
case "$scope" in
  all|backend|default|classic|orbit|electron) ;;
  *) echo "未知 PREFLIGHT_SCOPE: $scope" >&2; exit 2 ;;
esac

FAILED_GATES=()
CURRENT_STEP=""

step() {
  CURRENT_STEP=$1
  printf '\n==[preflight:%s] %s ==\n' "$scope" "$1"
}

# 记录失败门禁名(依赖 bash 动态作用域写调用方的 local failed)。
# 2026-07-25 CI 事故复盘:结尾只报「存在未通过门禁」,定位具体失败项要翻全量日志。
gate_failed() {
  failed=1
  FAILED_GATES+=("[$scope] $CURRENT_STEP")
}

prepare_embed_dirs() {
  for d in web/default/dist web/classic/dist; do
    if [ ! -f "$d/index.html" ]; then
      mkdir -p "$d"
      printf '<!doctype html>\n' > "$d/index.html"
    fi
  done
}

install_web() {
  step "Web 冻结依赖安装（isolated linker）"
  # Classic 的 Semi 依赖必须使用 date-fns@2/date-fns-tz@1；isolated linker 防止其 peer
  # 被 Default 的 date-fns@4 提升污染，同时仍共用唯一的 web/bun.lock。
  (cd web && bun install --frozen-lockfile --linker=isolated)
}

run_backend() {
  local failed=0

  step "Repository secret scan"
  go run github.com/zricethezav/gitleaks/v8@v8.30.1 dir \
    --no-banner --no-color --redact . || gate_failed

  step "Gitee release sync unit tests"
  PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/tests -p 'test_*.py' || gate_failed

  step "deploy 脚本 helper lint"
  bash scripts/lint-deploy-helpers.sh deploy/ops || gate_failed
  bash scripts/check-docker-context-secrets.sh || gate_failed

  step "Go 全仓格式检查"
  local fmt
  # The audit/fix workflow creates new regression tests before they are staged.
  # Checking only git-tracked files lets malformed untracked Go sources bypass
  # both local preflight and a release assembled from the working tree.
  fmt=$({ git ls-files -z '*.go'; git ls-files -z --others --exclude-standard -- '*.go'; } | xargs -0 gofmt -l)
  if [ -n "$fmt" ]; then
    printf 'Go 文件尚未格式化：\n%s\n' "$fmt" >&2
    gate_failed
  fi

  # Package-loading tools evaluate go:embed directives even before the final
  # build. A clean backend-only checkout has no frontend dist artifacts yet,
  # so create the same inert placeholders before any package scope analysis.
  step "准备 go:embed 前端目录"
  prepare_embed_dirs

  step "Go 源码体积门禁"
  bash scripts/check-go-file-size.sh || gate_failed

  step "Go first-party package 范围门禁"
  bash scripts/check-go-package-scope.sh || gate_failed

  local go_packages=()
  while IFS= read -r package; do
    [ -n "$package" ] && go_packages+=("$package")
  done < <(bash scripts/list-first-party-go-packages.sh)
  if [ "${#go_packages[@]}" -eq 0 ]; then
    echo "Go first-party package 列表为空" >&2
    gate_failed
    return 1
  fi

  step "Go first-party vulnerability scan"
  go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 "${go_packages[@]}" || gate_failed

  step "Go first-party correctness static analysis"
  go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 \
    -checks='SA*' "${go_packages[@]}" || gate_failed

  step "Go first-party 编译"
  go build "${go_packages[@]}" || gate_failed

  step "Go first-party vet"
  go vet "${go_packages[@]}" || gate_failed

  step "Go first-party 测试（race）"
  go test "${go_packages[@]}" -race -count=1 || gate_failed

  return "$failed"
}

run_default() {
  local failed=0
  install_web || { gate_failed; return 1; }

  step "Web dependency audit"
  node scripts/audit-gate.mjs --tool bun --dir web --scope web || gate_failed

  step "Default 测试"
  (cd web/default && bun run test) || gate_failed

  step "Default 类型检查"
  (cd web/default && bun run typecheck) || gate_failed

  step "Default lint"
  (cd web/default && bun run lint) || gate_failed

  step "Default 格式检查"
  (cd web/default && bun run format:check) || gate_failed

  step "Default i18n source/catalog parity"
  (cd web/default && bun run i18n:sync) || gate_failed

  step "Default source file size budget"
  (cd web/default && bun run source-size:check) || gate_failed

  step "Default dependency graph"
  (cd web/default && bun run knip) || gate_failed

  step "Default 生产构建"
  (cd web/default && bun run build) || gate_failed

  step "Default initial bundle budget"
  (cd web/default && bun run bundle:check) || gate_failed

  return "$failed"
}

run_classic() {
  local failed=0
  install_web || { gate_failed; return 1; }

  step "Classic 依赖隔离"
  local classic_date_fns classic_date_fns_tz default_date_fns
  classic_date_fns=$(cd web/classic && node -p "require('./node_modules/date-fns/package.json').version") || { gate_failed; return 1; }
  classic_date_fns_tz=$(cd web/classic && node -p "require('./node_modules/date-fns-tz/package.json').version") || { gate_failed; return 1; }
  default_date_fns=$(cd web/default && node -p "require('./node_modules/date-fns/package.json').version") || { gate_failed; return 1; }
  [ "$classic_date_fns" = "2.30.0" ] || { echo "Classic date-fns=$classic_date_fns，期望 2.30.0" >&2; gate_failed; }
  [ "$classic_date_fns_tz" = "1.3.8" ] || { echo "Classic date-fns-tz=$classic_date_fns_tz，期望 1.3.8" >&2; gate_failed; }
  case "$default_date_fns" in
    4.*) ;;
    *) echo "Default date-fns=$default_date_fns，期望保持 4.x" >&2; gate_failed ;;
  esac

  step "Classic 格式检查"
  (cd web/classic && bun run lint) || gate_failed

  step "Classic ESLint"
  (cd web/classic && bun run eslint) || gate_failed

  step "Classic source file size budget"
  (cd web/classic && node scripts/check-source-file-size.mjs) || gate_failed

  step "Classic 安全回归测试"
  (cd web/classic && bun run test) || gate_failed

  step "Classic 生产构建"
  (cd web/classic && bun run build) || gate_failed

  step "Classic initial bundle budget"
  (cd web/classic && bun run bundle:check) || gate_failed

  return "$failed"
}

run_orbit() {
  local failed=0

  step "Orbit 冻结依赖"
  (cd bulb-orbit/v2 && npm ci) || { gate_failed; return 1; }

  step "Orbit dependency audit"
  node scripts/audit-gate.mjs --tool npm --dir bulb-orbit/v2 --scope orbit --level moderate || gate_failed

  step "Orbit 类型检查"
  (cd bulb-orbit/v2 && npm run typecheck) || gate_failed

  step "Orbit lint"
  (cd bulb-orbit/v2 && npm run lint) || gate_failed

  step "Orbit 单元测试"
  (cd bulb-orbit/v2 && npm test) || gate_failed

  step "Orbit 生产构建"
  (cd bulb-orbit/v2 && npm run build) || gate_failed

  if [ "${RUN_ORBIT_E2E:-0}" = "1" ]; then
    step "Orbit Chromium E2E"
    (cd bulb-orbit/v2 && npx playwright install --with-deps chromium) || gate_failed
    (cd bulb-orbit/v2 && npm run e2e) || gate_failed
  fi

  return "$failed"
}

run_electron() {
  local failed=0

  step "准备 Electron 内嵌后端"
  prepare_embed_dirs
  if [ "$(uname -s)" != "Linux" ]; then
    if [ "$scope" = "electron" ]; then
      echo "Electron package smoke 目前只支持 Linux CI runner" >&2
      return 1
    fi
    echo "SKIP: Electron package smoke 由 Linux CI/Release 强制执行（当前主机非 Linux）"
    return 0
  fi
  local smoke_version actual_version
  smoke_version="ci-smoke-${GITHUB_SHA:-local}"
  if ! go build -ldflags "-s -w -X github.com/QuantumNous/new-api/common.Version=$smoke_version" -o new-api; then
    gate_failed
    return 1
  fi
  actual_version=$(./new-api --version) || { gate_failed; return 1; }
  if [ "$actual_version" != "$smoke_version" ]; then
    echo "Electron 内嵌后端版本为 '$actual_version'，期望 '$smoke_version'" >&2
    gate_failed
  fi

  step "Electron clean package smoke"
  (cd electron && npm ci) || { gate_failed; return 1; }
  node scripts/audit-gate.mjs --tool npm --dir electron --scope electron --level moderate || gate_failed
  (cd electron && npm run prepare:runtime) || { gate_failed; return 1; }
  (cd electron && npx electron-builder --dir --linux) || gate_failed
  if [ "$failed" -eq 0 ]; then
    local packaged_backend="electron/dist/linux-unpacked/resources/bin/new-api"
    if [ ! -x "$packaged_backend" ]; then
      echo "Electron package 未包含可执行后端: $packaged_backend" >&2
      gate_failed
    else
      actual_version=$("$packaged_backend" --version) || gate_failed
      [ "$actual_version" = "$smoke_version" ] || {
        echo "Electron package 内后端版本为 '$actual_version'，期望 '$smoke_version'" >&2
        gate_failed
      }
    fi
  fi

  return "$failed"
}

overall=0
for gate in backend default classic orbit electron; do
  if [ "$scope" != "all" ] && [ "$scope" != "$gate" ]; then
    continue
  fi
  "run_$gate" || overall=1
done

if [ "$overall" -ne 0 ]; then
  step "存在未通过门禁"
  if [ "${#FAILED_GATES[@]}" -gt 0 ]; then
    printf '未通过门禁列表：\n' >&2
    for g in "${FAILED_GATES[@]}"; do
      printf '  ✗ %s\n' "$g" >&2
    done
  fi
  exit "$overall"
fi

step "全部已启用门禁通过"
