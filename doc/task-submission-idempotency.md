# Asynchronous task idempotency and recovery

Task-producing endpoints support the `Idempotency-Key` request header for generic Task, Midjourney submission, and InsightFace/SwapFace submission routes. The key is optional for backward compatibility.

## Client contract

- A key must be 1–128 ASCII letters, digits, or `.`, `_`, `~`, `:`, `-`.
- A key is scoped by the authoritative tenant attribution, normalized direct request Host, authenticated user ID, authenticated API token ID, HTTP method, and route. Forwarded host headers are not trusted for this identity.
- The key is also bound to a stable digest of method, path, the exact raw query string, `Content-Type`, `Accept`, and the exact request body. Reusing the same scoped key with a different request returns HTTP 409 and never calls the provider.
- Concurrent requests with one scoped key have one atomic owner. A duplicate `preparing` or `uncertain` request never calls the provider.
- Once provider acceptance has been frozen, retries replay the exact stored status, body, and safe response headers without another provider call. Replay lookup occurs before local response-spool admission, so backpressure for new work does not block crash recovery.
- A new key is claimed only after response capacity is reserved. A local 503 capacity failure therefore does not consume the key.

The database stores only versioned SHA-256 identity/request fingerprints. It never stores or logs the raw `Idempotency-Key`, request body, bearer token, channel API key, or upstream private headers. Fingerprints are deliberately stable across application secret rotation; changing the fingerprint format requires a versioned dual-read migration for the full retained idempotency window.

Committed and aborted idempotency records are retained for 168 hours by default. Set `TASK_SUBMISSION_IDEMPOTENCY_RETENTION_HOURS` to a value of at least 24 hours to change that window. The cleanup job never deletes `preparing`, `uncertain`, or `accepted` recovery authority, and it preserves committed authority while a dependent billing projection is still pending. After a terminal record ages out, reuse of its former key is treated as a new submission, so clients should not expect replay beyond the configured window.

## Durable public response

Provider success is written to a bounded private spool and then frozen in `task_submission_recoveries.payload` together with the task and billing facts. The durable response is limited to 1 MiB of valid JSON. Only these headers can be frozen:

- `Content-Type`
- `Content-Language`
- `Location`
- `Retry-After`
- `X-Request-Id`
- `X-Oneapi-Request-Id`
- `X-New-Api-Other-Ratios`

Hop-by-hop headers, headers named by `Connection`, `Authorization`, cookies, API keys, control characters, and non-allowlisted upstream headers are rejected or discarded.

## Uncertain-state operations

Before the provider call, the recovery row records only non-secret investigation data: request ID, user/token/tenant, normalized Host and route, channel, provider/platform, model, action, public task ID, initial quota, attempt time, and one-way fingerprints. The reconciler never auto-refunds `uncertain` rows. Its critical alert includes at most ten request IDs.

Administrators can use:

- `GET /api/task-submission-recovery?status=uncertain&kind=task&limit=50`
- `GET /api/task-submission-recovery/:request_id?kind=task`
- `POST /api/task-submission-recovery/:request_id/resolve?kind=task`

`kind` is `task` or `midjourney`. Resolution bodies use one of:

```json
{"outcome":"unknown"}
```

```json
{"outcome":"rejected","reason":"Provider support confirmed that no task was created"}
```

```json
{
  "outcome": "accepted",
  "reason": "Provider console confirmed acceptance",
  "provider_task_id": "provider-task-123",
  "final_quota": 100,
  "public_response": {
    "status": 200,
    "headers": {"Content-Type": ["application/json"]},
    "body": "{\"id\":\"task_public_id\"}"
  }
}
```

`rejected` atomically cancels the reserved settlement and freezes the exact refund intent. `accepted` validates the provider ID and the supported top-level public response identity (`id`, `task_id`, `data`, or Midjourney `result`), reconstructs a secret-free local task, freezes settlement plus the initial log/counter/QuotaData projection, and applies it exactly once. SwapFace acceptance is finalized immediately and its commission snapshot is dispatched exactly once; normal asynchronous tasks remain deferred for their terminal poll. `unknown` intentionally leaves funds reserved and the row uncertain.

All resolution writes require `AdminAuth` and create an operation audit record. The management API never returns the private payload, response payload, last database error, request fingerprint, or idempotency fingerprint.

## Deployment and migration

The idempotency column is nullable for existing rows. Migration first adds nullable columns, then creates `idx_task_submission_idempotency_fingerprint` as a single-column unique index. This two-step sequence is required because SQLite cannot add a unique column to a populated table. Startup migration must also finish adding `users.tenant_id` before relay traffic is admitted; the claim path fails closed on missing or unreadable tenant attribution. Drain old application versions before serving traffic on the new schema so every new task uses the claim gate.
