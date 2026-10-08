#!/usr/bin/env bash
# Fail closed unless a successful tag-push release preflight witness matches
# this checkout. This script does not run preflight and does not publish.
#
# P3-B contract:
#   exit 0   witness accepted; a later change may skip the duplicate preflight
#   exit 10  witness unavailable; the caller must run scripts/preflight.sh
#   exit 20  witness rejected; the caller must fail and must not skip preflight
#   exit 2   usage
#
# Exit 10 is the safe fallback (timeout, missing run, permission, or an
# unreadable artifact). Another tag on this same commit is not this tag's
# witness: while this tag's push run is absent the script keeps polling and
# exits 10. It does not report wrong-tag for that race. Exit 20 is a witness
# for this tag that must not be reused: fork, wrong SHA, workflow_dispatch,
# a CI run, a failed or cancelled run, or a hash that does not match.
#
# The default poll is 45 minutes so it ends before a 60-minute publish verify
# job is killed. P3-B must not spend that whole wait and then start a full
# preflight in the remaining 15 minutes. A slow fallback needs its own time
# budget inside the verify job; a timeout during that fallback is a failure,
# not a skipped gate.
set -euo pipefail
export LC_ALL=C LANG=C

readonly WORKFLOW_FILE="release-preflight.yml"
readonly WORKFLOW_PATH=".github/workflows/release-preflight.yml"
readonly ARTIFACT_NAME="preflight-witness"
readonly SCHEMA="new-api.release-preflight-witness.v1"
readonly DEFAULT_TIMEOUT_SECONDS=2700
readonly DEFAULT_POLL_SECONDS=15

tmp=""
run_id=""
run_attempt=""
token=""

cleanup() {
  if [ -n "$tmp" ]; then
    rm -rf -- "$tmp"
  fi
}
trap cleanup EXIT

finish() {
  local code="$1"
  local reason="$2"
  local class
  case "$code" in
    0) class="accepted" ;;
    10) class="unavailable" ;;
    20) class="rejected" ;;
    *) class="usage" ;;
  esac
  if [ "$code" -eq 0 ]; then
    printf 'preflight-witness: accepted reason=accepted run_id=%s run_attempt=%s\n' \
      "$run_id" "$run_attempt"
  else
    printf 'preflight-witness: %s reason=%s\n' "$class" "$reason"
  fi
  exit "$code"
}

usage() {
  cat >&2 <<'EOF'
usage: verify-preflight-witness.sh --tag TAG --sha SHA [--repository OWNER/REPO] [--root DIR]

Reads GH_TOKEN or GITHUB_TOKEN. The token must be allowed to read Actions.
EOF
  finish 2 usage
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || finish 2 usage
}

print_config() {
  printf 'timeout_seconds=%s\n' "$DEFAULT_TIMEOUT_SECONDS"
  printf 'poll_seconds=%s\n' "$DEFAULT_POLL_SECONDS"
  printf 'workflow_path=%s\n' "$WORKFLOW_PATH"
  printf 'artifact_name=%s\n' "$ARTIFACT_NAME"
  printf 'schema=%s\n' "$SCHEMA"
  exit 0
}

assert_token_can_read_actions() {
  local kind
  if ! kind="$(TOKEN="$token" python3 -c '
import base64, json, os, sys
token = os.environ.get("TOKEN", "")
parts = token.split(".")
if len(parts) != 3:
    print("opaque")
    raise SystemExit(0)

def decode(segment):
    padding = "=" * ((4 - len(segment) % 4) % 4)
    return base64.urlsafe_b64decode(segment + padding)

try:
    payload = json.loads(decode(parts[1]))
except Exception:
    print("malformed")
    raise SystemExit(0)
permissions = payload.get("permissions", None)
if not isinstance(permissions, dict):
    print("opaque-jwt")
    raise SystemExit(0)
if permissions.get("actions") in ("read", "write"):
    print("ok")
    raise SystemExit(0)
print("missing")
')"; then
    finish 10 actions-read-required
  fi
  case "$kind" in
    ok|opaque|opaque-jwt) ;;
    *) finish 10 actions-read-required ;;
  esac
}

hash_file() {
  local file="$1"
  [ -f "$file" ] && [ ! -L "$file" ] || finish 2 usage
  sha256sum -- "$file" | awk '{print $1}'
}

# Download a GitHub API resource. Auth failures exit 10. Transport failures
# return 1 so the caller can retry runs, or fail closed for artifacts.
gh_get() {
  local url="$1"
  local dest="$2"
  local kind="$3"
  local err="$tmp/gh.err"
  : >"$err"
  if gh api -H "Accept: application/vnd.github+json" "$url" >"$dest" 2>"$err"; then
    return 0
  fi
  if grep -Eqi 'not accessible|HTTP 401|HTTP 403|Bad credentials|Requires authentication' "$err" "$dest"; then
    finish 10 actions-read-required
  fi
  case "$kind" in
    probe|runs)
      if grep -Eqi 'HTTP 404|"Not Found"' "$err" "$dest"; then
        finish 10 workflow-missing
      fi
      return 1
      ;;
    *)
      return 1
      ;;
  esac
}

