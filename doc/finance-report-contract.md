## Financial Reporting — FROZEN API CONTRACT + IMPLEMENTATION MAP

Status: **FROZEN**. Backend and frontend build against this in parallel. Every fact below is grounded in the 10 reader findings with `file:line` / symbol citations. Inferred or missing facts are quarantined in §7 (Open risks). Where readers disagreed, the resolved decision is stated inline and re-listed in §7.

Two reports, four data lenses each:
- **Admin** (`/api/admin/finance/**`, `middleware.AdminAuth()`): platform-wide totals + sortable per-agent/per-tenant ranking, cross-tenant.
- **Agent self-service** (`/api/tenant/finance/**`, `middleware.UserAuth()` + `app.AgentOwnerAuth()`): scoped to ONE tenant, `tenant_id` taken only from `agentTenantID(c)`.

Lenses: (a) earnings/commission by source + wallet balances; (b) recharge/order paid/cost/spread; (c) consumption cost via `logs→users.tenant_id`; (d) withdrawals.

---

## 1. Endpoint catalog

### Response envelope (FROZEN — actual code, not the abstract doc)

The authoritative wire shape is what `respondOK`/`respondErr` actually emit in `internal/mtwire/http.go`, which both the backend (mtwire-handlers) and frontend (`ApiResponse<T>` at `web/default/src/features/agent-earnings/types.ts:53`) agree on:

```jsonc
// Success — respondOK(c, data), HTTP 200 ALWAYS (http.go:80-82)
{ "success": true, "message": "", "data": <object|array> }

// Error — respondErr(c, err), HTTP = apperr.HTTPStatusOf(err) (http.go:85-91)
{ "success": false, "message": "<human>", "code": "<STABLE_CODE>" }
```

Asymmetry is intentional and must be preserved: success has `data` + NO `code`; error has `code` + NO `data`. Frontend branches on `success` (lib/api.ts interceptor).

**Pagination decision (resolves a reader conflict):** `doc/api-contract.md` describes a FLATTENED `{ data, total, page, page_size }`, but `respondOK` structurally can only place a single value under `data` (mtwire-handlers: "NO PAGINATION convention exists"). We therefore nest pagination INSIDE `data`:

```jsonc
"data": { "items": [ ... ], "total": <int>, "page": <int>, "page_size": <int> }
```

**Export decision:** export is a **query param `?format=csv|pdf`** on the two `detail` endpoints (not separate endpoints). When `format` is present the handler streams a file with `Content-Type: text/csv` or `application/pdf` and `Content-Disposition: attachment; filename=...`. This is the ONE documented exception to the JSON envelope. Both CSV and PDF stacks are greenfield (existing-stats: "No CSV or PDF export utility exists anywhere").

### Common request params

| param | type | applies to | notes |
|---|---|---|---|
| `start_timestamp` | int64 (epoch seconds, UTC) | all | mirrors native `/api/data` (`controller/usedata.go`) |
| `end_timestamp` | int64 (epoch seconds, UTC) | all | must be ≥ start; range ≤ 366 days else `STATS_RANGE_INVALID` |
| `granularity` | string `day\|week\|month` | trend | invalid → `REPORT_GRANULARITY_INVALID` |
| `lens` | string `earnings\|recharge\|consumption\|withdrawals` | trend, detail | invalid → `REPORT_LENS_INVALID` |
| `sort_by` | string | admin ranking | one of the numeric DTO fields below |
| `order` | string `asc\|desc` (default `desc`) | admin ranking | |
| `page`, `page_size` | int (default 1 / 20) | ranking, detail | |
| `format` | string `csv\|pdf` | detail only | absent → JSON; bad value → `REPORT_FORMAT_INVALID` |

`source_type` enum (FROZEN, `internal/agent/model.go:68-81`): `recharge_spread`, `consume_commission`, `tokenplan_spread`, `tokenplan_commission`, `manual_adjustment`.
Withdrawal `status` enum (FROZEN, `model.go:152-159`): `pending`, `approved`, `rejected` (NO `withdrawn`/`frozen`/`paid` — those are wallet amounts, not statuses).

---

