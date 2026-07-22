#!/usr/bin/env bash
# List first-party Go package directories from versioned and non-ignored sources.
set -euo pipefail

cd "$(dirname "$0")/.."

{
  git ls-files --cached --others --exclude-standard -- '*.go'
} | while IFS= read -r file; do
  case "$file" in
    node_modules/*|*/node_modules/*) continue ;;
  esac
  dirname "$file"
done | LC_ALL=C sort -u | while IFS= read -r dir; do
  if [ "$dir" = "." ]; then
    echo "."
  else
    echo "./$dir"
  fi
done
