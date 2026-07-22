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

step() {
  printf '\n==[preflight:%s] %s ==\n' "$scope" "$1"
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
    --no-banner --no-color --redact . || failed=1

  step "Gitee release sync unit tests"
  PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/tests -p 'test_*.py' || failed=1

  step "deploy 脚本 helper lint"
  bash scripts/lint-deploy-helpers.sh deploy/ops || failed=1
  bash scripts/check-docker-context-secrets.sh || failed=1

  step "Go 全仓格式检查"
  local fmt
  # The audit/fix workflow creates new regression tests before they are staged.
  # Checking only git-tracked files lets malformed untracked Go sources bypass
  # both local preflight and a release assembled from the working tree.
  fmt=$({ git ls-files -z '*.go'; git ls-files -z --others --exclude-standard -- '*.go'; } | xargs -0 gofmt -l)
  if [ -n "$fmt" ]; then
    printf 'Go 文件尚未格式化：\n%s\n' "$fmt" >&2
    failed=1
  fi

  step "Go 源码体积门禁"
  bash scripts/check-go-file-size.sh || failed=1

  step "Go first-party package 范围门禁"
  bash scripts/check-go-package-scope.sh || failed=1

  local go_packages=()
  while IFS= read -r package; do
    [ -n "$package" ] && go_packages+=("$package")
  done < <(bash scripts/list-first-party-go-packages.sh)
  if [ "${#go_packages[@]}" -eq 0 ]; then
    echo "Go first-party package 列表为空" >&2
    return 1
  fi

  step "Go first-party vulnerability scan"
  go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 "${go_packages[@]}" || failed=1

  step "Go first-party correctness static analysis"
  go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 \
    -checks='SA*' "${go_packages[@]}" || failed=1

  step "准备 go:embed 前端目录"
  prepare_embed_dirs

  step "Go first-party 编译"
  go build "${go_packages[@]}" || failed=1

  step "Go first-party vet"
  go vet "${go_packages[@]}" || failed=1

  step "Go first-party 测试（race）"
  go test "${go_packages[@]}" -race -count=1 || failed=1

  return "$failed"
}

run_default() {
  local failed=0
  install_web || return 1

  step "Web dependency audit"
  (cd web && bun audit) || failed=1

  step "Default 测试"
  (cd web/default && bun run test) || failed=1

  step "Default 类型检查"
  (cd web/default && bun run typecheck) || failed=1

  step "Default lint"
  (cd web/default && bun run lint) || failed=1

  step "Default 格式检查"
  (cd web/default && bun run format:check) || failed=1

  step "Default i18n source/catalog parity"
  (cd web/default && bun run i18n:sync) || failed=1

  step "Default source file size budget"
  (cd web/default && bun run source-size:check) || failed=1

  step "Default dependency graph"
  (cd web/default && bun run knip) || failed=1

  step "Default 生产构建"
  (cd web/default && bun run build) || failed=1

  step "Default initial bundle budget"
  (cd web/default && bun run bundle:check) || failed=1

  return "$failed"
}

run_classic() {
  local failed=0
  install_web || return 1

  step "Classic 依赖隔离"
  local classic_date_fns classic_date_fns_tz default_date_fns
  classic_date_fns=$(cd web/classic && node -p "require('./node_modules/date-fns/package.json').version") || return 1
  classic_date_fns_tz=$(cd web/classic && node -p "require('./node_modules/date-fns-tz/package.json').version") || return 1
  default_date_fns=$(cd web/default && node -p "require('./node_modules/date-fns/package.json').version") || return 1
  [ "$classic_date_fns" = "2.30.0" ] || { echo "Classic date-fns=$classic_date_fns，期望 2.30.0" >&2; failed=1; }
  [ "$classic_date_fns_tz" = "1.3.8" ] || { echo "Classic date-fns-tz=$classic_date_fns_tz，期望 1.3.8" >&2; failed=1; }
  case "$default_date_fns" in
    4.*) ;;
    *) echo "Default date-fns=$default_date_fns，期望保持 4.x" >&2; failed=1 ;;
  esac

  step "Classic 格式检查"
  (cd web/classic && bun run lint) || failed=1

  step "Classic ESLint"
  (cd web/classic && bun run eslint) || failed=1

  step "Classic source file size budget"
  (cd web/classic && node scripts/check-source-file-size.mjs) || failed=1

  step "Classic 安全回归测试"
  (cd web/classic && bun run test) || failed=1

  step "Classic 生产构建"
  (cd web/classic && bun run build) || failed=1

  step "Classic initial bundle budget"
  (cd web/classic && bun run bundle:check) || failed=1

  return "$failed"
}

run_orbit() {
  local failed=0

  step "Orbit 冻结依赖"
  (cd bulb-orbit/v2 && npm ci) || return 1

  step "Orbit dependency audit"
  (cd bulb-orbit/v2 && npm audit --audit-level=moderate) || failed=1

  step "Orbit 类型检查"
  (cd bulb-orbit/v2 && npm run typecheck) || failed=1

  step "Orbit lint"
  (cd bulb-orbit/v2 && npm run lint) || failed=1

  step "Orbit 单元测试"
  (cd bulb-orbit/v2 && npm test) || failed=1

  step "Orbit 生产构建"
  (cd bulb-orbit/v2 && npm run build) || failed=1

  if [ "${RUN_ORBIT_E2E:-0}" = "1" ]; then
    step "Orbit Chromium E2E"
    (cd bulb-orbit/v2 && npx playwright install --with-deps chromium) || failed=1
    (cd bulb-orbit/v2 && npm run e2e) || failed=1
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
    return 1
  fi
  actual_version=$(./new-api --version) || return 1
  if [ "$actual_version" != "$smoke_version" ]; then
    echo "Electron 内嵌后端版本为 '$actual_version'，期望 '$smoke_version'" >&2
    failed=1
  fi

  step "Electron clean package smoke"
  (cd electron && npm ci) || return 1
  (cd electron && npm audit --audit-level=moderate) || failed=1
  (cd electron && npm run prepare:runtime) || return 1
  (cd electron && npx electron-builder --dir --linux) || failed=1
  if [ "$failed" -eq 0 ]; then
    local packaged_backend="electron/dist/linux-unpacked/resources/bin/new-api"
    if [ ! -x "$packaged_backend" ]; then
      echo "Electron package 未包含可执行后端: $packaged_backend" >&2
      failed=1
    else
      actual_version=$("$packaged_backend" --version) || failed=1
      [ "$actual_version" = "$smoke_version" ] || {
        echo "Electron package 内后端版本为 '$actual_version'，期望 '$smoke_version'" >&2
        failed=1
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
  exit "$overall"
fi

step "全部已启用门禁通过"