### 1.1 `GET /api/admin/finance/summary`
- **Auth/middleware:** `router.Group("/api/admin/finance").Use(middleware.AdminAuth())` — global, cross-tenant (NOT Host-scoped).
- **Params:** `start_timestamp`, `end_timestamp`.
- **Response:**
```jsonc
{ "success": true, "message": "", "data": {
  "range": { "start_timestamp": 1750000000, "end_timestamp": 1752000000 },
  "earnings": {
    "total_earned_cny": 1234.56,
    "by_source": [
      { "source_type": "consume_commission",   "amount_cny": 800.00 },
      { "source_type": "tokenplan_spread",      "amount_cny": 400.00 },
      { "source_type": "tokenplan_commission",  "amount_cny": 0 },
      { "source_type": "recharge_spread",       "amount_cny": 0 },   // phantom, always 0 today
      { "source_type": "manual_adjustment",     "amount_cny": 34.56 } // may be negative
    ],
    "wallet_total": {
      "withdrawable_cny": 500.00, "frozen_cny": 100.00,
      "total_earned_cny": 1234.56, "api_balance_usd": 0
    }
  },
  "recharge": {
    "recharge_paid_cny": 5000.00,        // SUM(payment_orders.actual_paid) WHERE type='recharge' AND status='credited'
    "subscription_paid_cny": 2000.00,    // SUM(pending_subscription_orders.retail_price) on activated SUB
    "subscription_cost_cny": 1500.00,    // SUM(pending_subscription_orders.agent_cost_price)
    "subscription_spread_cny": 500.00,   // FALLBACK: SUM(agent_earning_logs.amount) WHERE source_type='tokenplan_spread'
    "recharge_spread_cny": 0             // never emitted (recharge.go:61-66 TODO)
  },
  "consumption": {
    "used_quota": 123456789, "used_cost_usd": 246.91, "used_cost_cny": 1802.46,
    "calls": 4567, "tokens": 9876543
  },
  "withdrawals": {
    "pending_cny": 100.00,   // SUM(agent_withdrawals.amount) status=pending  (== wallet frozen by invariant)
    "frozen_cny": 100.00,    // SUM(agent_wallets.frozen_withdraw_amount)
    "withdrawn_cny": 900.00, // SUM(agent_withdrawals.amount) status=approved  (paid out, gone)
    "rejected_cny": 50.00
  },
  "exchange": { "quota_per_unit": 500000, "usd_exchange_rate": 7.3 }
}}
```

### 1.2 `GET /api/admin/finance/trend`
- **Auth:** `AdminAuth()` (global).
- **Params:** `start_timestamp`, `end_timestamp`, `granularity`, `lens`.
- **Response:** series points carry lens-specific metric fields; `bucket` is a calendar label, `bucket_ts` is epoch-seconds of the bucket start (for VChart time axis).
```jsonc
{ "success": true, "message": "", "data": {
  "lens": "earnings", "granularity": "day",
  "series": [
    { "bucket": "2026-06-28", "bucket_ts": 1750032000, "amount_cny": 123.45 }
  ]
}}
```
Per-lens series point fields:
- `earnings`: `amount_cny`
- `recharge`: `recharge_paid_cny`, `subscription_paid_cny`, `subscription_cost_cny`, `subscription_spread_cny`
- `consumption`: `used_quota`, `used_cost_cny`, `calls`, `tokens`
- `withdrawals`: `pending_cny`, `withdrawn_cny`, `rejected_cny`

Bucket label format: day `YYYY-MM-DD`; week `YYYY-MM-DD` (Monday week-start, UTC); month `YYYY-MM`.

### 1.3 `GET /api/admin/finance/agents` (per-agent / per-tenant ranking, sortable)
- **Auth:** `AdminAuth()` (global, groups by `tenant_id`).
- **Params:** `start_timestamp`, `end_timestamp`, `sort_by`, `order`, `page`, `page_size`.
- **Response:**
```jsonc
{ "success": true, "message": "", "data": {
  "items": [
    {
      "tenant_id": 12, "agent_name": "Acme", "owner_user_id": 345, "owner_username": "acme_admin",
      "total_earned_cny": 1234.56,
      "consume_commission_cny": 800.00, "tokenplan_spread_cny": 400.00, "manual_adjustment_cny": 34.56,
      "recharge_paid_cny": 5000.00, "subscription_paid_cny": 2000.00, "subscription_cost_cny": 1500.00,
      "consumption_used_quota": 123456789, "consumption_cost_cny": 1802.46,
      "withdrawn_cny": 900.00, "pending_withdraw_cny": 100.00, "withdrawable_cny": 500.00
    }
  ],
  "total": 37, "page": 1, "page_size": 20,
  "sort_by": "total_earned_cny", "order": "desc"
}}
```
`sort_by` ∈ {`total_earned_cny`, `recharge_paid_cny`, `subscription_paid_cny`, `consumption_cost_cny`, `consumption_used_quota`, `withdrawn_cny`, `pending_withdraw_cny`, `withdrawable_cny`}.

### 1.4 `GET /api/admin/finance/detail`
- **Auth:** `AdminAuth()` (global).
- **Params:** `start_timestamp`, `end_timestamp`, `lens` (required), `page`, `page_size`, optional `format=csv|pdf`.
- **Response (JSON, `lens=earnings`):**
```jsonc
{ "success": true, "message": "", "data": {
  "items": [
    { "tenant_id": 12, "agent_name": "Acme", "source_type": "consume_commission",
      "amount_cny": 0.12, "reference": "req_abc", "created_at": "2026-06-28T12:00:00Z" }
  ],
  "total": 980, "page": 1, "page_size": 20
}}
```
Per-lens `items[]` shapes:
- `earnings`: `{ tenant_id, agent_name, source_type, amount_cny, reference, created_at }` (mirrors `earningOut` agent.go:658-688 + tenant cols)
- `withdrawals`: `{ id, tenant_id, agent_name, amount_cny, status, created_at, reviewed_at }` (= `withdrawalOut` agent.go:380-388)
- `recharge`: `{ order_no, tenant_id, kind: "recharge"|"subscription", provider, amount_usd, actual_paid_cny, agent_cost_price_cny, status, created_at }`
- `consumption`: `{ tenant_id, agent_name, model_name, calls, tokens, used_quota, used_cost_cny }`
- **Response (`?format=csv`):** `Content-Type: text/csv`, `Content-Disposition: attachment; filename="finance-<lens>-<start>-<end>.csv"`, first row = header matching the JSON field names.
- **Response (`?format=pdf`):** `Content-Type: application/pdf`, attachment.

