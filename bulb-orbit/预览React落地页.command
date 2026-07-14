#!/usr/bin/env bash
# WeDream React 落地页本地预览。无需后端（把 /api 代理指向空端口，让 app 外壳快速放行）。
cd "$(dirname "$0")/../web/default" || exit 1
echo "════════════════════════════════════════════════════"
echo "  启动中… 首次约等 5-10 秒（编译）"
echo "  启动后浏览器打开： http://localhost:3000/landing-react"
echo "  停止：在本窗口按 Ctrl+C"
echo "════════════════════════════════════════════════════"
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:59999 exec bun run dev
