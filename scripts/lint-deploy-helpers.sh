#!/usr/bin/env bash
# deploy 脚本「调用了未定义 helper 函数」lint（audit#8 出厂根因的针对性防线）：
#   bash -n 抓不到"调用未定义函数"（纯运行期缺陷）、本机未装 shellcheck——deploy.sh 曾调用只存在于
#   服务器侧 lib.sh 的 warn，set -euo pipefail 下运行期 exit 127，且恰好顶替了缺失的阻断闸门。
# 规则：词表 = 目录内所有 *.sh 定义过的函数名并集；对每个文件，凡以词表函数为命令首词的调用，
#   必须由本文件定义、或由其 source 的同目录文件提供，否则判失败。
# 用法：lint-deploy-helpers.sh [目录]（默认 deploy/ops；可传测试夹具目录自测）。
set -euo pipefail
export LC_ALL=C LANG=C
DIR="${1:-deploy/ops}"
[ -d "$DIR" ] || { echo "✗ 目录不存在: $DIR" >&2; exit 1; }

defs_of() { { grep -hoE '^[A-Za-z_][A-Za-z0-9_]*\(\)' "$@" 2>/dev/null || true; } | tr -d '()' | sort -u; }

vocab="$(defs_of "$DIR"/*.sh)"
fail=0
for f in "$DIR"/*.sh; do
  visible="$(defs_of "$f")"
  # 本文件 source/. 的同目录脚本，其定义也可见（容忍 "$(dirname "$0")/lib.sh" 之类的路径拼写）。
  for s in $(sed -nE 's/^[[:space:]]*(source|\.)[[:space:]]+(.*)/\2/p' "$f" | grep -oE '[A-Za-z0-9_.-]+\.sh' || true); do
    sf="$DIR/$(basename "$s")"
    [ -f "$sf" ] && visible="$visible
$(defs_of "$sf")"
  done
  for fn in $vocab; do
    # 注意写成 if 而非 `grep -qx && continue`——后者在未命中时整句返回 1，会被本脚本自己的 set -e 杀死。
    if printf '%s\n' "$visible" | grep -qx "$fn"; then continue; fi
    # 只匹配"命令首词"位置的调用（行首/;/&&/||/管道/$( 之后），排除注释行与定义行本身。
    hits="$(grep -nE "(^|[;&|]|\\\$\\()[[:space:]]*${fn}([[:space:]]|\"|'|$)" "$f" \
      | grep -vE '^[0-9]+:[[:space:]]*#' | grep -vE "${fn}\(\)" || true)"
    if [ -n "$hits" ]; then
      echo "✗ $f 调用了未定义的 helper「$fn」（定义仅在同目录其它脚本且本文件未 source）："
      printf '%s\n' "$hits" | sed 's/^/    /'
      fail=1
    fi
  done
done
if [ "$fail" != "0" ]; then exit 1; fi
echo "✓ deploy 脚本 helper lint 通过（$DIR）"
