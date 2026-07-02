# 代理分层 (agent-tiering, level-driven) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace the decorative agent `type` (normal/oem/api) with a `level`-driven capability gate (0=普通/basic, 1=独立/independent) so that subdomain + custom-domain + site-branding unlock only at level>=1, with admin-manual promotion.
**Architecture:** `agent_profiles.level` becomes the single source of truth for capability gating; the `type` column/enum is deleted and existing rows are backfilled to level=1. A `RequireAgentLevel(1)` gin middleware plus in-handler depth checks gate the independent-site routes; subdomain provisioning moves out of tenant-create (L0 gets none) and into admin promotion (L0→L1). The frontend swaps the type selector for a level selector, derives badges from level, and gates the two independent-site pages behind `level>=1` via an extended `/api/tenant/agent-context`.
**Tech Stack:** Go 1.21+ (gin+GORM), React 19 (web/default)

## Global Constraints
- Go module `github.com/QuantumNous/new-api`; multi-tenant increments under `internal/`.
- Build/migrate/deploy/E2E ON THE SERVER (test stack `newapi_test`, 3100) — local you can run Go unit tests only.
- .ts/.tsx carry a GNU AGPL header (copy from a sibling). No NEW frontend files are created by this plan (guards reuse the existing `/403` route).
- i18n: English key = identity; Chinese in `web/default/src/i18n/locales/zh.json`. Both files nest keys under a top-level `"translation"` object.
- TDD: failing test → run(fail) → minimal impl → run(pass) → commit.
- NAMING TRAP: agent `level` (this feature) ≠ user `tier` (downstream group default/vip, already in use in distribution.go). Keep distinct.
- Deleting `agent_profiles.type` is a destructive schema change — the migration must set every existing agent_profiles.level=1 BEFORE/while dropping type.
- SERIAL / SHARED integration points (coordinate; do not parallelize): **`router/mt-router.go`** (Task 3) and the **`App.Migrate()` chain in `internal/mtwire/wire.go`** (Task 2). `internal/mtwire/agent.go` is touched by Tasks 1/2/4/5 — execute those tasks strictly in order.
- Method-name decision (kept to bound churn): the repo/service methods `SetAgentType` / `GetAgentType` KEEP their names (they persist/read the agent profile) but LOSE the `AgentType` parameter/return. Renaming them was rejected to avoid editing ~12 call sites twice.
- `internal/agent/errors.go` `CodeAgentTypeInvalid` / `ErrAgentTypeInvalid` (`AGENT_TYPE_INVALID`) are RETAINED — they validate `AgentParams` (cost/ratio/discount/level), not the deleted enum. Do not delete them.

---

## File Structure

### Backend — `internal/agent`
- `model.go` — DELETE `AgentType` enum + `Valid()`; ADD `CanAPI bool` to `AgentParams`.
- `model_test.go` — DELETE `TestAgentType_Valid`; keep/extend `TestAgentParams_Validate`.
- `port.go` — drop `AgentType` from `AgentService.SetAgentType` + `AgentRepo.SetAgentType/GetAgentType`; ADD `AgentService.AgentLevel(ctx, tenantID) (int, error)`.
- `service.go` — drop `AgentType` from `SetAgentType`; ADD `AgentLevel` impl.
- `service_test.go` — update signatures; ADD `AgentLevel` tests.
- `repo.go` — `MemRepo`: drop `AgentType` from `SetAgentType/GetAgentType`, drop `agentRecord.t`.
- `repo_test.go` — update `GetAgentType` call.
- `gormrepo/gormrepo.go` — `profileRow`: DROP `Type`, ADD `CanAPI`; update `SetAgentType/GetAgentType/AgentRow/ListProfiles`.
- `gormrepo/gormrepo_test.go` — update `TestSetAgentType_Upsert`; ADD can_api round-trip.

### Backend — `internal/tenant`
- `port.go` — `CreateTenantInput` += `SkipSubdomain bool`; `TenantService` += `EnsureSubdomain(ctx, tenantID int64, slug string) error`.
- `service.go` — `Create` honours `SkipSubdomain`; ADD `EnsureSubdomain` impl.
- `service_test.go` — ADD subdomain-skip + ensure-subdomain-idempotent tests.

### Backend — `internal/mtwire`
- `agent.go` — DTOs (drop `Type`, add `CanAPI`); create/update handlers (level-gated subdomain); `creditConsumeCommission`/`buildAgentOut` (drop type); ADD `RequireAgentLevel` + `ensureAgentLevel` + `errAgentLevelLocked`; `HandleAgentContext` (+level/+can_api); ADD `migrateAgentProfilesDropType`.
- `custom_domain.go` — depth `ensureAgentLevel(c,1)` at handler entry (4 handlers).
- `siteconfig.go` — depth `ensureAgentLevel(c,1)` at handler entry (3 handlers).
- `seed.go` — replace `agent.AgentTypeNormal` arg; demo tenant keeps subdomain.
- `wire.go` — call `migrateAgentProfilesDropType` inside `Migrate()`.
- `agent_gate_test.go` (NEW) — unit test `RequireAgentLevel` red/green.

### Backend — `router`
- `mt-router.go` — move custom-domain + site-config routes into an `agentIndependent` sub-group behind `app.RequireAgentLevel(1)`.

### Frontend — `web/default/src`
- `features/agents/types.ts` — DELETE `agentTypeValues`/`AgentType`; drop `type` from `Agent`/`AgentPayload`.
- `features/agents/lib/agent-form.ts` — drop `type` from schema/defaults/converters; default create `level: 0`.
- `features/agents/lib/index.ts` — replace `agentTypeLabel` → `agentLevelLabel`; drop `AgentType` import.
- `features/agents/components/agent-mutate-drawer.tsx` — remove type selector; convert level field to 普通/独立 select; drop `type` from submit.
- `features/agents/components/agents-columns.tsx` — remove `type` column; render `level` as a derived badge.
- `lib/agent-context.ts` — `AgentContext` becomes `{ is_agent_owner, level, can_api }`; fail-closed default.
- `components/layout/types.ts` — `BaseNavItem` += `agentLevelMin?: number`.
- `hooks/use-sidebar-data.ts` — add `agentLevelMin: 1` to Custom Domain + Site Branding items.
- `hooks/use-sidebar-view.ts` — read owner+level from the object; filter items by `agentLevelMin`.
- `routes/_authenticated/custom-domain/index.tsx` + `site-branding/index.tsx` — guard on owner **and** level>=1.
- 11 owner-only route guards — update destructure to the object shape (mechanical).
- `i18n/locales/en.json` + `zh.json` — new keys.

---

### Task 1: Additive schema — `can_api`, `AgentLevel` helper, DTO plumbing (compiles, keeps `type`)

Purely additive so the tree keeps compiling and all existing tests stay green. No destructive change here.

**Files:**
- `internal/agent/model.go` (`AgentParams` ~33-46)
- `internal/agent/port.go` (`AgentService` ~11-17)
- `internal/agent/service.go` (~34-37 end)
- `internal/agent/gormrepo/gormrepo.go` (`profileRow` ~27-38; `SetAgentType` ~107-128; `GetAgentType` ~130-147; `AgentRow` ~326-334; `ListProfiles` ~337-355)
- `internal/agent/gormrepo/gormrepo_test.go` (append)
- `internal/agent/service_test.go` (append)
- `internal/mtwire/agent.go` (`agentOut` ~370-385; `buildAgentOut` ~748-766; `HandleAdminListAgents` ~529-544)

**Interfaces:**
- Consumes: `agent.AgentParams` (existing).
- Produces:
  - `AgentParams` gains `CanAPI bool`.
  - `AgentService.AgentLevel(ctx context.Context, tenantID int64) (int, error)` — returns the profile's level; `0, nil` when the tenant is not an agent.

- [ ] **Step 1:** Write failing test for `AgentLevel` + `CanAPI` round-trip. Append to `internal/agent/service_test.go`:
```go
func TestAgentLevel_ReturnsProfileLevel(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, nil)
	if err := svc.SetAgentType(ctx, 7, AgentTypeNormal, AgentParams{Level: 1, CanAPI: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	lvl, err := svc.AgentLevel(ctx, 7)
	if err != nil || lvl != 1 {
		t.Fatalf("AgentLevel(7) = (%d,%v), want (1,nil)", lvl, err)
	}
	// 未设代理的租户 → level 0（非错误）。
	if lvl, err := svc.AgentLevel(ctx, 99); err != nil || lvl != 0 {
		t.Fatalf("AgentLevel(99) = (%d,%v), want (0,nil)", lvl, err)
	}
}
```
  And append to `internal/agent/gormrepo/gormrepo_test.go`:
```go
// TestGetAgentType_RoundTripsCanAPI 确认 can_api 列随资料持久化并读回。
func TestGetAgentType_RoundTripsCanAPI(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if err := r.SetAgentType(ctx, 5, agent.AgentTypeNormal, agent.AgentParams{Level: 1, CanAPI: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, got, found, err := r.GetAgentType(ctx, 5)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if !got.CanAPI {
		t.Fatalf("CanAPI = %v, want true", got.CanAPI)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (compile error: `CanAPI`/`AgentLevel` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/agent/... -run 'AgentLevel|CanAPI'
```

- [ ] **Step 3:** Add `CanAPI` to `AgentParams` in `internal/agent/model.go` (after the `Level int` field, ~42):
```go
	// Level 代理等级，≥ 0。
	Level int
	// CanAPI 开放 API 能力占位（本期恒不 gate 任何东西，正交于 level；proposal「开放API」独立后续项目）。
	CanAPI bool
```

- [ ] **Step 4:** Add `AgentLevel` to the `AgentService` interface in `internal/agent/port.go` (inside `AgentService`, after `GetWallet` ~16):
```go
	// GetWallet 返回租户维度的代理钱包（API 额度 / 可提现 / 累计收益）。
	GetWallet(ctx context.Context, tenantID int64) (*AgentWallet, error)
	// AgentLevel 返回该租户代理档位（能力 gate 的唯一真相）；非代理租户返回 0（非错误）。
	AgentLevel(ctx context.Context, tenantID int64) (int, error)
```
  And implement it in `internal/agent/service.go` (append after `GetWallet` ~37):
```go
// AgentLevel 返回租户代理档位（0=普通 / 1=独立）；未设代理的租户返回 0（非错误），供能力 gate 使用。
func (s *agentService) AgentLevel(ctx context.Context, tenantID int64) (int, error) {
	_, p, found, err := s.repo.GetAgentType(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return p.Level, nil
}
```

- [ ] **Step 5:** Persist `can_api` in the GORM repo. In `internal/agent/gormrepo/gormrepo.go`:
  - Add the column to `profileRow` (after `Level` ~31):
```go
	Level           int       `gorm:"column:level;not null;default:0"`
	CanAPI          bool      `gorm:"column:can_api;not null;default:false"`
```
  - In `SetAgentType`, set `CanAPI: p.CanAPI` in the `profileRow{...}` literal (after `Level: p.Level`, ~113) and add `"can_api"` to the `AssignmentColumns` list (~123-126):
```go
		Level:           p.Level,
		CanAPI:          p.CanAPI,
```
```go
		DoUpdates: clause.AssignmentColumns([]string{
			"type", "level", "can_api", "cost_price_cny", "package_discount",
			"commission_ratio", "discount_floor", "updated_at",
		}),
```
  - In `GetAgentType`, return `CanAPI: row.CanAPI` in the `agent.AgentParams{...}` literal (~140-146):
```go
		Level:           row.Level,
		CanAPI:          row.CanAPI,
```

- [ ] **Step 6:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/agent/...
```

- [ ] **Step 7:** Surface `can_api` in the admin DTO (additive; no gating). In `internal/mtwire/agent.go`:
  - `agentOut` (~370-385) add after `Level int`:
```go
	Level           int     `json:"level"`
	CanAPI          bool    `json:"can_api"`
```
  - `buildAgentOut` (~748-766) add to the returned `agentOut{...}` after `Level: p.Level,`:
```go
		Level:           p.Level,
		CanAPI:          p.CanAPI,
```
  - `HandleAdminListAgents` (~529-544): the list builds `agentOut` from `AgentRow`. Add `CanAPI` to `AgentRow` in `gormrepo.go` (~326-334) and its `ListProfiles` mapping (~344-352), then set it in the list handler. In `gormrepo.go`:
```go
type AgentRow struct {
	TenantID        int64
	UserID          int64
	Type            agent.AgentType
	Level           int
	CanAPI          bool
	CostPriceCNY    float64
	PackageDiscount float64
	CommissionRatio float64
}
```
```go
		out = append(out, AgentRow{
			TenantID:        p.TenantID,
			UserID:          p.UserID,
			Type:            agent.AgentType(p.Type),
			Level:           p.Level,
			CanAPI:          p.CanAPI,
			CostPriceCNY:    p.CostPriceCNY,
			PackageDiscount: p.PackageDiscount,
			CommissionRatio: p.CommissionRatio,
		})
```
  - In `HandleAdminListAgents` `out = append(out, agentOut{...})` (~529) add after `Level: p.Level,`:
```go
			Level:           p.Level,
			CanAPI:          p.CanAPI,
```

- [ ] **Step 8:** Run full build + agent tests — expect PASS.
```
cd /Users/cc/newapi628 && go build ./... && go test ./internal/agent/... ./internal/mtwire/...
```

- [ ] **Step 9:** Commit.
```
cd /Users/cc/newapi628 && git add internal/agent internal/mtwire/agent.go && git commit -m "feat(agent): add can_api field + AgentLevel gate helper (additive)"
```

---

### Task 2: Delete `AgentType` + destructive migration (drop `type`, backfill `level=1`) — ATOMIC

This is the compile-breaking removal; it MUST land as one green build. Removing the DB `type` column is co-located here (not Task 1) because GORM would panic if the struct dropped `Type` while the column lived, or vice-versa. **Touches `wire.go` `Migrate()` — serial integration point.**

**Files:**
- `internal/agent/model.go` (DELETE enum ~10-30)
- `internal/agent/model_test.go` (DELETE `TestAgentType_Valid` ~9-26)
- `internal/agent/port.go` (`AgentService.SetAgentType` ~14; `AgentRepo.SetAgentType/GetAgentType` ~52-55)
- `internal/agent/service.go` (`SetAgentType` ~17-32)
- `internal/agent/service_test.go` (all `SetAgentType`/`GetAgentType` calls)
- `internal/agent/repo.go` (`agentRecord` ~10-14; `SetAgentType` ~63-68; `GetAgentType` ~70-78)
- `internal/agent/repo_test.go` (~11-12)
- `internal/agent/gormrepo/gormrepo.go` (`profileRow.Type` ~30; `SetAgentType` ~107-128; `GetAgentType` ~131-147; `AgentRow.Type` ~329; `ListProfiles` ~347)
- `internal/agent/gormrepo/gormrepo_test.go` (`TestSetAgentType_Upsert` ~32-56)
- `internal/mtwire/agent.go` (DTOs ~376/410/419; create ~444-505; update ~563-611; `creditConsumeCommission` ~189; `buildAgentOut` ~748-766)
- `internal/mtwire/seed.go` (`seedDemoAgent` ~93)
- `internal/mtwire/wire.go` (`Migrate()` ~254-309)

**Interfaces (new signatures — apply everywhere):**
- `AgentService.SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error`
- `AgentRepo.SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error`
- `AgentRepo.GetAgentType(ctx context.Context, tenantID int64) (p AgentParams, found bool, err error)`

- [ ] **Step 1:** Rewrite the agent-package tests to the NEW (typeless) signatures so the compiler drives red→green. In `internal/agent/model_test.go` DELETE the whole `TestAgentType_Valid` function (~9-26). In `internal/agent/service_test.go` replace every `SetAgentType`/`GetAgentType` call to drop the type arg/return, e.g.:
```go
func TestSetAgentType_PersistsValid(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, nil)

	params := AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 2}
	if err := svc.SetAgentType(ctx, 7, params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotParams, found, err := repo.GetAgentType(ctx, 7)
	if err != nil || !found {
		t.Fatalf("profile not persisted: found=%v err=%v", found, err)
	}
	if gotParams != params {
		t.Fatalf("persisted = %+v, want %+v", gotParams, params)
	}
}

func TestSetAgentType_InvalidParamsRejected(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemRepo(), nil)
	cases := []struct {
		name string
		p    AgentParams
	}{
		{"negative cost", AgentParams{CostPrice: -1}},
		{"commission >1", AgentParams{CommissionRatio: 2}},
		{"discount <0", AgentParams{PackageDiscount: -0.5}},
		{"negative level", AgentParams{Level: -3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertCode(t, svc.SetAgentType(ctx, 1, c.p), CodeAgentTypeInvalid)
		})
	}
}

func TestSetAgentType_GuardBlocksBelowProtection(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, stubGuard{blockCode: "RATIO_BELOW_FLOOR"})
	assertCode(t, svc.SetAgentType(ctx, 1, AgentParams{PackageDiscount: 0.5, DiscountFloor: 0.8}), "RATIO_BELOW_FLOOR")
	if _, found, _ := repo.GetAgentType(ctx, 1); found {
		t.Fatal("profile must not be persisted when guard blocks")
	}
}

func TestSetAgentType_GuardAdmitsAtOrAboveFloor(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemRepo(), stubGuard{blockCode: "RATIO_BELOW_FLOOR"})
	if err := svc.SetAgentType(ctx, 1, AgentParams{PackageDiscount: 0.9, DiscountFloor: 0.8}); err != nil {
		t.Fatalf("expected admit, got %v", err)
	}
}
```
  DELETE `TestSetAgentType_InvalidTypeRejected` (~40-45) — the invalid-type path no longer exists.
  In `internal/agent/service_test.go` `TestAgentLevel_ReturnsProfileLevel` (added in Task 1) change `svc.SetAgentType(ctx, 7, AgentTypeNormal, AgentParams{...})` → `svc.SetAgentType(ctx, 7, AgentParams{Level: 1, CanAPI: true})`.
  In `internal/agent/repo_test.go` (~11): `if _, found, err := repo.GetAgentType(ctx, 123); err != nil || found {`.
  In `internal/agent/gormrepo/gormrepo_test.go` rewrite `TestSetAgentType_Upsert` and the Task-1 `TestGetAgentType_RoundTripsCanAPI` to the typeless API:
```go
func TestSetAgentType_Upsert(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	p1 := agent.AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 1}
	if err := r.SetAgentType(ctx, 7, p1); err != nil {
		t.Fatalf("set1: %v", err)
	}
	p2 := agent.AgentParams{CostPrice: 20, PackageDiscount: 0.8, CommissionRatio: 0.3, Level: 2}
	if err := r.SetAgentType(ctx, 7, p2); err != nil {
		t.Fatalf("set2: %v", err)
	}
	got, found, err := r.GetAgentType(ctx, 7)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got != p2 {
		t.Fatalf("got %+v, want %+v", got, p2)
	}
	if _, found, _ := r.GetAgentType(ctx, 99); found {
		t.Fatal("tenant 99 must not be an agent")
	}
}