### 1.5 `GET /api/tenant/finance/summary`
- **Auth:** registered on the existing `agentSelf` subgroup (`tenantGroup.Group("", middleware.UserAuth(), app.AgentOwnerAuth())`, mt-router.go:66). `tenant_id` from `agentTenantID(c)` only; `tenantID <= 0 → respondErr(c, errAgentForbidden)` (AGENT_FORBIDDEN/403).
- **Params:** `start_timestamp`, `end_timestamp`.
- **Response:** identical body shape to §1.1 but for the single tenant (no cross-tenant rollup). `wallet_total` becomes the tenant's single `agent_wallets` row.

### 1.6 `GET /api/tenant/finance/trend`
- **Auth:** `agentSelf` (UserAuth + AgentOwnerAuth), `tenant_id` from ctx.
- **Params:** `start_timestamp`, `end_timestamp`, `granularity`, `lens`.
- **Response:** identical to §1.2, scoped to the caller's tenant.

### 1.7 `GET /api/tenant/finance/detail`
- **Auth:** `agentSelf`, `tenant_id` from ctx.
- **Params:** `start_timestamp`, `end_timestamp`, `lens`, `page`, `page_size`, optional `format=csv|pdf`.
- **Response:** identical to §1.4 but rows omit `tenant_id`/`agent_name` (single-tenant) and are filtered to the caller's tenant. Export streams CSV/PDF for that tenant only.

---

## 2. Data sources per lens

All money columns persist as `decimal(20,8)` and cross Go interfaces as `float64`. Aggregation queries below DO NOT EXIST yet — every `SUM`/`GROUP BY` is greenfield (earnings-ledger, withdrawals, existing-stats all confirm only flat `ORDER BY created_at desc` list methods exist).

### Lens (a) — Agent earnings/commission by source + wallet balances → **CNY**
- **Tables:** `agent_earning_logs` (`earningRow`, `internal/agent/gormrepo/gormrepo.go:58-70`) and `agent_wallets` (`walletRow`, gormrepo.go:44-54).
- **Keying:** `tenant_id` (indexed `idx_agent_earnings_tenant`; `agent_wallets.tenant_id` is PK).
- **Aggregation (by source):**
  ```sql
  SELECT source_type, SUM(amount) AS amount_cny
  FROM agent_earning_logs
  WHERE created_at BETWEEN :start_dt AND :end_dt   -- created_at is DATETIME; convert epoch→time.Unix
    /* agent: */ AND tenant_id = :tid
  GROUP BY source_type;
  ```
- **Wallet balances:** `AgentService.GetWallet(tenantID)` (`internal/agent/service.go`) → `withdrawable_balance`, `frozen_withdraw_amount`, `total_earned`, `api_balance` (read-only). For admin platform total, `SUM` the columns across all `agent_wallets`. `GetWallet` returns a zero-value wallet (no error) when missing → report shows 0, not 404.
- **Caveats:** `recharge_spread` bucket is always 0/empty (never written). `manual_adjustment.amount` may be NEGATIVE (model.go:105) → totals are not monotonic. `agent_wallets.total_earned` is a denormalized running Σ that should reconcile with `SUM(agent_earning_logs.amount)`.

### Lens (b) — Recharge/order paid/cost/spread per tenant → **CNY** (+ credited `amount_usd`)
No single table yields paid/cost/spread (recharge-orders: "CANNOT get a clean per-tenant paid/cost/spread triple from any single order table").
- **Recharge paid:** `payment_orders` (`orderRow`, `internal/payment/gormrepo/gormrepo.go`):
  ```sql
  SELECT tenant_id, SUM(actual_paid) AS recharge_paid_cny
  FROM payment_orders
  WHERE type='recharge' AND status='credited' AND created_at BETWEEN :s AND :e
    /* agent: */ AND tenant_id=:tid
  GROUP BY tenant_id;
  ```
  No `agent_cost`/`spread` columns exist here.
- **Subscription paid + cost:** `pending_subscription_orders` (`pendingRow`, `internal/tokenplan/gormrepo/gormrepo.go:112-122`, the ONLY per-order row with both `retail_price` and `agent_cost_price`) JOIN `mt_subscription_orders` (`subscriptionOrderRow`, `internal/mtwire/subscription_bridge.go:60-71`) for completion status:
  ```sql
  SELECT p.tenant_id,
         SUM(p.retail_price)     AS subscription_paid_cny,
         SUM(p.agent_cost_price) AS subscription_cost_cny
  FROM pending_subscription_orders p
  JOIN mt_subscription_orders o ON o.order_no = p.order_id
  WHERE o.status='activated' AND o.created_at BETWEEN :s AND :e
    /* agent: */ AND p.tenant_id=:tid
  GROUP BY p.tenant_id;
  ```
