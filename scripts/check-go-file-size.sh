#!/usr/bin/env bash
# Prevent production Go files from growing back into multi-thousand-line modules.
set -euo pipefail

cd "$(dirname "$0")/.."

default_budget=1500
test_budget=2500
failures=0

while IFS= read -r file; do
  case "$file" in
    *.generated.go|*.gen.go) continue ;;
  esac

  lines=$(wc -l < "$file" | tr -d ' ')
  budget=$default_budget
  case "$file" in
    *_test.go) budget=$test_budget ;;
  esac
  if [ "$lines" -gt "$budget" ]; then
    echo "$file: $lines lines (budget $budget)" >&2
    failures=$((failures + 1))
  fi
done < <(
  git ls-files --cached --others --exclude-standard -- '*.go' \
    | LC_ALL=C sort -u
)

if [ "$failures" -ne 0 ]; then
  echo "Go source file size check failed; split oversized files by stable domain responsibility." >&2
  exit 1
fi

echo "Go source file size check passed (production: $default_budget lines; tests: $test_budget lines; legacy allowances: 0)."
