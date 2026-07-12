#!/usr/bin/env bash
# 上传服务器前的检查调度（Master 在每次 rsync/部署前必跑；任一不过即中止上传）。
# 与云端 .github/workflows/ci.yml 保持同一套检查（audit #10：把测试搬进 CI、堵 SKIP_PREFLIGHT 逃生口）。
set -euo pipefail
cd "$(dirname "$0")/.."
echo "==[preflight] 1/6 gofmt（仅告警）=="
# 只查自有 internal/（cmd/ 已随 fork 基线合并移除）。暂仅告警——internal/ 有历史未格式化文件
# 且与并行 WIP 纠缠（见 ci.yml），清账（gofmt -w internal）后改回阻断。
fmt=$(gofmt -l internal 2>/dev/null || true)
[ -z "$fmt" ] || echo "⚠ 未格式化（暂不阻断）: $fmt"
# 根 main.go //go:embed web/{default,classic}/dist——本地无 dist 则 go build 失败；塞占位(Docker 部署用真 dist 覆盖)
for d in web/default/dist web/classic/dist; do [ -f "$d/index.html" ] || { mkdir -p "$d"; printf '<!doctype html>\n' > "$d/index.html"; }; done
# build 全树编译校验；vet/test 只跑自有 internal/（上游 new-api 代码有 unreachable-code 等 vet 违规、非我们所有）。
echo "==[preflight] 2/6 go build =="; go build ./...
echo "==[preflight] 3/6 go vet(internal) =="; go vet ./internal/...
echo "==[preflight] 4/6 go test internal(-race) =="; go test ./internal/... -race -count=1 >/dev/null
# 前端用 bun（web/bun.lock 为真源，勿用 pnpm）：workspace 根装依赖 → web/default 测试+构建。
echo "==[preflight] 5/6 frontend test =="
( cd web && bun install --frozen-lockfile >/dev/null )
( cd web/default && bun test )
echo "==[preflight] 6/6 frontend build =="
( cd web/default && bun run build >/dev/null )
echo "==[preflight] ✅ 全部通过，可上传 =="
