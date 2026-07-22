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

Approval protection remains the default audit mode. Unless
`GITHUB_ENVIRONMENT_APPROVALS_REQUIRED=false` is explicitly set, every
Environment must also have at least one required reviewer and administrator
bypass disabled. The repository workflows currently set that exception because
GitHub returned HTTP 422 when required reviewers were configured for this
private personal repository: the account's billing plan does not expose that
protection rule. The API still reports `can_admins_bypass: true`; the audit
records it as evidence but cannot enforce an unavailable setting. If the
repository moves to a plan and owner type that supports private-repository
approval protection, configure the rules and remove the exception.

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
GITHUB_ENVIRONMENT_APPROVALS_REQUIRED=false \
scripts/verify-github-environments.sh
```

A nonzero exit means publishing must remain blocked. Review wait-timer values
in the emitted evidence and apply the organization's desired delay. Environment
secret values are never read or exported by this audit.

As of the read-only API check on 2026-07-22, both named Environments exist and
their branch/tag policies match the baseline above. Required reviewers and the
administrator-bypass control remain unavailable under the current private
repository plan, and that limitation is kept explicit rather than represented
as an enabled protection.

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
