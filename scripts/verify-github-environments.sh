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

approvals_required="${GITHUB_ENVIRONMENT_APPROVALS_REQUIRED:-true}"
case "$approvals_required" in
  true|false) ;;
  *)
    echo "GITHUB_ENVIRONMENT_APPROVALS_REQUIRED must be true or false" >&2
    exit 1
    ;;
esac

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
  branch_policy="$(jq '.deployment_branch_policy.custom_branch_policies == true' "$response")"
  admin_bypass="$(jq 'if has("can_admins_bypass") then .can_admins_bypass else true end' "$response")"
  wait_timer="$(jq '[.protection_rules[]? | select(.type == "wait_timer") | .wait_timer] | first // 0' "$response")"

  case "$environment" in
    container-publish)
      expected_policies='[{"name":"alpha","type":"branch"},{"name":"main","type":"branch"},{"name":"nightly","type":"branch"},{"name":"[0-9]*","type":"tag"},{"name":"v*","type":"tag"}]'
      ;;
    release-publish)
      expected_policies='[{"name":"[0-9]*","type":"tag"},{"name":"v*","type":"tag"}]'
      ;;
    *)
      echo "Environment '$environment' has no approved deployment policy baseline" >&2
      failed=1
      continue
      ;;
  esac

  policies='[]'
  if [ "$branch_policy" != true ]; then
    echo "Environment '$environment' does not use selected deployment branches and tags" >&2
    failed=1
  else
    policy_response="$tmp/$environment-policies.json"
    if ! gh api -H 'Accept: application/vnd.github+json' \
        "/repos/$repo/environments/$encoded/deployment-branch-policies" > "$policy_response"; then
      echo "Environment '$environment' deployment policies are unreadable" >&2
      failed=1
    else
      policies="$(jq -c '[.branch_policies[]? | {name, type: (.type // "branch")}] | sort_by(.type, .name)' "$policy_response")"
      if ! jq -e --argjson expected "$expected_policies" \
          '[.branch_policies[]? | {name, type: (.type // "branch")}] | sort_by(.type, .name) == $expected' \
          "$policy_response" >/dev/null; then
        echo "Environment '$environment' deployment branch/tag policies differ from the approved baseline" >&2
        failed=1
      fi
    fi
  fi

  if [ "$approvals_required" = true ]; then
    [ "$reviewers" -ge 1 ] || { echo "Environment '$environment' has no required reviewer" >&2; failed=1; }
    [ "$admin_bypass" = false ] || { echo "Environment '$environment' allows administrator bypass" >&2; failed=1; }
  fi

  jq --arg name "$environment" \
    --argjson reviewers "$reviewers" \
    --argjson branch_policy "$branch_policy" \
    --argjson admin_bypass "$admin_bypass" \
    --argjson wait_timer "$wait_timer" \
    --argjson approvals_required "$approvals_required" \
    --argjson policies "$policies" \
    '. + [{environment:$name,required_reviewers:$reviewers,approval_protection_required:$approvals_required,branch_policy:$branch_policy,deployment_policies:$policies,can_admins_bypass:$admin_bypass,wait_timer_minutes:$wait_timer}]' \
    "$tmp/evidence.json" > "$tmp/evidence.next"
  mv "$tmp/evidence.next" "$tmp/evidence.json"
done

if [ -n "$evidence_out" ]; then
  mkdir -p "$(dirname "$evidence_out")"
  install -m 600 "$tmp/evidence.json" "$evidence_out"
fi
cat "$tmp/evidence.json"
[ "$failed" = 0 ] || exit 1
