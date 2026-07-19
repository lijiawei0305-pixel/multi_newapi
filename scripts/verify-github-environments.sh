#!/usr/bin/env bash
# Read-only, fail-closed audit of GitHub Environment protection. This script
# never changes repository settings. Publish workflows call it before building
# or uploading artifacts so a missing/unprotected Environment cannot silently
# become a cosmetic label.
set -euo pipefail
export LC_ALL=C LANG=C

command -v gh >/dev/null 2>&1 || { echo "gh is required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }

repo="${GITHUB_REPOSITORY:-}"
if [ -z "$repo" ]; then
  repo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
fi
printf '%s\n' "$repo" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' \
  || { echo "invalid GitHub repository identity: $repo" >&2; exit 1; }

if [ "$#" -eq 0 ]; then
  set -- container-publish release-publish
fi

evidence_out="${GITHUB_ENVIRONMENT_EVIDENCE_OUT:-}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/newapi-environment-audit.XXXXXX")"
trap 'rm -rf -- "$tmp"' EXIT INT TERM
printf '[]\n' > "$tmp/evidence.json"
failed=0

for environment in "$@"; do
  printf '%s\n' "$environment" | grep -Eq '^[A-Za-z0-9_.-]+$' || {
    echo "invalid Environment name: $environment" >&2
    failed=1
    continue
  }
  encoded="$(jq -rn --arg value "$environment" '$value|@uri')"
  response="$tmp/$environment.json"
  if ! gh api -H 'Accept: application/vnd.github+json' \
      "/repos/$repo/environments/$encoded" > "$response"; then
    echo "Environment '$environment' is missing or unreadable" >&2
    failed=1
    continue
  fi

  reviewers="$(jq '[.protection_rules[]? | select(.type == "required_reviewers") | .reviewers[]?] | length' "$response")"
  branch_policy="$(jq '(.deployment_branch_policy.protected_branches == true) or (.deployment_branch_policy.custom_branch_policies == true)' "$response")"
  admin_bypass="$(jq 'if has("can_admins_bypass") then .can_admins_bypass else true end' "$response")"
  wait_timer="$(jq '[.protection_rules[]? | select(.type == "wait_timer") | .wait_timer] | first // 0' "$response")"

  [ "$reviewers" -ge 1 ] || { echo "Environment '$environment' has no required reviewer" >&2; failed=1; }
  [ "$branch_policy" = true ] || { echo "Environment '$environment' has no deployment branch/tag policy" >&2; failed=1; }
  [ "$admin_bypass" = false ] || { echo "Environment '$environment' allows administrator bypass" >&2; failed=1; }

  jq --arg name "$environment" \
    --argjson reviewers "$reviewers" \
    --argjson branch_policy "$branch_policy" \
    --argjson admin_bypass "$admin_bypass" \
    --argjson wait_timer "$wait_timer" \
    '. + [{environment:$name,required_reviewers:$reviewers,branch_policy:$branch_policy,can_admins_bypass:$admin_bypass,wait_timer_minutes:$wait_timer}]' \
    "$tmp/evidence.json" > "$tmp/evidence.next"
  mv "$tmp/evidence.next" "$tmp/evidence.json"
done

if [ -n "$evidence_out" ]; then
  mkdir -p "$(dirname "$evidence_out")"
  install -m 600 "$tmp/evidence.json" "$evidence_out"
fi
cat "$tmp/evidence.json"
[ "$failed" = 0 ] || exit 1