- **Spread — DOCUMENTED FALLBACK to earnings ledger:** the realized spread is authoritative in `agent_earning_logs` (`source_type='tokenplan_spread'`, `source_id=order_no`). `recharge_spread` is a phantom source (no writer). So `subscription_spread_cny = SUM(agent_earning_logs.amount WHERE source_type='tokenplan_spread')` and `recharge_spread_cny` is hard-coded 0.
- **Keying:** `tenant_id` on all three tables.
- **Excluded:** legacy native `topups` table (`model/topup.go`) — has NO `tenant_id`, NOT attributable; out of scope.

### Lens (c) — Consumption/usage cost per tenant via raw `logs`→`users.tenant_id` → **CNY**
- **Tables:** native `logs` (`model.Log`, table `logs`, NO `TableName()` override) JOIN `users`. `logs` has NO tenant column — only `user_id` links out (usage-logs-tenant-join). `users.tenant_id` exists only as a raw-ALTER DB column (`migrateUsersTenantID`, agent.go:49-64), NOT in `model.User`.
- **Precise JOIN sketch (same-DB path, MySQL):**
  ```sql
  SELECT
    u.tenant_id                                  AS tenant_id,
    DATE_FORMAT(FROM_UNIXTIME(l.created_at), :bucket_fmt) AS bucket,   -- day '%Y-%m-%d', month '%Y-%m'
    SUM(l.quota)                                 AS used_quota,
    COUNT(*)                                     AS calls,
    SUM(l.prompt_tokens + l.completion_tokens)   AS tokens
  FROM logs l
  JOIN users u ON u.id = l.user_id
  WHERE l.type = 2                                -- LogTypeConsume (model/log.go:87); EXCLUDE topup/manage/etc.
    AND u.deleted_at IS NULL                      -- raw Table() bypasses GORM soft-delete (distribution.go:204)
    AND l.created_at >= :start_ts AND l.created_at <= :end_ts   -- created_at is UNIX seconds int64
    /* agent:  */ AND u.tenant_id = :tid
    /* admin:  */ AND u.tenant_id <> 0            -- exclude main-site / unattributed
  GROUP BY u.tenant_id, bucket
  ORDER BY bucket ASC;
  ```
  (week: compute Monday week-start via `FROM_UNIXTIME` + `WEEKDAY`; label `YYYY-MM-DD`.)
- **quota→CNY formula (real constants):**
  `used_cost_usd = quota / common.QuotaPerUnit` and `used_cost_cny = used_cost_usd * operation_setting.USDExchangeRate`
  where `common.QuotaPerUnit = 500000` (`common/constants.go:62`) and `operation_setting.USDExchangeRate = 7.3` default (`setting/operation_setting/payment_setting_old.go:18`). Read both as LIVE package vars at query time (DB-backed runtime options). This is exactly `consumeCommissionCNY` with `ratio=1` (`internal/mtwire/agent.go:216-222`), so report cost reconciles with `consume_commission` earnings. Guard non-positive → 0. DO NOT route through display-type formatters (`logger.LogQuota`, FE `formatQuotaWithCurrency`) — they emit USD/tokens unless site display type is CNY.
- **Index to ADD:** `idx_logs_user_created (user_id, created_at)` (current `idx_user_id_id=(user_id,id)` does NOT cover a `created_at` range). Add via idempotent `information_schema`-guarded raw `ALTER` (same pattern as `migrateUsersTenantID` agent.go:49-64) hung in `App.Migrate()` (wire.go ~228-234). NEVER edit `model.Log`/`model.User` struct tags.

### Lens (d) — Withdrawals (withdrawn/frozen/pending) → **CNY**
- **Tables:** `agent_withdrawals` (`withdrawalRow`, gormrepo.go:74-86, indexed `idx_agent_withdrawals_tenant`, `idx_agent_withdrawals_status`) and `agent_wallets.frozen_withdraw_amount`.
- **Aggregation:**
  ```sql
  SELECT status, SUM(amount) AS amount_cny
  FROM agent_withdrawals
  WHERE created_at BETWEEN :s AND :e  /* agent: */ AND tenant_id=:tid
  GROUP BY status;
  ```
- **Mapping (FROZEN, statuses are exactly pending/approved/rejected):**
  - `pending_cny` = SUM(status=`pending`) — in-flight; equals `agent_wallets.frozen_withdraw_amount` by money-conservation invariant.
  - `withdrawn_cny` = SUM(status=`approved`) — paid out offline, gone from system (do NOT add back to withdrawable).
  - `frozen_cny` = `SUM(agent_wallets.frozen_withdraw_amount)` (== `pending_cny` by invariant; both exposed for reconciliation).
  - `rejected_cny` = SUM(status=`rejected`).
- **Keying:** `tenant_id`.

---

## 3. Money & time representation (ONE rule, applied everywhere)

**Money on the wire = plain JSON number (`float64`), currency encoded by field-name suffix.** NOT integer cents, NOT decimal string. (earnings-ledger, mtwire-handlers, api-contract-doc all converge: existing DTOs pass `withdrawable_cny`/`amount_cny`/`used_usd` as raw float64; doc examples are bare numbers like `119`, `220`.)
- `_cny` suffix → CNY ¥ (earnings, spread, recharge paid, withdrawals, consumption cost).
- `_usd` suffix → USD (credited quota value, optional consumption cost in USD).
- Raw quota/token/call counts → exact integers, fields `used_quota`, `tokens`, `calls`, `quota_per_unit`.
- **Never sum `_usd` and `_cny` into one field.**
- **Precision:** round `_cny`/`_usd` aggregates to 2 decimals at the handler edge (consistent with `usagePct` round2, http.go:597); keep integer counts exact. Do NOT inherit the `*USD` field names from `stats.AdminStats.TotalEarningUSD` — the underlying `agent_earning_logs.amount` is CNY (the documented unit-naming hazard).

