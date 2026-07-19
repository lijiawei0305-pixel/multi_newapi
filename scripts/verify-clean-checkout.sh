#!/usr/bin/env bash
# Prove that a CI gate starts from exactly the checked-out commit and has no
# tracked, staged, or untracked release inputs supplied by runner state.
set -euo pipefail
export LC_ALL=C LANG=C

root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "not inside a Git checkout" >&2
  exit 1
}
cd "$root"

git diff --quiet -- || { echo "tracked files differ from HEAD" >&2; exit 1; }
git diff --cached --quiet -- || { echo "index differs from HEAD" >&2; exit 1; }
untracked="$(git ls-files --others --exclude-standard)"
[ -z "$untracked" ] || {
  printf 'untracked release inputs exist:\n%s\n' "$untracked" >&2
  exit 1
}

head_sha="$(git rev-parse HEAD)"
if [ -n "${EXPECTED_CHECKOUT_SHA:-}" ] && [ "$head_sha" != "$EXPECTED_CHECKOUT_SHA" ]; then
  printf 'checkout HEAD %s does not equal expected %s\n' "$head_sha" "$EXPECTED_CHECKOUT_SHA" >&2
  exit 1
fi
printf 'clean-checkout sha=%s\n' "$head_sha"
