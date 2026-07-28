# Agent money fixed-point migration

Agent wallet, earning, and withdrawal amounts use signed `BIGINT` columns in
units of `1e-8`. The pre-existing `DECIMAL(20,8)` columns remain compatibility
read models because an immediately previous binary only knows those columns.

## Money expand and rollback contract

The unit columns are intentionally nullable and have no database default. This
keeps `ALTER TABLE ... ADD COLUMN` on PostgreSQL 9.6 on its fast additive path.
Application writes always provide units, and versioned compatibility triggers
guarantee that a committed row never remains NULL:

- an old binary insert/update that omits units converts decimal to units and
  normalizes the decimal mirror;
- a current binary update that changes units derives the decimal mirror from
  units in the same statement;
- when both sides change, non-NULL units win; when units are absent, decimal is
  the legacy source;
- out-of-range legacy decimal writes abort instead of being clamped.

The triggers are permanent for as long as release-only rollback to a decimal-
only binary is supported. Removing them is a later contract step and requires
first retiring every compatible old image.

SQLite stores legacy decimals as binary64 values. It cannot distinguish values
near `MaxInt64 / 1e8`, so decimal-authoritative migration and rollback writes
are conservatively limited to `±90,000,000,000`. Current unit-authoritative
writes retain the full signed int64 range.

This compatibility statement applies to the money columns only. The new
withdrawal request idempotency and payout-reference claim protocols are not
understood by an old application binary; see the deployment gate below.

## One-time import

Triggers are installed before the import, closing the old-writer window. A
transactional marker contains a random claim token. After `INSERT ... ON
CONFLICT DO NOTHING`, the importer reads the persisted token to decide whether
it owns the migration; it never relies on driver-specific `RowsAffected`.

Once a marker exists, later startups return without updating wallet or history
tables. Mirror consistency belongs to the compatibility triggers and the
read-only operations reconciliation check, not an unconditional startup repair.

The first import must still scan and update every legacy wallet, earning, and
withdrawal row once. On a very large history table this can exceed the normal
readiness/deploy timeout and hold write locks, especially on MySQL and
PostgreSQL 9.6. Schedule that first rollout in a maintenance window, size the
readiness timeout for the measured table volume, and keep the pre-migration
database backup until the integer reconciliation checks pass. Later restarts do
not repeat the backfill.

## Exact idempotency and payout-reference migration

Text uniqueness cannot rely on the database's default collation: common MySQL
collations equate values such as `KeyA` and `keya`, while PostgreSQL and SQLite
normally do not. A second one-time migration (`exact_key_hashes_v4`) therefore
backfills lowercase SHA-256 hex columns for earning source tuples, non-empty
withdrawal request keys, and payout references. Request keys and payout
references are first normalized with the application's `strings.TrimSpace`
rule, then uniqueness is enforced on the hashes while the canonical text is
retained for exact collision checks and operator display. Existing non-null
but incorrect hashes are recomputed rather than trusted.

The migration verifies any short-lived pre-hash `agent_payout_ref_claims` and
hash-based `agent_payout_ref_claims_v2` rows against their canonical paid
withdrawals, then claims every historical payout reference in the disjoint
`agent_payout_ref_claims_v3` namespace. Conflicting, orphaned, or mismatched
historical ownership fails startup instead of selecting an arbitrary winner.
Both old claim tables remain intact as migration evidence; runtime ownership
uses v3.
After the replacement `(tenant_id, request_key_hash)` unique index is live, the
collation-sensitive raw request-key unique index is removed. The legacy unique
earning source-tuple and long raw `idem_key` indexes are also removed (the
source tuple retains a non-unique lookup index); `idem_key_hash` becomes the
only cross-database earning idempotency authority.

This migration scans and may update `agent_earning_logs` and
`agent_withdrawals` once and builds new indexes. Include it in the same
maintenance-window sizing as the money import, especially for large
MySQL/PostgreSQL histories.

## Deployment gate for agent financial mutations

Do not use a mixed old/new backend fleet for earning writers, withdrawal
requests, or `mark-paid`. An old node does not write `idem_key_hash`, ignores
withdrawal `Idempotency-Key`, and does not write the v3 payout claim, so no
database-only guarantee can make those old code paths safe. Use this order:

1. pause/drain agent earning settlement (including background billing writers),
   withdrawal, and `mark-paid` traffic, then stop every old API node;
2. back up the database and let one new master complete both migrations;
3. run the integer/hash/claim reconciliation checks;
4. start only new API nodes, then publish the frontend and re-enable traffic.

If an application rollback is required, keep earning settlement, withdrawal
creation, and `mark-paid` paused until all nodes are back on the new protocol.
An old master can also try to recreate the removed collation-sensitive earning
unique index, which is invalid after case-distinct source IDs have been
accepted. The permanent money triggers still preserve balance-column
compatibility, but the old binary cannot preserve earning, request, or payout
idempotency.

SQLite deployments must use the modernc/glebarez parameters
`_pragma=busy_timeout(30000)&_txlock=immediate`. The application adds a valid
pragma when missing and normalizes unsafe deferred locking in `SQLITE_PATH`;
immediate transactions prevent concurrent
read-to-write upgrades from leaking `SQLITE_BUSY` during wallet state changes.

## Verification

SQLite behavior is covered by the regular repository tests. The opt-in
`TestAgentMoneyExternalDialect` owns and drops only `agent_*` tables in a
database whose name ends in `_test`. CI runs it against MySQL with both default
affected-row semantics and `clientFoundRows=true`, plus PostgreSQL, covering
legacy import, `1e-8`, `0.1 + 0.7`, amounts above `10,000`, repeated migration,
and old-binary writes.