**Time:**
- **Request range:** epoch seconds (UTC) via `start_timestamp` / `end_timestamp` (int64), mirroring native `/api/data` (`controller/usedata.go`). Convert to `time.Time` (`time.Unix(s,0).UTC()`) for the DATETIME tables; use directly against `logs.created_at` (epoch).
- **Response timestamps:** ISO-8601 UTC RFC3339 via `isoUTC()` (`internal/mtwire/http.go:615`), e.g. `2026-06-28T12:00:00Z`; zero time → `""`.
- **Trend buckets:** `bucket` = calendar label (day `YYYY-MM-DD`, week `YYYY-MM-DD` Monday-start, month `YYYY-MM`); `bucket_ts` = epoch seconds (UTC) of bucket start for the chart axis. Compute all four lenses on a UTC calendar basis (use `FROM_UNIXTIME` for `logs`) so buckets align across lenses. MySQL-only SQL must be dialect-guarded via `common.UsingMainDatabase(common.DatabaseTypeMySQL)` (existing-stats / `model/usedata_rankings.go:51`).
- **Range guard:** reject `start > end` or span > 366 days with `STATS_RANGE_INVALID` (400). (Native 31-day cap is too small for a finance report.)

---

## 4. Backend implementation map

### Files to ADD
- `internal/report/reportrepo/reportrepo.go` — concrete `Repo struct { db *gorm.DB }`; constructor `New(db *gorm.DB) *Repo`. Raw `db.Table("agent_earning_logs")` / `Table("payment_orders")` / `Table("logs").Joins(...)` aggregate queries (raw Table() reads, never importing sibling model structs — matches the upstream-rebase-safe convention agent.go:46-48). Methods:
  - `SummaryEarnings(ctx, tenantID *int64, s, e int64) ([]SourceSum, error)` — `SUM(amount) GROUP BY source_type`.
  - `WalletTotals(ctx, tenantID *int64) (WalletAgg, error)`.
  - `RechargePaid(ctx, tenantID *int64, s, e int64) (map[int64]float64, error)`.
  - `SubscriptionPaidCost(ctx, tenantID *int64, s, e int64) (map[int64]PaidCost, error)`.
  - `ConsumptionCost(ctx, tenantID *int64, s, e int64) (...)` and `ConsumptionTrend(...granularity...)`.
  - `Withdrawals(ctx, tenantID *int64, s, e int64) (map[string]float64, error)`.
  - `TrendEarnings/TrendRecharge/TrendWithdrawals(ctx, tenantID *int64, s, e int64, granularity string)`.
  - `AgentRanking(ctx, s, e int64, sortBy, order string, page, pageSize int) ([]AgentRankRow, int64, error)`.
  - `DetailEarnings/DetailWithdrawals/DetailRecharge/DetailConsumption(ctx, tenantID *int64, s, e int64, page, pageSize int)`.
  - `tenantID *int64`: `nil` = admin/global (cross-tenant), non-nil = agent scope.
- `internal/report/reportrepo/migrate.go` — `AutoMigrate(db *gorm.DB) error`: idempotent `information_schema`-guarded raw `ALTER` adding `idx_logs_user_created (user_id, created_at)` and `idx_agent_earnings_tenant_created (tenant_id, created_at)`.
- `internal/mtwire/report.go` — 7 handlers (`func (a *App) HandleAdminFinanceSummary/Trend/Agents/Detail`, `HandleTenantFinanceSummary/Trend/Detail`), snake_case DTOs, param parsing (`strconv.ParseInt(c.Query("start_timestamp"),...)`, `c.Query("granularity")`, etc.), and new `apperr` error-code package vars.
- `internal/mtwire/report_export.go` — CSV via std `encoding/csv`; PDF via a vendored Go lib (pick one — `pdfcpu`/`gofpdf`; greenfield). Streams to `c.Writer` with `Content-Disposition`.

### Files to MODIFY
- `internal/mtwire/wire.go` — add `ReportRepo *reportrepo.Repo` field to `App` (struct at wire.go:41-97, follow the concrete-`*Repo` precedent for aggregate methods); construct in `New(db)`; chain `reportrepo.AutoMigrate(db)` in `App.Migrate()` near `migrateUsersTenantID` (wire.go:228) / line ~234.
- `router/mt-router.go` — register routes inside `SetMtRouter`:
  - Admin: `financeAdmin := router.Group("/api/admin/finance"); financeAdmin.Use(middleware.AdminAuth())` then `financeAdmin.GET("/summary"|"/trend"|"/agents"|"/detail", app.Handle...)`. (Global; do NOT add `TenantMiddleware` — ranking is cross-tenant.)
  - Agent: add to the existing `agentSelf` subgroup (mt-router.go:66): `agentSelf.GET("/finance/summary"|"/finance/trend"|"/finance/detail", app.Handle...)`.
