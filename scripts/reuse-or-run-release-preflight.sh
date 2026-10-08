#!/usr/bin/env bash
# Reuse one successful tag-push release-preflight witness, or run full preflight.
# workflow_dispatch always runs full preflight. This script does not publish,
# sign, or grant the witness any write permission.
#
# scripts/verify-preflight-witness.sh exits:
#   0   accept the witness and skip the duplicate preflight
#   10  witness unavailable (missing, timeout, or unreadable); run full preflight
#   2   caller configuration error; log it and run full preflight
#   20  source rejected; fail immediately
#   *   fail closed
#
# The verify job timeout must cover this whole fallback, not only the wait:
#   witness wait 2700 + full preflight 3600 + toolchain 900 + prefix/slack 1200
#   = 8400 seconds = 140 minutes
set -euo pipefail
export LC_ALL=C LANG=C

readonly WITNESS_WAIT_SECONDS=2700
readonly PREFLIGHT_BUDGET_SECONDS=3600
readonly TOOLCHAIN_BUDGET_SECONDS=900
readonly PREFIX_SLACK_SECONDS=1200
readonly WITNESS_POLL_SECONDS=15

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
printf 'preflight-reuse: verify budget seconds=%s\n' \
  "$((WITNESS_WAIT_SECONDS + PREFLIGHT_BUDGET_SECONDS + TOOLCHAIN_BUDGET_SECONDS + PREFIX_SLACK_SECONDS))" >&2

run_full_preflight() {
  if [ "${PREFLIGHT_SCOPE:-}" != "all" ] || [ "${RUN_ORBIT_E2E:-}" != "1" ]; then
    echo "::error::full preflight requires PREFLIGHT_SCOPE=all and RUN_ORBIT_E2E=1" >&2
    exit 1
  fi
  export PREFLIGHT_SCOPE=all
  export RUN_ORBIT_E2E=1
  bash "$root/scripts/preflight.sh"
}

# GH_TOKEN and GITHUB_TOKEN must name the same Actions read token. A mismatch
# is a caller bug: record it and run preflight instead of skipping the gate.
bind_actions_token() {
  if [ -n "${GH_TOKEN:-}" ] && [ -n "${GITHUB_TOKEN:-}" ] && [ "$GH_TOKEN" != "$GITHUB_TOKEN" ]; then
    echo "::error::GH_TOKEN and GITHUB_TOKEN disagree; running full preflight" >&2
    run_full_preflight
    exit 0
  fi
  if [ -z "${GH_TOKEN:-}" ] && [ -n "${GITHUB_TOKEN:-}" ]; then
    export GH_TOKEN="$GITHUB_TOKEN"
  fi
  if [ -n "${GH_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ]; then
    export GITHUB_TOKEN="$GH_TOKEN"
  fi
}

event="${GITHUB_EVENT_NAME:-}"
ref="${GITHUB_REF:-}"

if [ "$event" != "push" ] || [[ "$ref" != refs/tags/* ]]; then
  echo "preflight-reuse: ${event:-unknown} ${ref:-noref} runs full preflight" >&2
  run_full_preflight
  exit 0
fi

tag="${ref#refs/tags/}"
if ! sha="$(git rev-parse HEAD)"; then
  echo "::error::cannot resolve checkout SHA; running full preflight" >&2
  run_full_preflight
  exit 0
fi
repository="${GITHUB_REPOSITORY:-}"

bind_actions_token

set +e
PREFLIGHT_WITNESS_TIMEOUT_SECONDS="$WITNESS_WAIT_SECONDS" \
PREFLIGHT_WITNESS_POLL_SECONDS="$WITNESS_POLL_SECONDS" \
  bash "$root/scripts/verify-preflight-witness.sh" \
    --tag "$tag" \
    --sha "$sha" \
    --repository "$repository" \
    --root "$root"
code=$?
set -e

case "$code" in
  0)
    echo "preflight-reuse: accepted witness; skipping duplicate preflight" >&2
    exit 0
    ;;
  10)
    echo "preflight-reuse: witness unavailable; running full preflight" >&2
    run_full_preflight
    ;;
  2)
    echo "::error::preflight witness invocation was misconfigured; running full preflight" >&2
    run_full_preflight
    ;;
  20)
    echo "::error::preflight witness rejected; refusing to skip preflight" >&2
    exit 1
    ;;
  *)
    echo "::error::preflight witness returned unexpected exit ${code}; failing closed" >&2
    exit 1
    ;;
esac
