# Data-service least-privilege and immutable-image rollout

This runbook is deliberately split into reversible phases. Repository changes do
not alter a running server by themselves. Do not start the rollout without a
maintenance window, a successful paired backup, and tested rollback access.

## Current immutable baseline

A read-only inspection of the production host on 2026-07-19 recorded:

| Service | Existing tag | Verified linux/amd64 digest |
| --- | --- | --- |
| MySQL | `mysql:8.2` | `mysql@sha256:212fe73edca5df6ff14826d5eb975c914bfb91f82a2e923f9050568f99525da1` |
| Redis | `redis:8.8.0` | `redis@sha256:234c902a2db49461a129e2d4aeff85b28cf20187ed274a67f6e50995fa713c7b` |

`deploy/docker-compose.test.yml` pins these exact artifacts. The pin removes tag
drift; it does **not** claim that MySQL 8.2 is the desired long-term release.

## Phase 0 — external prerequisites (no server mutation)

1. Generate two distinct secrets offline:

   ```bash
   openssl rand -hex 32   # MYSQL_APP_PASSWORD
   openssl rand -hex 32   # REDIS_PASSWORD
   ```

2. Install `age` and `rclone`, configure a remote owned by an independent
   account/region, and enable provider-side Object Lock/immutable retention.

3. Add these lines to `/root/newapi-test/.env`, keep the file mode
   `600`, and do not commit the values:

   ```dotenv
   MYSQL_APP_USER=newapi
   MYSQL_APP_PASSWORD=<64 hex characters>
   REDIS_APP_USER=newapi
   REDIS_PASSWORD=<different 64 hex characters>
   BACKUP_OFFSITE_REQUIRED=1
   BACKUP_OFFSITE_REMOTE=<rclone-remote>:<bucket/prefix>
   BACKUP_OFFSITE_AGE_RECIPIENT=<age public recipient>
   ```

4. Run the normal repository preflight. `deploy.sh` performs a read-only format,
   uniqueness, and file-mode check before it takes the operations lock or runs a
   backup. Missing or malformed values stop the release before production state
   changes.

## Phase 1 — reversible credential migration

Run the normal `deploy.sh`. Its mandatory paired backup happens before release
replacement. During the first migration from an older release, the deploy driver
first checks whether the one manifest created by this deployment already has a
receipt strictly bound to its name, actual SHA-256, and full-download
verification. A valid receipt from the current server backup is reused to avoid
duplicating the immutable upload; an invalid existing receipt stops the release.
When an older backup script produced no receipt, the driver extracts only the
audited `lib.sh` and `offsite-copy.sh` from the local clean `HEAD` into an
isolated server-side sibling. While retaining the same operations lock, it
requires the encrypted upload, full-download SHA-256 check, and receipt before
it uploads or swaps the release. It never overlays the currently running release.
On the first hardened start:

- `mysql-access-bootstrap` waits for MySQL, then idempotently creates or rotates
  `newapi@%` and grants privileges only on ``new-api-test``. It uses root only in
  the one-shot bootstrap container; the application waits for successful
  completion and connects as `newapi`. The Compose file uses MySQL's supported
  default `caching_sha2_password` rather than the 8.4-removed
  `default-authentication-plugin` switch.
- Redis writes a mode-`600` ephemeral ACL file, disables the anonymous `default`
  user, removes destructive/admin commands from the named application user, and
  then delegates to the official entrypoint so the server drops to the image's
  non-root `redis` account with AOF unchanged.
- application and operations Redis clients authenticate. Secrets are provided
  through environment/stdin, not command-line arguments.

Acceptance checks:

```bash
cd /root/newapi-test/deploy/ops
./healthcheck.sh
./backup.sh
./reconcile.sh
```

Then confirm, without printing credentials, that the application MySQL session
is not `root` and Redis rejects an unauthenticated `PING` while the authenticated
ops helper succeeds. Record only usernames, grants, image digests, and exit
status—never secret values.

### Rollback

If bootstrap, readiness, backup, or reconciliation fails, keep maintenance mode
enabled and run the normal paired release rollback. The previous Compose file
uses root/anonymous connections, so recreating the previous MySQL/Redis/app
containers restores the previous connection behavior without deleting either
data volume. Do **not** drop the new MySQL user during incident rollback; an
unused schema-scoped account is safer than adding a second destructive step.
Rotate/remove it only after the old release is stable and a fresh backup exists.

## Phase 2 — MySQL 8.4 LTS migration

Never upgrade the production data directory as an experiment. First resolve an
approved fixed 8.4 patch manifest digest, then run the disposable paired-restore
workflow with:

```bash
MYSQL_IMAGE='mysql:8.4.x@sha256:<approved-manifest-digest>' \
  REQUIRE_DOCKER=1 deploy/ops/tests/restore-integration.sh
```

The authoritative GitHub workflow currently tests both the production 8.2
baseline and the fixed `mysql:8.4.10@sha256:c592c15aaf4a1961e15d82eb31ea5987dda862d1c4b1e93424438c0e91dc1f8d`
LTS candidate. A green candidate matrix run is repository evidence only: archive
the run URL and digest before any production change, and still perform the
maintenance-window checks below against the actual deployment.

The isolated drill must prove startup, schema migration, writes, paired backup,
destructive restore, readiness, and financial reconciliation. Archive its run
URL and digest. Only then set `MYSQL_IMAGE` in the production `.env` and schedule
the real maintenance rollout.

Rollback after an on-disk MySQL upgrade must use the pre-upgrade logical paired
backup with the pinned 8.2 image and a fresh/known-compatible volume. Never point
8.2 at a data directory already upgraded by 8.4. This is why the mandatory
pre-release backup and external maintenance window are release blockers.

## Remaining external proof

Repository code can enforce credential shape, least-privilege startup, digest
pinning, and rollback order. It cannot prove that production `.env` was updated,
that the 8.4 isolated drill passed, or that the maintenance rollback was
observed. Keep R03 operationally open until those artifacts are archived.