classify_runs() {
  jq -c \
    --arg sha "$sha" \
    --arg tag "$tag" \
    --arg repo "$repository" \
    --arg path "$WORKFLOW_PATH" \
    '
      def push_identity:
        .head_sha == $sha
        and .head_branch == $tag
        and .path == $path
        and .event == "push"
        and (.repository.full_name // "") == $repo
        and (.head_repository.full_name // "") == $repo
        and (.id | type) == "number"
        and (.run_attempt | type) == "number";
      def nonterminal:
        .status == "queued"
        or .status == "in_progress"
        or .status == "waiting"
        or .status == "pending"
        or .status == "requested";
      if (.workflow_runs | type) != "array" or (.total_count | type) != "number" then
        {decision:"unavailable", reason:"api-error"}
      elif .total_count != (.workflow_runs | length) then
        {decision:"unavailable", reason:"response-truncated"}
      else
        ([.workflow_runs[] | select(push_identity)] | sort_by(.id) | last) as $newest
        | if $newest != null then
            if ($newest | nonterminal) then
              {decision:"wait", reason:"in-progress", run_id:$newest.id, run_attempt:$newest.run_attempt}
            elif $newest.status == "completed" and $newest.conclusion == "success" then
              {decision:"accept", reason:"accepted", run_id:$newest.id, run_attempt:$newest.run_attempt}
            else
              {decision:"reject", reason:"conclusion", run_id:$newest.id, conclusion:($newest.conclusion // $newest.status // "unknown")}
            end
          elif any(.workflow_runs[];
                .head_sha == $sha and .head_branch == $tag and .path == $path and .event == "push"
                and (((.repository.full_name // "") != $repo) or ((.head_repository.full_name // "") != $repo)))
            then {decision:"reject", reason:"fork"}
          elif any(.workflow_runs[]; .path != $path) then
            {decision:"reject", reason:"wrong-workflow"}
          elif any(.workflow_runs[];
                .head_sha == $sha and .head_branch == $tag and .path == $path and .event != "push")
            then {decision:"reject", reason:"wrong-event"}
          elif any(.workflow_runs[];
                .head_sha != $sha and .head_branch == $tag and .path == $path) then
            {decision:"reject", reason:"wrong-sha"}
          else
            {decision:"wait", reason:"not-found"}
          end
      end
    ' "$1"
}

verify_witness_json() {
  local witness="$1"
  if ! jq -e 'type == "object"' "$witness" >/dev/null 2>"$tmp/jq.err"; then
    finish 10 artifact-unreadable
  fi
  if ! jq -e \
    --arg schema "$SCHEMA" \
    --arg repo "$repository" \
    --arg path "$WORKFLOW_PATH" \
    --arg ref "$full_ref" \
    --arg sha "$sha" \
    --arg run_id "$run_id" \
    --arg run_attempt "$run_attempt" \
    '
      keys == [
        "event_name",
        "head_repository",
        "preflight_sha256",
        "ref",
        "repository",
        "run_attempt",
        "run_id",
        "run_orbit_e2e",
        "schema",
        "scope",
        "sha",
        "workflow_path",
        "workflow_sha256"
      ]
      and .schema == $schema
      and .event_name == "push"
      and .head_repository == $repo
      and .repository == $repo
      and .workflow_path == $path
      and .ref == $ref
      and .sha == $sha
      and .scope == "all"
      and .run_orbit_e2e == true
      and (.run_id | type) == "number"
      and (.run_attempt | type) == "number"
      and (.run_id | tostring) == $run_id
      and (.run_attempt | tostring) == $run_attempt
      and (.preflight_sha256 | test("^[0-9a-f]{64}$"))
      and (.workflow_sha256 | test("^[0-9a-f]{64}$"))
      and (.sha | test("^[0-9a-f]{40}$"))
    ' "$witness" >/dev/null 2>"$tmp/jq.err"; then
    finish 20 binding
  fi
  local preflight_got workflow_got
  preflight_got="$(jq -r '.preflight_sha256' "$witness")"
  workflow_got="$(jq -r '.workflow_sha256' "$witness")"
  if [ "$preflight_got" != "$preflight_hash" ]; then
    finish 20 preflight-hash
  fi
  if [ "$workflow_got" != "$workflow_hash" ]; then
    finish 20 workflow-hash
  fi
}

extract_witness() {
  python3 -c '
import sys, zipfile
from pathlib import PurePosixPath
source, dest = sys.argv[1], sys.argv[2]
try:
    archive = zipfile.ZipFile(source)
except zipfile.BadZipFile:
    raise SystemExit(1)
names = [name for name in archive.namelist() if not name.endswith("/")]
if len(names) != 1:
    raise SystemExit(2)
member = names[0]
parts = PurePosixPath(member).parts
if member.startswith("/") or ".." in parts or member != "preflight-witness.json":
    raise SystemExit(2)
info = archive.getinfo(member)
if info.file_size <= 0 or info.file_size > 8192:
    raise SystemExit(3)
data = archive.read(member)
if len(data) == 0 or len(data) > 8192 or b"\x00" in data:
    raise SystemExit(3)
with open(dest, "wb") as handle:
    handle.write(data)
' "$tmp/witness.zip" "$tmp/witness.json"
}

accept_run() {
  local artifacts_url artifact_decision artifact_id extract_status download_url
  artifacts_url="/repos/${repository}/actions/runs/${run_id}/artifacts"
  if ! gh_get "$artifacts_url" "$tmp/artifacts.json" artifact; then
    finish 10 artifact-unreadable
  fi
  if ! artifact_decision="$(jq -c --arg name "$ARTIFACT_NAME" '
    if (.artifacts | type) != "array" or (.total_count | type) != "number" then
      {decision:"unavailable", reason:"artifact-unreadable"}
    elif .total_count != (.artifacts | length) then
      {decision:"unavailable", reason:"artifact-unreadable"}
    else
      [.artifacts[] | select(.name == $name and .expired == false)] as $named
      | if ($named | length) > 1 then
          {decision:"reject", reason:"ambiguous-artifact"}
        elif ($named | length) == 0 then
          {decision:"unavailable", reason:"artifact-missing"}
        elif ($named[0].id | type) != "number"
          or ($named[0].size_in_bytes | type) != "number"
          or $named[0].size_in_bytes < 0
          or $named[0].size_in_bytes > 65536 then
          {decision:"unavailable", reason:"artifact-unreadable"}
        else
          {decision:"download", reason:"ok", artifact_id:$named[0].id}
        end
    end
  ' "$tmp/artifacts.json")"; then
    finish 10 artifact-unreadable
  fi
  local artifact_kind artifact_reason
  artifact_kind="$(jq -r '.decision' <<<"$artifact_decision")"
  artifact_reason="$(jq -r '.reason' <<<"$artifact_decision")"
  case "$artifact_kind" in
    download) ;;
    reject) finish 20 "$artifact_reason" ;;
    unavailable) finish 10 "$artifact_reason" ;;
    *) finish 10 artifact-unreadable ;;
  esac
  artifact_id="$(jq -r '.artifact_id' <<<"$artifact_decision")"
  [[ "$artifact_id" =~ ^[0-9]+$ ]] || finish 10 artifact-unreadable
  # Ignore archive_download_url. It is an untrusted redirect target.
  download_url="/repos/${repository}/actions/artifacts/${artifact_id}/zip"
  if ! gh_get "$download_url" "$tmp/witness.zip" zip; then
    finish 10 artifact-unreadable
  fi
  local zip_bytes
  zip_bytes="$(wc -c <"$tmp/witness.zip")"
  if [ "$zip_bytes" -le 0 ] || [ "$zip_bytes" -gt 65536 ]; then
    finish 10 artifact-unreadable
  fi
  extract_status=0
  extract_witness || extract_status=$?
  case "$extract_status" in
    0) ;;
    2) finish 20 binding ;;
    *) finish 10 artifact-unreadable ;;
  esac
  verify_witness_json "$tmp/witness.json"
  finish 0 accepted
}

repository="${GITHUB_REPOSITORY:-}"
tag=""
sha=""
root=""
timeout="${PREFLIGHT_WITNESS_TIMEOUT_SECONDS:-$DEFAULT_TIMEOUT_SECONDS}"
poll="${PREFLIGHT_WITNESS_POLL_SECONDS:-$DEFAULT_POLL_SECONDS}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      ;;
    --print-config)
      print_config
      ;;
    --tag)
      [ "$#" -ge 2 ] || usage
      tag="$2"
      shift 2
      ;;
    --sha)
      [ "$#" -ge 2 ] || usage
      sha="$2"
      shift 2
      ;;
    --repository)
      [ "$#" -ge 2 ] || usage
      repository="$2"
      shift 2
      ;;
    --root)
      [ "$#" -ge 2 ] || usage
      root="$2"
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

