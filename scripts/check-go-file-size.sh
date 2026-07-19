#!/usr/bin/env bash
# Prevent production Go files from growing back into multi-thousand-line modules.
set -euo pipefail

cd "$(dirname "$0")/.."

default_budget=1500
test_budget=2500
failures=0

legacy_budget() {
  case "$1" in
    relay/common/override.go) echo 2105 ;;
    relay/channel/gemini/relay-gemini.go) echo 1808 ;;
    internal/report/reportrepo/reportrepo.go) echo 1714 ;;
    *) echo 0 ;;
  esac
}

while IFS= read -r file; do
  case "$file" in
    *.generated.go|*.gen.go) continue ;;
  esac

  lines=$(wc -l < "$file" | tr -d ' ')
  budget=$default_budget
  case "$file" in
    *_test.go) budget=$test_budget ;;
  esac
  legacy=$(legacy_budget "$file")
  if [ "$legacy" -gt 0 ]; then
    if [ "$lines" -le "$default_budget" ]; then
      echo "$file: $lines lines; remove its stale legacy allowance" >&2
      failures=$((failures + 1))
      continue
    fi
    budget=$legacy
  fi
  if [ "$lines" -gt "$budget" ]; then
    echo "$file: $lines lines (budget $budget)" >&2
    failures=$((failures + 1))
  fi
done < <(rg --files -g '*.go')

for file in \
  relay/common/override.go \
  relay/channel/gemini/relay-gemini.go \
  internal/report/reportrepo/reportrepo.go; do
  if [ ! -f "$file" ]; then
    echo "$file: legacy allowance points to a missing file" >&2
    failures=$((failures + 1))
  fi
done

if [ "$failures" -ne 0 ]; then
  echo "Go source file size check failed; split oversized files by stable domain responsibility." >&2
  exit 1
fi

echo "Go source file size check passed (production: $default_budget lines; tests: $test_budget lines; legacy allowances: 3)."
