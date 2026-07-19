# Release governance evidence

Publish workflows fail closed through `scripts/verify-github-environments.sh`.
The check is read-only and requires both `container-publish` and
`release-publish` to have:

- at least one required reviewer;
- a protected-branch or custom deployment branch/tag policy; and
- administrator bypass disabled.

The daily/manual `GitHub Environment Governance Audit` workflow stores a
sanitized JSON artifact (counts and booleans only, no secrets). If the built-in
workflow token cannot read Environment settings, configure the repository
secret `ENVIRONMENT_AUDIT_TOKEN` with the narrowest read-only repository
administration/environment permission supported by the organization.

Configure rules in GitHub repository settings; this repository intentionally
does not mutate them. After configuration, run:

```bash
GITHUB_REPOSITORY=OWNER/REPO scripts/verify-github-environments.sh
```

A nonzero exit means publishing must remain blocked. Review wait-timer values
in the emitted evidence and apply the organization's desired delay. Environment
secret values are never read or exported by this audit.

As of the read-only API check on 2026-07-19, the repository returned
`total_count: 0`; neither named Environment existed. R06 therefore remains an
external GitHub-settings blocker until an administrator creates and protects
both Environments and the audit workflow succeeds.

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
