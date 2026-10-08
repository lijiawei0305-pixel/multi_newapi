#!/usr/bin/env bash
# Write the source-bound witness for one release-preflight workflow run.
# The file records the checkout that just passed preflight. It is not itself
# proof: scripts/verify-preflight-witness.sh must match it to the GitHub run.
set -euo pipefail
export LC_ALL=C LANG=C

readonly WORKFLOW_PATH=".github/workflows/release-preflight.yml"
readonly SCHEMA="new-api.release-preflight-witness.v1"

die() {
  printf '%s\n' "$1" >&2
  exit "${2:-1}"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1" 2
}

require_cmd jq
require_cmd git
require_cmd sha256sum

root="${PREFLIGHT_WITNESS_ROOT:-}"
if [ -z "$root" ]; then
  root="$(git rev-parse --show-toplevel 2>/dev/null)" || die "not inside a git checkout" 2
fi
cd "$root"

out="${WITNESS_OUT:-}"
[ -n "$out" ] || die "WITNESS_OUT is required" 2
case "$out" in
  *$'\n'*) die "WITNESS_OUT is not a single path" 2 ;;
esac

event="${GITHUB_EVENT_NAME:-}"
ref="${GITHUB_REF:-}"
sha="${GITHUB_SHA:-}"
repo="${GITHUB_REPOSITORY:-}"
head_repo="${WITNESS_EVENT_REPOSITORY:-}"
run_id="${GITHUB_RUN_ID:-}"
run_attempt="${GITHUB_RUN_ATTEMPT:-}"
workflow_ref="${GITHUB_WORKFLOW_REF:-}"
scope="${PREFLIGHT_SCOPE:-}"
orbit="${RUN_ORBIT_E2E:-}"

[ "$orbit" = "1" ] || die "RUN_ORBIT_E2E must be 1" 1
[ "$scope" = "all" ] || die "PREFLIGHT_SCOPE must be all" 1
case "$event" in
  push|workflow_dispatch) ;;
  *) die "refusing to witness event: $event" 1 ;;
esac
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || die "GITHUB_SHA must be a lowercase commit" 2
[[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "GITHUB_REPOSITORY is invalid" 2
[ "$head_repo" = "$repo" ] || die "refusing a cross-repository witness event" 1
[[ "$run_id" =~ ^[0-9]+$ ]] || die "GITHUB_RUN_ID must be numeric" 2
[[ "$run_attempt" =~ ^[0-9]+$ ]] || die "GITHUB_RUN_ATTEMPT must be numeric" 2

case "$ref" in
  refs/tags/*)
    tag="${ref#refs/tags/}"
    [[ "$tag" =~ ^[0-9A-Za-z][0-9A-Za-z._-]{0,79}$ ]] || die "tag ref is not witness-safe" 1
    case "$tag" in
      nightly*) die "nightly tags are outside this witness workflow" 1 ;;
    esac
    ;;
  refs/heads/*)
    [ "$event" = "workflow_dispatch" ] || die "push witnesses require a tag ref" 1
    branch="${ref#refs/heads/}"
    [[ "$branch" =~ ^[0-9A-Za-z][0-9A-Za-z._/-]{0,199}$ ]] || die "branch ref is not witness-safe" 1
    ;;
  *) die "refusing ref: $ref" 1 ;;
esac

expected_workflow_ref="${repo}/${WORKFLOW_PATH}@${ref}"
[ "$workflow_ref" = "$expected_workflow_ref" ] || die "workflow ref is not bound to this event" 1

preflight_file="scripts/preflight.sh"
[ -f "$preflight_file" ] && [ ! -L "$preflight_file" ] || die "preflight script is missing" 2
[ -f "$WORKFLOW_PATH" ] && [ ! -L "$WORKFLOW_PATH" ] || die "witness workflow file is missing" 2

if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  [ "$(git rev-parse HEAD)" = "$sha" ] || die "checkout HEAD does not match GITHUB_SHA" 1
  if ! git diff --quiet --; then
    die "witness refuses a dirty worktree" 1
  fi
  if ! git diff --cached --quiet --; then
    die "witness refuses a dirty index" 1
  fi
  untracked="$(git ls-files --others --exclude-standard)"
  if [ -n "$untracked" ]; then
    printf 'witness refuses untracked files:\n%s\n' "$untracked" >&2
    exit 1
  fi
  for file in "$preflight_file" "$WORKFLOW_PATH"; do
    worktree_blob="$(git hash-object -- "$file")"
    head_blob="$(git rev-parse "HEAD:${file}")"
    [ "$worktree_blob" = "$head_blob" ] || die "witness file is not the committed blob: $file" 1
  done
fi

preflight_hash="$(sha256sum -- "$preflight_file" | awk '{print $1}')"
workflow_hash="$(sha256sum -- "$WORKFLOW_PATH" | awk '{print $1}')"
[[ "$preflight_hash" =~ ^[0-9a-f]{64}$ ]] || die "preflight hash was not computed" 1
[[ "$workflow_hash" =~ ^[0-9a-f]{64}$ ]] || die "workflow hash was not computed" 1

out_dir="$(dirname -- "$out")"
[ -d "$out_dir" ] || die "WITNESS_OUT directory does not exist" 2
tmp_out="$(mktemp "${out_dir}/preflight-witness.XXXXXX")"
cleanup() {
  rm -f -- "$tmp_out"
}
trap cleanup EXIT

jq -n \
  --arg schema "$SCHEMA" \
  --arg repository "$repo" \
  --arg head_repository "$head_repo" \
  --arg workflow_path "$WORKFLOW_PATH" \
  --arg workflow_sha256 "$workflow_hash" \
  --arg event_name "$event" \
  --arg ref "$ref" \
  --arg sha "$sha" \
  --arg scope "all" \
  --arg preflight_sha256 "$preflight_hash" \
  --argjson run_orbit_e2e true \
  --argjson run_id "$run_id" \
  --argjson run_attempt "$run_attempt" \
  '{
    schema: $schema,
    repository: $repository,
    head_repository: $head_repository,
    workflow_path: $workflow_path,
    workflow_sha256: $workflow_sha256,
    event_name: $event_name,
    ref: $ref,
    sha: $sha,
    scope: $scope,
    run_orbit_e2e: $run_orbit_e2e,
    preflight_sha256: $preflight_sha256,
    run_id: $run_id,
    run_attempt: $run_attempt
  }' >"$tmp_out"

jq -e \
  --arg schema "$SCHEMA" \
  --arg sha "$sha" \
  --arg ref "$ref" \
  --arg event "$event" \
  --arg repo "$repo" \
  '.schema == $schema and .sha == $sha and .ref == $ref and .event_name == $event and .repository == $repo and .run_orbit_e2e == true and .scope == "all"' \
  "$tmp_out" >/dev/null || die "witness JSON failed its own binding check" 1

mv -f -- "$tmp_out" "$out"
trap - EXIT
printf 'wrote preflight witness for %s %s\n' "$event" "$ref" >&2