- `doc/api-contract.md` — register the new error codes in §1.1 and add the endpoints to §2 (W5 doc-sync discipline).

### How each handler gets `tenant_id`
- **Admin handlers:** no tenant scope; `ctx := reqCtx(c)`, call repo with `tenantID = nil`. Consumption excludes `tenant_id=0`; ranking `GROUP BY tenant_id`.
- **Agent handlers:** `tenantID := agentTenantID(c); if tenantID <= 0 { respondErr(c, errAgentForbidden); return }` (the authoritative DB-verified guard, agent.go:323-330). Pass `&tenantID`. NEVER read tenant from body/query.
- Always thread `ctx := reqCtx(c)` (not raw `c.Request.Context()`) so `appctx.TenantID(ctx)` is available.

### New error codes to register (mtwire `apperr.New(...)` package vars + `doc/api-contract.md §1.1`)
- Reuse existing: `STATS_RANGE_INVALID` (400, already registered), `AGENT_FORBIDDEN` (403, `errAgentForbidden`).
- Add: `REPORT_GRANULARITY_INVALID` (400), `REPORT_LENS_INVALID` (400), `REPORT_FORMAT_INVALID` (400), `REPORT_EXPORT_FAILED` (500). Module-prefixed per the §1.1 hard rule.

---

## 5. Frontend implementation map

Mirror `web/default/src/features/agent-earnings`. New feature dir name: **`web/default/src/features/financial-report/`**.

### Files to ADD (feature)
- `features/financial-report/api.ts` — `import { api } from '@/lib/api'`; thin async fns returning `Promise<ApiResponse<T>>` (`return res.data`), snake_case params:
  `getAdminFinanceSummary(p)`, `getAdminFinanceTrend(p)`, `getAdminFinanceAgents(p)`, `getAdminFinanceDetail(p)`, `getTenantFinanceSummary(p)`, `getTenantFinanceTrend(p)`, `getTenantFinanceDetail(p)`. Export download: `api.get(url, { params, responseType:'blob', skipBusinessError:true })` then trigger a Blob download (greenfield helper).
- `features/financial-report/types.ts` — `FinanceSummary`, `TrendPoint`, `AgentRankRow`, detail item types, plus reuse `ApiResponse<T>`. Money fields `*_cny`/`*_usd: number`; `bucket: string`, `bucket_ts: number`.
- `features/financial-report/lib/index.ts` — `cny()`/`usd()` formatters, `formatDateTime()` (tolerant unix s/ms/ISO via `@/lib/dayjs`), `sourceTypeLabel()`, bucket/granularity helpers.
- `features/financial-report/admin.tsx` — exports `function AdminFinancialReport()` (platform page).
- `features/financial-report/agent.tsx` — exports `function AgentFinancialReport()` (tenant page).
- `features/financial-report/components/*.tsx` — shared: `summary-cards.tsx` (KPI), `trend-chart.tsx` (VChart), `breakdown-by-source.tsx`, `detail-table.tsx`, `agent-ranking-table.tsx` (admin-only, sortable), `export-buttons.tsx`. Use `@/components/ui/*` (table/button/select), `@/components/status-badge`, `lucide-react` icons, `sonner` toasts; wrap pages in `SectionPageLayout` (`.Title/.Actions/.Content`). Add `data-testid` for E2E.

### Route files under `routes/_authenticated/`
- `routes/_authenticated/finance-report/index.tsx` (AGENT): `createFileRoute('/_authenticated/finance-report/')({ beforeLoad, component: AgentFinancialReport })`; `beforeLoad` does `queryClient.fetchQuery(agentContextQueryOptions)` and `throw redirect({ to:'/403' })` if `!is_agent_owner` (mirror agent-earnings route).
- `routes/_authenticated/admin/finance-report/index.tsx` (ADMIN): `beforeLoad` gates on `role < ROLE.ADMIN → /403` (mirror `routes/_authenticated/withdrawals/index.tsx`), `component: AdminFinancialReport`.

### Sidebar nav (`hooks/use-sidebar-data.ts`)
- Agent entry (personal group, next to 'My Earnings'): `{ title: t('Financial Report'), url: '/finance-report', icon: ChartColumnBig (lucide), agentOwnerOnly: true }`.
- Admin entry (admin group): `{ title: t('Financial Report'), url: '/admin/finance-report', icon: ChartColumnBig, requiredRole: ROLE.ADMIN }`.
- **Single-writer rule:** both entries are added in ONE edit to avoid a write conflict (see §6).

### api.ts shape (the shared client)
Calls go through the single shared `api` axios instance (`web/default/src/lib/api.ts:42`): cookie auth + injected `New-Api-User` header (required — cookie alone fails authHelper), GET dedup, `{success:false}` interceptor toast. Use `useQuery` with array `queryKey`s (`['admin-finance-summary', params]`, `['tenant-finance-trend', params]`, …), `placeholderData:(prev)=>prev`, `invalidateQueries` to refresh.

### Chart library
**VChart** (`@visactor/react-vchart`) + `@/lib/use-chart-theme.ts` (lazy `ThemeManager`, light/dark). Mirror `features/dashboard/components/flow/flow-charts.tsx`. **Do NOT use recharts** (dead code inside `components/ui/chart.tsx`). This is the first feature-level chart consumer of the report.

