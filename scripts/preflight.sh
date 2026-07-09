#!/usr/bin/env bash
# 上传服务器前的检查调度（Master 在每次 rsync/部署前必跑；任一不过即中止上传）。
# 与云端 .github/workflows/ci.yml 保持同一套检查（audit #10：把测试搬进 CI、堵 SKIP_PREFLIGHT 逃生口）。
set -euo pipefail
cd "$(dirname "$0")/.."
echo "==[preflight] 1/6 gofmt =="
fmt=$(gofmt -l internal 2>/dev/null || true)   # 只查自有 internal/（cmd/ 已随 fork 基线合并移除）
[ -z "$fmt" ] || { echo "✗ 未格式化: $fmt"; exit 1; }
echo "==[preflight] 2/6 go build =="; go build ./...
echo "==[preflight] 3/6 go vet =="; go vet ./...
echo "==[preflight] 4/6 go test (-race) =="; go test ./... -race -count=1 >/dev/null
# 前端用 bun（web/bun.lock 为真源，勿用 pnpm）：workspace 根装依赖 → web/default 测试+构建。
echo "==[preflight] 5/6 frontend test =="
( cd web && bun install --frozen-lockfile >/dev/null )
( cd web/default && bun test )
echo "==[preflight] 6/6 frontend build =="
( cd web/default && bun run build >/dev/null )
echo "==[preflight] ✅ 全部通过，可上传 =="
