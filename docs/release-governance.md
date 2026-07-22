# Release governance evidence

Publish workflows fail closed through `scripts/verify-github-environments.sh`.
The check is read-only and always requires both `container-publish` and
`release-publish` to exist, use selected deployment branches and tags, and
match the repository's exact approved policy baseline:

- `container-publish`: branches `main`, `alpha`, and `nightly`; tags `v*` and
  `[0-9]*`;
- `release-publish`: tags `v*` and `[0-9]*`.

The publishing workflows independently validate semantic version tags and
source ancestry before they reach these Environments. The Environment patterns
therefore constrain the eligible ref classes, while the workflow checks enforce
the exact release format and commit.

Formal Electron tags additionally fail before artifact upload unless the
`release-publish` Environment provides `WINDOWS_CSC_LINK` and
`WINDOWS_CSC_KEY_PASSWORD`. `electron-builder` signs the Windows executables,
and the workflow verifies every release-root `.exe` with Authenticode and a
trusted timestamp before producing checksums or uploading artifacts. A manual
rerun must select an existing version tag; non-release package smoke stays in
the main CI workflow, remains unsigned, and is never uploaded to a Release.

Manual Gitee synchronization must likewise select the same existing version
tag that is supplied in the `tag_name` input. The verification job binds that
tag to the workflow ref, checked-out commit, and `origin/main` ancestry before
the GitHub-hosted publishing job enters `release-publish` and receives the
Gitee credential. The workflow does not depend on an unregistered self-hosted
runner. Its repository-owned `scripts/gitee_release.py` client uses only the
Python standard library, reads the token from the step environment, keeps it
out of URLs and process arguments, and streams multipart asset uploads without
runtime package installation.

Every repository workflow uses the strict default: each Environment must have
at least one required reviewer and administrator bypass must be disabled. The
private personal repository's current billing plan returned HTTP 422 when those
rules were configured and still reports `can_admins_bypass: true`. The
repository no longer weakens the check to accommodate that plan limitation;
governance and every formal publish fail closed until the repository moves to a
plan/owner type that exposes the required protection and the rules are enabled.

The daily/manual `GitHub Environment Governance Audit` workflow stores a
sanitized JSON artifact containing policy names, types, counts, and booleans;
no secrets are read. The built-in workflow token has `actions: read`, which is
the permission GitHub requires for the Environment and deployment-policy read
APIs. `ENVIRONMENT_AUDIT_TOKEN` remains an optional fallback for installations
whose built-in token cannot read those settings.

Configure rules in GitHub repository settings; this repository intentionally
does not mutate them. After configuration, run:

```bash
GITHUB_REPOSITORY=OWNER/REPO \
scripts/verify-github-environments.sh
```

A nonzero exit means publishing must remain blocked. Review wait-timer values
in the emitted evidence and apply the organization's desired delay. Environment
secret values are never read or exported by this audit.

As of the read-only API check on 2026-07-22, both named Environments exist and
their branch/tag policies match the baseline above. Required reviewers and the
administrator-bypass control remain unavailable under the current private
repository plan, so the strict check intentionally blocks governance and formal
publishing rather than representing the Environments as protected.

## Clean release source boundary

`scripts/verify-clean-checkout.sh` is an explicit CI gate: tracked, staged, and
untracked inputs must be absent and `HEAD` must equal the workflow commit.
`deploy/ops/deploy.sh` independently refuses a dirty index/worktree and creates
the upload with `git archive HEAD`; ignored caches or local runner files are not
read into the release archive.

The previously reported 1,781-entry worktree was committed and pushed. Before
this remediation batch began, local `main`, `origin/main`, and tag
`deploy-20260719-210941` all resolved to
`da99ecd836b80a17a2b7fadb1436e5ac16c4a913`, with an empty status. The clean
checkout CI for that commit completed successfully:
<https://github.com/lijiawei0305-pixel/multi_newapi/actions/runs/29688097488>.
The paired disposable restore drill also succeeded:
<https://github.com/lijiawei0305-pixel/multi_newapi/actions/runs/29688097540>.

R07 is therefore closed for the prior audit snapshot. Every later change still
needs its own commit and a green clean-checkout run; old evidence is not reused
as proof for a new release.