### Locale files
Use English source strings as keys and keep `en.json` as the base catalog. Locale JSON files must never be edited directly: populate all six locales (`en/zh/fr/ja/ru/vi`) through the project `scripts/add-missing-keys.mjs` workflow, then run `bun run i18n:sync` from `web/default`. The sync gate requires every source key in every locale and rejects missing, extra, placeholder-mismatched, untranslated, and stale allowlist entries.

### License header + route-tree regen
- Every new `.ts`/`.tsx` file MUST carry the exact 18-line QuantumNous AGPL header; verify with `bun run copyright:check` (add with `bun run copyright`).
- `routeTree.gen.ts` is auto-generated by the rsbuild `tanstackRouter` plugin (`rsbuild.config.ts`) on `bun run dev`/`build` — NEVER hand-edit; just run dev/build to register the two new routes.

---

## 6. Work split for parallel agents

Disjoint file ownership, ordered by dependency. "Contract" = this document (already frozen), so frontend and backend can both start against it immediately.

| Slice | Owns (files) | Depends on | Parallelism |
|---|---|---|---|
| **S1 — BE aggregation/repo** | `internal/report/reportrepo/reportrepo.go`, `internal/report/reportrepo/migrate.go` | contract only | **Start immediately, fully parallel** |
| **S2 — BE handlers + wiring + routes** | `internal/mtwire/report.go`, MODIFY `internal/mtwire/wire.go`, MODIFY `router/mt-router.go`, MODIFY `doc/api-contract.md` | S1 method signatures (agree up front) | Develop DTOs/handlers in parallel with S1; **integrate after S1** |
| **S3 — BE export** | `internal/mtwire/report_export.go` | S2 DTO shapes | Parallel with S2 after DTOs frozen |
| **S4 — FE shared scaffolding** | `features/financial-report/api.ts`, `types.ts`, `lib/index.ts` | contract only | **Start immediately, fully parallel with S1/S2** |
| **S5 — FE admin page + nav + route** | `features/financial-report/admin.tsx`, `features/financial-report/components/*`, `routes/_authenticated/admin/finance-report/index.tsx`, **MODIFY `hooks/use-sidebar-data.ts` (BOTH nav entries)** | S4 | After S4; owns shared components + sidebar |
| **S6 — FE agent page + route** | `features/financial-report/agent.tsx`, `routes/_authenticated/finance-report/index.tsx` | S4, and consumes S5's shared `components/*` | After S4/S5 component contracts |
| **S7 — i18n** | Apply all six locale values through the script-only workflow, then run `bun run i18n:sync` | S5/S6 string keys collected | **Last; single writer** (i18n:sync rewrites all locales) |

Notes:
- **Fully parallel from t0:** S1 and S4 (no shared files, contract-only).
- **Must wait:** S2 needs S1's repo signatures (lock these in a 10-minute kickoff); S3 needs S2's DTOs; S6 reuses S5's shared `components/*` (S5 owns/creates them); S7 runs last because `i18n:sync` touches every locale file.
- **Single-writer files (one owner each, to avoid merge conflicts):** `hooks/use-sidebar-data.ts` → S5 only; `internal/mtwire/wire.go` + `router/mt-router.go` → S2 only; locale JSON → S7 only. `routeTree.gen.ts` is owned by nobody (auto-generated).

---

## 7. Open risks / decisions made

**Decisions that resolved reader conflicts (re-check if assumptions change):**
1. **Envelope:** chose the ACTUAL code shape `{success, message, data}` / `{success, message, code}` (`http.go:80-91`) over `doc/api-contract.md`'s abstract `{code,message}`/`{data}`. Frontend `ApiResponse<T>` agrees.
2. **Pagination nested in `data`** (`data:{items,total,page,page_size}`) because `respondOK` can only place one value under `data`; this contradicts `doc/api-contract.md`'s FLATTENED `{data,total,page,page_size}`. Implementer must NOT expect sibling keys.
3. **Money = plain JSON float64 with `_cny`/`_usd` suffix**, rounded to 2 decimals. The contract never states cents-int vs decimal-string (api-contract-doc: "undocumented decision point") — pinned here. If a downstream consumer needs exact decimals, this is the field to revisit.
4. **Request range params = epoch seconds** (`start_timestamp`/`end_timestamp`, native `/api/data` style), while response timestamps are ISO-8601. There is NO existing date-range query convention (api-contract-doc) — this is net-new.
5. **Recharge spread = earnings-ledger fallback** (`tokenplan_spread`); `recharge_spread` hard-coded 0 (phantom source, recharge.go:61-66 TODO). If/when an agent recharge cost/markup field is added, the `recharge_spread_cny` field becomes real with no schema change.
6. **Admin ranking is global** (`AdminAuth` only, no `TenantMiddleware`) and groups by `tenant_id`. If product wants a Host-scoped single-tenant admin view, prepend `app.TenantMiddleware()` (precedent: `/api/admin/subscriptions`).
7. **Admin route gate uses `ROLE.ADMIN`** (matching backend `AdminAuth`=`RoleAdminUser` and the `withdrawals` route gate). The sidebar `withdrawals` admin entry uses `ROLE.SUPER_ADMIN` (frontend reader, use-sidebar-data.ts:257) — a mismatch; we standardize on `ROLE.ADMIN` for both route and sidebar. Re-check desired admin tier with product.

