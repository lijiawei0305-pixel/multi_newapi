#!/usr/bin/env bash
# 上传服务器前的检查调度（Master 在每次 rsync/部署前必跑；任一不过即中止上传）。
set -euo pipefail
cd "$(dirname "$0")/.."
echo "==[preflight] 1/5 gofmt =="
fmt=$(gofmt -l cmd internal 2>/dev/null || true)
[ -z "$fmt" ] || { echo "✗ 未格式化: $fmt"; exit 1; }
echo "==[preflight] 2/5 go build =="; go build ./...
echo "==[preflight] 3/5 go vet =="; go vet ./...
echo "==[preflight] 4/5 go test (-race) =="; go test ./... -race -count=1 >/dev/null
echo "==[preflight] 5/5 frontend build =="
( cd web && { [ -d node_modules ] || pnpm install; } && pnpm build >/dev/null )
echo "==[preflight] ✅ 全部通过，可上传 =="