require_cmd gh
require_cmd jq
require_cmd python3
require_cmd sha256sum
require_cmd date
require_cmd wc

[ -n "$tag" ] || usage
[ -n "$sha" ] || usage
[ -n "$repository" ] || usage
[[ "$tag" =~ ^[0-9A-Za-z][0-9A-Za-z._-]{0,79}$ ]] || finish 2 usage
case "$tag" in
  nightly*) finish 20 wrong-tag ;;
esac
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || finish 2 usage
[[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || finish 2 usage
[[ "$timeout" =~ ^[0-9]+$ ]] || finish 2 usage
[[ "$poll" =~ ^[0-9]+$ ]] || finish 2 usage
if [ "$timeout" -gt "$DEFAULT_TIMEOUT_SECONDS" ]; then
  timeout="$DEFAULT_TIMEOUT_SECONDS"
fi

if [ -n "${GH_TOKEN:-}" ] && [ -n "${GITHUB_TOKEN:-}" ] && [ "$GH_TOKEN" != "$GITHUB_TOKEN" ]; then
  finish 2 usage
fi
token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
[ -n "$token" ] || finish 2 usage
if [ -z "${GH_TOKEN:-}" ]; then
  export GH_TOKEN="$token"
fi

if [ -z "$root" ]; then
  root="$(git rev-parse --show-toplevel 2>/dev/null)" || finish 2 usage
fi
root="$(cd "$root" && pwd)"
cd "$root"
preflight_hash="$(hash_file scripts/preflight.sh)"
workflow_hash="$(hash_file "$WORKFLOW_PATH")"
[[ "$preflight_hash" =~ ^[0-9a-f]{64}$ ]] || finish 2 usage
[[ "$workflow_hash" =~ ^[0-9a-f]{64}$ ]] || finish 2 usage
full_ref="refs/tags/${tag}"

assert_token_can_read_actions

tmp="$(mktemp -d "${TMPDIR:-/tmp}/preflight-witness.XXXXXX")"
encoded_sha="$(jq -rn --arg value "$sha" '$value | @uri')"
runs_url="/repos/${repository}/actions/workflows/${WORKFLOW_FILE}/runs?head_sha=${encoded_sha}&per_page=100"
probe_url="/repos/${repository}/actions/workflows/${WORKFLOW_FILE}"

if ! gh_get "$probe_url" "$tmp/workflow.json" probe; then
  finish 10 api-error
fi
if ! jq -e --arg path "$WORKFLOW_PATH" '.path == $path' "$tmp/workflow.json" >/dev/null; then
  finish 20 wrong-workflow
fi
if ! jq -e '.state == "active"' "$tmp/workflow.json" >/dev/null; then
  finish 10 workflow-disabled
fi

start="$(date +%s)"
deadline="$((start + timeout))"
loops=0
max_loops=1000
if [ "$poll" -eq 0 ]; then
  max_loops=8
fi
last_wait_reason="not-found"

while true; do
  loops="$((loops + 1))"
  if [ "$loops" -gt "$max_loops" ]; then
    if [ "$last_wait_reason" = "in-progress" ]; then
      finish 10 timeout
    fi
    finish 10 "$last_wait_reason"
  fi
  now="$(date +%s)"
  if ! gh_get "$runs_url" "$tmp/runs.json" runs; then
    if [ "$now" -ge "$deadline" ]; then
      finish 10 api-error
    fi
    sleep "$poll"
    continue
  fi
  if ! classified="$(classify_runs "$tmp/runs.json")"; then
    if [ "$now" -ge "$deadline" ]; then
      finish 10 api-error
    fi
    sleep "$poll"
    continue
  fi
  decision="$(jq -r '.decision' <<<"$classified")"
  reason="$(jq -r '.reason' <<<"$classified")"
  case "$decision" in
    accept)
      run_id="$(jq -r '.run_id' <<<"$classified")"
      run_attempt="$(jq -r '.run_attempt' <<<"$classified")"
      [[ "$run_id" =~ ^[0-9]+$ ]] || finish 20 binding
      [[ "$run_attempt" =~ ^[0-9]+$ ]] || finish 20 binding
      [ "$run_attempt" -ge 1 ] || finish 20 binding
      accept_run
      ;;
    reject)
      if [ "$reason" = "conclusion" ]; then
        printf 'preflight witness run_id=%s conclusion=%s\n' \
          "$(jq -r '.run_id // ""' <<<"$classified")" \
          "$(jq -r '.conclusion // ""' <<<"$classified")" >&2
      fi
      finish 20 "$reason"
      ;;
    unavailable)
      finish 10 "$reason"
      ;;
    wait)
      last_wait_reason="$reason"
      if [ "$now" -ge "$deadline" ]; then
        if [ "$reason" = "in-progress" ]; then
          finish 10 timeout
        fi
        finish 10 "$reason"
      fi
      sleep "$poll"
      ;;
    *)
      finish 10 api-error
      ;;
  esac
done