**Inferred-not-confirmed facts (implementer must verify):**
- **`payment_orders.created_at` / `mt_subscription_orders.created_at` type:** assumed `time.Time` DATETIME (consistent with other internal tables). Recharge-orders reader listed `created_at` without a type. Verify before writing range filters (epoch→`time.Unix` conversion depends on it).
- **Consumption JOIN is only valid when `LOG_DB == DB`** (same MySQL schema). usage-logs-tenant-join: `LOG_DB` may be ClickHouse or a separate DB, in which case a single `logs JOIN users` is impossible — fall back to two-step (read `user_id` set from `users` via `idx_users_tenant`, then aggregate `logs WHERE user_id IN (...)` on `LOG_DB`). The repo MUST gate on `LOG_DB==DB` (and dialect) at runtime.
- **`tenant_id=0` double meaning:** 0 = main-site user OR attribution lookup-miss (`userTenantID` returns 0 on any error). Excluding `tenant_id=0` also drops silently-unattributed users. Additionally, an agent-owner's OWN `users.tenant_id` is often 0 even though they own a tenant (`moderationTenantID` resolves owner→tenant via `tenants.owner_user_id`). Decide whether an agent's own personal consumption should count under their tenant — currently it will NOT.
- **USD vs CNY reconciliation:** `tenant_billing_logs` stores USD; `agent_earning_logs` stores CNY. This contract sources the consumption lens from raw `logs` (quota→CNY) and earnings from `agent_earning_logs` (CNY), so the report stays CNY-consistent; it deliberately does NOT mix in `tenant_billing_logs.charged_usd`. If a future "gross profit" figure is wanted, the USD/CNY reconciliation must be designed explicitly.
- **Week-bucket alignment:** epoch `logs` and DATETIME ledgers must use the SAME calendar week definition (Monday-start UTC) or buckets won't align across lenses. The contract mandates `FROM_UNIXTIME`-based calendar bucketing for `logs` to match the ledgers — verify the resulting `bucket_ts` aligns.
- **`manual_adjustment` negatives** and **silent OnConflict-dedup** mean ledger-derived totals are de-duplicated but non-monotonic; report UI must tolerate negative source buckets and downward movement.
- **PDF library is unchosen** (no Go PDF dep vendored). S3 must pick and vendor one; treat as the highest-uncertainty backend task.
- **`STATS_CROSS_TENANT` (403)** exists in `doc/api-contract.md §1.1` but the actual agent guard returns `AGENT_FORBIDDEN`. We use `AGENT_FORBIDDEN` for the agent endpoints (matches real `errAgentForbidden`); `STATS_CROSS_TENANT` is not emitted by these handlers.
- **Reject-reason field bug** (withdrawals reader: backend binds `{remark}`, FE posts `{reason}`) is pre-existing and out of scope, but if the detail/export surfaces a withdrawal rejection reason it will be empty.

---

## 8. Post-implementation review outcome (2026-07-01)

Implemented + adversarially reviewed (7 reviewers grounding findings against real symbols). Result: **no backend symbol/SQL/contract defects, no frontend import/type defects.** Fixes applied:

- **[major · fixed]** `features/financial-report/api.ts` `downloadFinanceDetail` now sets `skipErrorHandler: true` (in addition to `skipBusinessError`) so the export blob path fully bypasses the global axios error interceptor — the component's localized toast is the sole failure handler (was double-toasting a raw axios string). Precedent: `lib/agent-context.ts`.
- **[minor · fixed]** `admin.tsx` + `agent.tsx` now pass `series={trendQuery.data?.lens === lens ? series : []}` so the trend chart only consumes points whose response `lens` matches the active tab (no one-render flash of stale-lens keys on tab switch).

Deferred (tracked, not blocking):

- **[minor] Consumption-trend timezone.** The MySQL same-DB consumption path buckets via `DATE_FORMAT(FROM_UNIXTIME(logs.created_at), …)`, which renders using the MySQL **session** `time_zone`; the DATETIME ledger lenses and all Go-side `bucket_ts` math are strict UTC. If the server session tz ≠ UTC, consumption buckets shift vs the other lenses. **Action:** verify `SELECT @@session.time_zone` is UTC/`+00:00` on the server during build; if not, pin it (DSN `time_zone` param or UTC-aligned integer-epoch bucketing). The non-MySQL/split-LOG_DB path already buckets in Go (UTC-correct).
- **[minor] Split-LOG_DB IN-clause.** The two-step fallback (separate/ClickHouse log DB) issues `user_id IN (…full non-zero-tenant user set…)` for admin scope — can exceed bound-param caps on huge user populations. Not hit on the default same-DB MySQL deployment (uses the JOIN path). Chunk in batches if a split log DB is ever enabled.
- **[out of scope] Pre-existing** duplicate top-level `"failed"` key in `zh.json` (lines 95 & 2011) — not introduced by this feature; all 6 locales parse as valid JSON.

**Still pending:** server-side `go build ./...` + frontend `tsgo -b`/`build` (route-tree regen + typecheck) — no local toolchain, so compilation has not yet been verified.
