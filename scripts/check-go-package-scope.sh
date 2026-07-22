#!/usr/bin/env bash
# Keep backend build/vet/test scoped to first-party, non-ignored Go sources.
set -euo pipefail

cd "$(dirname "$0")/.."

packages=()
while IFS= read -r package; do
  [ -n "$package" ] && packages+=("$package")
done < <(bash scripts/list-first-party-go-packages.sh)

if [ "${#packages[@]}" -eq 0 ]; then
  echo "first-party Go package list is empty" >&2
  exit 1
fi

if printf '%s\n' "${packages[@]}" | rg -q '(^|/)node_modules(/|$)'; then
  echo "first-party Go package list contains node_modules" >&2
  exit 1
fi

listed_count=$(printf '%s\n' "${packages[@]}" | LC_ALL=C sort -u | wc -l | tr -d ' ')
if [ "$listed_count" -ne "${#packages[@]}" ]; then
  echo "first-party Go package list contains duplicates" >&2
  exit 1
fi

# Exact directories also prove that every listed location is a buildable Go package.
go list "${packages[@]}" >/dev/null

echo "Go package scope check passed (${#packages[@]} first-party packages; ignored sources excluded)."