func TestGetAgentType_RoundTripsCanAPI(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if err := r.SetAgentType(ctx, 5, agent.AgentParams{Level: 1, CanAPI: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, found, err := r.GetAgentType(ctx, 5)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if !got.CanAPI {
		t.Fatalf("CanAPI = %v, want true", got.CanAPI)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (compile: production code still has old signatures + `AgentType`).
```
cd /Users/cc/newapi628 && go vet ./internal/agent/... 2>&1 | head
```

- [ ] **Step 3:** Delete the enum in `internal/agent/model.go` — remove lines 10-30 entirely (the `AgentType` type, the three constants, and `Valid()`). The file now begins (after imports/comment) at `AgentParams`. Leave the `ErrAgentTypeInvalid` usage in `Validate()` untouched.

- [ ] **Step 4:** Update `internal/agent/port.go`:
```go
	// SetAgentType 设代理成本价/折扣/分润/等级/can_api（经 PricingGuard 校验）。
	// 参数非法返回 AGENT_TYPE_INVALID；折扣击穿保护线时原样上浮守卫错误。
	SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error
```
  (in `AgentService`, ~12-14), and in `AgentRepo` (~52-55):
```go
	// SetAgentType 持久化代理资料（按 tenantID 主键 upsert）。
	SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error
	// GetAgentType 读取代理资料；found=false 表示该租户尚未设代理。
	GetAgentType(ctx context.Context, tenantID int64) (p AgentParams, found bool, err error)
```

- [ ] **Step 5:** Update `internal/agent/service.go` `SetAgentType` (~17-32):
```go
// SetAgentType 校验代理参数后落库；参数非法返回 AGENT_TYPE_INVALID。
// 折扣若击穿主站保护线，则原样上浮 PricingGuard 的错误码（如 RATIO_BELOW_FLOOR）。
func (s *agentService) SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if s.guard != nil {
		if err := s.guard.ValidateGroupRatio(p.PackageDiscount, p.DiscountFloor); err != nil {
			return err // 跨模块错误原样上浮（detailed-design §6.4）
		}
	}
	return s.repo.SetAgentType(ctx, tenantID, p)
}
```
  And the Task-1 `AgentLevel` impl: change `_, p, found, err := s.repo.GetAgentType(...)` to `p, found, err := s.repo.GetAgentType(ctx, tenantID)`.

- [ ] **Step 6:** Update `internal/agent/repo.go` (`MemRepo`):
```go
type agentRecord struct {
	params    AgentParams
	updatedAt time.Time
}
```
```go
func (r *MemRepo) SetAgentType(_ context.Context, tenantID int64, p AgentParams) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[tenantID] = agentRecord{params: p, updatedAt: r.now()}
	return nil
}

func (r *MemRepo) GetAgentType(_ context.Context, tenantID int64) (AgentParams, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.profiles[tenantID]
	if !ok {
		return AgentParams{}, false, nil
	}
	return rec.params, true, nil
}
```

- [ ] **Step 7:** Update `internal/agent/gormrepo/gormrepo.go`:
  - Delete the `Type` field from `profileRow` (~30).
  - `SetAgentType` (~107-128): drop the `t agent.AgentType` param + the `Type:` field + `"type"` from `AssignmentColumns`:
```go
// SetAgentType 按 tenant_id 主键 upsert 代理资料（设代理 / 改代理复用）。
func (r *Repo) SetAgentType(ctx context.Context, tenantID int64, p agent.AgentParams) error {
	now := r.now()
	row := profileRow{
		TenantID:        tenantID,
		Level:           p.Level,
		CanAPI:          p.CanAPI,
		CostPriceCNY:    p.CostPrice,
		PackageDiscount: p.PackageDiscount,
		CommissionRatio: p.CommissionRatio,
		DiscountFloor:   p.DiscountFloor,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"level", "can_api", "cost_price_cny", "package_discount",
			"commission_ratio", "discount_floor", "updated_at",
		}),
	}).Create(&row).Error
}
```
  - `GetAgentType` (~131-147): drop the `AgentType` return:
```go
// GetAgentType 读取代理资料；found=false 表示该租户尚未设代理。
func (r *Repo) GetAgentType(ctx context.Context, tenantID int64) (agent.AgentParams, bool, error) {
	var row profileRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return agent.AgentParams{}, false, nil
		}
		return agent.AgentParams{}, false, err
	}
	return agent.AgentParams{
		CostPrice:       row.CostPriceCNY,
		PackageDiscount: row.PackageDiscount,
		CommissionRatio: row.CommissionRatio,
		Level:           row.Level,
		CanAPI:          row.CanAPI,
		DiscountFloor:   row.DiscountFloor,
	}, true, nil
}
```
  - Remove `Type` from `AgentRow` (~329) and from the `ListProfiles` mapping (~347). `AgentRow` now: `TenantID/UserID/Level/CanAPI/CostPriceCNY/PackageDiscount/CommissionRatio`.

- [ ] **Step 8:** Update `internal/mtwire/agent.go` (all type usages):
  - `agentOut` (~376): DELETE `Type string json:"type"`.
  - `agentCreateIn` (~410): DELETE `Type string json:"type"`.
  - `agentPatchIn` (~419): DELETE `Type *string json:"type"`.
  - `creditConsumeCommission` (~189): `params, found, err := a.AgentRepo.GetAgentType(ctx, tenantID)` (drop leading `_,`).
  - `buildAgentOut` (~748): drop `at agent.AgentType` param + `Type: string(at)`. New signature:
```go
func (a *App) buildAgentOut(ctx context.Context, tenantID, ownerUserID int64, slug, name, status string, p agent.AgentParams) agentOut {
```
    and remove the `Type: string(at),` line from its returned literal.
  - `HandleAdminCreateAgent` (~444-505): delete `at := agent.AgentType(in.Type)` and the `if !at.Valid() {...}` block (~453-456); call `a.AgentService.SetAgentType(ctx, t.ID, params)`; final line `respondOK(c, a.buildAgentOut(ctx, t.ID, in.OwnerUserID, t.Slug, t.Name, string(t.Status), params))`.
  - `HandleAdminUpdateAgent` (~563-611): change `curType, curParams, _, err := a.AgentRepo.GetAgentType(...)` → `curParams, _, err := a.AgentRepo.GetAgentType(ctx, tenantID)`; DELETE the `if in.Type != nil { curType = ... }` block (~573-575); call `a.AgentService.SetAgentType(ctx, tenantID, curParams)`; final `respondOK(c, a.buildAgentOut(ctx, tenantID, t.OwnerUserID, t.Slug, t.Name, string(t.Status), curParams))`.
  - `HandleAdminListAgents` (~529): drop the `Type: string(p.Type),` line.

- [ ] **Step 9:** Update `internal/mtwire/seed.go` `seedDemoAgent` (~93): `if err := a.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{CostPrice: 50, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 1}); err != nil {`.

- [ ] **Step 10:** Add the destructive migration. Append to `internal/mtwire/agent.go` (near `migrateUsersTenantID`):
```go
// migrateAgentProfilesDropType 一次性破坏性迁移：现有代理全部升为独立档（level=1）后删除废弃的 type 列。
// 幂等：以 type 列是否仍存在为一次性信号——列已删即跳过，绝不重复回填（避免每次启动重置 level）。
// 与 migrateUsersTenantID 同套路：information_schema 守卫的 raw MySQL；本地 sqlite 不覆盖，服务器验证。
func migrateAgentProfilesDropType(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'agent_profiles' AND column_name = 'type'`,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil // type 列已删：一次性迁移已执行，幂等跳过
	}
	// 回填：现有代理全部升为独立档（不拉黑已有站点，spec §3 迁移）。仅当 type 列尚存时执行，故只跑一次。
	if err := db.Exec(`UPDATE agent_profiles SET level = 1`).Error; err != nil {
		return err
	}
	return db.Exec(`ALTER TABLE agent_profiles DROP COLUMN type`).Error
}
```
  Wire it into `internal/mtwire/wire.go` `Migrate()` — add just before `return migrateSubscriptionBridge(a.DB)` (~308):
```go
	// 代理分层：现有代理回填 level=1 + 删废弃 type 列（一次性、information_schema 守卫，见 agent.go）。
	if err := migrateAgentProfilesDropType(a.DB); err != nil {
		return err
	}
	return migrateSubscriptionBridge(a.DB)
```

- [ ] **Step 11:** Run — expect PASS (whole tree compiles + agent/mtwire tests green).
```
cd /Users/cc/newapi628 && go build ./... && go test ./internal/agent/... ./internal/mtwire/...
```

- [ ] **Step 12:** Verify zero residual references.
```
cd /Users/cc/newapi628 && grep -rn "AgentType\b\|AgentTypeNormal\|AgentTypeOEM\|AgentTypeAPI\|\.Type\b" internal/agent internal/mtwire/agent.go internal/mtwire/seed.go | grep -v "ErrAgentTypeInvalid\|CodeAgentTypeInvalid" || echo "clean"
```
  Expect `clean`.

- [ ] **Step 13:** Commit.
```
cd /Users/cc/newapi628 && git add internal/agent internal/mtwire && git commit -m "feat(agent)!: delete AgentType, backfill level=1 + drop agent_profiles.type"
```

---

### Task 3: Gate custom-domain + site-branding behind `RequireAgentLevel(1)` (middleware + depth). SERIAL: `router/mt-router.go`

**Files:**
- `internal/mtwire/agent.go` (error var block ~33-40; append middleware/helper)
- `internal/mtwire/agent_gate_test.go` (NEW)
- `internal/mtwire/custom_domain.go` (handler entries ~64, ~80, ~96, ~107)
- `internal/mtwire/siteconfig.go` (handler entries ~111, ~121, ~138)
- `router/mt-router.go` (agentSelf group ~104-111)

**Interfaces:**
- Consumes: `AgentService.AgentLevel` (Task 1), `agentTenantID(c)` (existing).
- Produces: `App.RequireAgentLevel(min int) gin.HandlerFunc`; `App.ensureAgentLevel(c *gin.Context, min int) bool`; `errAgentLevelLocked` (`AGENT_LEVEL_LOCKED`, 403).

- [ ] **Step 1:** Write the failing gate test. Create `internal/mtwire/agent_gate_test.go`:
```go
package mtwire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/agent"
)

// TestRequireAgentLevel_GatesByLevel: L0 → 403 AGENT_LEVEL_LOCKED（中止）；L1 → 放行。
func TestRequireAgentLevel_GatesByLevel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := agent.NewMemRepo()
	ctx := context.Background()
	_ = repo.SetAgentType(ctx, 7, agent.AgentParams{Level: 0}) // 普通
	_ = repo.SetAgentType(ctx, 9, agent.AgentParams{Level: 1}) // 独立
	app := &App{AgentService: agent.NewService(repo, nil)}

	// L0：中止 + 403。
	w0 := httptest.NewRecorder()
	c0, _ := gin.CreateTestContext(w0)
	c0.Set(ginKeyAgentTenant, int64(7))
	app.RequireAgentLevel(1)(c0)
	if !c0.IsAborted() || w0.Code != http.StatusForbidden {
		t.Fatalf("L0: aborted=%v code=%d, want abort/403", c0.IsAborted(), w0.Code)
	}

	// L1：放行（未中止）。
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Set(ginKeyAgentTenant, int64(9))
	app.RequireAgentLevel(1)(c1)
	if c1.IsAborted() {
		t.Fatalf("L1: unexpectedly aborted (code=%d)", w1.Code)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (`RequireAgentLevel` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestRequireAgentLevel
```

- [ ] **Step 3:** Add the error + helper + middleware to `internal/mtwire/agent.go`. In the error var block (~33-40) add:
```go
	errAgentGroupNotModel = apperr.New("AGENT_GROUP_NOT_MODEL", "仅可调整模型分组的倍率", http.StatusBadRequest)
	errAgentLevelLocked   = apperr.New("AGENT_LEVEL_LOCKED", "该能力需升级为独立代理后开启", http.StatusForbidden)
```
  Append after `agentTenantID` (~337):
```go
// ensureAgentLevel 纵深校验当前代理租户档位 ≥ min：不足以 AGENT_LEVEL_LOCKED 响应并返回 false。
// 用于路由中间件（RequireAgentLevel）与 handler 入口双保险（spec §5.2.3 纵深）。
func (a *App) ensureAgentLevel(c *gin.Context, min int) bool {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return false
	}
	lvl, err := a.AgentService.AgentLevel(c.Request.Context(), tenantID)
	if err != nil {
		respondErr(c, err)
		return false
	}
	if lvl < min {
		respondErr(c, errAgentLevelLocked)
		return false
	}
	return true
}

// RequireAgentLevel 是「独立能力」路由门禁：须挂在 AgentOwnerAuth 之后（依赖其写入的 agentTenantID）。
func (a *App) RequireAgentLevel(min int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.ensureAgentLevel(c, min) {
			c.Abort()
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 4:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestRequireAgentLevel
```

- [ ] **Step 5:** Add in-handler depth checks. At the FIRST line of each handler body, insert `if !a.ensureAgentLevel(c, 1) { return }`:
  - `internal/mtwire/custom_domain.go`: `HandleAgentBindCustomDomain` (~64), `HandleAgentGetCustomDomain` (~80), `HandleAgentVerifyCustomDomain` (~96), `HandleAgentUnbindCustomDomain` (~107). Example:
```go
func (a *App) HandleAgentBindCustomDomain(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	...
```
  - `internal/mtwire/siteconfig.go`: `HandleAgentGetSiteConfig` (~111), `HandleAgentUpdateSiteConfig` (~121), `HandleAgentUploadLogo` (~138). Same one-liner at the top of each.

- [ ] **Step 6:** Gate the routes in `router/mt-router.go`. Inside the `agentSelf` block, REMOVE the 7 custom-domain + site-config route registrations (~103-111) and register them in a nested sub-group instead. Replace lines ~103-111 with:
```go
				// 独立档能力（level>=1）：自定义域名 + 站点装修。挂 RequireAgentLevel(1)（AgentOwnerAuth 之后）。
				agentIndependent := agentSelf.Group("", app.RequireAgentLevel(1))
				{
					// 自定义域名（OEM，§6.2/§6.3）：绑定 / 查状态 / 触发 TXT 校验 / 解绑（owner + level>=1）。
					agentIndependent.POST("/custom-domain", app.HandleAgentBindCustomDomain)
					agentIndependent.GET("/custom-domain", app.HandleAgentGetCustomDomain)
					agentIndependent.POST("/custom-domain/verify", app.HandleAgentVerifyCustomDomain)
					agentIndependent.DELETE("/custom-domain", app.HandleAgentUnbindCustomDomain)
					// 站点装修（OEM 最小版，§5/§9）：读/改装修配置 + 上传 Logo（owner + level>=1）。
					agentIndependent.GET("/site-config", app.HandleAgentGetSiteConfig)
					agentIndependent.PUT("/site-config", app.HandleAgentUpdateSiteConfig)
					agentIndependent.POST("/site-config/logo", app.HandleAgentUploadLogo)
				}
```
  Leave the finance routes (~112-115) inside `agentSelf` as-is.

- [ ] **Step 7:** Run build + mtwire tests — expect PASS.
```
cd /Users/cc/newapi628 && go build ./... && go test ./internal/mtwire/
```

- [ ] **Step 8:** Commit.
```
cd /Users/cc/newapi628 && git add internal/mtwire router/mt-router.go && git commit -m "feat(agent): gate custom-domain + site-branding behind RequireAgentLevel(1)"
```

---

### Task 4: Subdomain gating — L0 create provisions NO subdomain; promotion (PATCH 0→1) provisions `<slug>.wedreamhub.com` (RISKIEST)

**Why riskiest:** this changes the tenant-creation flow. Today `tenant.Create` ALWAYS writes a `<slug>.wedreamhub.com` domain row, and `ResolveByHost`/`GetTenantByDomain` use it as a resolution key. A subdomain-less tenant must NOT crash resolution. Chosen design keeps the *default* behaviour (provision) so all existing tenant tests stay green; only the agent-create path opts OUT via a new `SkipSubdomain` flag, and promotion opts back IN via idempotent `EnsureSubdomain`. The resolver already does NOT cache negative lookups, so no cache invalidation is needed after promotion.

> **RESOLVED (was OPEN QUESTION):** L0 self-service pages (earnings/withdrawals/promotion/my-users) no longer depend on Host→tenant resolution. Resolved decision: **new agents default to level 0**, and **L0 console access is delivered by Task 9** (owner-based resolution — `AgentOwnerAuthByUser` sets the agent tenant from the logged-in user's OWNED tenant, independent of Host). **No change to the subdomain-gating logic here** (L0 still provisions no subdomain; subdomains stay an L1-only capability) — only the agent-self *auth* path changes (Task 9), so an L0 owner reaches their console on the main site while L1 also works on their subdomain.

**Files:**
- `internal/tenant/port.go` (`CreateTenantInput` ~28-33; `TenantService` ~15-21)
- `internal/tenant/service.go` (`Create` ~22-44; append `EnsureSubdomain`)
- `internal/tenant/service_test.go` (append)
- `internal/mtwire/agent.go` (`HandleAdminCreateAgent` ~476-484; `HandleAdminUpdateAgent` ~592-611)
- `internal/mtwire/seed.go` (`ensureDemoTenant` ~137-141)

**Interfaces:**
- `CreateTenantInput` gains `SkipSubdomain bool` (zero value false ⇒ provision, preserving current behaviour).
- `TenantService.EnsureSubdomain(ctx context.Context, tenantID int64, slug string) error` — idempotent; creates `<slug>.wedreamhub.com` if absent, returns nil if already present.

- [ ] **Step 1:** Write failing tests. Append to `internal/tenant/service_test.go`:
```go
func TestService_Create_SkipSubdomain(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()

	tn, err := svc.Create(ctx, CreateTenantInput{Slug: "basic", SkipSubdomain: true})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	// 无子域名：解析该 Host 返回 NotFound（不炸），但租户本身仍可按 id 读到。
	if _, err := repo.GetTenantByDomain(ctx, "basic.wedreamhub.com"); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("subdomain must NOT be provisioned for L0, got err=%v", err)
	}
	if _, err := repo.GetTenant(ctx, tn.ID); err != nil {
		t.Fatalf("tenant must still resolve by id: %v", err)
	}
}

func TestService_EnsureSubdomain_Idempotent(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()
	tn, _ := svc.Create(ctx, CreateTenantInput{Slug: "grow", SkipSubdomain: true})

	if err := svc.EnsureSubdomain(ctx, tn.ID, "grow"); err != nil {
		t.Fatalf("EnsureSubdomain: %v", err)
	}
	got, err := repo.GetTenantByDomain(ctx, "grow.wedreamhub.com")
	if err != nil || got.ID != tn.ID {
		t.Fatalf("after ensure, domain must map to tenant: got=%v err=%v", got, err)
	}
	// 幂等：再次调用不报错。
	if err := svc.EnsureSubdomain(ctx, tn.ID, "grow"); err != nil {
		t.Fatalf("EnsureSubdomain must be idempotent, got %v", err)
	}
}
```
  Confirm `service_test.go` imports `errors` (add to the import block if missing).

- [ ] **Step 2:** Run — expect FAIL (`SkipSubdomain`/`EnsureSubdomain` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/tenant/ -run 'SkipSubdomain|EnsureSubdomain'
```

- [ ] **Step 3:** Add the field + interface method in `internal/tenant/port.go`:
```go
// CreateTenantInput 是 TenantService.Create 的入参。
type CreateTenantInput struct {
	Slug             string
	Name             string
	TokenplanEnabled bool
	// SkipSubdomain=true 时不派生 `<slug>.wedreamhub.com` 域名映射（普通档 L0 代理无独立子域名，
	// spec §5.2.2）。零值 false = 保持既有行为（派生子域名）。
	SkipSubdomain bool
}
```
  In the `TenantService` interface (~15-21), add:
```go
	Create(ctx context.Context, in CreateTenantInput) (*Tenant, error)
	// EnsureSubdomain 幂等派生 `<slug>.wedreamhub.com` 域名映射（管理员升档 L0→L1 时调用）；已存在则无操作。
	EnsureSubdomain(ctx context.Context, tenantID int64, slug string) error
	SetStatus(ctx context.Context, id int64, s TenantStatus) error
```

- [ ] **Step 4:** Implement in `internal/tenant/service.go`. Guard the domain write in `Create` (~35-42):
```go
	if !in.SkipSubdomain {
		d := &TenantDomain{
			TenantID:  t.ID,
			Domain:    DomainForSlug(in.Slug),
			IsPrimary: true,
		}
		if err := s.repo.CreateDomain(ctx, d); err != nil {
			return nil, err
		}
	}
	return t, nil
```
  Append `EnsureSubdomain` (needs `errors` import in service.go):
```go
// EnsureSubdomain 幂等派生二级域名 `<slug>.wedreamhub.com`（管理员升档时调用）。
// 域名已存在（映射到本租户）时 CreateDomain 返回 ErrSlugDuplicate，视为已就绪 → nil。
func (s *tenantService) EnsureSubdomain(ctx context.Context, tenantID int64, slug string) error {
	d := &TenantDomain{TenantID: tenantID, Domain: DomainForSlug(slug), IsPrimary: true}
	if err := s.repo.CreateDomain(ctx, d); err != nil {
		if errors.Is(err, ErrSlugDuplicate) {
			return nil // 已派生：幂等
		}
		return err
	}
	return nil
}
```
  Update the `import` line in `service.go` from `import "context"` to:
```go
import (
	"context"
	"errors"
)
```

- [ ] **Step 5:** Run — expect PASS (new tests + all existing tenant tests green, since default still provisions).
```
cd /Users/cc/newapi628 && go test ./internal/tenant/...
```

- [ ] **Step 6:** Wire the level→subdomain policy in `internal/mtwire/agent.go`.
  - `HandleAdminCreateAgent` — the `tenant.Create` call (~476-480) becomes:
```go
	t, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             in.Slug,
		Name:             in.Name,
		TokenplanEnabled: true,
		SkipSubdomain:    params.Level < 1, // L0 普通：不发子域名（spec §5.2.2）
	})
```
  - `HandleAdminUpdateAgent` — after the successful `SetAgentType` (~592-595) and before the optional name/status block, provision on promotion:
```go
	// SetAgentType 内含参数 + 折扣保护线校验（非法即上浮，不落库）。
	if err := a.AgentService.SetAgentType(ctx, tenantID, curParams); err != nil {
		respondErr(c, err)
		return
	}
	// 升档 → 独立档：幂等派生子域名 `<slug>.wedreamhub.com`（resolver 不缓存负结果，无需失效缓存）。
	if in.Level != nil && *in.Level >= 1 {
		if err := a.TenantService.EnsureSubdomain(ctx, tenantID, t.Slug); err != nil {
			respondErr(c, err)
			return
		}
	}
```

- [ ] **Step 7:** Keep the demo tenant on a subdomain (it seeds at L1). In `internal/mtwire/seed.go` `ensureDemoTenant` (~137-141), the `Create` call is unchanged (default provisions) — verify it does NOT set `SkipSubdomain`. No edit needed; add a confirming comment:
```go
	created, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             demoSlug,
		Name:             demoName,
		TokenplanEnabled: true,
		// demo 代理 seed 为 L1（见 seedDemoAgent），保留子域名 tokendream.wedreamhub.com。
	})
```

- [ ] **Step 8:** Run build + tenant/mtwire tests — expect PASS.
```
cd /Users/cc/newapi628 && go build ./... && go test ./internal/tenant/... ./internal/mtwire/...
```

- [ ] **Step 9:** Commit.
```
cd /Users/cc/newapi628 && git add internal/tenant internal/mtwire/agent.go internal/mtwire/seed.go && git commit -m "feat(agent): L0 create provisions no subdomain; promotion provisions <slug>.wedreamhub.com"
```

- [ ] **Step 10:** SERVER verification (after deploy — see Task 8 finish): create an L0 agent → confirm `<slug>.wedreamhub.com` 404s "站点未开通" and the agent still exists; PATCH level 0→1 → confirm the subdomain now resolves. Command template:
```
# on server, against newapi_test (3100):
curl -s -H "Host: <slug>.wedreamhub.com" http://127.0.0.1:3100/api/tenant/current   # before promotion: not found
# after admin PATCH level=1:
curl -s -H "Host: <slug>.wedreamhub.com" http://127.0.0.1:3100/api/tenant/current   # resolves to tenant
```

---

### Task 5: Add `level` + `can_api` to `GET /api/tenant/agent-context`

Thin glue over already-tested pieces (`isAgentOwner` + `GetAgentType`); the gate is `go build` + a server curl (an httptest would require full sqlite tenant+agent wiring — out of proportion for field-copying).

**Files:**
- `internal/mtwire/agent.go` (`HandleAgentContext` ~362-364; add `agentContextOut` DTO near the other DTOs ~370)

**Interfaces:**
- Produces JSON `{ "is_agent_owner": bool, "level": int, "can_api": bool }`.

- [ ] **Step 1:** Add the DTO next to `agentOut` (~370) in `internal/mtwire/agent.go`:
```go
// agentContextOut 是 GET /api/tenant/agent-context 响应：前端据此隐藏菜单 + 路由守卫 gate。
type agentContextOut struct {
	IsAgentOwner bool `json:"is_agent_owner"`
	Level        int  `json:"level"`
	CanAPI       bool `json:"can_api"`
}
```

- [ ] **Step 2:** Replace `HandleAgentContext` (~362-364):
```go
func (a *App) HandleAgentContext(c *gin.Context) {
	out := agentContextOut{IsAgentOwner: a.isAgentOwner(c)}
	if out.IsAgentOwner {
		if t := tenantFrom(c); t != nil {
			if p, found, err := a.AgentRepo.GetAgentType(c.Request.Context(), t.ID); err == nil && found {
				out.Level = p.Level
				out.CanAPI = p.CanAPI
			}
		}
	}
	respondOK(c, out)
}
```

- [ ] **Step 3:** Run build + mtwire tests — expect PASS.
```
cd /Users/cc/newapi628 && go build ./... && go test ./internal/mtwire/
```

- [ ] **Step 4:** Commit.
```
cd /Users/cc/newapi628 && git add internal/mtwire/agent.go && git commit -m "feat(agent): expose level + can_api on /api/tenant/agent-context"
```

- [ ] **Step 5:** SERVER verification (after deploy): as an L1 agent owner on their Host → `{ is_agent_owner:true, level:1, can_api:false }`; as a non-owner → `{ is_agent_owner:false, level:0, can_api:false }`.

---

### Task 6: Frontend — replace type selector with level (普通/独立); derive badge; delete `agentTypeValues`

Local gate: `bun run typecheck` (red when a deleted symbol is still referenced → green when all refs updated) + `bun run lint`. Real `bun run build` + deploy is on the SERVER.

**Files:**
- `web/default/src/features/agents/types.ts` (~32-34, ~44, ~74)
- `web/default/src/features/agents/lib/agent-form.ts` (~31, ~45, ~57, ~70)
- `web/default/src/features/agents/lib/index.ts` (~19-21, ~39-51)
- `web/default/src/features/agents/components/agent-mutate-drawer.tsx` (~123, ~252-296)
- `web/default/src/features/agents/components/agents-columns.tsx` (~25, ~64-89)

**Interfaces (TS):**
- `Agent` loses `type`; keeps `level: number`.
- `AgentPayload` loses `type`; keeps `level: number`.
- `agentLevelLabel(level: number, t: TFunction): string` replaces `agentTypeLabel`.

- [ ] **Step 1:** `types.ts` — DELETE the `agentTypeValues`/`AgentType` block (~32-34) and the two `type: AgentType` fields (~44 in `Agent`, ~74 in `AgentPayload`). Replace the doc comment above the deleted block with:
```ts
/** One row of the admin agent table. `status` is a tolerant string; `level`
 *  drives capability gating (0=普通/basic, 1=独立/independent). */
export interface Agent {
  id: number
  owner_user_id: number
  owner_username: string
  slug: string
  name: string
  level: number
```
  (i.e. `level: number` now sits where `type` was; `level` already existed below — remove the now-duplicate `level` field if present, keep one). Ensure `AgentPayload` reads:
```ts
export interface AgentPayload {
  owner_user_id: number
  slug: string
  name: string
  cost_price_cny: number
  package_discount: number
  commission_ratio: number
  level: number
}
```

- [ ] **Step 2:** `lib/agent-form.ts` — remove `type` from schema/defaults/converters:
  - schema (~31): delete the `type: z.enum(['normal', 'oem', 'api']),` line.
  - `AGENT_FORM_DEFAULTS` (~41-50): delete `type: 'normal',`; set `level: 0` (new agents default to 普通/basic; promotion is manual):
```ts
export const AGENT_FORM_DEFAULTS: AgentFormValues = {
  owner_user_id: 0,
  slug: '',
  name: '',
  cost_price_cny: 0,
  package_discount: 1,
  commission_ratio: 0,
  level: 0,
}
```
  - `agentToFormValues` (~52-63): delete `type: agent.type || 'normal',`.
  - `formValuesToPayload` (~65-76): delete `type: values.type,`.

- [ ] **Step 3:** `lib/index.ts` — replace `agentTypeLabel` with `agentLevelLabel` and drop the `AgentType` import (~19-21, ~39-51):
```ts
import type { TFunction } from 'i18next'
import type { StatusVariant } from '@/components/status-badge'
```
```ts
/** i18n label for an agent level (0=普通/basic, 1=独立/independent). */
export function agentLevelLabel(level: number, t: TFunction): string {
  return level >= 1 ? t('Independent Agent') : t('Basic Agent')
}
```
  (delete the old `agentTypeLabel` function entirely.)

- [ ] **Step 4:** `agent-mutate-drawer.tsx` — in `onSubmit` (~120-128) delete the `type: payload.type,` line from the `updateAgent(...)` body. Replace the `type` FormField (~252-277) with a level select, and DELETE the old numeric `level` FormField (~279-296) so there is exactly one level control:
```tsx
                <FormField
                  control={form.control}
                  name='level'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Agent Level')}</FormLabel>
                      <FormControl>
                        <NativeSelect
                          value={String(field.value ?? 0)}
                          onChange={(e) =>
                            field.onChange(Number(e.target.value) || 0)
                          }
                        >
                          <NativeSelectOption value='0'>
                            {t('Basic Agent')}
                          </NativeSelectOption>
                          <NativeSelectOption value='1'>
                            {t('Independent Agent')}
                          </NativeSelectOption>
                        </NativeSelect>
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Independent unlocks subdomain, custom domain and site branding. Promote manually when the agent performs well.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
```
  (`numberChange` may become unused — if `bun run lint` flags it, delete the helper at ~76-83.)

- [ ] **Step 5:** `agents-columns.tsx` — swap the import (~25) `agentStatusMeta, agentTypeLabel, cny, num` → `agentStatusMeta, agentLevelLabel, cny, num`. DELETE the `type` column (~64-77). Replace the `level` column (~78-89) with a derived badge:
```tsx
      {
        accessorFn: (row) => row.level,
        id: 'level',
        header: t('Level'),
        meta: { mobileBadge: true },
        cell: ({ row }) => (
          <StatusBadge
            label={agentLevelLabel(row.original.level, t)}
            variant={row.original.level >= 1 ? 'info' : 'neutral'}
            copyable={false}
          />
        ),
        size: 110,
      },
```
  (If `'neutral'` is not a valid `StatusVariant`, use `'success'`/`'info'` per the union in `@/components/status-badge`; check the type and pick two distinct variants.)

- [ ] **Step 6:** Run typecheck + lint — expect PASS (and confirm no residual refs).
```
cd /Users/cc/newapi628/web/default && bun run typecheck && bun run lint src/features/agents
cd /Users/cc/newapi628/web/default && grep -rn "agentTypeValues\|agentTypeLabel\|AgentType\|\.type\b" src/features/agents || echo "clean"
```
  Expect `clean` (aside from unrelated words).

- [ ] **Step 7:** Commit.
```
cd /Users/cc/newapi628 && git add web/default/src/features/agents && git commit -m "feat(agent-ui): replace type selector with level (普通/独立); derive badge from level"
```

---

### Task 7: Frontend — gate Custom Domain / Site Branding on `level>=1` (context object, sidebar, route guards)

`agent-context.ts` becomes the single source of truth (one query, one fetch) returning the full object. All 13 owner-only route guards + the sidebar must read `.is_agent_owner` off the object; the two independent-site guards additionally check `.level`. A grep step at the end guarantees no guard was left reading the raw (now-object) value as a boolean (which would silently pass everyone → security regression).

**Files:**
- `web/default/src/lib/agent-context.ts` (~34-58)
- `web/default/src/hooks/use-sidebar-view.ts` (~58-74)
- `web/default/src/components/layout/types.ts` (`BaseNavItem` ~25-44)
- `web/default/src/hooks/use-sidebar-data.ts` (Custom Domain ~197-201, Site Branding ~202-206)
- `web/default/src/routes/_authenticated/custom-domain/index.tsx` (~27-34)
- `web/default/src/routes/_authenticated/site-branding/index.tsx` (~27-34)
- 11 owner-only guards (mechanical, same 2-line change):
  `finance-report/index.tsx`, `redemptions/index.tsx`, `promotion-channels/index.tsx`, `agent-listings/index.tsx`, `my-moderation/index.tsx`, `my-violations/index.tsx`, `agent-tickets/index.tsx`, `agent-tickets/$ticketId.tsx`, `agent-earnings/index.tsx`, `my-users/index.tsx`, `my-groups/index.tsx` (all under `web/default/src/routes/_authenticated/`)

**Interfaces (TS):**
- `AgentContext = { is_agent_owner: boolean; level: number; can_api: boolean }`.
- `BaseNavItem` gains `agentLevelMin?: number`.

- [ ] **Step 1:** `agent-context.ts` — change the type + fetcher to the object (fail-closed defaults):
```ts
export type AgentContext = {
  is_agent_owner: boolean
  level: number
  can_api: boolean
}

type AgentContextEnvelope = {
  success?: boolean
  data?: Partial<AgentContext> | null
}

const CLOSED: AgentContext = { is_agent_owner: false, level: 0, can_api: false }

async function fetchAgentContext(): Promise<AgentContext> {
  try {
    const res = await api.get<AgentContextEnvelope>(
      '/api/tenant/agent-context',
      { skipBusinessError: true, skipErrorHandler: true }
    )
    const d = res.data?.data
    return {
      is_agent_owner: Boolean(d?.is_agent_owner),
      level: Number(d?.level ?? 0),
      can_api: Boolean(d?.can_api),
    }
  } catch {
    return CLOSED
  }
}
```
  `agentContextQueryOptions` stays but its `queryFn` now yields `AgentContext`.

- [ ] **Step 2:** `components/layout/types.ts` — add to `BaseNavItem` (after `agentOwnerOnly?` ~43):
```ts
  agentOwnerOnly?: boolean
  /**
   * Minimum agent level required to see this item (see `/api/tenant/agent-context`).
   * Used to gate independent-site items (Custom Domain, Site Branding) at level>=1.
   */
  agentLevelMin?: number
```

- [ ] **Step 3:** `use-sidebar-data.ts` — add `agentLevelMin: 1` to the two items:
```tsx
          {
            title: t('Custom Domain'),
            url: '/custom-domain',
            icon: Globe,
            agentLevelMin: 1,
          },
          {
            title: t('Site Branding'),
            url: '/site-branding',
            icon: Palette,
            agentLevelMin: 1,
          },
```

- [ ] **Step 4:** `use-sidebar-view.ts` — read owner+level off the object and filter by `agentLevelMin` (~58-74):
```ts
  const agentCtx = useQuery(agentContextQueryOptions).data
  const isAgentOwner = agentCtx?.is_agent_owner ?? false
  const agentLevel = agentCtx?.level ?? 0

  const rootNavGroups = useMemo<NavGroup[]>(() => {
    const role = userRole ?? ROLE.GUEST
    const isAdmin = role >= ROLE.ADMIN
    return configFilteredRoot
      .filter((group) => (group.id === 'admin' ? isAdmin : true))
      .filter((group) => !group.agentOwnerOnly || isAgentOwner)
      .map((group) => {
        const items = group.items.filter(
          (item) =>
            (item.requiredRole === undefined || role >= item.requiredRole) &&
            (!item.agentOwnerOnly || isAgentOwner) &&
            (!item.agentLevelMin || agentLevel >= item.agentLevelMin)
        )
        return items.length === group.items.length ? group : { ...group, items }
      })
  }, [configFilteredRoot, userRole, isAgentOwner, agentLevel])
```

- [ ] **Step 5:** Update the two independent-site guards. `custom-domain/index.tsx` `beforeLoad` (~27-34):
```tsx
  beforeLoad: async ({ context }) => {
    const ctx = await context.queryClient.fetchQuery(agentContextQueryOptions)
    if (!ctx.is_agent_owner || ctx.level < 1) {
      throw redirect({ to: '/403' })
    }
  },
```
  Apply the identical change to `site-branding/index.tsx`.

- [ ] **Step 6:** Update the 11 owner-only guards (mechanical). In EACH file listed above, change:
```tsx
    const isAgentOwner = await context.queryClient.fetchQuery(
      agentContextQueryOptions
    )
    if (!isAgentOwner) {
      throw redirect({ to: '/403' })
    }
```
  to:
```tsx
    const ctx = await context.queryClient.fetchQuery(agentContextQueryOptions)
    if (!ctx.is_agent_owner) {
      throw redirect({ to: '/403' })
    }
```

- [ ] **Step 7:** Run typecheck + lint + a safety grep — expect PASS + `clean`.
```
cd /Users/cc/newapi628/web/default && bun run typecheck && bun run lint src/routes/_authenticated src/hooks src/lib
cd /Users/cc/newapi628/web/default && grep -rn "fetchQuery(\s*agentContextQueryOptions" src/routes/_authenticated | wc -l   # expect 13
cd /Users/cc/newapi628/web/default && grep -rn "if (!isAgentOwner)" src/routes/_authenticated || echo "clean"
```
  The last grep MUST print `clean` (no guard left comparing the object as a bare boolean).

- [ ] **Step 8:** Commit.
```
cd /Users/cc/newapi628 && git add web/default/src/lib/agent-context.ts web/default/src/hooks web/default/src/components/layout/types.ts web/default/src/routes/_authenticated && git commit -m "feat(agent-ui): gate custom-domain + site-branding on level>=1 (sidebar + route guards)"
```

---

### Task 8: i18n (en identity + zh) and full-stack verification

**Files:**
- `web/default/src/i18n/locales/en.json` (under `translation`)
- `web/default/src/i18n/locales/zh.json` (under `translation`)

- [ ] **Step 1:** Add the new keys. In `en.json` (identity values):
```json
    "Agent Level": "Agent Level",
    "Basic Agent": "Basic Agent",
    "Independent Agent": "Independent Agent",
    "Independent unlocks subdomain, custom domain and site branding. Promote manually when the agent performs well.": "Independent unlocks subdomain, custom domain and site branding. Promote manually when the agent performs well.",
```
  In `zh.json`:
```json
    "Agent Level": "代理档位",
    "Basic Agent": "普通代理",
    "Independent Agent": "独立代理",
    "Independent unlocks subdomain, custom domain and site branding. Promote manually when the agent performs well.": "独立档解锁子域名、自定义域名与站点品牌；代理表现良好时由管理员手动升档。",
```
  (The pre-existing `"Level"`, `"Custom Domain"`, `"Site Branding"` keys stay. `"Agent Type"`, `"OEM"`, `"Normal"`, `"API"` keys may now be unused by the agents feature — leave them; `bun run knip` / `bun run i18n:sync` can reconcile later.)

- [ ] **Step 2:** Verify i18n consistency + frontend build sanity.
```
cd /Users/cc/newapi628/web/default && node scripts/sync-i18n.mjs && bun run typecheck && bun run lint
```

- [ ] **Step 3:** Commit.
```
cd /Users/cc/newapi628 && git add web/default/src/i18n/locales && git commit -m "i18n(agent): add level labels (普通/独立) + promotion hint"
```

- [ ] **Step 4:** SERVER build + deploy + migrate on test stack `newapi_test` (3100). Confirm `App.Migrate()` ran `migrateAgentProfilesDropType` (log line present; `SHOW COLUMNS FROM agent_profiles` has NO `type`, HAS `can_api`; every existing `agent_profiles.level = 1`).
```
# on server:
ssh newapi628
# build the Go binary + web assets per deploy runbook, restart newapi_test, then:
mysql -e "SHOW COLUMNS FROM agent_profiles;" newapi_test        # no 'type', has 'can_api'
mysql -e "SELECT tenant_id, level FROM agent_profiles;" newapi_test   # all level=1
```

- [ ] **Step 5:** Playwright E2E (per spec §7): admin promotes an agent L0→L1 → that owner logs into `<slug>.wedreamhub.com` and sees Custom Domain + Site Branding unlocked; a fresh L0 agent sees neither (sidebar hides them, direct URL → /403). If Cloudflare/Turnstile blocks the run, temporarily disable → test → re-enable (confirm the toggle location before touching it).

---

### Task 9: Owner-based agent-console resolution — L0 reaches their console on the main site (SECURITY-CRITICAL). SERIAL: `router/mt-router.go`

Today the `agentSelf` group is gated by `AgentOwnerAuth` (agent.go ~296-327), which resolves the tenant from the **Host** (`tenantFrom(c)`) then checks `owner_user_id == session user`. An L0 agent has **no subdomain** (Task 4), so on the main site the Host does not resolve to their tenant → the middleware 403s and the owner cannot reach their own console. This task swaps the agent-self group onto a new **owner-based** middleware `AgentOwnerAuthByUser` that resolves the agent tenant from the **logged-in user's OWNED tenant** (owner→tenant, 1:1), independent of Host — so L0 works on the main site and L1 works on its subdomain (both resolve to the caller's own tenant).

> **SECURITY (mandatory, tested below):** the middleware resolves **only** to the tenant whose `owner_user_id == session user id` (a user owns ≤1 agent tenant, enforced at create by `ownerTaken`). It **never** reads a client-supplied tenant_id and **never** resolves to a tenant the user does not own. If the user owns no tenant → **403 `errAgentForbidden` + abort** (no fall-through). Because `agentTenantID(c)` is therefore always the *caller's own* tenant, cross-host access is not a leak: owner A visiting agent B's subdomain still only ever sees A's data (never B's). Handlers keep calling `agentTenantID(c)` unchanged.

> **This REPLACES Host-based `AgentOwnerAuth` for the agent-self group only.** `AgentOwnerAuth`/`isAgentOwner`/`HandleAgentContext` (Host-based owner signal) stay as-is — `HandleAgentContext` still answers "is the current user the owner of *this Host's* tenant" for the frontend, and `AgentOwnerAuth` is left in place (now unused by routes but harmless; do not delete). **Serial:** this edits `router/mt-router.go` (also touched by Task 3) — coordinate; Task 3's `agentIndependent` sub-group is unaffected (it nests inside `agentSelf` and still reads `agentTenantID`).

**Files:**
- `internal/tenant/gormrepo/gormrepo.go` (append `TenantByOwner` after `OwnerUserID` ~253)
- `internal/mtwire/agent.go` (append `AgentOwnerAuthByUser` after `AgentOwnerAuth` ~327)
- `internal/mtwire/agent_owner_by_user_test.go` (NEW)
- `router/mt-router.go` (`agentSelf` group declaration ~72-73)

**Interfaces:**
- Produces: `(*gormrepo.Repo).TenantByOwner(ctx context.Context, ownerUserID int64) (*tenant.Tenant, error)` — owner→tenant reverse (the inverse of the existing `OwnerUserID`; uses the existing `idx_tenants_owner` index). Returns `ErrTenantNotFound` when the user owns none. Concrete method on `*Repo` (NOT added to the `tenant.TenantRepo` interface — mirrors `OwnerUserID`/`SetOwnerUserID`/`UpdateName`, which are concrete-only; `App.TenantRepo` holds the concrete `*tenantrepo.Repo`, so no interface/`MemRepo` churn).
- Produces: `App.AgentOwnerAuthByUser() gin.HandlerFunc` — sets `ginKeyAgentTenant` from the caller's owned tenant; 403 `errAgentForbidden` + abort when the user is unauthenticated or owns no tenant. Host-independent (does not read `tenantFrom`).
- Consumes: `agentTenantID(c)` (unchanged; every agent-self handler keeps reading it).

- [ ] **Step 1:** Write the failing test. Create `internal/mtwire/agent_owner_by_user_test.go`:
```go
package mtwire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// newOwnerAuthTestApp 造最小 App：sqlite + tenants/tenant_domains（owner→tenant 反查所需）。
func newOwnerAuthTestApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	return &App{DB: db, TenantRepo: tenantrepo.New(db)}
}

// seedOwnedTenant 建租户并设 owner（owner→tenant 1:1），返回 tenant_id。
func seedOwnedTenant(t *testing.T, app *App, slug string, ownerUserID int64) int64 {
	t.Helper()
	ctx := context.Background()
	tn := &tenant.Tenant{Slug: slug, Name: slug, Status: tenant.StatusActive}
	if err := app.TenantRepo.CreateTenant(ctx, tn); err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	if err := app.TenantRepo.SetOwnerUserID(ctx, tn.ID, ownerUserID); err != nil {
		t.Fatalf("set owner for %s: %v", slug, err)
	}
	return tn.ID
}

// ownerAuthCtx 造一个「已登录(id=userID)、主站 Host」的 gin 上下文（不经 TenantMiddleware，
// 故 Host 解析不出任何租户 → 证明解析纯粹来自 owner，与 Host 无关）。
func ownerAuthCtx(t *testing.T, userID int64) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/tenant/earnings", nil)
	req.Host = "api.wedreamhub.com" // 主站 Host：无租户映射
	c.Request = req
	c.Set("id", int(userID)) // 模拟 new-api UserAuth 写入的 session 用户
	return c, w
}

// TestAgentOwnerAuthByUser_ResolvesOwnedTenantHostIndependent 覆盖 4 条安全断言：
// (a) owner 在主站 Host → 解析到自己的租户并放行；(b) 已登录非 owner → 403 中止；
// (c) 无归属用户 → 403 中止；(d) owner A 永不解析到 owner B 的租户（双向隔离）。
func TestAgentOwnerAuthByUser_ResolvesOwnedTenantHostIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newOwnerAuthTestApp(t)
	tidA := seedOwnedTenant(t, app, "alpha", 100) // 用户 100 拥有租户 A
	tidB := seedOwnedTenant(t, app, "beta", 200)  // 用户 200 拥有租户 B

	// (a) owner 100 在主站 Host → 放行 + agentTenantID == A。
	cA, wA := ownerAuthCtx(t, 100)
	app.AgentOwnerAuthByUser()(cA)
	if cA.IsAborted() || wA.Code != http.StatusOK {
		t.Fatalf("owner 100: aborted=%v code=%d, want not-aborted/200", cA.IsAborted(), wA.Code)
	}
	if got := agentTenantID(cA); got != tidA {
		t.Fatalf("owner 100 resolved to tenant %d, want %d (A)", got, tidA)
	}
	// (d) 双向隔离：A 的上下文绝不是 B 的租户。
	if agentTenantID(cA) == tidB {
		t.Fatalf("owner A must NEVER resolve to owner B's tenant")
	}
	cB, _ := ownerAuthCtx(t, 200)
	app.AgentOwnerAuthByUser()(cB)
	if got := agentTenantID(cB); got != tidB {
		t.Fatalf("owner 200 resolved to tenant %d, want %d (B)", got, tidB)
	}

	// (b)+(c) 已登录但不拥有任何租户 → 403 中止，且不写 agentTenantID。
	cNone, wNone := ownerAuthCtx(t, 999)
	app.AgentOwnerAuthByUser()(cNone)
	if !cNone.IsAborted() || wNone.Code != http.StatusForbidden {
		t.Fatalf("non-owner 999: aborted=%v code=%d, want abort/403", cNone.IsAborted(), wNone.Code)
	}
	if got := agentTenantID(cNone); got != 0 {
		t.Fatalf("non-owner must not have an agent tenant set, got %d", got)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (`TenantByOwner` / `AgentOwnerAuthByUser` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestAgentOwnerAuthByUser
```

- [ ] **Step 3:** Add the owner→tenant reverse lookup. In `internal/tenant/gormrepo/gormrepo.go`, append after `OwnerUserID` (~253):
```go
// TenantByOwner 直读某 owner_user_id 拥有的租户（owner→tenant 反查，OwnerUserID 的逆向；
// 走 idx_tenants_owner 索引）。业务上 owner 1:1 独占一租户（设代理时 ownerTaken 保证唯一）。
// owner_user_id<=0 直接返回 ErrTenantNotFound（主站/未归属租户默认 owner_user_id=0，绝不被空 session 误匹配）。
// 无匹配返回 (nil, ErrTenantNotFound)。供 owner-based 代理自助鉴权（AgentOwnerAuthByUser）做 Host 无关解析。
func (r *Repo) TenantByOwner(ctx context.Context, ownerUserID int64) (*tenant.Tenant, error) {
	if ownerUserID <= 0 {
		return nil, tenant.ErrTenantNotFound
	}
	var row tenantRow
	err := r.db.WithContext(ctx).Take(&row, "owner_user_id = ?", ownerUserID).Error
	return mapTenantResult(&row, err) // ErrRecordNotFound → tenant.ErrTenantNotFound
}
```

- [ ] **Step 4:** Add the middleware. In `internal/mtwire/agent.go`, append after `AgentOwnerAuth` (~327):
```go
// AgentOwnerAuthByUser 是代理自助端点的 owner-based 权威防线：从登录用户「拥有的租户」
// （tenants.owner_user_id == 当前 session 用户，1:1）解析 agentTenantID，与 Host 无关——
// 故 L0 无子域名也能在主站访问自己的控制台，L1 在子域名同样解析到自己的租户。
// 安全不变量：只解析到「当前用户拥有的」那一个租户；绝不接受客户端传 tenant_id；
// 未登录 / 不拥有任何租户 → 403 中止（不放行）。须挂在 new-api UserAuth 之后。
// 替代 agent-self 组原先的 Host-based AgentOwnerAuth（后者留作 HandleAgentContext 的 Host 判定，不删）。
func (a *App) AgentOwnerAuthByUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := int64(c.GetInt("id"))
		if userID <= 0 {
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		t, err := a.TenantRepo.TenantByOwner(c.Request.Context(), userID)
		if err != nil {
			// 该用户不拥有任何代理租户（含 ErrTenantNotFound）→ 403，绝不放行、绝不回退 Host。
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		c.Set(ginKeyAgentTenant, t.ID) // handler 只认这个已校验的租户 ID
		c.Next()
	}
}
```

- [ ] **Step 5:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestAgentOwnerAuthByUser ./internal/tenant/...
```

- [ ] **Step 6:** Wire the `agentSelf` group onto the new middleware. In `router/mt-router.go` (~72-73), replace:
```go
		// 代理自助（owner 维度）：UserAuth + AgentOwnerAuth（权威校验 Host 租户 owner == 当前用户）。
		agentSelf := tenantGroup.Group("", middleware.UserAuth(), app.AgentOwnerAuth())
```
  with:
```go
		// 代理自助（owner 维度）：UserAuth + AgentOwnerAuthByUser（从登录用户「拥有的租户」解析 agentTenantID，
		// 与 Host 无关 → L0 无子域名也能在主站访问自己的控制台；L1 在子域名同样解析到自己的租户）。
		agentSelf := tenantGroup.Group("", middleware.UserAuth(), app.AgentOwnerAuthByUser())
```
  Everything inside the block (including Task 3's `agentIndependent` sub-group) is unchanged — all those handlers read `agentTenantID(c)`, which the new middleware still populates.

- [ ] **Step 7:** Verify no agent-self handler depends on Host beyond `agentTenantID`. Grep the handlers registered under `agentSelf` for `tenantFrom(`:
```
cd /Users/cc/newapi628 && grep -rn "tenantFrom(c)" internal/mtwire/siteconfig.go internal/mtwire/custom_domain.go internal/mtwire/distribution.go internal/mtwire/report.go
```
  Expected/allowed hits: **only** `siteconfig.go` `HandleAgentGetSiteConfig` (~112) and `HandleAgentUpdateSiteConfig` (~132), which read the Host tenant for the site-config response. This is **safe**: site-config lives in Task 3's `agentIndependent` (level>=1) sub-group, and level>=1 agents always have a subdomain (Task 4), so on their independent site `TenantMiddleware` resolves the Host and `tenantFrom(c)` == the owner's tenant == `agentTenantID(c)`. Confirm `custom_domain.go` uses `agentTenantID(c)` only (no `tenantFrom`), and that `HandleRedeem` (distribution.go ~319) and `HandleAgentContext` are **not** in `agentSelf` (they are UserAuth-only user/signal endpoints, unaffected). If a future non-independent agent-self handler is found reading `tenantFrom(c)`, switch it to `a.TenantService.Get(ctx, agentTenantID(c))`.

- [ ] **Step 8:** Run build + full mtwire tests — expect PASS.
```
cd /Users/cc/newapi628 && go build ./... && go test ./internal/mtwire/ ./internal/tenant/...
```

- [ ] **Step 9:** Commit.
```
cd /Users/cc/newapi628 && git add internal/tenant/gormrepo/gormrepo.go internal/mtwire/agent.go internal/mtwire/agent_owner_by_user_test.go router/mt-router.go && git commit -m "feat(agent): owner-based agent-console resolution (L0 reaches console on main site)"
```

- [ ] **Step 10:** SERVER verification (after deploy — see Task 8 finish), against `newapi_test` (3100): create an L0 agent (owner user U, no subdomain); log in as U on the **main site** `api.wedreamhub.com`; call an agent-self route → resolves to U's tenant (200), NOT 403. A non-owner logged-in user → 403. Command template:
```
# on server (session cookie of owner U vs a non-owner):
curl -s -b owner.cookies    -H "Host: api.wedreamhub.com" http://127.0.0.1:3100/api/tenant/earnings   # owner U → 200, own tenant
curl -s -b nonowner.cookies -H "Host: api.wedreamhub.com" http://127.0.0.1:3100/api/tenant/earnings   # non-owner → 403 AGENT_FORBIDDEN
```

---

### Task 10: Per-agent promotion metrics for the admin edit drawer (总充值 / 分润收益 / 下级用户数). SERIAL: `router/mt-router.go`

Give the admin the data to judge a promotion (L0→L1): a read-only `GET /api/admin/agents/:id/metrics` returning `{ recharge_total_cny, commission_earned_cny, downstream_user_count }`, shown in the agent edit drawer. The backend **reuses the finance-report aggregates** — `reportrepo.RechargePaid` (钱包充值实付, summed over a lifetime window) and `reportrepo.WalletTotals(...).TotalEarnedCNY` (累计分润收益, already lifetime) — and adds one thin `CountTenantUsers` query for the downstream count (no existing per-agent user-count aggregate). `App.ReportRepo` is the concrete `*reportrepo.Repo`, so the new repo method needs no interface change.

> **Serial:** edits `router/mt-router.go` (also touched by Task 3 + Task 9) — coordinate. New route `GET /api/admin/agents/:id/metrics` does not collide with `PATCH /api/admin/agents/:id` (different verb + deeper path). Frontend `bun run build` is server-side (per Global Constraints); local gate is `bun run typecheck` + `bun run lint`. This task assumes **Task 6's drawer edits are already applied** (level select present; `type` removed from submit) since it appends after Task 8.

**Files:**
- `internal/report/reportrepo/reportrepo.go` (append `CountTenantUsers` near `scopeUserIDs` ~1319)
- `internal/mtwire/agent.go` (add `"time"` to imports ~9-27; add `agentMetricsOut` DTO near `agentOut` ~385; add `HandleAdminAgentMetrics` after `HandleAdminUpdateAgent` ~612)
- `internal/mtwire/agent_metrics_test.go` (NEW)
- `router/mt-router.go` (`adminAgentGroup` ~166-170)
- `web/default/src/features/agents/types.ts` (append `AgentMetrics`)
- `web/default/src/features/agents/api.ts` (append `getAgentMetrics`)
- `web/default/src/features/agents/components/agent-mutate-drawer.tsx` (read-only metrics section when editing)
- `web/default/src/i18n/locales/en.json` + `zh.json` (new keys)

**Interfaces:**
- Backend: `GET /api/admin/agents/:id/metrics` (AdminAuth) → `{ "recharge_total_cny": float, "commission_earned_cny": float, "downstream_user_count": int }`.
- Produces: `(*reportrepo.Repo).CountTenantUsers(ctx context.Context, tenantID int64) (int64, error)` — `count(users) where tenant_id=? and deleted_at is null` (same filter as the existing `scopeUserIDs`).
- Reuses (cite): `reportrepo.RechargePaid(ctx, &tid, 1, now)` (总充值) + `reportrepo.WalletTotals(ctx, &tid).TotalEarnedCNY` (分润收益).
- Frontend TS: `AgentMetrics` type; `getAgentMetrics(id): Promise<ApiResponse<AgentMetrics>>`.

- [ ] **Step 1:** Write the failing backend httptest. Create `internal/mtwire/agent_metrics_test.go`:
```go
package mtwire

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	reportrepo "github.com/QuantumNous/new-api/internal/report/reportrepo"
)

// newAgentMetricsTestApp 造最小 App：sqlite + 报表聚合读到的最小裸表（沿用 grouphook_test 裸表习惯）。
func newAgentMetricsTestApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	for _, s := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME)`,
		`CREATE TABLE payment_orders (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, type TEXT, status TEXT, actual_paid REAL, created_at DATETIME)`,
		`CREATE TABLE agent_wallets (tenant_id INTEGER PRIMARY KEY, withdrawable_balance REAL DEFAULT 0, frozen_withdraw_amount REAL DEFAULT 0, total_earned REAL DEFAULT 0, api_balance REAL DEFAULT 0)`,
	} {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return &App{DB: db, ReportRepo: reportrepo.New(db)}
}

// TestHandleAdminAgentMetrics_ReusesFinanceAggregates 验证端点复用 RechargePaid + WalletTotals
// 并加一条下级计数：租户 5 → 总充值 150、分润 23.8、下级用户 2（软删/别租户排除）。
func TestHandleAdminAgentMetrics_ReusesFinanceAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newAgentMetricsTestApp(t)
	// created_at/deleted_at 用固定 2023 UTC 秒，稳落在端点的 [1, now] 生命周期窗口内（避开 now 截断边界）。
	seedTS := time.Unix(1_700_000_000, 0).UTC()

	// 下级用户：租户 5 有 2 活跃(1,2) + 1 软删(3，排除)；租户 9 的 1 个(4，排除)。
	if err := app.DB.Exec(
		`INSERT INTO users (id, tenant_id, deleted_at) VALUES (1,5,NULL),(2,5,NULL),(3,5,?),(4,9,NULL)`, seedTS,
	).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	// 总充值：租户 5 两笔已入账 100+50=150；一笔 pending(999，排除)。
	if err := app.DB.Exec(
		`INSERT INTO payment_orders (tenant_id, type, status, actual_paid, created_at) VALUES
		 (5,'recharge','credited',100,?),(5,'recharge','credited',50,?),(5,'recharge','pending',999,?)`,
		seedTS, seedTS, seedTS,
	).Error; err != nil {
		t.Fatalf("seed payment_orders: %v", err)
	}
	// 分润收益：钱包累计已赚 23.8。
	if err := app.DB.Exec(`INSERT INTO agent_wallets (tenant_id, total_earned) VALUES (5, 23.8)`).Error; err != nil {
		t.Fatalf("seed agent_wallets: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/agents/5/metrics", nil)
	c.Params = gin.Params{{Key: "id", Value: "5"}}
	app.HandleAdminAgentMetrics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			RechargeTotalCNY    float64 `json:"recharge_total_cny"`
			CommissionEarnedCNY float64 `json:"commission_earned_cny"`
			DownstreamUserCount int64   `json:"downstream_user_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false; body=%s", w.Body.String())
	}
	if env.Data.RechargeTotalCNY != 150 {
		t.Fatalf("recharge_total_cny = %v, want 150", env.Data.RechargeTotalCNY)
	}
	if env.Data.CommissionEarnedCNY != 23.8 {
		t.Fatalf("commission_earned_cny = %v, want 23.8", env.Data.CommissionEarnedCNY)
	}
	if env.Data.DownstreamUserCount != 2 {
		t.Fatalf("downstream_user_count = %v, want 2", env.Data.DownstreamUserCount)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (`CountTenantUsers` / `HandleAdminAgentMetrics` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestHandleAdminAgentMetrics
```

- [ ] **Step 3:** Add the thin downstream-count query. In `internal/report/reportrepo/reportrepo.go`, append near `scopeUserIDs` (~1319):
```go
// CountTenantUsers 返回归属某租户的下级用户数（软删除排除），与 scopeUserIDs 同过滤口径
// （tenant_id=? AND deleted_at IS NULL）。供 admin 代理升档决策指标（无现成 per-agent 用户计数聚合，
// 故补此一条薄查询）。
func (r *Repo) CountTenantUsers(ctx context.Context, tenantID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Table("users").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
```

- [ ] **Step 4:** Add the DTO + handler. In `internal/mtwire/agent.go`:
  - Add `"time"` to the import block (~9-27), e.g. after `"strings"`:
```go
	"strconv"
	"strings"
	"time"
```
  - Add the DTO next to `agentOut` (~385):
```go
// agentMetricsOut 是 GET /api/admin/agents/:id/metrics 响应：代理升档决策的只读指标
// （总充值 / 累计分润 / 下级用户数），复用 reportrepo 财务聚合 + 一条下级计数薄查询。
type agentMetricsOut struct {
	RechargeTotalCNY    float64 `json:"recharge_total_cny"`
	CommissionEarnedCNY float64 `json:"commission_earned_cny"`
	DownstreamUserCount int64   `json:"downstream_user_count"`
}
```
  - Append the handler after `HandleAdminUpdateAgent` (~612):
```go
// HandleAdminAgentMetrics GET /api/admin/agents/:id/metrics —— 代理升档决策指标（需 AdminAuth；id=tenant_id）。
// 只读复用 reportrepo：总充值=RechargePaid([1,now] 求和)；分润收益=WalletTotals.TotalEarnedCNY（累计）；
// 下级用户数=CountTenantUsers（薄查询）。绝不接受客户端传除 :id 外的任何口径。
func (a *App) HandleAdminAgentMetrics(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tenantID <= 0 {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	// 总充值：生命周期窗口 [1, now] 上复用 RechargePaid（单租户 map 至多一条，求和即总额）。
	rechargeMap, err := a.ReportRepo.RechargePaid(ctx, &tenantID, 1, time.Now().Unix())
	if err != nil {
		respondErr(c, err)
		return
	}
	var rechargeTotal float64
	for _, v := range rechargeMap {
		rechargeTotal += v
	}
	// 分润收益：钱包累计已赚（生命周期；缺行返回零值不报错）。
	wallet, err := a.ReportRepo.WalletTotals(ctx, &tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	// 下级用户数：薄计数查询（tenant_id=? AND deleted_at IS NULL）。
	userCount, err := a.ReportRepo.CountTenantUsers(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, agentMetricsOut{
		RechargeTotalCNY:    round2(rechargeTotal),
		CommissionEarnedCNY: round2(wallet.TotalEarnedCNY),
		DownstreamUserCount: userCount,
	})
}
```

- [ ] **Step 5:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestHandleAdminAgentMetrics
```

- [ ] **Step 6:** Register the route. In `router/mt-router.go` `adminAgentGroup` block (~166-170), add after the PATCH line:
```go
		adminAgentGroup.GET("", app.HandleAdminListAgents)
		adminAgentGroup.POST("", app.HandleAdminCreateAgent)
		adminAgentGroup.PATCH("/:id", app.HandleAdminUpdateAgent)
		adminAgentGroup.GET("/:id/metrics", app.HandleAdminAgentMetrics) // 升档决策指标（只读）
```

- [ ] **Step 7:** Run build + mtwire/report tests — expect PASS. Then commit backend.
```
cd /Users/cc/newapi628 && go build ./... && go test ./internal/mtwire/ ./internal/report/...
cd /Users/cc/newapi628 && git add internal/report/reportrepo/reportrepo.go internal/mtwire/agent.go internal/mtwire/agent_metrics_test.go router/mt-router.go && git commit -m "feat(agent): admin per-agent promotion metrics endpoint (recharge/commission/downstream)"
```

- [ ] **Step 8:** Frontend types + API. In `web/default/src/features/agents/types.ts`, append:
```ts
/** Read-only promotion-decision metrics for one agent (GET /api/admin/agents/:id/metrics). */
export interface AgentMetrics {
  recharge_total_cny: number
  commission_earned_cny: number
  downstream_user_count: number
}
```
  In `web/default/src/features/agents/api.ts`, add `AgentMetrics` to the `./types` import and append:
```ts
export async function getAgentMetrics(
  id: number
): Promise<ApiResponse<AgentMetrics>> {
  const res = await api.get(`/api/admin/agents/${id}/metrics`)
  return res.data
}
```

- [ ] **Step 9:** Show the 3 read-only numbers in the drawer when editing. In `web/default/src/features/agents/components/agent-mutate-drawer.tsx`:
  - Extend the lucide import (~23) to include `TrendingUp`: `import { CreditCard, TrendingUp, UserCog } from 'lucide-react'`.
  - Add `getAgentMetrics` to the `../api` import (~58): `import { createAgent, getAgentMetrics, updateAgent } from '../api'`.
  - Add `cny` to the `../lib` import block (~59-65): add `cny,` alongside the existing imports.
  - Inside the component (after the existing `useQuery` for users, ~92-99), fetch metrics when editing:
```tsx
  // Read-only promotion metrics — only when editing an existing agent.
  const { data: metricsRes } = useQuery({
    queryKey: ['admin-agent-metrics', currentRow?.id],
    queryFn: () => getAgentMetrics(currentRow!.id),
    enabled: open && isEdit && !!currentRow?.id,
  })
  const metrics = metricsRes?.data
```
  - Render a read-only section (place it right after the Identity `SideDrawerSection`, before Commercials ~300). Only shown when editing:
```tsx
            {isEdit && (
              <SideDrawerSection>
                <h3 className='flex items-center gap-2 text-sm font-medium'>
                  <TrendingUp className='h-4 w-4' />
                  {t('Promotion metrics')}
                </h3>
                <div className='grid grid-cols-3 gap-3'>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Total recharge (¥)')}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? cny(metrics.recharge_total_cny) : '—'}
                    </div>
                  </div>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Commission earned (¥)')}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? cny(metrics.commission_earned_cny) : '—'}
                    </div>
                  </div>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Downstream users')}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? String(metrics.downstream_user_count) : '—'}
                    </div>
                  </div>
                </div>
                <FormDescription>
                  {t(
                    'Lifetime totals to help you decide whether to promote this agent to independent (level 1).'
                  )}
                </FormDescription>
              </SideDrawerSection>
            )}
```
  (`cny` is the shared ¥ formatter exported from `../lib`, same one `agents-columns.tsx` uses.)

- [ ] **Step 10:** i18n keys. In `en.json` (identity values):
```json
    "Promotion metrics": "Promotion metrics",
    "Total recharge (¥)": "Total recharge (¥)",
    "Commission earned (¥)": "Commission earned (¥)",
    "Downstream users": "Downstream users",
    "Lifetime totals to help you decide whether to promote this agent to independent (level 1).": "Lifetime totals to help you decide whether to promote this agent to independent (level 1).",
```
  In `zh.json`:
```json
    "Promotion metrics": "升档参考指标",
    "Total recharge (¥)": "总充值 (¥)",
    "Commission earned (¥)": "分润收益 (¥)",
    "Downstream users": "下级用户数",
    "Lifetime totals to help you decide whether to promote this agent to independent (level 1).": "累计数据，辅助你判断是否将该代理升为独立档（level 1）。",
```

- [ ] **Step 11:** Frontend gate — typecheck + lint (real `bun run build` is server-side).
```
cd /Users/cc/newapi628/web/default && node scripts/sync-i18n.mjs && bun run typecheck && bun run lint src/features/agents
```

- [ ] **Step 12:** Commit frontend.
```
cd /Users/cc/newapi628 && git add web/default/src/features/agents web/default/src/i18n/locales && git commit -m "feat(agent-ui): show per-agent promotion metrics (recharge/commission/downstream) in edit drawer"
```

- [ ] **Step 13:** SERVER verification (after deploy): open the admin agents page → edit an agent that has recharge/commission history → the drawer shows non-zero 总充值 / 分润收益 / 下级用户数 matching the finance report for that tenant; `curl -s -b admin.cookies http://127.0.0.1:3100/api/admin/agents/<tid>/metrics` returns the same three numbers.

---

**Tasks 11-15 (below) implement the money model v2 from spec §9 (三线分离：差异化只在消耗计费层；L0 提成 / L1 差价) inside the consumption-billing hook.** This SUPERSEDES the earlier v1 task list (old Tasks 11-15, "L0 提成+9折 / L1 差价") — the 9折/`invite_discount_rate` machinery is dropped entirely, not carried forward. 红线 reminder (applies to every task below): this touches the CONSUMPTION BILLING path. Every new code path must be (a) idempotent on `requestID` (no double-charge/double-credit on retry, via the existing `AgentRepo.AppendEarning` `(tenant_id,source_type,source_id)` unique-index pattern), (b) best-effort / failure-isolated (an error here must never block the user's actual request or the core quota deduction — same `defer recover()` + log-and-continue posture as the existing `creditConsumeCommission`), and (c) structurally incapable of crediting a negative/underflowing markup (卖价 ≥ 底价 is enforced both at set-time, Task 12, and re-checked defensively at credit-time, Task 13). Tasks 13/14 both edit `internal/mtwire/agent.go`'s `creditConsumeCommission` in strict sequence (13 → 14) — same file-ordering discipline as the plan's existing note about Tasks 1/2/4/5. Local gate for Tasks 11-15 is `go test` (no server dependency for red/green); `go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/...` is the local "does everything still compile" smoke check — **note:** a bare `go build ./...` from repo root currently fails in this checkout on `main.go`'s `//go:embed web/classic/dist` (that directory is a `bun run build` artifact that isn't present locally; per Global Constraints, full builds happen on the server). This is pre-existing and unrelated to Tasks 11-15 — the package-scoped command above covers every package they touch.

### Task 11: Per-agent 底价倍率 (`AgentParams.BottomPriceRatio`) — schema + admin DTO/handler + validation

Introduces the one new field the rest of Part 2 depends on (spec §9.7): a per-agent, admin-set floor for that agent's consumption-pricing 卖价, independent of the existing `PackageDiscount`/`DiscountFloor` (those two serve the tokenplan package-discount guard in `agentService.SetAgentType` — spec §9.1 line ②, unrelated and untouched). Purely additive: new struct field, new DB column that GORM `AutoMigrate` adds automatically on next boot (same as Task 1's `CanAPI` — no destructive migration, no `information_schema` guard needed). `0` means "not configured"; the fallback-to-platform-baseline behaviour for that case is wired in Task 12 (this task only adds the field itself end-to-end: struct → validation → gormrepo → admin DTOs/handlers).

**Files:**
- `internal/agent/model.go` (`AgentParams` ~11-26; `Validate()` ~30-43).
- `internal/agent/model_test.go` (`TestAgentParams_Validate` ~9-30 — extend cases).
- `internal/agent/gormrepo/gormrepo.go` (`profileRow` ~27-38; `SetAgentType` ~107-128; `GetAgentType` ~131-148; `AgentRow` ~327-335; `ListProfiles` ~338-356).
- `internal/agent/gormrepo/gormrepo_test.go` (append round-trip test).
- `internal/mtwire/agent.go` (`agentOut` ~470-485; `agentCreateIn` ~513-522; `agentPatchIn` ~525-533; `HandleAdminCreateAgent` ~544-608; `HandleAdminUpdateAgent` ~653-719, patch block ~675-689; `HandleAdminListAgents` ~611-649; `buildAgentOut` ~896-907).

**Interfaces:**
- Produces: `agent.AgentParams.BottomPriceRatio float64` — `0` = unconfigured (Task 12 falls back to platform baseline); `>0` = admin-set consumption-pricing floor for this agent, uniform across all model groups.
- No interface/method-signature changes anywhere — `AgentService`/`AgentRepo`/`MemRepo` all pass `AgentParams` through opaquely (whole-struct copy, no field-by-field mapping), so `internal/agent/port.go`, `service.go`, and `repo.go` (MemRepo) need **zero code changes** for this task.

- [ ] **Step 1:** Write the failing tests. Extend `internal/agent/model_test.go`'s `TestAgentParams_Validate` cases slice (~15-23):
```go
	cases := []struct {
		name     string
		p        AgentParams
		wantCode string // "" 表示放行
	}{
		{"all zero ok", AgentParams{}, ""},
		{"typical ok", AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 3}, ""},
		{"boundary ratios ok", AgentParams{CommissionRatio: 1, PackageDiscount: 1}, ""},
		{"bottom price ratio ok", AgentParams{BottomPriceRatio: 0.7}, ""},
		{"negative cost", AgentParams{CostPrice: -0.01}, CodeAgentTypeInvalid},
		{"commission below 0", AgentParams{CommissionRatio: -0.1}, CodeAgentTypeInvalid},
		{"commission above 1", AgentParams{CommissionRatio: 1.01}, CodeAgentTypeInvalid},
		{"discount below 0", AgentParams{PackageDiscount: -0.1}, CodeAgentTypeInvalid},
		{"discount above 1", AgentParams{PackageDiscount: 1.5}, CodeAgentTypeInvalid},
		{"negative level", AgentParams{Level: -1}, CodeAgentTypeInvalid},
		{"negative bottom price ratio", AgentParams{BottomPriceRatio: -0.01}, CodeAgentTypeInvalid},
	}
```
  Note the `"all zero ok"` case is unchanged and MUST keep passing — `BottomPriceRatio: 0` is a legal, meaningful value (spec §9.7: "未配置"), not an error.
  Append to `internal/agent/gormrepo/gormrepo_test.go`:
```go
// TestGetAgentType_RoundTripsBottomPriceRatio 确认 bottom_price_ratio 随资料持久化并读回
// （spec agent-tiering §9.7：消耗计费底价倍率，独立于 package_discount/discount_floor）。
func TestGetAgentType_RoundTripsBottomPriceRatio(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if err := r.SetAgentType(ctx, 5, agent.AgentParams{Level: 1, BottomPriceRatio: 0.7}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, found, err := r.GetAgentType(ctx, 5)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got.BottomPriceRatio != 0.7 {
		t.Fatalf("BottomPriceRatio = %v, want 0.7", got.BottomPriceRatio)
	}
	// 未配置底价倍率的代理：零值，不是错误（spec §9.7 “0=未配置”）。
	if err := r.SetAgentType(ctx, 6, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set unconfigured: %v", err)
	}
	got2, _, _ := r.GetAgentType(ctx, 6)
	if got2.BottomPriceRatio != 0 {
		t.Fatalf("BottomPriceRatio = %v, want 0 (unconfigured)", got2.BottomPriceRatio)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (compile error: `BottomPriceRatio` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/agent/... -run 'AgentParams_Validate|RoundTripsBottomPriceRatio'
```

- [ ] **Step 3:** Add the field + validation in `internal/agent/model.go`. In `AgentParams` (after `DiscountFloor float64`, ~26):
```go
	// DiscountFloor 主站折扣/倍率保护下限：设代理时经 PricingGuard 校验 PackageDiscount ≥ DiscountFloor。
	// 本轮新增字段（design 的 params 仅列 4 项），见报告默认假设。
	DiscountFloor float64
	// BottomPriceRatio 消耗计费底价倍率（spec agent-tiering §9.7；管理员按代理设，与上面 PackageDiscount/
	// DiscountFloor 完全独立——那两个服务 tokenplan 套餐折扣保护线，这个服务「模型消耗」计费的四档价格
	// 阶梯下限）。0 = 未配置（HandleAgentSetGroupRatio 的地板回退平台基准，见 internal/mtwire/distribution.go
	// consumeFloorRatio，Task 12）；>0 = 该代理卖价（tenant_groups 覆盖倍率）的下限，同时是差价入账公式
	// （Task 13 creditRatioMarkup）的减数——两处必须同一口径，否则记账错误（见 consumeFloorRatio 注释）。
	// 跨全部模型分组统一一个比例（不逐分组设——底价是「对该代理的批发折扣比例」，与逐模型定价的 ModelRatio
	// 相乘即天然逐模型生效，无需再逐分组重复配置）。
	BottomPriceRatio float64
```
  In `Validate()` (~30-43), add a case (after the `Level < 0` case):
```go
func (p AgentParams) Validate() error {
	switch {
	case p.CostPrice < 0:
		return ErrAgentTypeInvalid
	case p.CommissionRatio < 0 || p.CommissionRatio > 1:
		return ErrAgentTypeInvalid
	case p.PackageDiscount < 0 || p.PackageDiscount > 1:
		return ErrAgentTypeInvalid
	case p.Level < 0:
		return ErrAgentTypeInvalid
	case p.BottomPriceRatio < 0:
		return ErrAgentTypeInvalid
	default:
		return nil
	}
}
```
  Note: deliberately **no upper bound** on `BottomPriceRatio` (unlike `PackageDiscount`'s `[0,1]`) — it's a ratio multiplied against `ModelRatio`, not a discount fraction; a value `>1` is unusual but not structurally invalid, and the spec doesn't ask for a cap here.

- [ ] **Step 4:** Persist `bottom_price_ratio` in the GORM repo. In `internal/agent/gormrepo/gormrepo.go`:
  - Add the column to `profileRow` (after `DiscountFloor`, ~35):
```go
	DiscountFloor    float64   `gorm:"column:discount_floor;type:decimal(20,8);not null;default:0"`
	BottomPriceRatio float64   `gorm:"column:bottom_price_ratio;type:decimal(20,8);not null;default:0"`
```
  - In `SetAgentType` (~108-128), add `BottomPriceRatio: p.BottomPriceRatio,` to the `profileRow{...}` literal (after `DiscountFloor: p.DiscountFloor,`) and `"bottom_price_ratio"` to the `AssignmentColumns` list:
```go
func (r *Repo) SetAgentType(ctx context.Context, tenantID int64, p agent.AgentParams) error {
	now := r.now()
	row := profileRow{
		TenantID:         tenantID,
		Level:            p.Level,
		CanAPI:           p.CanAPI,
		CostPriceCNY:     p.CostPrice,
		PackageDiscount:  p.PackageDiscount,
		CommissionRatio:  p.CommissionRatio,
		DiscountFloor:    p.DiscountFloor,
		BottomPriceRatio: p.BottomPriceRatio,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"level", "can_api", "cost_price_cny", "package_discount",
			"commission_ratio", "discount_floor", "bottom_price_ratio", "updated_at",
		}),
	}).Create(&row).Error
}
```
  - In `GetAgentType` (~131-148), add `BottomPriceRatio: row.BottomPriceRatio,` to the returned `agent.AgentParams{...}` literal:
```go
	return agent.AgentParams{
		CostPrice:        row.CostPriceCNY,
		PackageDiscount:  row.PackageDiscount,
		CommissionRatio:  row.CommissionRatio,
		Level:            row.Level,
		CanAPI:           row.CanAPI,
		DiscountFloor:    row.DiscountFloor,
		BottomPriceRatio: row.BottomPriceRatio,
	}, true, nil
```
  - `AgentRow` (~327-335) + `ListProfiles` (~338-356): add the field too (Step 6 needs it for `HandleAdminListAgents`):
```go
type AgentRow struct {
	TenantID         int64
	UserID           int64
	Level            int
	CanAPI           bool
	CostPriceCNY     float64
	PackageDiscount  float64
	CommissionRatio  float64
	BottomPriceRatio float64
}
```
```go
		out = append(out, AgentRow{
			TenantID:         p.TenantID,
			UserID:           p.UserID,
			Level:            p.Level,
			CanAPI:           p.CanAPI,
			CostPriceCNY:     p.CostPriceCNY,
			PackageDiscount:  p.PackageDiscount,
			CommissionRatio:  p.CommissionRatio,
			BottomPriceRatio: p.BottomPriceRatio,
		})
```

- [ ] **Step 5:** Run — expect PASS (agent + gormrepo packages).
```
cd /Users/cc/newapi628 && go test ./internal/agent/...
```

- [ ] **Step 6:** Wire the field through the admin HTTP layer in `internal/mtwire/agent.go`:
  - `agentOut` (~470-485) — add after `CommissionRatio float64 \`json:"commission_ratio"\``:
```go
	CommissionRatio  float64 `json:"commission_ratio"`
	BottomPriceRatio float64 `json:"bottom_price_ratio"`
```
  - `agentCreateIn` (~513-522) — add after `DiscountFloor float64 \`json:"discount_floor"\``:
```go
	DiscountFloor    float64 `json:"discount_floor"`
	BottomPriceRatio float64 `json:"bottom_price_ratio"`
```
  - `agentPatchIn` (~525-533) — add after `DiscountFloor *float64 \`json:"discount_floor"\``:
```go
	DiscountFloor    *float64 `json:"discount_floor"`
	BottomPriceRatio *float64 `json:"bottom_price_ratio"`
```
  - `HandleAdminCreateAgent` (~550-556) — add `BottomPriceRatio: in.BottomPriceRatio,` to the `params := agent.AgentParams{...}` literal:
```go
	params := agent.AgentParams{
		CostPrice:        in.CostPriceCNY,
		PackageDiscount:  in.PackageDiscount,
		CommissionRatio:  in.CommissionRatio,
		Level:            in.Level,
		DiscountFloor:    in.DiscountFloor,
		BottomPriceRatio: in.BottomPriceRatio,
	}
```
  - `HandleAdminUpdateAgent` (~675-689) — add after the `DiscountFloor` patch block:
```go
	if in.DiscountFloor != nil {
		curParams.DiscountFloor = *in.DiscountFloor
	}
	if in.BottomPriceRatio != nil {
		curParams.BottomPriceRatio = *in.BottomPriceRatio
	}
```
  - `HandleAdminListAgents` (~631-646) — add `BottomPriceRatio: p.BottomPriceRatio,` inside the `agentOut{...}` literal (after `CommissionRatio: p.CommissionRatio,`; `p` here is the `agent.AgentRow` from Step 4).
  - `buildAgentOut` (~896-907) — add `BottomPriceRatio: p.BottomPriceRatio,` to the returned `agentOut{...}` literal (after `CommissionRatio: p.CommissionRatio,`; `p` here is `agent.AgentParams`).

- [ ] **Step 7:** Run full build + agent/mtwire tests — expect PASS.
```
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/agent/... ./internal/mtwire/...
```

- [ ] **Step 8:** Commit.
```
cd /Users/cc/newapi628 && git add internal/agent internal/mtwire/agent.go && git commit -m "feat(agent): add per-agent BottomPriceRatio field (consumption-pricing floor, spec §9.7)"
```

---

### Task 12: `HandleAgentSetGroupRatio` floor uses the agent's own 底价, and is gated to level≥1

Closes spec §9.3/§9.7 (卖价 floor = this agent's 底价, not the global baseline) and §9.5 ("L0 cannot set 卖价") together — both are edits to the same handler, and landing only one would leave the other spec requirement unmet. The gate reuses `a.ensureAgentLevel(c, 1)` (Task 3, already implemented — `internal/mtwire/agent.go:390-406`, `errAgentLevelLocked` already declared at `agent.go:41`); no new gate mechanism is introduced. This task also introduces `consumeFloorRatio`, the **single** function both this task's write path (卖价 floor) and Task 13's read path (差价 crediting) call — spec §9.7 is explicit that these two must never disagree about what "this agent's floor" means, or markup crediting silently over-pays the agent relative to what was actually enforced at set-time.

**Files:**
- `internal/mtwire/distribution.go` (`HandleAgentSetGroupRatio` ~397-435; `HandleAgentListGroups` ~373-395; append `consumeFloorRatio` next to `modelGroupBaseline` ~505-509).
- `internal/mtwire/distribution_test.go` (`newGroupRatioApp` ~142-160 — extend with `AgentRepo`/`AgentService`; `TestHandleAgentSetGroupRatio` ~162-203 — seed tenant to level 1 so it keeps passing under the new gate; `TestHandleAgentListGroups` ~205-252 — unaffected in assertions, only harness rewiring; append new tests).

**Interfaces:**
- Consumes: `App.ensureAgentLevel(c *gin.Context, min int) bool` (Task 3, existing, unchanged).
- Produces: `consumeFloorRatio(bottomPriceRatio float64, group string) float64` (pure function) — `bottomPriceRatio > 0` wins; else falls back to `modelGroupBaseline(group)`. Shared by this task and Task 13.

- [ ] **Step 1:** Write the failing tests. In `internal/mtwire/distribution_test.go`, add two imports (`"github.com/QuantumNous/new-api/internal/agent"` and `agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"`) to the import block, then replace `newGroupRatioApp` (~142-160):
```go
// newGroupRatioApp 装配 App：sqlite + model_groups + users + tenant_groups + agent 四表 + Repos。
// AgentRepo/AgentService 供 ensureAgentLevel（level 门禁）+ consumeFloorRatio（该代理底价，Task 12/
// spec §9.7）用；同一份底层 sqlite 存储，SetAgentType 写入的档位/底价对两者立即可见（无缓存分歧）。
func newGroupRatioApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := modelgroup.AutoMigrate(db); err != nil {
		t.Fatalf("model_groups migrate: %v", err)
	}
	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	ar := agentrepo.New(db)
	return &App{
		DB: db, ModelGroupRepo: modelgroup.New(db), TenantRepo: tenantrepo.New(db),
		AgentRepo: ar, AgentService: agent.NewService(ar, nil),
	}
}
```
  Update `TestHandleAgentSetGroupRatio` (~162-165) to seed tenant 7 to level 1 (required now that the gate exists — add right after `const tenantID = int64(7)`, before the existing model-group registration):
```go
func TestHandleAgentSetGroupRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(7)
	if err := app.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{Level: 1}); err != nil { // Task 12 门禁：需 L1
		t.Fatalf("set L1: %v", err)
	}
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	// ... rest of the function (the 4 sub-cases) is unchanged ...
```
  Append the new level-gate test:
```go
// TestHandleAgentSetGroupRatio_RequiresLevel1 覆盖分层门禁（spec §9.5）：L0（普通档）设卖价 →
// 403 AGENT_LEVEL_LOCKED，且不落 tenant_groups；L1（独立档）→ 200 放行（既有校验——下限/仅模型
// 分组——照常生效）。
func TestHandleAgentSetGroupRatio_RequiresLevel1(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios

	if err := app.AgentService.SetAgentType(ctx, 70, agent.AgentParams{Level: 0}); err != nil {
		t.Fatalf("set L0: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 71, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set L1: %v", err)
	}

	c, rec := newAgentCtx(70, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_LEVEL_LOCKED" {
		t.Fatalf("L0 must be locked, got %+v", r)
	}
	if _, found, _ := app.TenantRepo.LookupEnabledGroupRatio(ctx, 70, "claude-kiro"); found {
		t.Fatal("L0 must not have persisted a group override")
	}

	c, rec = newAgentCtx(71, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("L1 should be allowed, got %+v", r)
	}
}

// TestHandleAgentSetGroupRatio_FloorUsesAgentBottomPriceRatio 是本任务的核心用例（spec §9.7）：
// 一旦该代理配置了 BottomPriceRatio，卖价下限改用它而非全局平台基准——即便该值高于平台基准，
// 曾经合法的加价现在也可能被挡（下限收紧，不是放宽）。
func TestHandleAgentSetGroupRatio_FloorUsesAgentBottomPriceRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(72)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 平台基准 = 0.3

	// 该代理底价 0.5（高于平台基准 0.3）。
	if err := app.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{Level: 1, BottomPriceRatio: 0.5}); err != nil {
		t.Fatalf("set L1 with bottom price ratio: %v", err)
	}

	// 0.4：曾经（对平台基准=0.3 而言）合法，但现在 < 该代理底价 0.5 → 拒。
	c, rec := newAgentCtx(tenantID, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "RATIO_BELOW_FLOOR" {
		t.Fatalf("0.4 must be rejected by the agent's own 0.5 floor, got %+v", r)
	}

	// 0.5：等于该代理底价 → 放行（边界=允许，同 pricing.Guard.ValidateGroupRatio 的既有语义）。
	c, rec = newAgentCtx(tenantID, "PUT", `{"ratio":0.5}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("0.5 (== agent floor) should be admitted, got %+v", r)
	}
}

// TestHandleAgentListGroups_FloorReflectsAgentBottomPriceRatio 覆盖 spec §9.7 的展示口径：
// Floor 字段跟随该代理的 BottomPriceRatio；PlatformRatio 保持平台基准不变（两者语义分离）。
func TestHandleAgentListGroups_FloorReflectsAgentBottomPriceRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(73)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 平台基准 = 0.3
	if err := app.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{Level: 1, BottomPriceRatio: 0.6}); err != nil {
		t.Fatalf("set L1 with bottom price ratio: %v", err)
	}

	c, rec := newAgentCtx(tenantID, "GET", "", nil)
	app.HandleAgentListGroups(c)
	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("list groups should succeed, got %+v", r)
	}
	var rows []modelGroupRatioOut
	if err := json.Unmarshal(r.Data, &rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 model group, got %d (%+v)", len(rows), rows)
	}
	row := rows[0]
	if row.PlatformRatio != 0.3 {
		t.Fatalf("PlatformRatio must stay at platform baseline, got %v", row.PlatformRatio)
	}
	if row.Floor != 0.6 {
		t.Fatalf("Floor must reflect the agent's own BottomPriceRatio, got %v, want 0.6", row.Floor)
	}
	if row.Ratio != 0.3 {
		t.Fatalf("Ratio (effective price, no override set) must still be platform baseline, got %v", row.Ratio)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (`TestHandleAgentSetGroupRatio_RequiresLevel1`/`_FloorUsesAgentBottomPriceRatio`/`TestHandleAgentListGroups_FloorReflectsAgentBottomPriceRatio` fail: no gate yet, floor still uses the global baseline; the harness/import changes themselves should already compile since `AgentRepo`/`AgentService` are existing `App` fields).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'TestHandleAgentSetGroupRatio|TestHandleAgentListGroups'
```

- [ ] **Step 3:** Add `consumeFloorRatio` next to `modelGroupBaseline` in `internal/mtwire/distribution.go` (~505-509 area, in the "辅助" section):
```go
// modelGroupBaseline 取某模型分组的主站基准倍率（= 无个性化底价时的组合下限）：经 groupRatioOf 包级 seam
// （默认 ratio_setting.GetGroupRatio；未命中其内部返回 1）。单测可注入 seam 控制基准。
func modelGroupBaseline(group string) float64 {
	return groupRatioOf(group)
}

// consumeFloorRatio 返回「该代理在某模型分组上设卖价」的下限（spec §9.7 四档价格阶梯的地板）：
// bottomPriceRatio（该代理的 AgentParams.BottomPriceRatio，跨全部模型分组统一一个比例）> 0 时优先；
// 未配置（<=0）回退平台基准 modelGroupBaseline(group)（历史行为，安全默认——不允许低于官方直客价）。
//
// 必须是 HandleAgentSetGroupRatio（写：卖价下限）与 creditRatioMarkup（读：差价入账的减数，Task 13）
// 共用的唯一口径——两处若各算各的，会出现「卖价被下限挡住却在入账时被当成 0 底价整单算成代理利润」
// 的记账错误（§9.7 record-keeping 警示）。纯函数，无需 App 接收者。
func consumeFloorRatio(bottomPriceRatio float64, group string) float64 {
	if bottomPriceRatio > 0 {
		return bottomPriceRatio
	}
	return modelGroupBaseline(group)
}
```
  Update `HandleAgentSetGroupRatio` (~397-435) — add the level gate as the first line, capture `ctx` once, and swap the floor:
```go
// HandleAgentSetGroupRatio PUT /api/tenant/groups/:group —— 设本租户某模型分组的覆盖倍率（= 卖价）。
// 校验：① 需 level≥1（spec §9.5：L0 不能自设卖价），否则 AGENT_LEVEL_LOCKED；
// ② group 必须是已登记的模型分组（IsModelGroup），否则 AGENT_GROUP_NOT_MODEL；
// ③ ratio ≥ 该代理消耗计费底价（consumeFloorRatio；未配置则回退平台基准，spec §9.7），低于返回
// RATIO_BELOW_FLOOR（经 pricing.Guard）。
func (a *App) HandleAgentSetGroupRatio(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	group := strings.TrimSpace(c.Param("group"))
	if group == "" {
		respondErr(c, errAgentInputInvalid)
		return
	}
	// 仅允许调模型分组的折扣系数；层级名 / 未登记分组一律拒（其 modelFactor 恒为 1，调了无意义且语义混淆）。
	if !a.ModelGroupRepo.IsModelGroup(group) {
		respondErr(c, errAgentGroupNotModel)
		return
	}
	var body struct {
		Ratio float64 `json:"ratio"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	// 卖价下限 = 该代理底价（未配置回退平台基准）；spec §9.7：与 creditRatioMarkup 同一口径。
	bottom := float64(0)
	if params, found, err := a.AgentRepo.GetAgentType(ctx, tenantID); err == nil && found {
		bottom = params.BottomPriceRatio
	}
	floor := consumeFloorRatio(bottom, group)
	if err := pricing.NewGuard().ValidateGroupRatio(body.Ratio, floor); err != nil {
		respondErr(c, err) // RATIO_BELOW_FLOOR
		return
	}
	if err := a.TenantRepo.UpsertGroup(ctx, tenantID, group, body.Ratio); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, modelGroupRatioOut{
		GroupName: group, Ratio: body.Ratio, PlatformRatio: modelGroupBaseline(group), Floor: floor, HasOverride: true,
	})
}
```
  Note `PlatformRatio` in the response now always reports `modelGroupBaseline(group)` (platform's own reference point), deliberately decoupled from `Floor` (this agent's actual enforced minimum) — before this task the two were always numerically identical, which is why the field split wasn't visible until now.

- [ ] **Step 4:** Update `HandleAgentListGroups` (~373-395) to show the same per-agent floor:
```go
// HandleAgentListGroups GET /api/tenant/groups —— 本租户可调的模型分组倍率：
// 列出每个「已登记模型分组」的主站基准（platform_ratio）+ 本租户当前覆盖（ratio/has_override）+
// 该代理的卖价下限（floor，spec §9.7：未配置底价则回退平台基准，与 HandleAgentSetGroupRatio 同口径）。
// 仅列模型分组（层级由管理员/代理另设，不在此）。
func (a *App) HandleAgentListGroups(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ctx := reqCtx(c)
	bottom := float64(0)
	if params, found, err := a.AgentRepo.GetAgentType(ctx, tenantID); err == nil && found {
		bottom = params.BottomPriceRatio
	}
	names := a.ModelGroupRepo.ListEnabled() // 已启用模型分组名（升序）
	out := make([]modelGroupRatioOut, 0, len(names))
	for _, name := range names {
		base := modelGroupBaseline(name) // 平台基准（PlatformRatio；未覆盖时的实际生效价）
		row := modelGroupRatioOut{
			GroupName: name, Ratio: base, PlatformRatio: base, Floor: consumeFloorRatio(bottom, name),
		}
		if override, found, err := a.TenantRepo.LookupEnabledGroupRatio(ctx, tenantID, name); err == nil && found {
			row.Ratio = override
			row.HasOverride = true
		}
		out = append(out, row)
	}
	respondOK(c, out)
}
```
  Note `Ratio`'s no-override default stays `base` (platform baseline) — **not** the agent's floor. Absent an explicit 卖价 override, users are billed at the platform baseline (`resolveModelGroup2D`'s existing behaviour, unchanged); `BottomPriceRatio` is only ever a *ceiling on how low the agent may go*, never an implicit default price.

- [ ] **Step 5:** Run — expect PASS (new tests + pre-existing `TestHandleAgentSetGroupRatio`/`TestHandleAgentListGroups`, both now level-1-seeded / harness-rewired but otherwise unchanged in their assertions).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'TestHandleAgentSetGroupRatio|TestHandleAgentListGroups'
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/...
```

- [ ] **Step 6:** Commit.
```
cd /Users/cc/newapi628 && git add internal/mtwire/distribution.go internal/mtwire/distribution_test.go && git commit -m "feat(billing): gate HandleAgentSetGroupRatio to level>=1, floor to agent's own 底价 (spec §9.5/§9.7)"
```

---

### Task 13: L1 差价入账 (`creditRatioMarkup`) + thread realized pricing data through `ConsumeCommission`. SERIAL — hook signature (Step 6)

Implements spec §9.4's `markup = rawUnits × (chargedGroupRatio − 底价) = chargedQuota − rawUnits × 底价` (**2026-07-02 v3 formula fix, Change 2** — the original formula `(卖价−底价)×token数×ModelRatio` silently dropped the realized tier/vip factor, so an agent's own vip discount cost nothing out of their markup, which directly conflicts with Task 16's new "agent can self-set their own vip force" mechanism (Change 1) and its "whoever sets it bears it" premise. The fix substitutes the **realized** combined ratio `chargedGroupRatio` for the tier-less "卖价", so a discount the agent grants their own users is reflected in their own differential.). The hard part: **the current `agenthook.ConsumeCommission` hook signature carries neither which model group was billed nor what ratio was actually applied**, and the markup needs both. Reading the real pricing pipeline (`relay/helper/price.go` `HandleGroupRatio`, `types/price_data.go` `PriceData`/`GroupRatioInfo`) shows `relayInfo.PriceData.GroupRatioInfo.GroupRatio` already holds the **exact combined ratio** (`用户层级优惠 × 分组倍率`, i.e. `tier × modelFactor`) that was multiplied into this specific charge, and `relayInfo.UsingGroup` holds the billed model group — both already computed and sitting on `relayInfo` at the two `agenthook.ConsumeCommission(...)` call sites (`service/quota.go:456`, `service/text_quota.go:484`). Threading `usingGroup` + that realized ratio through lets `token数×ModelRatio` be recovered **exactly** as `quotaUnits ÷ chargedGroupRatio` — no re-deriving `用户层级优惠`/`分组倍率` from a second, potentially-stale lookup (a real risk: an agent could change their 卖价, or Task 16's per-tenant vip force, in the gap between charge-time and settle-time). This is a **deliberate improvement over re-querying** the group ratio at credit time, not just "the same as reading `usingGroup` alone" — see the code comment on `ratioMarkupQuotaUnits` in Step 4. **Note the formula no longer needs the raw `sellRatio` value** (Step 4) — only whether an override exists at all (the "did this agent opt into markup on this group" gate) — since `chargedGroupRatio` already carries whatever ratio was actually charged.

**⚠️ Deployment-ordering note (paired with Task 16, read before shipping either alone):** this task's formula fix, by itself, already makes agent markup sensitive to *whatever* tier ratio landed in `chargedGroupRatio` — and until Task 16 ships, the only possible source of a non-1 tier ratio for an agent's downstream vip user is still the **platform's global** `GroupRatio['vip']` (Task 16 hasn't touched `resolveModelGroup2D` yet). So shipping this task alone, before Task 16, opens a real window where an L1 agent's markup income fluctuates with an admin's platform-wide vip-ratio changes — an outcome the agent didn't cause and can't see coming. That is "platform decides, agent pays," the wrong direction for "whoever sets it bears it." **Tasks 13 and 16 are a logical pair and should ship in the same deployment.** If they must ship separately, confirm with the user whether that transition window is acceptable first.

**Why a hook-signature change instead of avoiding it:** without `usingGroup` there's no way to know which model-group override (if any) applies to *this* consumption event; without the realized `chargedGroupRatio` there's no way to recover `token数×ModelRatio` without re-deriving `用户层级优惠` from a fresh, possibly-different-from-charge-time DB lookup. The extension is purely additive (two more parameters) and mechanical across exactly 4 files. This task's body changes to `creditConsumeCommission` are **signature-only** — the function still unconditionally credits L0-style commission after this task (unchanged from today); Task 14 restructures the body into the level-based dispatch.

**Files:**
- `internal/agent/model.go` (`EarningSource` consts ~50-61; `Valid()` ~64-72 — add `SourceRatioMarkup`).
- `internal/agent/model_test.go` (`TestEarningSource_Valid` ~32-50 — extend).
- `internal/mtwire/distribution.go` (consumes `consumeFloorRatio`, Task 12 — no edit here).
- `internal/mtwire/consume_markup.go` (NEW) — `ratioMarkupQuotaUnits`, `creditRatioMarkup`.
- `internal/mtwire/consume_markup_test.go` (NEW).
- `internal/platform/agenthook/agenthook.go` (`ConsumeCommission` var ~17-21). SERIAL.
- `service/quota.go` (call site ~454-458). SERIAL.
- `service/text_quota.go` (call site ~483-485). SERIAL.
- `internal/mtwire/agent.go` (`creditConsumeCommission` signature line only ~198).

**Interfaces:**
- Changes: `agenthook.ConsumeCommission func(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string, chargedGroupRatio float64)` (was 4 args; `usingGroup` and `chargedGroupRatio` appended).
- Produces: `agent.SourceRatioMarkup EarningSource = "ratio_markup"`.
- Produces: `ratioMarkupQuotaUnits(chargedQuota int64, chargedGroupRatio, bottomRatio float64) int64` (pure; **v3**: no longer takes a `sellRatio` parameter — the formula only needs `chargedGroupRatio` vs `bottomRatio`, see Task intro).
- Produces: `(a *App) creditRatioMarkup(ctx context.Context, tenantID, userID, quotaUnits int64, usingGroup, requestID, billingSource string, chargedGroupRatio, bottomPriceRatio float64)` — best-effort, `requestID`-idempotent, credits the L1 tenant's own wallet. (Signature unchanged from the original Task 13 draft — only its internal call into `ratioMarkupQuotaUnits` changes, Step 4.)

- [ ] **Step 1:** Write the failing tests. First extend `internal/agent/model_test.go`'s `TestEarningSource_Valid` cases slice (~34-44):
```go
	cases := []struct {
		s    EarningSource
		want bool
	}{
		{SourceRechargeSpread, true},
		{SourceConsumeCommission, true},
		{SourceTokenplanSpread, true},
		{SourceTokenplanCommission, true},
		{SourceRatioMarkup, true},
		{SourceManualAdjustment, true},
		{EarningSource(""), false},
		{EarningSource("bonus"), false},
	}
```
  Create `internal/mtwire/consume_markup_test.go`:
```go
package mtwire

import (
	"context"
	"math"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/modelgroup"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// newRatioMarkupTestApp 装配最小 App：sqlite(:memory:) + model_groups + tenants/tenant_domains/
// tenant_groups + agent 四表（AgentRepo/AgentEarnings）+ 原生 users(id,tenant_id)。供「L1 差价入账」用例；
// seedUser 复用 grouphook_test.go 的既有 helper（同包）。
func newRatioMarkupTestApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if err := modelgroup.AutoMigrate(db); err != nil {
		t.Fatalf("modelgroup migrate: %v", err)
	}
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	ar := agentrepo.New(db)
	return &App{
		DB: db, ModelGroupRepo: modelgroup.New(db), TenantRepo: tenantrepo.New(db),
		AgentRepo: ar, AgentEarnings: agent.NewEarningSink(ar),
	}
}

// TestRatioMarkupQuotaUnits 覆盖差价的纯函数核心（**v3 修订**，spec §9.4）：rawUnits=charged/chargedGroupRatio
// 精确反推 token×ModelRatio；markup = charged − rawUnits×bottomRatio。**关键行为变化（v2→v3）**：markup
// 现在随 chargedGroupRatio 里包含的层级优惠（vip）成比例收缩——同样的 rawUnits，tier 越低（折扣越深），
// markup 越小（见 "vip tier 0.8" 用例；v2 时代这里曾是 "same raw units, same markup" 的 tier-无关断言，
// 已被 §9.6.1 的"代理自担 vip"取代，不再成立）。chargedGroupRatio<=bottomRatio（未加价 / 配置漂移 /
// vip 折扣过深）、非法参数 → 0，恒不为负（MANDATORY safety，Task 15 进一步压测）。
func TestRatioMarkupQuotaUnits(t *testing.T) {
	cases := []struct {
		name              string
		chargedQuota      int64
		chargedGroupRatio float64
		bottomRatio       float64
		want              int64
	}{
		{"default tier (no discount): charged=900 @ 0.9, bottom 0.7", 900, 0.9, 0.7, 200},
		// 0.72=0.8(代理自设 vip 力度)×0.9(卖价)；同样 1000 rawUnits，vip 让 markup 从 200 缩到 20 ——
		// 代理自担折扣（取代 v2 的 "same raw units, same markup" tier-无关断言）。
		{"vip tier 0.8 (agent's own override): charged=720 @ 0.72, bottom 0.7", 720, 0.72, 0.7, 20},
		// 更深的 vip(0.5)进一步压到跌破 bottom → clamp 0，结构上不会为负（Task 15 覆盖更完整的跨 tier 扫描）。
		{"vip tier 0.5 (deeper discount, drops below bottom): charged=450 @ 0.45, bottom 0.7", 450, 0.45, 0.7, 0},
		{"at floor exactly (chargedGroupRatio == bottomRatio): no markup", 900, 0.7, 0.7, 0},
		{"below floor (drift / over-discount): clamps to 0, never negative", 900, 0.6, 0.7, 0},
		{"zero charged", 0, 0.9, 0.7, 0},
		{"non-positive chargedGroupRatio", 900, 0, 0.7, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ratioMarkupQuotaUnits(c.chargedQuota, c.chargedGroupRatio, c.bottomRatio); got != c.want {
				t.Fatalf("ratioMarkupQuotaUnits(%d,%v,%v) = %d, want %d",
					c.chargedQuota, c.chargedGroupRatio, c.bottomRatio, got, c.want)
			}
		})
	}
}

// TestCreditRatioMarkup_CreditsL1WalletIdempotently 验证差价入账：命中卖价覆盖 → 按公式入 L1 钱包
// (source=ratio_markup)；同 requestID 重复调用不重复入账（幂等）；无卖价覆盖 → 不入账；该代理底价
// 高于卖价（配置漂移）→ 不入账、不倒扣。
func TestCreditRatioMarkup_CreditsL1WalletIdempotently(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register model group: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 平台基准 = 0.3

	seedUser(t, app, 100, 5)
	if err := app.TenantRepo.UpsertGroup(ctx, 5, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	// charged=1200 quota，chargedGroupRatio=0.45（tier=1×卖价 0.45），底价未配置（0）→回退平台基准 0.3。
	// rawUnits=1200/0.45=2666.67，markup=(0.45-0.3)×2666.67=400。
	app.creditRatioMarkup(ctx, 5, 100, 1200, "claude-kiro", "req-md-1", "wallet", 0.45, 0)

	w, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	wantCNY := consumeCommissionCNY(400, 1, operation_setting.USDExchangeRate)
	if math.Abs(w.WithdrawableBalance-wantCNY) > 1e-6 {
		t.Fatalf("withdrawable = %v, want %v", w.WithdrawableBalance, wantCNY)
	}

	// 幂等：同 requestID 重复调用不重复入账。
	app.creditRatioMarkup(ctx, 5, 100, 1200, "claude-kiro", "req-md-1", "wallet", 0.45, 0)
	w2, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w2.WithdrawableBalance != w.WithdrawableBalance {
		t.Fatalf("idempotency broken: withdrawable = %v, want %v", w2.WithdrawableBalance, w.WithdrawableBalance)
	}

	// 无覆盖（租户 9 未设卖价）：不入账。
	seedUser(t, app, 200, 9)
	app.creditRatioMarkup(ctx, 9, 200, 1200, "claude-kiro", "req-md-2", "wallet", 0.3, 0)
	w9, err := app.AgentRepo.GetWallet(ctx, 9)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w9.WithdrawableBalance != 0 {
		t.Fatalf("no-override tenant must not earn markup, got %v", w9.WithdrawableBalance)
	}

	// 该代理显式配置的底价（0.5）高于卖价（0.45）：配置漂移场景，防御性跳过，不倒扣、不入账。
	seedUser(t, app, 300, 11)
	if err := app.TenantRepo.UpsertGroup(ctx, 11, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}
	app.creditRatioMarkup(ctx, 11, 300, 1200, "claude-kiro", "req-md-3", "wallet", 0.45, 0.5)
	w11, err := app.AgentRepo.GetWallet(ctx, 11)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w11.WithdrawableBalance != 0 {
		t.Fatalf("chargedGroupRatio<=bottomRatio (drift) must not credit anything, got %v", w11.WithdrawableBalance)
	}

	// 非模型分组（层级名）：即便凑巧有 tenant_groups 行，也不产生差价（IsModelGroup 门禁）。
	seedUser(t, app, 400, 12)
	app.creditRatioMarkup(ctx, 12, 400, 1200, "vip", "req-md-4", "wallet", 0.36, 0)
	w12, err := app.AgentRepo.GetWallet(ctx, 12)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w12.WithdrawableBalance != 0 {
		t.Fatalf("non-model-group usingGroup must not credit markup, got %v", w12.WithdrawableBalance)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (compile: `SourceRatioMarkup`/`ratioMarkupQuotaUnits`/`creditRatioMarkup` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/agent/... ./internal/mtwire/ -run 'EarningSource|RatioMarkup'
```

- [ ] **Step 3:** Add the new earning source. In `internal/agent/model.go`, add after `SourceTokenplanCommission` (~58):
```go
	// SourceTokenplanCommission 套餐内消耗分润。
	SourceTokenplanCommission EarningSource = "tokenplan_commission"
	// SourceRatioMarkup 差价入账（L1/独立档：卖价高于底价的部分，按官方 token×ModelRatio 折算；
	// spec agent-tiering §9.4）。
	SourceRatioMarkup EarningSource = "ratio_markup"
	// SourceManualAdjustment 人工调整（管理员修正，金额可正可负）。
	SourceManualAdjustment EarningSource = "manual_adjustment"
```
  And extend `Valid()` (~64-72):
```go
func (s EarningSource) Valid() bool {
	switch s {
	case SourceRechargeSpread, SourceConsumeCommission,
		SourceTokenplanSpread, SourceTokenplanCommission, SourceRatioMarkup, SourceManualAdjustment:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4:** Create `internal/mtwire/consume_markup.go` with the pure math + the crediting orchestration:
```go
package mtwire

// L1（独立档）差价入账实现（spec agent-tiering §9.4/§9.7/§9.9）。独立文件，紧邻 distribution.go 的
// consumeFloorRatio（Task 12：写路径的卖价地板）——本文件的 creditRatioMarkup 直接复用它，两处必须
// 同一口径（否则会出现「卖价被地板挡住却在入账时按 0 底价整单算成代理利润」的记账错误）。

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ratioMarkupQuotaUnits 是差价入账的纯函数核心（spec §9.4 **v3 修订**：markup=rawUnits×(chargedGroupRatio−底价)
// = chargedQuota − rawUnits×底价）：
//
//	rawUnits = chargedQuota / chargedGroupRatio
//	markup   = chargedQuota − rawUnits × bottomRatio
//
// chargedGroupRatio 必须是「结算时实际生效的组合倍率」（用户层级优惠×分组倍率，来自
// relayInfo.PriceData.GroupRatioInfo.GroupRatio；层级优惠现在可能是代理自设的 vip 力度覆盖，spec
// §9.6.1）——用实际生效值反推 rawUnits，而不是重新查一遍「当前」的层级/分组倍率，从而与代理事后
// 改卖价/vip 力度的竞态解耦。
//
// **v2→v3 关键变化**：公式不再单独接收 sellRatio 参数，直接用 chargedGroupRatio（已经把层级优惠乘
// 进去）与 bottomRatio 的差值展开——代理自设的 vip 折扣因而**直接体现**在 markup 里（层级优惠越深、
// markup 越小），不再是 v2 时代"层级优惠已经在 chargedGroupRatio 里被除掉、与 markup 完全无关"的
// tier-不变量（spec §9.6 曾标注为已知差距，现由 §9.6.1 解决）。
//
// chargedGroupRatio<=bottomRatio（未加价；或管理员事后把底价调到卖价之上——配置漂移防御；或代理
// 自设/调深 vip 力度导致这一单的实际生效价跌破底价——同一类漂移，同一处兜底，不必为此单开分支）或
// 任一参数非正 → 0：结构上绝不产生负值（MANDATORY safety：无论 chargedGroupRatio 因为哪个原因被
// 拉低，markup 只会趋近 0，不会为负；这同时是 spec §9.6.1"卖价×vip 力度 ≥ 底价"floor 承诺的结算侧
// 兜底）。
func ratioMarkupQuotaUnits(chargedQuota int64, chargedGroupRatio, bottomRatio float64) int64 {
	if chargedQuota <= 0 || chargedGroupRatio <= 0 || chargedGroupRatio <= bottomRatio {
		return 0
	}
	rawUnits := float64(chargedQuota) / chargedGroupRatio
	markup := float64(chargedQuota) - rawUnits*bottomRatio
	if markup <= 0 { // 代数上该分支在上面的 guard 后不可达；保留作 belt-and-suspenders（浮点边界防御）。
		return 0
	}
	return int64(markup)
}

// creditRatioMarkup 是 L1（level≥1）差价入账实现（**v3 修订**，spec §9.4/§9.6.1）：先确认该用户所属
// 租户在 usingGroup 上是否设了卖价覆盖（tenant_groups；无覆盖则不入账——未设卖价的模型分组，用户按
// 平台直客价付费，不视为隐式底价加价；命中与否才是"是否入账"的判据，覆盖的具体数值本身 v3 起不再
// 参与 markup 计算，下方详述）× consumeFloorRatio(bottomPriceRatio, usingGroup)（Task 12 同口径地板）
// 算出 markup（直接用 chargedGroupRatio——已经把代理自设 vip 力度乘进去的实际生效倍率，§9.6.1——
// 而不是裸卖价），按 requestID 幂等入账到 L1 自己的钱包（source=ratio_markup）。best-effort：失败
// 不阻断调用方（由 creditConsumeCommission 的 panic 兜底覆盖，Task 14）。
//
// **v2→v3**：旧版本读取 sellRatio（卖价覆盖的具体数值）传入 markup 公式；新公式改用 chargedGroupRatio
// （已含层级优惠）直接对 bottomRatio 求差，sellRatio 的返回值不再需要——但**查询本身仍必须保留**，
// 因为它是"这个模型分组是否有卖价覆盖"这个入账资格判据的唯一来源（无覆盖=用户按平台价付费=不产生
// 差价，即便 chargedGroupRatio 本身合法非零）。
func (a *App) creditRatioMarkup(ctx context.Context, tenantID, userID, quotaUnits int64, usingGroup, requestID, billingSource string, chargedGroupRatio, bottomPriceRatio float64) {
	if tenantID <= 0 || quotaUnits <= 0 || requestID == "" || usingGroup == "" || chargedGroupRatio <= 0 {
		return
	}
	if a.ModelGroupRepo == nil || !a.ModelGroupRepo.IsModelGroup(usingGroup) {
		return // 非模型分组（层级名等）：无「卖价」概念，不产生差价
	}
	if _, hit := a.resolveTenantGroupRatio(ctx, userID, usingGroup); !hit {
		return // 未设卖价覆盖：用户按平台直客价付费，不视为隐式底价加价
	}
	bottom := consumeFloorRatio(bottomPriceRatio, usingGroup) // 与 HandleAgentSetGroupRatio 同口径（Task 12）
	markupQuota := ratioMarkupQuotaUnits(quotaUnits, chargedGroupRatio, bottom)
	if markupQuota <= 0 {
		return
	}
	// markup 已是「计费额」口径（quota 单位），直接按 ratio=1 换算 CNY（不再乘任何分润比例）。
	cny := consumeCommissionCNY(markupQuota, 1, operation_setting.USDExchangeRate)
	if cny <= 0 {
		return
	}
	if err := a.AgentEarnings.AddEarning(ctx, agent.EarningEntry{
		TenantID:   tenantID,
		UserID:     userID,
		SourceType: agent.SourceRatioMarkup,
		SourceID:   requestID,
		Amount:     cny,
		Remark:     "ratio_markup:" + usingGroup + ":" + billingSource,
	}); err != nil {
		common.SysError("mtwire: credit ratio markup failed: " + err.Error())
	}
}
```

- [ ] **Step 5:** Run — expect PASS (markup math + crediting, in isolation — the hook itself is not yet threaded).
```
cd /Users/cc/newapi628 && go test ./internal/agent/... ./internal/mtwire/ -run 'EarningSource|RatioMarkup'
```

- [ ] **Step 6:** Thread the hook signature. **SERIAL — this changes a call signature shared by two `service/` call sites; both must be updated in the same commit or the tree won't compile.** In `internal/platform/agenthook/agenthook.go` (~17-21):
```go
// ConsumeCommission 在一次成功的 PostConsume 之后被调用，按所属代理档位二选一计佣入账
// （level==0 → 提成 consume_commission；level≥1 → 差价 ratio_markup；见 mtwire.creditConsumeCommission，
// Task 14）。参数：userID=消费用户；quotaUnits=本次消费的 new-api 内部额度单位（$1=common.QuotaPerUnit，
// 可正可负，实现侧只对正向消费计佣）；requestID=幂等键来源；billingSource="wallet"|"subscription"
// （区分钱包桶/套餐桶）；usingGroup=本次计费实际使用的分组（relayInfo.UsingGroup）；chargedGroupRatio=
// 本次计费实际生效的组合倍率（用户层级优惠×分组倍率，relayInfo.PriceData.GroupRatioInfo.GroupRatio）——
// 后两者供 L1 差价入账精确反推 token×ModelRatio，spec agent-tiering §9.4。
// nil = 未装配。实现必须自身幂等且 best-effort（失败仅记日志，不返回错误）。
var ConsumeCommission func(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string, chargedGroupRatio float64)
```
  In `service/quota.go` (~454-458):
```go
	if relayInfo != nil && agenthook.ConsumeCommission != nil {
		if total := int64(quota) + int64(preConsumedQuota); total > 0 {
			agenthook.ConsumeCommission(int64(relayInfo.UserId), total, relayInfo.RequestId, relayInfo.BillingSource,
				relayInfo.UsingGroup, relayInfo.PriceData.GroupRatioInfo.GroupRatio)
		}
	}
```
  In `service/text_quota.go` (~483-485):
```go
	if agenthook.ConsumeCommission != nil && summary.Quota > 0 {
		agenthook.ConsumeCommission(int64(relayInfo.UserId), int64(summary.Quota), relayInfo.RequestId, relayInfo.BillingSource,
			relayInfo.UsingGroup, relayInfo.PriceData.GroupRatioInfo.GroupRatio)
	}
```
  In `internal/mtwire/agent.go`, change only the `creditConsumeCommission` signature line (~198) — **body unchanged**, the two new parameters are accepted but not yet consumed (Task 14 wires them in):
```go
func (a *App) creditConsumeCommission(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string, chargedGroupRatio float64) {
```
  `agenthook.ConsumeCommission = a.creditConsumeCommission` (`agent.go` `InstallHooks`, ~116) needs **no textual change** — it's a method-value assignment that now simply refers to the 6-arg method.

- [ ] **Step 7:** Run — expect PASS (full tree compiles; all mtwire/agent/service tests green).
```
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/... ./service/...
```

- [ ] **Step 8:** Verify zero stale 4-arg call sites remain.
```
cd /Users/cc/newapi628 && grep -rn "ConsumeCommission(int64(relayInfo" service/ | grep -v "UsingGroup\|GroupRatio" || echo "clean"
```
  Expect `clean`.

- [ ] **Step 9:** Commit.
```
cd /Users/cc/newapi628 && git add internal/agent/model.go internal/agent/model_test.go internal/mtwire/consume_markup.go internal/mtwire/consume_markup_test.go internal/platform/agenthook/agenthook.go service/quota.go service/text_quota.go internal/mtwire/agent.go && git commit -m "feat(billing): thread usingGroup+chargedGroupRatio through ConsumeCommission hook + add L1 ratio-markup crediting"
```

---

### Task 14: Tier-based earning dispatch in `creditConsumeCommission` (no-double-credit)

Wires Task 13's `creditRatioMarkup` together with the existing L0 commission math: `creditConsumeCommission` becomes a thin dispatcher on `params.Level`, extracting the existing L0 body into `creditL0Commission` (symmetric with `creditRatioMarkup`) so the switch itself is a small diff, not a re-derivation. This is the task that makes spec §9.9's "按档二选一…绝不同时" a **tested invariant**, not an assumption.

**Operational note (flagged, not a code change here):** Task 2 of this plan (already implemented — see `git log`, commit `2f07403`) backfilled every *existing* agent's `agent_profiles.level` to `1` (independent) when the `type` column was dropped. Combined with this task, that means: the moment Tasks 11-14 are deployed, every pre-existing agent tenant instantly stops earning `consume_commission` and starts earning `ratio_markup` instead — which is `0` for any agent that hasn't configured a model-group 卖价 via `HandleAgentSetGroupRatio` (most won't have, since that's a newer opt-in feature, and Task 12 additionally now requires level≥1 *and* clears the new floor). This is the *intended* behavior per spec §9.9 ("二选一"), not a bug, but it is a real, immediate earnings-drop for any already-live agent relying on `commission_ratio`. **Confirm this is acceptable before deploying Part 2 to a server with real agents on it** — no task in this plan changes that outcome, since it's what the spec explicitly asks for.

**Files:**
- `internal/mtwire/agent.go` (`creditConsumeCommission` ~198-238 — full restructure into a dispatcher + `creditL0Commission`).
- `internal/mtwire/agent_tiering_test.go` (NEW).

**Interfaces:**
- Changes: `creditConsumeCommission`'s body (signature unchanged from Task 13).
- Produces: `(a *App) creditL0Commission(ctx context.Context, tenantID, userID, quotaUnits int64, requestID, billingSource string, commissionRatio float64)` (extracted, same logic that exists today, unmodified).

- [ ] **Step 1:** Write the failing tests. Create `internal/mtwire/agent_tiering_test.go` (reuses `newRatioMarkupTestApp` + `seedUser` from Task 13's `consume_markup_test.go`, same package):
```go
package mtwire

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/modelgroup"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// TestCreditConsumeCommission_TierSwitch_NeverBothSources 是核心不变量测试（spec §9.9，防双发）：
// 同一次 creditConsumeCommission 调用，L0 租户只产生 consume_commission、L1 租户只产生 ratio_markup，
// 两个 source 绝不同时出现——即便 L1 租户也配了非零 commission_ratio（刻意保留非零值：证明 L1 分支
// 根本不读这个字段，而不只是恰好为 0）。
func TestCreditConsumeCommission_TierSwitch_NeverBothSources(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register model group: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 基准 = 0.3

	// L0 租户 5：commission_ratio=0.2；用户 100。
	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L0 agent: %v", err)
	}
	seedUser(t, app, 100, 5)

	// L1 租户 9：也配了 commission_ratio=0.2（刻意）+ claude-kiro 卖价覆盖 0.45（基准 0.3）；用户 200。
	if err := app.AgentRepo.SetAgentType(ctx, 9, agent.AgentParams{Level: 1, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L1 agent: %v", err)
	}
	if err := app.TenantRepo.UpsertGroup(ctx, 9, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}
	seedUser(t, app, 200, 9)

	quota := int64(1200)
	chargedRatio := 0.45 // tier=1（default）× 卖价 0.45
	app.creditConsumeCommission(100, quota, "req-tier-l0", "wallet", "claude-kiro", chargedRatio)
	app.creditConsumeCommission(200, quota, "req-tier-l1", "wallet", "claude-kiro", chargedRatio)

	assertSingleSource := func(tenantID int64, requestID, wantSource string) {
		t.Helper()
		var rows []struct{ SourceType string }
		if err := app.DB.Table("agent_earning_logs").
			Select("source_type").
			Where("tenant_id = ? AND source_id = ?", tenantID, requestID).
			Find(&rows).Error; err != nil {
			t.Fatalf("query earning logs: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("tenant %d requestID %s: got %d earning rows, want exactly 1 (never both sources)", tenantID, requestID, len(rows))
		}
		if rows[0].SourceType != wantSource {
			t.Fatalf("tenant %d requestID %s: source = %q, want %q", tenantID, requestID, rows[0].SourceType, wantSource)
		}
	}
	assertSingleSource(5, "req-tier-l0", "consume_commission")
	assertSingleSource(9, "req-tier-l1", "ratio_markup")

	// 互斥的另一半：L0 绝不该有 ratio_markup；L1 绝不该有 consume_commission/tokenplan_commission。
	var crossLeak int64
	app.DB.Table("agent_earning_logs").Where("tenant_id = ? AND source_type = ?", 5, "ratio_markup").Count(&crossLeak)
	if crossLeak != 0 {
		t.Fatalf("L0 tenant must never get ratio_markup, found %d rows", crossLeak)
	}
	app.DB.Table("agent_earning_logs").
		Where("tenant_id = ? AND source_type IN ?", 9, []string{"consume_commission", "tokenplan_commission"}).Count(&crossLeak)
	if crossLeak != 0 {
		t.Fatalf("L1 tenant must never get consume_commission/tokenplan_commission, found %d rows", crossLeak)
	}
}

// TestCreditConsumeCommission_RetrySameRequestID_NoDoubleCredit 覆盖红线要求的「强幂等」（both branches）：
// 同一 requestID 被重复调用（模拟钩子被意外重放）—— L1 差价场景下钱包余额与台账行数都不应二次变动。
func TestCreditConsumeCommission_RetrySameRequestID_NoDoubleCredit(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register model group: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios

	if err := app.AgentRepo.SetAgentType(ctx, 9, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set L1 agent: %v", err)
	}
	if err := app.TenantRepo.UpsertGroup(ctx, 9, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}
	seedUser(t, app, 200, 9)

	for i := 0; i < 3; i++ { // 重放 3 次，同 requestID。
		app.creditConsumeCommission(200, 1200, "req-retry-1", "wallet", "claude-kiro", 0.45)
	}
	w, err := app.AgentRepo.GetWallet(ctx, 9)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	// charged=1200, chargedGroupRatio=0.45, sellRatio=0.45, bottom=平台基准0.3(未配置) → rawUnits=2666.67, markup=400。
	wantCNY := consumeCommissionCNY(400, 1, operation_setting.USDExchangeRate)
	if math.Abs(w.WithdrawableBalance-wantCNY) > 1e-6 {
		t.Fatalf("after 3x replay: withdrawable = %v, want %v (must credit exactly once)", w.WithdrawableBalance, wantCNY)
	}
	var count int64
	app.DB.Table("agent_earning_logs").Where("tenant_id = ? AND source_id = ?", 9, "req-retry-1").Count(&count)
	if count != 1 {
		t.Fatalf("earning rows for req-retry-1 = %d, want exactly 1", count)
	}
}

// TestCreditConsumeCommission_NoTenant_NoAttribution_Unaffected 锁定不回归：主站用户 / 未归属用户
// （tenant_id=0）——无论调用多少次——既不产生任何 earning 行，也不 panic、不阻断调用方。
func TestCreditConsumeCommission_NoTenant_NoAttribution_Unaffected(t *testing.T) {
	app := newRatioMarkupTestApp(t)
	seedUser(t, app, 300, 0) // tenant_id=0：主站用户

	app.creditConsumeCommission(300, 1200, "req-main-1", "wallet", "claude-kiro", 1.0)

	var count int64
	app.DB.Table("agent_earning_logs").Count(&count)
	if count != 0 {
		t.Fatalf("main-site user must never produce an earning row, got %d", count)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (the tier switch doesn't exist yet: `creditConsumeCommission` still unconditionally uses the L0/`consume_commission` path regardless of `params.Level`, so `TestCreditConsumeCommission_TierSwitch_NeverBothSources`'s L1 assertions fail — tenant 9 gets `consume_commission` instead of `ratio_markup`).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'TierSwitch|NoDoubleCredit|NoTenant_NoAttribution'
```

- [ ] **Step 3:** Restructure `creditConsumeCommission` into a dispatcher + extract `creditL0Commission`. In `internal/mtwire/agent.go`, replace the body (~198-238, the Task 13 signature-only version):
```go
// creditConsumeCommission 是 agenthook.ConsumeCommission 实现：userId→users.tenant_id→agent
// level→按档二选一入账（spec agent-tiering §9.9）：level==0 → L0 提成（creditL0Commission）；
// level≥1 → L1 差价（creditRatioMarkup，Task 13）。幂等键=requestID。best-effort：失败不阻断扣费。
func (a *App) creditConsumeCommission(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string, chargedGroupRatio float64) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: creditConsumeCommission panic recovered")
		}
	}()
	if userID <= 0 || quotaUnits <= 0 || requestID == "" {
		return
	}
	ctx := context.Background()
	tenantID := a.userTenantID(ctx, userID)
	if tenantID <= 0 {
		return // 主站用户 / 未归属：无代理分润
	}
	params, found, err := a.AgentRepo.GetAgentType(ctx, tenantID)
	if err != nil || !found {
		return // 该租户未设代理
	}
	// 按档二选一（spec §9.9）：level==0 → L0 提成通路；level≥1 → L1 差价通路。NEVER both——两条路径
	// 在此分支互斥，绝不重叠调用。
	if params.Level == 0 {
		a.creditL0Commission(ctx, tenantID, userID, quotaUnits, requestID, billingSource, params.CommissionRatio)
		return
	}
	a.creditRatioMarkup(ctx, tenantID, userID, quotaUnits, usingGroup, requestID, billingSource, chargedGroupRatio, params.BottomPriceRatio)
}

// creditL0Commission 是 L0（普通档）计费通路：官方原价提成（commission_ratio × quotaUnits，公式不变，
// spec §9.5——v2 不再有邀请 9 折，纯提成）。从 creditConsumeCommission 抽出以保持按档分支清晰；
// panic 由调用方的 defer 统一兜底。
func (a *App) creditL0Commission(ctx context.Context, tenantID, userID, quotaUnits int64, requestID, billingSource string, commissionRatio float64) {
	if commissionRatio <= 0 {
		return
	}
	cny := consumeCommissionCNY(quotaUnits, commissionRatio, operation_setting.USDExchangeRate)
	if cny <= 0 {
		return
	}
	// 钱包桶 → consume_commission；套餐桶 → tokenplan_commission（账目区分；两类均经此单点）。
	source := agent.SourceConsumeCommission
	if billingSource == "subscription" {
		source = agent.SourceTokenplanCommission
	}
	if err := a.AgentEarnings.AddEarning(ctx, agent.EarningEntry{
		TenantID:   tenantID,
		UserID:     userID,
		SourceType: source,
		SourceID:   requestID,
		Amount:     cny,
		Remark:     "consume:" + billingSource,
	}); err != nil {
		common.SysError("mtwire: credit consume commission failed: " + err.Error())
	}
}
```

- [ ] **Step 4:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'TierSwitch|NoDoubleCredit|NoTenant_NoAttribution'
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/... ./service/...
```

- [ ] **Step 5:** Commit.
```
cd /Users/cc/newapi628 && git add internal/mtwire/agent.go internal/mtwire/agent_tiering_test.go && git commit -m "feat(billing): tier-based earning dispatch in creditConsumeCommission (L0 commission XOR L1 markup)"
```

---

### Task 15: vip 层级优惠交互测试 + MANDATORY safety 不变量 (agent bears its own realized discount, never-negative, floor-respecting)

**2026-07-02 v3 重写（Change 2 落地后，原地改写，不是新增独立任务）：** v2 版本的这个任务测的是"markup 与用户层级无关、platform 兜底"——那是**旧公式**（`(卖价−底价)×token×ModelRatio`，不含层级）的真实推论，且明确标注为一处需用户确认的"谁设谁担"差距（spec §9.6 原文，现已改写）。**Task 13 的公式修订（Change 2）已经推翻这个推论**：新公式用 `chargedGroupRatio`（实际生效的组合倍率，含层级优惠）直接计算，markup **不再是 tier-无关的**。本任务因此整体重写，测的不再是"tier-invariant"而是相反的性质：代理为自己实际发生的折扣买单，同时结构上绝不倒扣。

Closes out spec §9.4（v3 公式）/§9.6.1：`HandleAgentSetUserTier`（代理指定自己名下哪些用户享受 vip）与 `HandleAgentSetTierRatio`（Task 16，代理自设该层级的力度）都不在本任务改动范围——本任务只测已经在 Task 13 改完的 `creditRatioMarkup`/`ratioMarkupQuotaUnits` 这两个纯函数/方法在跨 tier 场景下的行为，不关心 `chargedGroupRatio` 里的层级优惠具体是从哪条轴解析出来的（那是 Task 16 的职责，本任务与它正交）。三件事：(a) markup 随 `chargedGroupRatio`（从而随其中包含的层级优惠）成比例变化——tier 越低（折扣越深），markup 越小，直至地板；(b) "谁承担折扣成本"从 v2 的"platform 恒定兜底"变为"折扣由谁的 `chargedGroupRatio` 承担、就由谁的账面体现"——代理按自己实际发生的折扣比例分成，折扣越深代理分得越少，与 §9.6.1 的"谁设谁担"一致（不是 v2 时代"代理旱涝保收、平台单方面让利"）；(c) MANDATORY safety 性质（"折扣不能让代理实收跌破底价"）在全 tier 范围结构性成立，不只是 Task 13 已覆盖的单个场景。

**⚠️ 部署顺序提醒（不是本任务要解决的，但必须显式标注，避免被误当成本任务已经覆盖）：** 本任务（连同 Task 13/14）运行时，`resolveModelGroup2D` 尚未被 Task 16 修改——也就是说，在 Task 16 上线**之前**，能让 `chargedGroupRatio` 变小的层级优惠**唯一可能的来源仍是平台全局** `GroupRatio['vip']`（任何 L1 代理的下级用户，只要被标记 vip，都在吃这同一个平台全局折扣，§9.6 v2 行为）。这意味着 Task 13 的公式修订一旦单独上线（Task 16 还没上），会产生一个此前不存在的效果——L1 代理的差价收入开始随"平台管理员调整全局 vip 折扣"这个代理完全无法控制、甚至可能毫不知情的动作而波动。这不是"谁设谁担"，是"平台设、代理担"，方向反了。**Task 13 与 Task 16 因此逻辑上是一对，应在同一次上线中一起部署**；若因排期必须分开上线，须在 Task 13 独立上线前与用户确认这段过渡期是否可接受。本任务的测试不模拟这个过渡态（不新增生产代码，只测 Task 13 已实现的纯函数/方法本身跨 tier 的数学性质，与"tier 从哪条轴解析出来"正交）。

**No new production code is expected to be required by this task** — the invariants below fall out of Task 12's floor（卖价≥底价 enforced at set-time）and Task 13's（v3-corrected）`ratioMarkupQuotaUnits`（结构上对 `chargedGroupRatio` 与 `bottomRatio` 的差值 clamp ≥ 0）。Consequently Step 1's tests are expected to **pass immediately** against the Task 11-14 implementation — this is a deliberate characterization/regression-locking step (proving the invariant holds, and pinning it so a future change can't silently break it), not a red→green TDD cycle. If any of these tests unexpectedly fail against your Task 11-14 implementation, that is a real bug in Task 12/13, not a signal to weaken this task's assertions.

**Files:**
- `internal/mtwire/consume_markup_test.go` (append/replace — same file as Task 13's markup tests; **replaces** the v2-era `TestCreditRatioMarkup_VipDiscountDoesNotAffectAgentMarkup_PlatformBearsCost`, whose name and assertion are now wrong, not left alongside a new test).

**Interfaces:**
- Consumes only: `ratioMarkupQuotaUnits` (Task 13, pure function, v3 3-arg signature), `(a *App) creditRatioMarkup` (Task 13).
- Produces: no new production symbols — tests only.

- [ ] **Step 1:** Write the tests. In `internal/mtwire/consume_markup_test.go`, **replace** the three v2-era tests appended by the original Task 15 (`TestRatioMarkupQuotaUnits_NeverNegative_AcrossTierRange`, `TestRatioMarkupQuotaUnits_BottomAboveSell_NeverCreditsNegative`, `TestCreditRatioMarkup_VipDiscountDoesNotAffectAgentMarkup_PlatformBearsCost`) with:
```go
// TestRatioMarkupQuotaUnits_NeverNegative_AcrossTierRange 是 MANDATORY safety 的结构性证明（**v3 改写**）：
// 无论层级优惠（tier，vip 力度深浅）取值如何，markup 恒 >= 0——即便 tier 深到让 chargedGroupRatio 跌破
// bottomRatio（guard 直接 clamp 到 0，不会算出负数）。**同时新增单调性断言（v3 新性质，取代 v2 的
// tier-无关断言）**：固定同样的「原始消耗」（rawUnits=1000），tier 越深（折扣越大），markup 非增——
// 代理自己给出的折扣越深，自己赚的差价只会越少或不变，绝不会更多。
func TestRatioMarkupQuotaUnits_NeverNegative_AcrossTierRange(t *testing.T) {
	const sellRatio = 0.9
	const bottomRatio = 0.7
	prev := int64(1<<62) // 极大值起步，第一次比较必过
	for _, tier := range []float64{1, 0.9, 0.8, 0.6, 0.3, 0.05} {
		chargedGroupRatio := tier * sellRatio
		charged := int64(1000 * chargedGroupRatio) // 「原始消耗」固定为 1000 个 token×ModelRatio 单位
		got := ratioMarkupQuotaUnits(charged, chargedGroupRatio, bottomRatio)
		if got < 0 {
			t.Fatalf("tier=%v: markup = %d, must never be negative", tier, got)
		}
		if got > prev {
			t.Fatalf("tier=%v: markup = %d > previous (shallower) tier's %d — deeper discount must never increase markup", tier, got, prev)
		}
		prev = got
	}
}

// TestRatioMarkupQuotaUnits_BottomAboveCharged_NeverCreditsNegative 覆盖两类都会让「实际生效倍率」
// 跌破底价的漂移——不区分成因，统一走同一个 clamp：markup 必须精确为 0，不是负数，绝不倒扣代理钱包。
// (a) 管理员在代理已设好卖价之后才把该代理底价调高到卖价之上（原 v2 用例的场景）；
// (b) 代理自己把 vip 力度设得太深，导致 vip 用户的 chargedGroupRatio 跌破底价（Change 1 新增场景——
// spec §9.6.1 明确标注"本轮不做设置时刻的穷举预防校验"，本用例锁定"结算时兜底"这条唯一防线确实生效）。
func TestRatioMarkupQuotaUnits_BottomAboveCharged_NeverCreditsNegative(t *testing.T) {
	cases := []struct {
		name              string
		chargedGroupRatio float64
		bottomRatio       float64
	}{
		{"admin raised bottom above an already-set 卖价 (no vip involved)", 0.45, 0.8},
		{"agent's own vip 力度 pushed chargedGroupRatio below bottom", 0.45 * 0.5, 0.4}, // 0.225 < 0.4
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ratioMarkupQuotaUnits(1000, c.chargedGroupRatio, c.bottomRatio)
			if got != 0 {
				t.Fatalf("drift (chargedGroupRatio %v <= bottom %v) must yield exactly 0, got %d", c.chargedGroupRatio, c.bottomRatio, got)
			}
		})
	}
}

// TestCreditRatioMarkup_AgentBearsOwnRealizedDiscount 是 spec §9.4（v3 公式）/§9.6.1 的核心记账断言，
// **取代 v2 版本的 TestCreditRatioMarkup_VipDiscountDoesNotAffectAgentMarkup_PlatformBearsCost（该测试
// 名字与断言现在都是错的，已删除，不是新增独立用例）**：对同样的「原始消耗」（rawUnits 相同），vip 用户
// （chargedGroupRatio 更低）为该代理带来的 markup **严格小于** default 用户（而不是 v2 断言的"完全相等"）
// ——折扣越深，代理自己的差价收入越少，折扣成本由代理自己的账面吸收，不是平台兜底。
func TestCreditRatioMarkup_AgentBearsOwnRealizedDiscount(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register model group: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 基准 = 0.3

	const sellRatio = 0.9
	const bottomRatio = 0.7 // 高于平台基准 0.3，模拟已配置底价的代理
	const rawUnits = 1000.0 // 两个用户「原始消耗」（token×ModelRatio）完全相同

	defaultTier := 1.0
	defaultCharged := int64(rawUnits * defaultTier * sellRatio) // 900
	vipTier := 0.8                                              // 代理自设的 vip 力度（§9.6.1；本测试不经 resolveModelGroup2D，直接注入已生效值）
	vipCharged := int64(rawUnits * vipTier * sellRatio)          // 720，vip 少付

	seedUser(t, app, 100, 5) // default 用户
	seedUser(t, app, 101, 5) // vip 用户，同一 L1 租户
	if err := app.TenantRepo.UpsertGroup(ctx, 5, "claude-kiro", sellRatio); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	app.creditRatioMarkup(ctx, 5, 100, defaultCharged, "claude-kiro", "req-vip-default", "wallet", defaultTier*sellRatio, bottomRatio)
	app.creditRatioMarkup(ctx, 5, 101, vipCharged, "claude-kiro", "req-vip-vip", "wallet", vipTier*sellRatio, bottomRatio)

	var rows []struct {
		SourceID string
		Amount   float64
	}
	if err := app.DB.Table("agent_earning_logs").
		Select("source_id, amount").Where("tenant_id = ?", 5).Order("source_id").Find(&rows).Error; err != nil {
		t.Fatalf("query earnings: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 markup rows (default + vip), got %d: %+v", len(rows), rows)
	}
	// rows[0]="req-vip-default", rows[1]="req-vip-vip"（字典序）。
	// v3：vip 行必须严格小于 default 行（代理自己吸收折扣），而不是 v2 断言的"两行相等"。
	if !(rows[1].Amount < rows[0].Amount) {
		t.Fatalf("agent must bear its own realized discount (vip markup must be strictly less than default's): default=%v vip=%v (rows=%+v)",
			rows[0].Amount, rows[1].Amount, rows)
	}
	// 具体数值锁定（而不仅仅是"更小"）：rawUnits=1000 时，default markup=(0.9-0.7)*1000=200，
	// vip markup=(0.72-0.7)*1000=20 —— 与 spec §9.4 的确认例子（底价0.5/卖价0.8/vip0.9→0.22 vs 0.3）
	// 同一套公式、不同数字的再验证。
	wantDefaultCNY := consumeCommissionCNY(200, 1, operation_setting.USDExchangeRate)
	wantVipCNY := consumeCommissionCNY(20, 1, operation_setting.USDExchangeRate)
	if math.Abs(rows[0].Amount-wantDefaultCNY) > 1e-6 {
		t.Fatalf("default markup = %v, want %v", rows[0].Amount, wantDefaultCNY)
	}
	if math.Abs(rows[1].Amount-wantVipCNY) > 1e-6 {
		t.Fatalf("vip markup = %v, want %v", rows[1].Amount, wantVipCNY)
	}
}
```

- [ ] **Step 2:** Run — expect PASS immediately (characterization tests against the already-(v3-)corrected Task 12-14 implementation; see this task's intro for why there's no red state here).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'RatioMarkupQuotaUnits|AgentBearsOwnRealizedDiscount'
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/... ./service/...
```

- [ ] **Step 3:** Commit.
```
cd /Users/cc/newapi628 && git add internal/mtwire/consume_markup_test.go && git commit -m "test(billing): lock in agent-bears-own-vip-discount markup accounting (spec §9.4 v3/§9.6.1)"
```

---

### Task 16: 代理自设 per-tenant vip 力度 (`HandleAgentSetTierRatio`/`HandleAgentListTierRatios`) + `resolveModelGroup2D` 层级轴接线. Change 1

**2026-07-02 新增（用户确认三处修订之一）。** 解决 spec §9.6 末尾曾标注、Task 15 曾如实测过的"已知差距"——建一个与既有"代理自设模型分组卖价"（`HandleAgentSetGroupRatio` → `tenant_groups`，Task 12）**同一套机制**的 per-tenant 覆盖，但开在**层级轴**（`userGroup`，不是 `usingGroup`）：代理可以给自己名下的 vip 层级单独设一个折扣力度，命中即用，未设则不打折（**不**回退平台全局 vip 倍率）。存储复用同一张 `tenant_groups` 表 + 同一对 `UpsertGroup`/`LookupEnabledGroupRatio` 方法（不建新表、不建新字段），靠一个 `tier:` 前缀把两条轴的命名空间物理隔开——今天没人会把模型分组取名叫 "vip"，但两条轴共享同一张表 + 同一个 `UNIQUE(tenant_id, group_name)` 键空间，前缀是零成本的永久隔离，不依赖命名约定。

**⚠️ 范围判断（保守方案，已选定并如下实现，但需用户确认；见配套报告）：** "No overlap"（代理下级用户的层级折扣只能来自代理自己的覆盖，不回退平台全局）本任务**只对 level>=1（L1/独立档）代理生效**。L0（普通档）代理没有调用 `HandleAgentSetTierRatio` 的权限（gate `level>=1`，同 `HandleAgentSetGroupRatio`），如果"No overlap"不分 level 一律套用，L0 下级用户会在毫无代理动作的情况下从"吃平台全局 vip 折扣"变成"tier=1，无折扣"——这是纯粹的负面变化（用户付更多、L0 代理也无法弥补，因为它压根没有自设折扣的能力）。本任务选择让**L0 下级用户的层级折扣行为与 Change 1 上线前完全一致**（继续吃平台全局），只让**L1 下级用户**进入"No overlap"的新语义。这是本任务在"如实按用户确认的规则实现"与"不引入未被要求、对 L0 单方面有害的副作用"之间做的判断，不是显而易见的唯一解——另一种同样自洽的替代方案是不分 level 一律套用"No overlap"。**本轮按"对 L0 零影响"的方案实现，请确认是否符合预期。**

**⚠️ 部署顺序提醒（与 Task 13/15 呼应，务必一起看）：** Task 13 的差价公式修订（Change 2）本身，在本任务上线前，就已经让「任何降低 `chargedGroupRatio` 的层级优惠」影响代理差价——而本任务上线前，L1 代理下级 vip 用户唯一可能吃到的层级优惠就是**平台全局** `GroupRatio['vip']`。也就是说：**如果 Task 13 单独上线、本任务还没上，会有一段时间代理的差价收入随平台管理员调整全局 vip 折扣而波动**——这不是"谁设谁担"，是"平台设、代理担"，方向反了。Task 13 与本任务逻辑上是一对，**应在同一次上线中一起部署**；分开上线前须与用户确认这段过渡期是否可接受。

**Files:**
- `internal/mtwire/distribution.go`（`allowedAgentTiers` ~443；新增 `agentOverridableTier`/`tierGroupKey` 辅助 + `tierRatioOut`/`HandleAgentSetTierRatio`/`HandleAgentListTierRatios`，紧邻 §6 "代理给下级用户设层级"之后）。
- `internal/mtwire/distribution_test.go`（复用 Task 12 的 `newGroupRatioApp`；新增测试）。
- `internal/mtwire/grouphook.go`（新增 `resolveTierRatio`，紧邻既有 `resolveTenantGroupRatio`）。
- `internal/mtwire/modelgroup.go`（`resolveModelGroup2D` ~83-103：层级轴改用 `a.resolveTierRatio`）。
- `internal/mtwire/modelgroup_test.go`（`newModelGroup2DTenantApp` 加 AgentRepo/AgentService；更新 `TestResolveModelGroup2D_TenantOverride` 的种子数据；新增 `TestResolveModelGroup2D_AgentVipOverride_DefaultsToOne`）。
- `router/mt-router.go`（`agentSelf` 组，紧邻既有 `/groups`/`groups/:group` 两行之后）。

**Interfaces:**
- Consumes: `App.ensureAgentLevel`（Task 3）、`AgentService.AgentLevel`（Task 1）、`TenantRepo.UpsertGroup`/`LookupEnabledGroupRatio`（既有，Phase 1）——**零新增持久化方法**。
- Produces: `agentOverridableTier(tier string) bool`；`tierGroupKey(tier string) string`（= `"tier:" + tier`）；`tierRatioOut` DTO；`(a *App) HandleAgentSetTierRatio(c *gin.Context)`（`PUT /api/tenant/tier-ratio/:tier`，gate level>=1）；`(a *App) HandleAgentListTierRatios(c *gin.Context)`（`GET /api/tenant/tier-ratio`，gate level>=1）；`(a *App) resolveTierRatio(ctx context.Context, userID int64, userGroup string) float64`（纯查询，无 panic 兜底——由调用方 `resolveModelGroup2D` 的 `defer recover()` 统一覆盖）。
- Changes: `resolveModelGroup2D` 的层级轴解析（`tier := groupRatioOf(userGroup)` → `tier := a.resolveTierRatio(...)`）——**行为变化仅限于**"用户归属某 L1 代理 且 userGroup 是可代理覆盖层级(今仅 vip)"这一种组合；主站直客、L0 代理下级、`default` 层级三种情况数值上与今天完全一致（见上方"范围判断"）。

- [ ] **Step 1：写失败测试。** 先改 `internal/mtwire/modelgroup_test.go` 的共享 harness——在 import 块加两行：
```go
	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
```
  替换 `newModelGroup2DTenantApp`：
```go
// newModelGroup2DTenantApp 在 newModelGroup2DApp 基础上加 users(id,tenant_id) + tenant_groups +
// TenantRepo + agent_profiles + AgentRepo/AgentService，供「代理 per-tenant 覆盖」(模型分组轴，既有)
// 与「代理自设 vip 力度」(层级轴，Change 1/spec §9.6.1)两类用例共用——resolveModelGroup2D 两条轴都要
// 解析 userID→租户→level/tenant_groups 覆盖。
func newModelGroup2DTenantApp(t *testing.T) *App {
	t.Helper()
	app := newModelGroup2DApp(t)
	if err := app.DB.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if err := tenantrepo.AutoMigrate(app.DB); err != nil { // tenants/tenant_domains/tenant_groups
		t.Fatalf("tenant migrate: %v", err)
	}
	app.TenantRepo = tenantrepo.New(app.DB)
	if err := agentrepo.AutoMigrate(app.DB); err != nil { // agent_profiles + agent_earning_logs + wallet
		t.Fatalf("agent migrate: %v", err)
	}
	ar := agentrepo.New(app.DB)
	app.AgentRepo = ar
	app.AgentService = agent.NewService(ar, nil)
	return app
}
```
  在 `TestResolveModelGroup2D_TenantOverride` 里，紧接既有两行 `app.TenantRepo.UpsertGroup(ctx, 7, "claude-kiro", 0.4)` / `UpsertGroup(ctx, 7, "default", 1.5)` 之后，插入：
```go
	// Level 门禁 + Change 1(vip 力度覆盖，spec §9.6.1):租户 7 设为 L1，并显式设 vip 覆盖 = 0.8
	// (与平台全局 vip 基准数值相同，仅为让本测试原有断言在"Default=1"新语义下继续成立——
	// "未设覆盖时不再回退平台全局"这一新行为由 TestResolveModelGroup2D_AgentVipOverride_DefaultsToOne
	// 单独覆盖)。
	if err := app.AgentService.SetAgentType(ctx, 7, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set tenant 7 to L1: %v", err)
	}
	if err := app.TenantRepo.UpsertGroup(ctx, 7, tierGroupKey("vip"), 0.8); err != nil {
		t.Fatalf("upsert vip tier override: %v", err)
	}
```
  追加新测试：
```go
// TestResolveModelGroup2D_AgentVipOverride_DefaultsToOne 覆盖 Change 1 的核心新行为(spec agent-tiering
// §9.6.1):"No overlap" —— L1(独立档)代理下级用户的层级折扣只能来自该代理自己的覆盖，从不回退平台
// 全局 GroupRatio['vip']；未配置覆盖 → tier=1(无折扣)。与此相对：① 主站直客(tenant_id=0)不受影响，
// 继续吃平台全局；② L0(普通档)代理下级用户也不受影响，继续吃平台全局(§9.6.1 标注为一处需确认的范围
// 判断——L0 没有 HandleAgentSetTierRatio 的调用权限，本实现选择让 L0 保持 v2 行为完全不变)。
func TestResolveModelGroup2D_AgentVipOverride_DefaultsToOne(t *testing.T) {
	ctx := context.Background()
	app := newModelGroup2DTenantApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // 平台全局 vip=0.8

	if err := app.AgentService.SetAgentType(ctx, 70, agent.AgentParams{Level: 1}); err != nil { // L1，未设 vip 覆盖
		t.Fatalf("set tenant 70 to L1: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 71, agent.AgentParams{Level: 0}); err != nil { // L0
		t.Fatalf("set tenant 71 to L0: %v", err)
	}
	seedUser(t, app, 500, 70) // L1 代理下级，未配置 vip 覆盖
	seedUser(t, app, 510, 71) // L0 代理下级
	seedUser(t, app, 600, 0)  // 主站直客

	cases := []struct {
		userID      int64
		user, using string
		want        float64
		note        string
	}{
		{500, "vip", "", 1, "L1 代理下级 vip 用户，代理未设覆盖 → Default=1(不回退平台全局 0.8)"},
		{510, "vip", "", 0.8, "L0 代理下级：无自设覆盖能力，行为不变，继续吃平台全局 0.8"},
		{600, "vip", "", 0.8, "主站直客：不受影响，继续吃平台全局 0.8"},
		{500, "default", "", 1, "default 层级本就不可代理覆盖，行为不变(=1)"},
		{500, "vip", "claude-kiro", 1 * stub2DRatios("claude-kiro"), "tier=1(未覆盖) × 模型分组基准(claude-kiro 未覆盖)=0.3"},
	}
	for _, c := range cases {
		r, ok := app.resolveModelGroup2D(c.userID, c.user, c.using)
		if !ok || !almostEqual(r, c.want) {
			t.Fatalf("resolve(%d,%q,%q) = (%v,%v), want (%v,true) — %s", c.userID, c.user, c.using, r, ok, c.want, c.note)
		}
	}

	// 租户 70 随后自设 vip 力度 0.5(比平台全局 0.8 折扣更深)→ 立即生效，且只影响自己的下级。
	if err := app.TenantRepo.UpsertGroup(ctx, 70, tierGroupKey("vip"), 0.5); err != nil {
		t.Fatalf("upsert vip tier override: %v", err)
	}
	if r, ok := app.resolveModelGroup2D(500, "vip", ""); !ok || !almostEqual(r, 0.5) {
		t.Fatalf("after override: resolve(500,vip,\"\") = (%v,%v), want (0.5,true)", r, ok)
	}
	if r, ok := app.resolveModelGroup2D(510, "vip", ""); !ok || !almostEqual(r, 0.8) {
		t.Fatalf("L0 tenant 71 must stay unaffected by tenant 70's override: got %v", r)
	}
	if r, ok := app.resolveModelGroup2D(600, "vip", ""); !ok || !almostEqual(r, 0.8) {
		t.Fatalf("main-site user must stay unaffected by tenant 70's override: got %v", r)
	}
}
```
  再往 `internal/mtwire/distribution_test.go` 追加（复用 Task 12 的 `newGroupRatioApp`，已含 AgentRepo/AgentService；`agent` 包 Task 12 已导入，无需再加 import）：
```go
// TestHandleAgentSetTierRatio_RequiresLevel1AndValidTier 覆盖 Change 1 的门禁 + 校验（spec agent-tiering
// §9.6.1）：L0 → 403 AGENT_LEVEL_LOCKED；"default"/模型分组名 → 400 AGENT_TIER_INVALID；
// "vip" + L1 → 200，落 tenant_groups[tenant, tierGroupKey("vip")]（与卖价覆盖同表，前缀隔离命名空间）。
func TestHandleAgentSetTierRatio_RequiresLevel1AndValidTier(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 80, agent.AgentParams{Level: 0}); err != nil {
		t.Fatalf("set L0: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 81, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set L1: %v", err)
	}

	// L0：锁。
	c, rec := newAgentCtx(80, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "vip"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_LEVEL_LOCKED" {
		t.Fatalf("L0 must be locked, got %+v", r)
	}

	// L1 + "default"：拒（default 恒 1，不可覆盖）。
	c, rec = newAgentCtx(81, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "default"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_TIER_INVALID" {
		t.Fatalf("default tier must be rejected, got %+v", r)
	}

	// L1 + 模型分组名（"claude-kiro"）：拒（两轴互斥，防串号）。
	c, rec = newAgentCtx(81, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "claude-kiro"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_TIER_INVALID" {
		t.Fatalf("model-group name must be rejected on the tier axis, got %+v", r)
	}

	// L1 + "vip"：放行，落库到 tierGroupKey 隔离的命名空间。
	c, rec = newAgentCtx(81, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "vip"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("L1 vip should be allowed, got %+v", r)
	}
	got, found, err := app.TenantRepo.LookupEnabledGroupRatio(ctx, 81, tierGroupKey("vip"))
	if err != nil || !found || got != 0.7 {
		t.Fatalf("tenant_groups[81,%q] = (%v,%v,%v), want (0.7,true,nil)", tierGroupKey("vip"), got, found, err)
	}
	// 隔离验证：模型分组轴的裸 "vip" 键必须不存在（两轴不串号）。
	if _, found, _ := app.TenantRepo.LookupEnabledGroupRatio(ctx, 81, "vip"); found {
		t.Fatal("bare \"vip\" key must not be written — tier overrides must use the tierGroupKey-prefixed namespace")
	}
}

// TestHandleAgentListTierRatios_ReflectsOverrideOrDefaultOne 覆盖列表展示：未覆盖展示 1（不是平台参考
// 值，避免 UI 暗示"没设=用平台的"）；已覆盖展示覆盖值 + has_override=true。
func TestHandleAgentListTierRatios_ReflectsOverrideOrDefaultOne(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // 平台参考 vip=0.8
	if err := app.AgentService.SetAgentType(ctx, 82, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set L1: %v", err)
	}

	c, rec := newAgentCtx(82, "GET", "", nil)
	app.HandleAgentListTierRatios(c)
	r := decodeResp(t, rec)
	var rows []tierRatioOut
	if err := json.Unmarshal(r.Data, &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Tier != "vip" || rows[0].Ratio != 1 || rows[0].HasOverride {
		t.Fatalf("no override yet: want [{vip,1,false}], got %+v", rows)
	}

	if err := app.TenantRepo.UpsertGroup(ctx, 82, tierGroupKey("vip"), 0.6); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	c, rec = newAgentCtx(82, "GET", "", nil)
	app.HandleAgentListTierRatios(c)
	r = decodeResp(t, rec)
	if err := json.Unmarshal(r.Data, &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Ratio != 0.6 || !rows[0].HasOverride || rows[0].PlatformRatio != 0.8 {
		t.Fatalf("after override: want [{vip,0.6,true,platform=0.8}], got %+v", rows)
	}
}
```

- [ ] **Step 2：跑测试，预期 FAIL**（编译错：`tierGroupKey`/`HandleAgentSetTierRatio`/`HandleAgentListTierRatios`/`tierRatioOut`/`resolveTierRatio` 未定义）。
```
cd /Users/cc/newapi628 && go vet ./internal/mtwire/... 2>&1 | head -30
```

- [ ] **Step 3：在 `internal/mtwire/distribution.go` 加辅助函数 + 两个端点。** 紧接既有 `allowedAgentTiers`（§6 顶部）之后：
```go
// agentOverridableTier 报告 tier 是否是代理可自设覆盖的层级(Change 1，spec §9.6.1)：必须在
// allowedAgentTiers 白名单内，且排除 "default"（恒 1，语义上无需、也不允许覆盖——HandleAgentSetUserTier
// 允许把用户"分配"到 default，但不允许代理给 default 这个层级本身设折扣力度，两个操作面不同）。
func agentOverridableTier(tier string) bool {
	if tier == "" || tier == "default" {
		return false
	}
	_, ok := allowedAgentTiers[tier]
	return ok
}

// tierGroupKey 把层级名映射为 tenant_groups 的存储 key：加 "tier:" 前缀。模型分组轴
// （HandleAgentSetGroupRatio）用裸模型分组名写入同一张表——两条轴共享 tenant_groups 的
// UNIQUE(tenant_id, group_name) 键空间，前缀是零成本的永久隔离，不依赖"没人把模型分组取名叫 vip"
// 这种命名约定。
func tierGroupKey(tier string) string { return "tier:" + tier }

// ============================================================================
// 6b) 我的层级折扣力度（代理自设 per-tenant vip 覆盖；Change 1，spec agent-tiering §9.6.1）
// ============================================================================
//
// 与 §5 的模型分组卖价覆盖"同一套机制"，开在层级轴：代理可给自己名下某个可覆盖层级
// （allowedAgentTiers 去掉 default——今仅 vip）单独设折扣力度。命中用覆盖值；未设 → 1（不打折，不回退
// 平台全局，"No overlap"）。存储复用 tenant_groups，key 加 tierGroupKey 前缀防止与模型分组名串号。

// tierRatioOut 是「我的层级折扣力度」行。platform_ratio 仅供参考（平台全局值，本轴未覆盖时不回退它，
// 与 modelGroupRatioOut 的 platform_ratio 语义不同——那边未覆盖时 ratio 就是 platform_ratio）。
type tierRatioOut struct {
	Tier          string  `json:"tier"`
	Ratio         float64 `json:"ratio"`
	PlatformRatio float64 `json:"platform_ratio"`
	HasOverride   bool    `json:"has_override"`
}

// HandleAgentListTierRatios GET /api/tenant/tier-ratio —— 本租户可覆盖层级（今仅 vip）的当前状态：
// 未覆盖展示 1（不是 platform_ratio——No overlap：代理下级用户从不吃平台全局 vip 折扣，UI 不能暗示
// "没设=用平台的"）；已覆盖展示覆盖值。gate: level>=1（同 HandleAgentSetGroupRatio 的门禁机制）。
func (a *App) HandleAgentListTierRatios(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ctx := reqCtx(c)
	names := make([]string, 0, len(allowedAgentTiers))
	for t := range allowedAgentTiers {
		if t == "default" {
			continue
		}
		names = append(names, t)
	}
	sort.Strings(names) // 确定性输出（今仅 "vip" 一项，未来扩充时保持稳定顺序）
	out := make([]tierRatioOut, 0, len(names))
	for _, tier := range names {
		row := tierRatioOut{Tier: tier, Ratio: 1, PlatformRatio: groupRatioOf(tier)}
		if override, found, err := a.TenantRepo.LookupEnabledGroupRatio(ctx, tenantID, tierGroupKey(tier)); err == nil && found {
			row.Ratio = override
			row.HasOverride = true
		}
		out = append(out, row)
	}
	respondOK(c, out)
}

// HandleAgentSetTierRatio PUT /api/tenant/tier-ratio/:tier —— 设本租户对某可覆盖层级的折扣力度覆盖
// （Change 1，spec §9.6.1）。校验：① level>=1（复用 ensureAgentLevel，Task 3 既有机制，不新建 gate）；
// ② tier 必须是可代理覆盖层级（agentOverridableTier：在 allowedAgentTiers 内且非 default），否则
// AGENT_TIER_INVALID（复用 HandleAgentSetUserTier 的既有错误码，语义一致："不允许的用户层级"）；
// ③ ratio 结构性校验 > 0（不设上限，与 §9.7 BottomPriceRatio 同哲学）。
//
// 本端点不做"卖价×vip 力度 ≥ 底价"的预防性穷举校验（不会在这里反查该代理名下所有已设卖价的模型分组）
// ——与"管理员改底价"端点本身也不做这种穷举校验是同一个先例（两个独立旋钮谁后设谁可能打破对方前提，
// 本项目现有选择是"结算时兜底，不在设置时穷举预防"，见 Task 13 creditRatioMarkup 的 clamp）。
func (a *App) HandleAgentSetTierRatio(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	tier := strings.TrimSpace(c.Param("tier"))
	if !agentOverridableTier(tier) {
		respondErr(c, errAgentTierInvalid)
		return
	}
	var body struct {
		Ratio float64 `json:"ratio"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Ratio <= 0 {
		respondErr(c, errAgentInputInvalid)
		return
	}
	if err := a.TenantRepo.UpsertGroup(reqCtx(c), tenantID, tierGroupKey(tier), body.Ratio); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, tierRatioOut{Tier: tier, Ratio: body.Ratio, PlatformRatio: groupRatioOf(tier), HasOverride: true})
}
```
  `distribution.go` 顶部 import 块加 `"sort"`。

- [ ] **Step 4：在 `internal/mtwire/grouphook.go` 加 `resolveTierRatio`**，紧接既有 `resolveTenantGroupRatio` 之后：
```go
// resolveTierRatio 是层级轴的倍率解析（Change 1，spec agent-tiering §9.6.1）：
//   - userGroup 不是「可代理覆盖层级」(agentOverridableTier；今仅 vip，default 恒排除) → 平台全局
//     groupRatioOf(userGroup)（既有行为，不变，不查库）。
//   - 是可代理覆盖层级，但用户不归属任何 L1（独立档）代理（主站直客 / L0 代理下级 / 未归属）→
//     平台全局 groupRatioOf(userGroup)（范围限定为仅 level>=1——L0 代理没有 HandleAgentSetTierRatio
//     的调用权限，因此 L0 下级用户的层级折扣行为与 Change 1 上线前完全一致；这是一处需用户确认的
//     判断，见 Task 16 顶部"范围判断"）。
//   - 是可代理覆盖层级 且 归属 L1 代理：该代理为此层级设了 enabled 覆盖 → 用覆盖值（代理自担，
//     §9.4）；未设置 → 1（"Default=1，no discount"——不回退平台全局，"No overlap"）。
//
// 自带 panic 兜底由调用方 resolveModelGroup2D 的 defer/recover 统一覆盖，此处不重复包一层。
func (a *App) resolveTierRatio(ctx context.Context, userID int64, userGroup string) float64 {
	baseline := groupRatioOf(userGroup)
	if !agentOverridableTier(userGroup) {
		return baseline
	}
	tenantID := a.userTenantID(ctx, userID)
	if tenantID <= 0 {
		return baseline // 主站直客：平台全局，既有行为不变
	}
	if a.AgentService == nil {
		return baseline // 未装配：安全回退（不查库、不改变现状）
	}
	lvl, err := a.AgentService.AgentLevel(ctx, tenantID)
	if err != nil || lvl < 1 {
		return baseline // L0 / 非代理 / 查询失败：无自设覆盖能力，沿用平台全局（对 L0 零行为变化）
	}
	if a.TenantRepo == nil {
		return baseline
	}
	if override, found, err := a.TenantRepo.LookupEnabledGroupRatio(ctx, tenantID, tierGroupKey(userGroup)); err == nil && found {
		return override
	}
	return 1 // L1 且未配置覆盖：Default=1，不回退平台全局（No overlap）
}
```

- [ ] **Step 5：接线 `resolveModelGroup2D`（`internal/mtwire/modelgroup.go` ~83-103）**，把层级轴从直接调 `groupRatioOf` 改成调 `a.resolveTierRatio`：
```go
	// 层级轴（Change 1，spec §9.6.1）：非「可代理覆盖层级」或用户不归属 L1 代理 → 平台全局
	// groupRatioOf(userGroup)（既有行为不变）；归属 L1 代理 → 该代理自设的 per-tenant 覆盖（未配置则 1，
	// 不回退平台全局，"No overlap"）。见 resolveTierRatio（grouphook.go）。
	tier := a.resolveTierRatio(context.Background(), userID, userGroup)
	factor := 1.0
	if usingGroup != "" && a.ModelGroupRepo.IsModelGroup(usingGroup) {
		factor = groupRatioOf(usingGroup) // 平台基准
		// 代理 per-tenant 覆盖（仅模型分组）：命中即用覆盖值（写入端已保证 ≥ 基准，代理加价）。
		if override, hit := a.resolveTenantGroupRatio(context.Background(), userID, usingGroup); hit {
			factor = override
		}
	}
	return tier * factor, true
```
  （函数其余部分——签名、开头的 `defer recover()`、`if a.ModelGroupRepo == nil` 早退——不变，只替换原来 `tier := groupRatioOf(userGroup)` 这一行及其后续。）

- [ ] **Step 6：注册路由。** `router/mt-router.go`，紧接既有两行之后（`agentSelf.PUT("/groups/:group", ...)`）：
```go
			// 我的层级折扣力度（代理自设 vip 覆盖；Change 1，spec §9.6.1）：列表 / 设覆盖（仅可代理
			// 覆盖层级——今仅 vip；default 不可覆盖）。
			agentSelf.GET("/tier-ratio", app.HandleAgentListTierRatios)
			agentSelf.PUT("/tier-ratio/:tier", app.HandleAgentSetTierRatio)
```

- [ ] **Step 7：跑测试，预期 PASS。**
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/... ./internal/agent/...
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/...
```

- [ ] **Step 8：提交。**
```
cd /Users/cc/newapi628 && git add internal/mtwire router/mt-router.go && git commit -m "feat(billing): agent-settable per-tenant vip tier override (Change 1, spec §9.6.1)"
```

---

### Task 17: 邀请链接折扣率字段预留 (`promotion.Channel.DiscountRate`) — schema-only，billing 不读. Change 3

**2026-07-02 新增（用户确认三处修订之一）。** 纯 schema 预留：每条推广/邀请渠道（`agent_promotion_channels`，一个代理可建多条）加一个 `discount_rate` 字段，默认 1.0（不打折），供**未来**"不同邀请链接可以有不同折扣力度"这个功能使用。**本轮不激活**——不加任何设置入口、不接入计费公式（§9.2/§9.4 均不读它）、不建退款/折扣通路。与 §9.5 末尾"明确不做邀请 9 折"不矛盾:那条禁止的是 v1 式、全局统一的邀请折扣退款通路;这里只是给"按渠道各自独立"的粒度预先占一个字段位置,避免以后要为这一列单独跑一次破坏性迁移。全程沿用 GORM `AutoMigrate` 自动加列(同 Task 1 的 `can_api`、Task 11 的 `bottom_price_ratio`),无需 `information_schema` 守卫的破坏性迁移脚本。

**Files:**
- `internal/promotion/model.go`（`Channel` struct）。
- `internal/promotion/service.go`（`CreateChannel`：新渠道固定写入 1.0）。
- `internal/promotion/gormrepo/gormrepo.go`（`channelRow` 加列；`CreateChannel`/`toChannel` round-trip）。
- `internal/promotion/gormrepo/gormrepo_test.go`（round-trip 测试）。
- `internal/mtwire/distribution.go`（`channelOut`/`toChannelOut`：只读展示，不新增设置端点）。

**Interfaces:**
- Produces: `promotion.Channel.DiscountRate float64` —— 预留，`1.0` = 不打折（本轮唯一允许的值，没有任何写入路径能把它设成别的值）。
- 无接口/方法签名变化——`PromotionService.CreateChannel`/`PromotionRepo.CreateChannel` 均不变（`Channel` 是整体传递的 struct，新增字段对调用方透明，同 Task 11 对 `AgentParams` 的处理方式）。

- [ ] **Step 1：写失败测试。** 追加到 `internal/promotion/gormrepo/gormrepo_test.go`（复用本文件既有的 `newTestRepo` helper，不新建）：
```go
// TestCreateChannel_DiscountRateRoundTrips 确认 discount_rate 随渠道持久化并读回；新建渠道默认 1.0
// （Change 3，spec agent-tiering §9.11——预留字段，本轮 billing 不读）。
func TestCreateChannel_DiscountRateRoundTrips(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	c := &promotion.Channel{TenantID: 1, ChannelCode: "disc_x", DiscountRate: 1}
	if err := r.CreateChannel(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := r.GetChannelByCode(ctx, "disc_x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DiscountRate != 1 {
		t.Fatalf("DiscountRate = %v, want 1 (reserved default)", got.DiscountRate)
	}
}
```

- [ ] **Step 2：跑测试，预期 FAIL**（编译错：`DiscountRate` 未定义）。
```
cd /Users/cc/newapi628 && go test ./internal/promotion/... -run DiscountRate
```

- [ ] **Step 3：加字段。** `internal/promotion/model.go` 的 `Channel` struct，`RegisteredCount` 之后：
```go
	RegisteredCount int64  // 经本渠道注册的用户数
	// DiscountRate 邀请折扣率（Change 3，spec §9.11：按渠道预留，本轮不激活）。1.0 = 不打折（本轮唯一
	// 允许的值——没有任何写入路径能把它设成别的值）。billing（§9.2/§9.4）不读这个字段。
	DiscountRate float64
```
  `internal/promotion/gormrepo/gormrepo.go` 的 `channelRow`，`RegisteredCount` 列之后：
```go
	RegisteredCount int64     `gorm:"column:registered_count;not null;default:0"`
	DiscountRate    float64   `gorm:"column:discount_rate;type:decimal(6,4);not null;default:1"`
```
  `CreateChannel`（gormrepo）的 `row := channelRow{...}` 字面量加 `DiscountRate: c.DiscountRate,`；`toChannel` 的返回字面量加 `DiscountRate: row.DiscountRate,`。

- [ ] **Step 4：新渠道固定预留值。** `internal/promotion/service.go` `CreateChannel` 的 `c := &Channel{...}` 字面量加 `DiscountRate: 1,`（新渠道一律不打折；本轮没有任何参数可以覆盖这个值）。

- [ ] **Step 5：跑测试，预期 PASS。**
```
cd /Users/cc/newapi628 && go test ./internal/promotion/...
```

- [ ] **Step 6：只读展示（不新增设置端点）。** `internal/mtwire/distribution.go` 的 `channelOut`（§2 推广渠道）加字段：
```go
type channelOut struct {
	ID              int64   `json:"id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	RegisteredCount int64   `json:"registered_count"`
	// DiscountRate 邀请折扣率（Change 3，预留占位，本轮恒 1.0，billing 不读；见 spec §9.11）。
	DiscountRate    float64 `json:"discount_rate"`
}
```
  `toChannelOut` 加 `DiscountRate: c.DiscountRate,`。

- [ ] **Step 7：跑全量，预期 PASS。**
```
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/promotion/... ./internal/mtwire/...
```

- [ ] **Step 8：提交。**
```
cd /Users/cc/newapi628 && git add internal/promotion internal/mtwire/distribution.go && git commit -m "feat(promotion): reserve per-channel discount_rate field (Change 3, spec §9.11) — schema only, not wired into billing"
```

---

## Self-review notes (coverage of the spec)

- Spec §5.1 (data model): `can_api` added (Task 1); `type` column + `AgentType` enum + frontend `agentTypeValues` deleted (Task 2 / Task 6); migration backfills level=1 then drops `type` (Task 2).
- Spec §5.2 (backend): `AgentLevel` helper (Task 1); typeless `SetAgentType`/`GetAgentType` (Task 2); subdomain gate on create + promotion (Task 4); `RequireAgentLevel(1)` route gate + in-handler depth (Task 3); migration (Task 2); L0 promotion-link attribution path is unchanged (`attributeByChannel`/`attributeByHost` never depended on a subdomain — verified, no code touched).
- Spec §5.3 (frontend): level selector + badge + deleted enum (Task 6); sidebar + route guards on level>=1 (Task 7); `/api/tenant/agent-context` extended (Task 5) and consumed (Task 7).
- Spec §6 (tests): level gate L0 reject / L1 pass (Task 3 unit + Task 8 E2E); L0 create no subdomain + promotion provisions (Task 4 unit + server); migration level=1 (Task 8 server); `can_api` placeholder does not gate (Task 1 round-trip, never read by any gate); create/edit works after `type` removed (Task 2/6).
- Deviations flagged: (a) DB `type`-drop co-located with code removal in Task 2 (compile-safety) rather than Task 1; (b) `SetAgentType`/`GetAgentType` names kept; (c) `SkipSubdomain` (default-provision) chosen over `ProvisionSubdomain` to keep existing tenant tests green; (d) new-agent create defaults to level 0 per "base tier" intent; (e) the drawer "key metrics for promotion decision" (spec §5.3.1) is delivered by Task 10 (per-agent promotion metrics 总充值/分润收益/下级用户数, reusing reportrepo aggregates + one thin downstream-count query); (f) the "not unlocked" page reuses `/403` (existing pattern) rather than a bespoke page.

**Part 2 (money model v2/v3, spec §9 — Tasks 11-17). This supersedes the earlier v1 self-review notes below (which covered the old "L0 提成+9折 / L1 差价" task list, now deleted). 2026-07-02 v3 update: Tasks 13/15 rewritten in place and Tasks 16-17 appended per 3 confirmed changes (Change 1 vip-tier override, Change 2 markup formula fix, Change 3 invite discount_rate reservation) — bullets below reflect the v3 end state, not the superseded v2 draft:**

- §9.1-9.3 (三线分离 · 消耗计费公式 · 四档价格阶梯): recharge/tokenplan lines are untouched — no task in this list touches `recharge_spread` or `Retail.SetListing`; the consumption formula itself (`resolveModelGroup2D`) is reused, with one v3 addition (Task 16's tier-axis `resolveTierRatio`, see §9.6 below — the model-group axis half of the formula is still unmodified by any task); the four-level ladder is realized purely through the new `BottomPriceRatio` field (Task 11) and its floor-wiring into `HandleAgentSetGroupRatio`/`HandleAgentListGroups` (Task 12).
- §9.4 (L1 差价入账,**v3 修订公式**): `creditRatioMarkup`/`ratioMarkupQuotaUnits` (Task 13, v3-corrected) compute `markup = chargedQuota − rawUnits×底价` directly off `chargedGroupRatio` — the realized, already-applied combined ratio from `relayInfo.PriceData.GroupRatioInfo.GroupRatio` — rather than the original v2 draft's tier-less "卖价" (which silently dropped any vip discount from the differential). `token×ModelRatio` is still recovered the same way (`chargedQuota ÷ chargedGroupRatio`), avoiding a second, race-prone lookup at settle time.
- §9.5 (L0 提成): formula unchanged (`commission_ratio × quotaUnits`), only extracted into `creditL0Commission` for the dispatch (Task 14); **no** 9折 — v1's `invite_discount_rate` mechanism is explicitly dropped, not carried forward or deprecated-in-place. Not to be confused with Task 17's `discount_rate` (Change 3) — that is an unrelated, unread, per-channel schema placeholder, not a revival of v1's mechanism.
- §9.6 / §9.6.1 (vip 优惠 / 谁设谁担,**Change 1,resolved**): the v2 draft characterized markup crediting as structurally tier-invariant (platform always bears vip cost) and explicitly flagged that as a gap needing user confirmation. **2026-07-02: confirmed and built.** Task 16 adds a per-tenant tier-axis override (`HandleAgentSetTierRatio`/`HandleAgentListTierRatios`, level≥1-gated, stored in the existing `tenant_groups` table under a `tier:`-prefixed key so it can't collide with the model-group axis) and wires it into `resolveModelGroup2D` via a new `resolveTierRatio` helper. Combined with Task 13's v3 formula, an agent's own vip force now shrinks their own markup (tested directly, Task 15 rewritten in place — `TestCreditRatioMarkup_AgentBearsOwnRealizedDiscount` replaces the v2-era `..._PlatformBearsCost` test, inverted assertion). Two judgment calls made and flagged for confirmation, not silently decided: (a) "No overlap" (agent's downstream vip users never fall back to the platform's global vip ratio) is scoped to **level≥1 tenants only** — L0 agents can't call the new endpoint, so their downstream vip users keep today's platform-global behavior unchanged, rather than silently losing their discount; (b) Task 16 does **not** add a preventive cross-check (agent's vip force vs. every already-configured 卖价 override) at set-time — the floor is enforced only at credit-time, via Task 13's `chargedGroupRatio ≤ 底价` clamp, mirroring this codebase's existing precedent for the analogous 底价-vs-卖价 drift problem (no set-time prevention there either).
- §9.7 (底价倍率新字段): `AgentParams.BottomPriceRatio` (Task 11); `consumeFloorRatio` (Task 12) is the single shared function both the write path (`HandleAgentSetGroupRatio`'s floor) and the read path (Task 13's `creditRatioMarkup`) call — the spec's explicit record-keeping-error warning ("两处必须同一口径") is satisfied by construction (one function, two call sites), not by convention alone. Task 16's vip-force floor is a corollary of this same clamp (compared against `chargedGroupRatio` post-Change-2), not a second mechanism.
- §9.9 (按档 gate + 幂等 / 红线): `HandleAgentSetGroupRatio` → level≥1, reusing Task 3's `ensureAgentLevel` (Task 12, no new gate mechanism) — Task 16's `HandleAgentSetTierRatio` reuses the identical gate; commission vs markup are mutually exclusive in `creditConsumeCommission`'s dispatch, tested directly (Task 14 `TestCreditConsumeCommission_TierSwitch_NeverBothSources`, including the "L1 also has a stray non-zero `commission_ratio` configured" adversarial case); every new write path (`creditRatioMarkup`, `creditL0Commission`) is `requestID`-idempotent via the existing `AgentRepo.AppendEarning` unique-index mechanism (retested end-to-end in Task 14's `TestCreditConsumeCommission_RetrySameRequestID_NoDoubleCredit`) and wrapped by `creditConsumeCommission`'s existing `defer recover()` (unchanged, never blocks the request); markup is structurally non-negative everywhere — Task 13's (v3-corrected) `ratioMarkupQuotaUnits` guard plus Task 15's (rewritten) cross-tier and drift characterization tests.
- §9.10 (tests): `BottomPriceRatio` round-trip + the existing `"all zero ok"` `Validate()` contract preserved (Task 11); floor uses the calling agent's own value and falls back to the platform baseline when unconfigured, `Floor`/`PlatformRatio` semantically split in the list response (Task 12); differential math (v3 formula) + no-override-means-no-credit + config-drift-safety + idempotency (Task 13); tier switch + no-double-credit + main-site-user unaffected (Task 14); **v3**: markup shrinks (non-increasing) across the tier range, never negative, and an agent's own vip force strictly reduces their own markup relative to a default-tier user with identical raw consumption (Task 15, rewritten — replaces the superseded v2 tier-invariance/platform-bears-cost characterization); tier-axis override round-trips + defaults to 1 (not platform baseline) for L1, stays at platform baseline for main-site/L0, and doesn't cross-contaminate the model-group axis's `tenant_groups` rows (Task 16).
- §9.11 (邀请折扣预留,**Change 3**): `promotion.Channel.DiscountRate` (Task 17) round-trips through `gormrepo`, defaults to `1.0` on every new channel, and is surfaced read-only on `channelOut` — no task in this list (including Task 17 itself) reads it from any billing code path.
- Deviations flagged (Part 2, not silently assumed): (a) the `ConsumeCommission` hook signature gained **two** new parameters (`usingGroup` *and* `chargedGroupRatio`), not the one a shallower read of "thread the group through" might suggest — recovering `token×ModelRatio` from an already-charged amount needs the *realized* combined ratio (§9.4/§9.6), not a value re-derived from a second, potentially-stale lookup at settle time, which would reopen a real race with an agent changing their own 卖价 (or, post-Task-16, their own vip force) between charge-time and settle-time (Task 13); (b) `HandleAgentListGroups`'s `Floor` field changes *meaning* for any existing consumer of that response — it used to always equal `PlatformRatio`, and now reflects the calling agent's own `BottomPriceRatio` when configured — a genuine, intentional behavior change, not just an internal refactor (Task 12); (c) v1's entire "邀请 9 折" mechanism (`invite_discount_rate` config var, quota-refund crediting, the dedicated `mt_invite_discount_grants` table) is dropped wholesale rather than migrated or deprecated in place — per this task list's own "Step 1" code-audit premise, none of it exists in this repo yet, so there is nothing to clean up; if that ever changes before this plan is executed, re-verify the premise before starting Task 11; (d) deploying Tasks 11-14 on top of this plan's already-implemented Task 2 (which backfilled every pre-existing agent to level=1) switches every such agent from commission-earning to markup-earning immediately, dropping to `$0` new earnings until they configure a 卖价 override — intentional per spec §9.9 ("二选一"), reflagged operationally in Task 14, unchanged from the equivalent v1 caveat; (e) **new, v3**: Tasks 13 and 16 are a deployment pair — shipping Task 13's formula fix alone (before Task 16) opens a window where L1 agents' markup income fluctuates with the platform's global vip ratio, which the agent doesn't control (both Task 13's and Task 16's intros flag this; confirm the transition window is acceptable if they must ship separately); (f) **new, v3**: Task 16's "No overlap" is scoped to level≥1 only (L0 unaffected) and does not add set-time preventive floor validation across an agent's other model-group 卖价 overrides — both are judgment calls made to bound scope/blast-radius, flagged in Task 16's own text for confirmation, not inferred silently.
