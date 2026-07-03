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

**Tasks 11-15 (below) implement the money model from spec §9 — L0 提成+9折 / L1 差价 — inside the consumption-billing hook.** 红线 reminder (applies to every task below): this touches the CONSUMPTION BILLING path. Every new code path must be (a) idempotent on `requestID` (no double-charge/double-credit on retry), (b) best-effort / failure-isolated (an error here must never block the user's actual request or the core quota deduction — same `defer recover()` + log-and-continue posture as the existing `creditConsumeCommission`), and (c) computed off **official price** as the base (the 9折 only reduces what the *user* pays; it must never shrink what an L0 agent earns, and the L1 markup base is the platform baseline, not the discounted price). Tasks 12/13/14 all edit `internal/mtwire/agent.go`'s `creditConsumeCommission` in strict sequence (12 → 13 → 14) — same file-ordering discipline as the plan's existing note about Tasks 1/2/4/5. Local gate for Tasks 11-15 is `go test` (no server dependency for red/green); `go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/...` is the local "does everything still compile" smoke check — **note:** a bare `go build ./...` from repo root currently fails in this checkout on `main.go`'s `//go:embed web/classic/dist` (that directory is a `bun run build` artifact that isn't present locally; per Global Constraints, full builds happen on the server). This is pre-existing and unrelated to Tasks 11-15 — the package-scoped command above covers every package they touch.

### Task 11: Config `invite_discount_rate` (default 0.9), admin-settable, read at billing

Introduces the one new knob the rest of Part 2 depends on. Reuses the **existing** native option machinery (`model/option.go` `InitOptionMap`/`UpdateOption`, exactly the `USDExchangeRate` pattern already used by `consumeCommissionCNY`) instead of inventing a new settings table or a new mtwire HTTP route: `PUT /api/option/` (native, `middleware.RootAuth()`-gated, `router/api-router.go:184-188`) already accepts `{"key":"<Any>","value":...}` and falls through to `model.UpdateOption` unconditionally (`controller/option.go:335`) for any key with no special case — so wiring one switch-case is sufficient for the setting to become admin-settable; no new endpoint. `GET /api/option/` (`controller/option.go:78-108`) already dumps `common.OptionMap` dynamically (sensitive-suffix filter only: Token/Secret/Key/api_key — `InviteDiscountRate` doesn't match), so it will show up there automatically too. New mtwire-owned logic (this task's `effectiveInviteDiscountRate` clamp, and Tasks 12/13's discount/markup code) lives in a **new file** `internal/mtwire/tiering_billing.go` rather than growing the already-large `agent.go` further, mirroring how `grouphook.go`/`modelgroup.go` were split out for the 2D-ratio feature.

**Not in scope (flagged, not silently done):** surfacing `InviteDiscountRate` as a labeled field in the admin **frontend** settings form (`web/default/src/features/system-settings/...`) — `USDExchangeRate` IS wired into an explicit settings form there, confirming that UI is hand-wired per field, not auto-generated from `OptionMap`. This task only makes the rate admin-settable via the existing generic option API (curl/Postman/any authenticated admin client); adding a UI field is a natural follow-up, not requested by this task list.

**Files:**
- `setting/operation_setting/agent_tiering_setting.go` (NEW) — own file, does not touch `payment_setting_old.go`/`payment_setting.go` (lowest upstream-merge-conflict surface).
- `model/option.go` (`InitOptionMap` ~84; `UpdateOption` switch, case `"USDExchangeRate"` ~419-420) — 2 additive lines, same pattern as the existing `USDExchangeRate` case.
- `internal/mtwire/tiering_billing.go` (NEW).
- `internal/mtwire/tiering_billing_test.go` (NEW).

**Interfaces:**
- Produces: `operation_setting.InviteDiscountRate float64` (package var, default `0.9`), persisted/read via the existing native option pipeline.
- Produces: `effectiveInviteDiscountRate() float64` (mtwire-private) — reads the var and clamps to `(0,1]`; out-of-range (admin typo) falls back to `1` (= no discount), never to a value that could inflate a charge or credit money into thin air.

- [ ] **Step 1:** Write the failing test. Create `internal/mtwire/tiering_billing_test.go`:
```go
package mtwire

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// TestEffectiveInviteDiscountRate 覆盖邀请折扣率读取 + 安全夹取：合法区间 (0,1] 原样返回；
// 越界（≤0 或 >1，管理员误填防御）一律回退 1（=不打折）——宁可少打一次折，也绝不放大扣费或倒找钱。
func TestEffectiveInviteDiscountRate(t *testing.T) {
	restore := operation_setting.InviteDiscountRate
	defer func() { operation_setting.InviteDiscountRate = restore }()

	cases := []struct {
		set  float64
		want float64
	}{
		{0.9, 0.9},
		{1, 1},
		{0, 1},
		{-0.1, 1},
		{1.01, 1},
	}
	for _, c := range cases {
		operation_setting.InviteDiscountRate = c.set
		if got := effectiveInviteDiscountRate(); got != c.want {
			t.Errorf("set=%v: effectiveInviteDiscountRate() = %v, want %v", c.set, got, c.want)
		}
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (compile error: `operation_setting.InviteDiscountRate` / `effectiveInviteDiscountRate` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestEffectiveInviteDiscountRate
```

- [ ] **Step 3:** Add the setting var. Create `setting/operation_setting/agent_tiering_setting.go`:
```go
// 代理分层计费的全局设置（spec agent-tiering §9.1）：邀请 9 折折扣率。独立文件，不改既有
// payment_setting_old.go / payment_setting.go（降低与上游合并冲突面）。
package operation_setting

// InviteDiscountRate 是被 L0（普通档）代理邀请的用户，其消耗按此折扣率结算（默认 0.9 = 9 折）；
// 差额由平台毛利承担，绝不影响代理提成基数（基数恒为官方原价，见 internal/mtwire/tiering_billing.go）。
// 经 PUT /api/option/ {"key":"InviteDiscountRate","value":0.9} 管理员可改（沿用 USDExchangeRate 同款
// model/option.go InitOptionMap + UpdateOption 落盘/读回模式，见该文件 ~84/~419-420）。
// 读取侧一律经 mtwire.effectiveInviteDiscountRate() 夹取到 (0,1]；此处只存管理员配置的原始值。
var InviteDiscountRate = 0.9
```

- [ ] **Step 4:** Add the clamped getter. Create `internal/mtwire/tiering_billing.go`:
```go
package mtwire

// 代理分层计费（spec agent-tiering §9）：邀请 9 折退返 + L1 差价入账。集中在本文件（而非并入已很大的
// agent.go），供 creditConsumeCommission（agent.go）按 level 调用；两条通路共用 requestID 幂等心法，
// 但落的是两本不同的账（前者退用户 quota，后者入代理钱包）。

import (
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ============================================================================
// 邀请 9 折（L0，§9.1）
// ============================================================================

// effectiveInviteDiscountRate 读取邀请折扣率配置（operation_setting.InviteDiscountRate，经
// PUT /api/option/ 管理员可改，见 model/option.go）并夹取到安全区间 (0,1]；越界（管理员误填 ≤0
// 或 >1）一律回退 1（=不打折）——宁可少打一次折，也绝不放大扣费或倒找钱。
func effectiveInviteDiscountRate() float64 {
	r := operation_setting.InviteDiscountRate
	if r <= 0 || r > 1 {
		return 1
	}
	return r
}
```

- [ ] **Step 5:** Wire the native option pipeline so the rate is admin-settable + persisted. In `model/option.go`:
  - `InitOptionMap()` — add immediately after the `USDExchangeRate` line (~84):
```go
	common.OptionMap["USDExchangeRate"] = strconv.FormatFloat(operation_setting.USDExchangeRate, 'f', -1, 64)
	common.OptionMap["InviteDiscountRate"] = strconv.FormatFloat(operation_setting.InviteDiscountRate, 'f', -1, 64)
```
  - `UpdateOption()` switch — add immediately after `case "USDExchangeRate":` (~419-420):
```go
	case "USDExchangeRate":
		operation_setting.USDExchangeRate, _ = strconv.ParseFloat(value, 64)
	case "InviteDiscountRate":
		operation_setting.InviteDiscountRate, _ = strconv.ParseFloat(value, 64)
```

- [ ] **Step 6:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestEffectiveInviteDiscountRate
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/...
```

- [ ] **Step 7:** Commit.
```
cd /Users/cc/newapi628 && git add setting/operation_setting/agent_tiering_setting.go model/option.go internal/mtwire/tiering_billing.go internal/mtwire/tiering_billing_test.go && git commit -m "feat(billing): add admin-settable invite_discount_rate (default 0.9)"
```

- [ ] **Step 8:** SERVER verification (after deploy, against `newapi_test` 3100 — this is the one piece of Task 11 a Go unit test can't cover, the native option round-trip through a live DB):
```
curl -s -b admin.cookies -X PUT http://127.0.0.1:3100/api/option/ -H 'Content-Type: application/json' -d '{"key":"InviteDiscountRate","value":0.85}'
curl -s -b admin.cookies http://127.0.0.1:3100/api/option/ | grep InviteDiscountRate   # expect "0.85"
```

---

### Task 12: Apply the 9折 at consumption for invited users. Touches `internal/mtwire/wire.go` (SERIAL — see Step 5)

Implements §9.1's "该次消耗额 × inviteDiscount；差额平台承担". The consuming user's *charge* must actually shrink — not just a bookkeeping note — so this refunds `quotaUnits × (1 − rate)` back onto the user's own wallet quota via the native `model.IncreaseUserQuota`, **after** the (unchanged, official-price) deduction already happened. This is deliberately a post-hoc credit rather than a pre-deduction price cut: the billing-hot-path pricing ratio (`resolveModelGroup2D` / `HandleGroupRatio`, `internal/mtwire/grouphook.go` + `modelgroup.go`) is **not touched** — for an L0 tenant no group-ratio override is possible in the first place (Task 15 gates that to level≥1), so the `quotaUnits` `creditConsumeCommission` already receives for an L0 user *is* the official-price amount; no ratio math is needed to find "official price" for this task. The commission math right below it is therefore **completely unchanged** — same `quotaUnits`, same formula — which is what keeps the commission base pinned to official price per the 红线.

**Idempotency:** a `requestID`-unique table `mt_invite_discount_grants` (new; `AutoMigrate`, no `information_schema` guard needed since it's a brand-new table, not an ALTER of an existing one) — same insert-then-side-effect shape as `agent_earning_logs`' `idem_key` (`internal/agent/gormrepo/gormrepo.go` `AppendEarning`, ~164-216): unique-index insert with `ON CONFLICT DO NOTHING`; only if the insert actually happened (`RowsAffected>0`) does the function call `model.IncreaseUserQuota`. A dedicated table (not reuse of `agent_earning_logs`) is required because this credits a **user's** quota, not a **tenant's** ¥ wallet — routing it through `AgentRepo.AppendEarning` would incorrectly credit the referring agent's withdrawable balance for money the agent never earned.

**Scope boundary (flagged):** the refund only applies when `billingSource != "subscription"`. Subscription/tokenplan consumption is metered against `PostConsumeUserSubscriptionDelta`'s own monthly-allowance bucket (`model/user_subscription.go`), not `users.quota` — crediting `users.quota` for subscription-billed usage would land the refund in the wrong pool entirely, and refunding into the *subscription* bucket would need a `subscriptionID` the current hook signature doesn't carry. L0 commission (`consume_commission`/`tokenplan_commission`) is unaffected by this boundary — it already fires for both buckets today and continues to.

**Files:**
- `internal/mtwire/tiering_billing.go` (append: `mtDiscountGrantRow`, `migrateInviteDiscountGrants`, `grantInviteDiscount`).
- `internal/mtwire/tiering_billing_test.go` (append).
- `internal/mtwire/agent.go` (`creditConsumeCommission` ~172-212 — restructure the early-return so the discount fires independently of `CommissionRatio`).
- `internal/mtwire/wire.go` (`Migrate()` ~254-316 — wire `migrateInviteDiscountGrants` into the chain; **SERIAL**, see Step 5).

**Interfaces:**
- Produces: `migrateInviteDiscountGrants(db *gorm.DB) error`.
- Produces: `(a *App) grantInviteDiscount(ctx context.Context, userID, quotaUnits int64, requestID string)` — best-effort, `requestID`-idempotent.
- Consumes: `effectiveInviteDiscountRate()` (Task 11); native `model.IncreaseUserQuota(id, quota int, db bool) error` (`model/user.go:914`).

- [ ] **Step 1:** Write the failing tests. Append to `internal/mtwire/tiering_billing_test.go` (add these imports to the file's import block: `context`, `math`, `gorm.io/gorm`, `github.com/glebarez/sqlite`, `github.com/QuantumNous/new-api/common`, `github.com/QuantumNous/new-api/internal/agent`, `github.com/QuantumNous/new-api/model`):
```go
// newCreditCommissionTestApp 装配可跑 creditConsumeCommission 的最小 App：sqlite(:memory:) +
// 原生 users(id,tenant_id,quota) + agent 四表（AgentRepo/AgentEarnings）+ model.DB 全局切换
// （grantInviteDiscount 经 model.IncreaseUserQuota 写原生 users，读的是包级 model.DB —— 同
// subscription_bridge_test.go 的 model.DB 切换套路，见该文件 ~205-207）。
func newCreditCommissionTestApp(t *testing.T) *App {
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
	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, quota INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	if err := migrateInviteDiscountGrants(db); err != nil {
		t.Fatalf("discount grant migrate: %v", err)
	}
	prevDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = prevDB })
	ar := agentrepo.New(db)
	return &App{DB: db, AgentRepo: ar, AgentEarnings: agent.NewEarningSink(ar)}
}

func seedCommissionUser(t *testing.T, app *App, userID, tenantID, quota int64) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO users (id, tenant_id, quota) VALUES (?, ?, ?)`, userID, tenantID, quota).Error; err != nil {
		t.Fatalf("seed user %d: %v", userID, err)
	}
}

// TestGrantInviteDiscount_CreditsQuotaIdempotently 验证 9 折退返：quota = charged×(1-rate)，
// 记入 mt_invite_discount_grants 幂等台账；同 requestID 重复调用不重复退款；rate=1（无折扣）不退款。
func TestGrantInviteDiscount_CreditsQuotaIdempotently(t *testing.T) {
	ctx := context.Background()
	app := newCreditCommissionTestApp(t)
	seedCommissionUser(t, app, 100, 5, 1000)

	restore := operation_setting.InviteDiscountRate
	defer func() { operation_setting.InviteDiscountRate = restore }()
	operation_setting.InviteDiscountRate = 0.9 // 9折：退 10%

	app.grantInviteDiscount(ctx, 100, 1000, "req-disc-1") // 1000×(1-0.9)=100 退返
	var quota int64
	if err := app.DB.Raw(`SELECT quota FROM users WHERE id = 100`).Scan(&quota).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	if quota != 1100 {
		t.Fatalf("quota = %d, want 1100 (1000 + 100 refund)", quota)
	}

	// 幂等：同 requestID 重复调用不重复退款。
	app.grantInviteDiscount(ctx, 100, 1000, "req-disc-1")
	if err := app.DB.Raw(`SELECT quota FROM users WHERE id = 100`).Scan(&quota).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	if quota != 1100 {
		t.Fatalf("idempotency broken: quota = %d, want 1100", quota)
	}

	// rate=1（无折扣）：refund 非正，不落库、不退款。
	operation_setting.InviteDiscountRate = 1
	app.grantInviteDiscount(ctx, 100, 1000, "req-disc-2")
	if err := app.DB.Raw(`SELECT quota FROM users WHERE id = 100`).Scan(&quota).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	if quota != 1100 {
		t.Fatalf("rate=1 must not refund, quota = %d, want 1100", quota)
	}
}

// TestCreditConsumeCommission_L0AppliesDiscountAndCommission 是本任务的核心集成用例：L0（level=0）
// 一次消耗 → 9 折退返(quota) + 官方原价提成(consume_commission) 同时生效、互不影响，都按 requestID 幂等。
func TestCreditConsumeCommission_L0AppliesDiscountAndCommission(t *testing.T) {
	ctx := context.Background()
	app := newCreditCommissionTestApp(t)
	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	seedCommissionUser(t, app, 100, 5, 1000)
	restore := operation_setting.InviteDiscountRate
	defer func() { operation_setting.InviteDiscountRate = restore }()
	operation_setting.InviteDiscountRate = 0.9

	quota := int64(2 * common.QuotaPerUnit) // 消耗 = $2
	app.creditConsumeCommission(100, quota, "req-l0-1", "wallet")

	// 9 折退返：0.1 × quota。
	var gotQuota int64
	if err := app.DB.Raw(`SELECT quota FROM users WHERE id = 100`).Scan(&gotQuota).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	wantRefund := int64(float64(quota) * 0.1)
	if gotQuota != 1000+wantRefund {
		t.Fatalf("quota after discount = %d, want %d", gotQuota, 1000+wantRefund)
	}
	// 提成：commission_ratio(0.2) × 官方原价（quota，未被折扣影响——基数不变，红线要求）。
	w, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	wantCNY := consumeCommissionCNY(quota, 0.2, operation_setting.USDExchangeRate)
	if math.Abs(w.WithdrawableBalance-wantCNY) > 1e-9 {
		t.Fatalf("commission = %v, want %v (must use pre-discount quota as base)", w.WithdrawableBalance, wantCNY)
	}

	// 幂等：同 requestID 重放，quota 与提成都不应二次变动。
	app.creditConsumeCommission(100, quota, "req-l0-1", "wallet")
	var gotQuota2 int64
	if err := app.DB.Raw(`SELECT quota FROM users WHERE id = 100`).Scan(&gotQuota2).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	if gotQuota2 != gotQuota {
		t.Fatalf("discount idempotency broken on replay: quota = %d, want %d", gotQuota2, gotQuota)
	}
	w2, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w2.WithdrawableBalance != w.WithdrawableBalance {
		t.Fatalf("commission idempotency broken on replay: %v, want %v", w2.WithdrawableBalance, w.WithdrawableBalance)
	}
}

// TestCreditConsumeCommission_L0DiscountSkipsSubscriptionBucket 锁定本任务的范围边界：
// billingSource="subscription" 不退 quota（订阅额度池是另一本账），但提成（tokenplan_commission）
// 照常生效——两者相互独立，跳过折扣不影响提成。
func TestCreditConsumeCommission_L0DiscountSkipsSubscriptionBucket(t *testing.T) {
	ctx := context.Background()
	app := newCreditCommissionTestApp(t)
	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	seedCommissionUser(t, app, 100, 5, 1000)
	restore := operation_setting.InviteDiscountRate
	defer func() { operation_setting.InviteDiscountRate = restore }()
	operation_setting.InviteDiscountRate = 0.9

	quota := int64(2 * common.QuotaPerUnit)
	app.creditConsumeCommission(100, quota, "req-l0-sub-1", "subscription")

	var gotQuota int64
	if err := app.DB.Raw(`SELECT quota FROM users WHERE id = 100`).Scan(&gotQuota).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	if gotQuota != 1000 {
		t.Fatalf("subscription bucket must not refund wallet quota, got %d, want unchanged 1000", gotQuota)
	}
	var count int64
	app.DB.Table("agent_earning_logs").
		Where("tenant_id = ? AND source_id = ? AND source_type = ?", 5, "req-l0-sub-1", "tokenplan_commission").
		Count(&count)
	if count != 1 {
		t.Fatalf("tokenplan_commission must still fire for subscription bucket, got %d rows", count)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (compile: `agentrepo`/`operation_setting`/`model` unresolved in the test file, `migrateInviteDiscountGrants`/`grantInviteDiscount` undefined).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'GrantInviteDiscount|CreditConsumeCommission'
```

- [ ] **Step 3:** Finish the test file's import block (add the alias used by `newCreditCommissionTestApp`). At the top of `internal/mtwire/tiering_billing_test.go`:
```go
package mtwire

import (
	"context"
	"math"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)
```

- [ ] **Step 4:** Implement `mtDiscountGrantRow` + `migrateInviteDiscountGrants` + `grantInviteDiscount`. Update `internal/mtwire/tiering_billing.go`'s import block and append below `effectiveInviteDiscountRate`:
```go
package mtwire

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)
```
```go
// mtDiscountGrantRow 是邀请 9 折退返台账（mt_invite_discount_grants，request_id 唯一）：
// 防止同一次消耗（同 requestID）被重复退款。与 agent_earning_logs 同一幂等模式——唯一索引 +
// ON CONFLICT DO NOTHING，仅首次插入成功才真正退款。
type mtDiscountGrantRow struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64     `gorm:"column:user_id;not null;index:idx_mt_discount_grants_user"`
	RequestID string    `gorm:"column:request_id;type:varchar(128);not null;uniqueIndex:idx_mt_discount_grants_request"`
	Quota     int64     `gorm:"column:quota;not null"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (mtDiscountGrantRow) TableName() string { return "mt_invite_discount_grants" }

// migrateInviteDiscountGrants 建 mt_invite_discount_grants 表。新表、GORM AutoMigrate 幂等，
// 不改任何既有表，故无需 migrateAgentProfilesDropType 那种 information_schema 守卫。
func migrateInviteDiscountGrants(db *gorm.DB) error {
	return db.AutoMigrate(&mtDiscountGrantRow{})
}

// grantInviteDiscount 幂等地把邀请 9 折差额退回被邀用户的 quota：refund = quotaUnits × (1 − rate)。
// requestID 幂等（先占 mt_invite_discount_grants 唯一行，仅首次插入成功才真正退款，重复调用直接跳过）。
// best-effort：调用方（creditConsumeCommission）已有 panic 兜底；本函数自身失败仅记日志，绝不上抛。
//
// 范围限定：仅钱包桶——订阅桶（tokenplan）的消耗计在用户订阅额度池（PostConsumeUserSubscriptionDelta），
// 不是 users.quota，退到钱包 quota 会记错池子；订阅桶邀请折扣需要新的"订阅额度退返"通路（需另加
// subscriptionID 等参数），本轮不做，调用方按 billingSource 门禁（见 creditConsumeCommission）。
//
// 已知的窗口（沿用 IncreaseUserQuota 既有风险，未新增）：占位行插入成功后，若进程在
// model.IncreaseUserQuota 真正落库前崩溃，用户会"占了坑但没退到钱"——与 distribution.go 兑换码
// 路径的"CAS 已翻但入账失败"是同一类已接受的边界情况，不引入新的资金不守恒风险。
func (a *App) grantInviteDiscount(ctx context.Context, userID, quotaUnits int64, requestID string) {
	if userID <= 0 || quotaUnits <= 0 || requestID == "" {
		return
	}
	rate := effectiveInviteDiscountRate()
	refund := int64(float64(quotaUnits) * (1 - rate))
	if refund <= 0 {
		return
	}
	res := a.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&mtDiscountGrantRow{
		UserID: userID, RequestID: requestID, Quota: refund, CreatedAt: time.Now(),
	})
	if res.Error != nil {
		common.SysError("mtwire: record invite discount grant failed: " + res.Error.Error())
		return
	}
	if res.RowsAffected == 0 {
		return // 幂等：同 requestID 已退款，不重复退
	}
	if err := model.IncreaseUserQuota(int(userID), int(refund), false); err != nil {
		common.SysError("mtwire: grant invite discount quota failed: " + err.Error())
	}
}
```

- [ ] **Step 5:** Wire the new migration into `Migrate()`. **SERIAL — `internal/mtwire/wire.go` is a shared/live file** (already mid-edit by the parallel payment-reconciliation work per `git status`; the tail below is the *current* real chain, which already differs from what Task 2 of this plan describes, because reconcile work landed after Task 2 was written). **Before editing, re-read the current tail of `Migrate()` and re-locate `return migrateReconcileHeartbeat(a.DB)` — do not blindly trust the line number.** As of this writing the tail is:
```go
	// 对账记录 + 心跳（reconcile-history）：历史列表 reconcile_runs + 单行心跳 reconcile_heartbeat。
	if err := migrateReconcileRuns(a.DB); err != nil {
		return err
	}
	return migrateReconcileHeartbeat(a.DB)
}
```
  Change the last line to an `if err :=` block and append the new call:
```go
	// 对账记录 + 心跳（reconcile-history）：历史列表 reconcile_runs + 单行心跳 reconcile_heartbeat。
	if err := migrateReconcileRuns(a.DB); err != nil {
		return err
	}
	if err := migrateReconcileHeartbeat(a.DB); err != nil {
		return err
	}
	// 代理分层计费：邀请 9 折退返幂等台账 mt_invite_discount_grants（request_id 唯一，防重复退款）。
	return migrateInviteDiscountGrants(a.DB)
}
```

- [ ] **Step 6:** Restructure `creditConsumeCommission` so the discount fires independent of `CommissionRatio`. In `internal/mtwire/agent.go`, replace the body (~175-212):
```go
func (a *App) creditConsumeCommission(userID int64, quotaUnits int64, requestID, billingSource string) {
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
	// L0（普通档）邀请 9 折：与提成是否 >0 无关——只要被 L0 邀请就退（§9.1）。仅钱包桶（订阅桶另有
	// 额度池，见 grantInviteDiscount 注释）。
	if params.Level == 0 && billingSource != "subscription" {
		a.grantInviteDiscount(ctx, userID, quotaUnits, requestID)
	}
	if params.CommissionRatio <= 0 {
		return // 分润比例为 0：无提成（9 折已在上面独立处理，不受影响）
	}
	cny := consumeCommissionCNY(quotaUnits, params.CommissionRatio, operation_setting.USDExchangeRate)
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
  (This is a pure control-flow split of the existing `err != nil || !found || params.CommissionRatio <= 0` early-return into two checks, with the discount call inserted between them — the commission math itself is untouched.)

- [ ] **Step 7:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'GrantInviteDiscount|CreditConsumeCommission'
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/...
```

- [ ] **Step 8:** Commit.
```
cd /Users/cc/newapi628 && git add internal/mtwire/tiering_billing.go internal/mtwire/tiering_billing_test.go internal/mtwire/agent.go internal/mtwire/wire.go && git commit -m "feat(billing): apply invite 9折 refund to L0-attributed users at consumption"
```

---

### Task 13: L1 markup-delta crediting. Touches the shared billing hook `agenthook.ConsumeCommission` (SERIAL — see Step 5)

Implements §9.2's "差价入账 = (L1倍率 − 官方倍率) × 官方原价消耗额 → L1 钱包". The hard part: **the current `agenthook.ConsumeCommission` hook signature does not carry which model group was billed**, and the markup depends on it (an L1 tenant's override is per model-group — `tenant_groups[tenant_id, group]`, `internal/mtwire/grouphook.go` `resolveTenantGroupRatio`). Confirmed by reading the only two call sites: `service/quota.go:456` and `service/text_quota.go:484` both pass `(userID, quota, requestID, billingSource)` — no group. `relaycommon.RelayInfo` already carries the value we need, `UsingGroup string` (`relay/common/relay_info.go:93`), which is the exact same value `relay/helper/price.go:62`'s `HandleGroupRatio` already passes to `grouphook.ModelGroup2DResolver(userID, userGroup, usingGroup)` when the charge was originally computed — so this task threads it through as a 5th hook parameter rather than duplicating price-computation logic or re-deriving it from something else.

**Why a hook-signature change instead of avoiding it:** without `usingGroup`, there is no way to know which model-group ratio (if any) applied to *this* consumption event, hence no way to compute the delta — silently guessing (e.g. "assume no override") would make L1 markup crediting either always-zero or wrong whenever an agent actually has an override configured, which defeats the feature. The extension is purely additive (one more `string` parameter) and mechanical across exactly 4 files.

**Markup math** (`ratioMarkupQuotaUnits`): since the amount already charged is `chargedQuota = base × tier × tenantFactor` and the official amount would have been `officialQuota = base × tier × baselineFactor`, dividing out gives `officialQuota = chargedQuota × baselineFactor / tenantFactor` without needing `base` or `tier` at all — `markupQuota = chargedQuota − officialQuota`. `baselineFactor`/`tenantFactor` are obtained by literally reusing the existing `modelGroupBaseline(group)` (`distribution.go:507`) and `resolveTenantGroupRatio(ctx, userID, group)` (`grouphook.go:27`) — the exact two lookups `resolveModelGroup2D` already does when the charge is computed, so the markup math mirrors the real pricing path instead of approximating it.

**Files:**
- `internal/platform/agenthook/agenthook.go` (`ConsumeCommission` var ~17-21).
- `service/quota.go` (call site ~456).
- `service/text_quota.go` (call site ~484).
- `internal/mtwire/agent.go` (`creditConsumeCommission` signature line only ~175).
- `internal/agent/model.go` (`EarningSource` consts ~70-92 — add `SourceRatioMarkup`).
- `internal/agent/model_test.go` (`TestEarningSource_Valid` ~51-69 — extend).
- `internal/mtwire/tiering_billing.go` (append: `resolveGroupFactors`, `ratioMarkupQuotaUnits`, `creditRatioMarkup`).
- `internal/mtwire/tiering_billing_test.go` (append).

**Interfaces:**
- Changes: `agenthook.ConsumeCommission func(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string)` (was 4 args; `usingGroup` appended).
- Produces: `agent.SourceRatioMarkup EarningSource = "ratio_markup"`.
- Produces: `(a *App) resolveGroupFactors(ctx, userID int64, usingGroup string) (baseline, tenantRatio float64)`.
- Produces: `ratioMarkupQuotaUnits(chargedQuota int64, baselineFactor, tenantFactor float64) int64` (pure).
- Produces: `(a *App) creditRatioMarkup(ctx, tenantID, userID, quotaUnits int64, usingGroup, requestID, billingSource string)` — best-effort, `requestID`-idempotent, credits the L1 tenant's own wallet.

- [ ] **Step 1:** Write the failing tests. First extend `internal/agent/model_test.go`'s `TestEarningSource_Valid` cases slice (~56-63), adding a case:
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
  Then append to `internal/mtwire/tiering_billing_test.go` (add `modelgroup` and `tenantrepo` to its import block: `"github.com/QuantumNous/new-api/internal/modelgroup"`, `tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"`):
```go
// newRatioMarkupTestApp 装配最小 App：sqlite(:memory:) + model_groups + tenants/tenant_domains/
// tenant_groups + agent 四表（AgentRepo/AgentEarnings）+ 原生 users(id,tenant_id)。供「L1 差价入账」用例。
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

// TestRatioMarkupQuotaUnits 覆盖差价 quota 计算：markup = charged − charged×baseline/tenant；
// 无覆盖(tenant==baseline)、覆盖率≤基准(配置漂移防御)、非法参数 → 0。
func TestRatioMarkupQuotaUnits(t *testing.T) {
	cases := []struct {
		name             string
		charged          int64
		baseline, tenant float64
		want             int64
	}{
		{"20% markup", 1200, 1.0, 1.2, 200}, // charged=1200(=1000@1.2)，官方价=1000，差价=200
		{"no override (equal)", 1000, 1.0, 1.0, 0},
		{"tenant below baseline (drift guard)", 800, 1.0, 0.8, 0},
		{"zero charged", 0, 1.0, 1.2, 0},
		{"non-positive baseline", 1200, 0, 1.2, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ratioMarkupQuotaUnits(c.charged, c.baseline, c.tenant); got != c.want {
				t.Fatalf("ratioMarkupQuotaUnits(%d,%v,%v) = %d, want %d", c.charged, c.baseline, c.tenant, got, c.want)
			}
		})
	}
}

// TestResolveGroupFactors 覆盖：命中模型分组覆盖 → (基准,覆盖)；未覆盖 → (基准,基准)；
// 非模型分组/未登记（如层级名 "default"）→ (1,1)（与 resolveModelGroup2D 的 modelFactor 分支同口径）。
func TestResolveGroupFactors(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register model group: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // 复用 modelgroup_test.go 的桩：claude-kiro=0.3

	seedUser(t, app, 100, 5) // 复用 grouphook_test.go 的 seedUser；用户100 → 租户5
	if err := app.TenantRepo.UpsertGroup(ctx, 5, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	if b, tr := app.resolveGroupFactors(ctx, 100, "claude-kiro"); b != 0.3 || tr != 0.45 {
		t.Fatalf("with override = (%v,%v), want (0.3,0.45)", b, tr)
	}
	seedUser(t, app, 200, 9) // 租户9：无覆盖
	if b, tr := app.resolveGroupFactors(ctx, 200, "claude-kiro"); b != 0.3 || tr != 0.3 {
		t.Fatalf("no override = (%v,%v), want (0.3,0.3)", b, tr)
	}
	if b, tr := app.resolveGroupFactors(ctx, 100, "default"); b != 1 || tr != 1 {
		t.Fatalf("non-model-group = (%v,%v), want (1,1)", b, tr)
	}
}

// TestCreditRatioMarkup_CreditsL1WalletIdempotently 验证差价入账：命中覆盖→按公式入 L1 钱包
// (source=ratio_markup)；同 requestID 重复调用不重复入账（幂等）；无覆盖→不入账。
func TestCreditRatioMarkup_CreditsL1WalletIdempotently(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register model group: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios

	seedUser(t, app, 100, 5)
	if err := app.TenantRepo.UpsertGroup(ctx, 5, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	// charged=1200 quota，baseline=0.3，tenant=0.45 → official=800，markup=400 quota。
	charged := int64(1200)
	app.creditRatioMarkup(ctx, 5, 100, charged, "claude-kiro", "req-md-1", "wallet")

	w, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	wantCNY := consumeCommissionCNY(400, 1, operation_setting.USDExchangeRate)
	if math.Abs(w.WithdrawableBalance-wantCNY) > 1e-9 {
		t.Fatalf("withdrawable = %v, want %v", w.WithdrawableBalance, wantCNY)
	}

	// 幂等：同 requestID 重复调用不重复入账。
	app.creditRatioMarkup(ctx, 5, 100, charged, "claude-kiro", "req-md-1", "wallet")
	w2, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w2.WithdrawableBalance != w.WithdrawableBalance {
		t.Fatalf("idempotency broken: withdrawable = %v, want %v", w2.WithdrawableBalance, w.WithdrawableBalance)
	}

	// 无覆盖（租户9未设覆盖）：不入账。
	seedUser(t, app, 200, 9)
	app.creditRatioMarkup(ctx, 9, 200, charged, "claude-kiro", "req-md-2", "wallet")
	w9, err := app.AgentRepo.GetWallet(ctx, 9)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w9.WithdrawableBalance != 0 {
		t.Fatalf("no-override tenant must not earn markup, got %v", w9.WithdrawableBalance)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (compile: `SourceRatioMarkup`/`resolveGroupFactors`/`ratioMarkupQuotaUnits`/`creditRatioMarkup` undefined; `modelgroup`/`tenantrepo` unresolved in the test file).
```
cd /Users/cc/newapi628 && go test ./internal/agent/... ./internal/mtwire/ -run 'EarningSource|RatioMarkup|ResolveGroupFactors'
```

- [ ] **Step 3:** Add the new earning source. In `internal/agent/model.go`, add after `SourceTokenplanCommission` (~78):
```go
	// SourceTokenplanCommission 套餐内消耗分润。
	SourceTokenplanCommission EarningSource = "tokenplan_commission"
	// SourceRatioMarkup 差价入账（L1/独立档：本租户模型分组倍率高于官方基准倍率的部分，
	// 按官方原价消耗额折算；§9.2）。
	SourceRatioMarkup EarningSource = "ratio_markup"
	// SourceManualAdjustment 人工调整（管理员修正，金额可正可负）。
	SourceManualAdjustment EarningSource = "manual_adjustment"
```
  And extend `Valid()` (~84-92):
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

- [ ] **Step 4:** Implement `resolveGroupFactors` + `ratioMarkupQuotaUnits` + `creditRatioMarkup`. Update `internal/mtwire/tiering_billing.go`'s import block (add `"github.com/QuantumNous/new-api/internal/agent"`):
```go
import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)
```
  Append below `grantInviteDiscount`:
```go
// ============================================================================
// L1 差价入账（独立档，§9.2）
// ============================================================================

// resolveGroupFactors 返回 usingGroup 的（平台基准, 本租户实际生效）模型分组倍率系数，与
// resolveModelGroup2D 的 modelFactor 分支同口径（modelgroup.go ~93-101）：非模型分组（未登记/
// 已禁用）恒 (1,1)（不产生差价）；已登记则基准=modelGroupBaseline，命中 tenant_groups 覆盖则
// tenantRatio=覆盖值，否则 tenantRatio=基准（无覆盖，差价天然为 0）。
func (a *App) resolveGroupFactors(ctx context.Context, userID int64, usingGroup string) (baseline, tenantRatio float64) {
	if a.ModelGroupRepo == nil || usingGroup == "" || !a.ModelGroupRepo.IsModelGroup(usingGroup) {
		return 1, 1
	}
	baseline = modelGroupBaseline(usingGroup)
	tenantRatio = baseline
	if override, hit := a.resolveTenantGroupRatio(ctx, userID, usingGroup); hit {
		tenantRatio = override
	}
	return baseline, tenantRatio
}

// ratioMarkupQuotaUnits 用「实际扣费额」反推「官方基准扣费额」再作差，得到代理加价差额（quota 单位）：
//
//	officialQuota = chargedQuota × baselineFactor / tenantFactor
//	markupQuota   = chargedQuota − officialQuota
//
// tenantFactor ≤ baselineFactor（无加价 / 配置漂移）或任一参数非正 → 0（绝不倒扣代理、绝不误判负收益）。
func ratioMarkupQuotaUnits(chargedQuota int64, baselineFactor, tenantFactor float64) int64 {
	if chargedQuota <= 0 || baselineFactor <= 0 || tenantFactor <= baselineFactor {
		return 0
	}
	officialQuota := float64(chargedQuota) * baselineFactor / tenantFactor
	return int64(float64(chargedQuota) - officialQuota)
}

// creditRatioMarkup 是 L1（level≥1）档「差价入账」实现：markup = 实际扣费额 − 官方基准扣费额，
// 按 requestID 幂等入账到 L1 自己的钱包（source=ratio_markup）。best-effort：失败不阻断扣费/调用方
// （由 creditConsumeCommission 的 panic 兜底覆盖）。与 L0 提成互斥——调用方按 level 二选一，绝不
// 同时调用两者（Task 14）。
func (a *App) creditRatioMarkup(ctx context.Context, tenantID, userID, quotaUnits int64, usingGroup, requestID, billingSource string) {
	if tenantID <= 0 || quotaUnits <= 0 || requestID == "" {
		return
	}
	baseline, tenantRatio := a.resolveGroupFactors(ctx, userID, usingGroup)
	markupQuota := ratioMarkupQuotaUnits(quotaUnits, baseline, tenantRatio)
	if markupQuota <= 0 {
		return // 无覆盖 / 覆盖未高于基准：无差价，不入账
	}
	// markup 已是「扣费额」口径（quota 单位），直接按 ratio=1 换算 CNY（不再乘任何分润比例）。
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

- [ ] **Step 5:** Extend the shared billing hook. **SERIAL — this changes a call signature shared by two `service/` call sites; both must be updated in the same commit or the tree won't compile.** In `internal/platform/agenthook/agenthook.go` (~17-21):
```go
// ConsumeCommission 在一次成功的 PostConsume 之后被调用，按所属代理档位二选一计佣入账
// （level==0 → 提成 consume_commission；level≥1 → 差价 ratio_markup；见 mtwire.creditConsumeCommission）。
// 参数：userID=消费用户；quotaUnits=本次消费的 new-api 内部额度单位（$1=common.QuotaPerUnit，可正可负，
// 实现侧只对正向消费计佣）；requestID=幂等键来源；billingSource="wallet"|"subscription"（区分钱包桶/
// 套餐桶）；usingGroup=本次计费实际使用的分组（relayInfo.UsingGroup，供 L1 差价入账反推官方基准倍率）。
// nil = 未装配。实现必须自身幂等且 best-effort（失败仅记日志，不返回错误）。
var ConsumeCommission func(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string)
```
  In `service/quota.go` (~456):
```go
	if relayInfo != nil && agenthook.ConsumeCommission != nil {
		if total := int64(quota) + int64(preConsumedQuota); total > 0 {
			agenthook.ConsumeCommission(int64(relayInfo.UserId), total, relayInfo.RequestId, relayInfo.BillingSource, relayInfo.UsingGroup)
		}
	}
```
  In `service/text_quota.go` (~484):
```go
	if agenthook.ConsumeCommission != nil && summary.Quota > 0 {
		agenthook.ConsumeCommission(int64(relayInfo.UserId), int64(summary.Quota), relayInfo.RequestId, relayInfo.BillingSource, relayInfo.UsingGroup)
	}
```
  In `internal/mtwire/agent.go`, change only the `creditConsumeCommission` signature line (~175) — body unchanged, `usingGroup` is accepted but not yet consumed (Task 14 wires it in):
```go
func (a *App) creditConsumeCommission(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string) {
```
  `agenthook.ConsumeCommission = a.creditConsumeCommission` (`agent.go` ~93, inside `InstallHooks`) needs **no textual change** — it's a method-value assignment that now simply refers to the 5-arg method.

- [ ] **Step 6:** Run — expect PASS.
```
cd /Users/cc/newapi628 && go test ./internal/agent/... ./internal/mtwire/ -run 'EarningSource|RatioMarkup|ResolveGroupFactors'
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/... ./service/...
```

- [ ] **Step 7:** Verify zero stale 4-arg call sites remain.
```
cd /Users/cc/newapi628 && grep -rn "ConsumeCommission(int64(relayInfo" service/ | grep -v UsingGroup || echo "clean"
```
  Expect `clean`.

- [ ] **Step 8:** Commit.
```
cd /Users/cc/newapi628 && git add internal/agent/model.go internal/agent/model_test.go internal/platform/agenthook/agenthook.go service/quota.go service/text_quota.go internal/mtwire/agent.go internal/mtwire/tiering_billing.go internal/mtwire/tiering_billing_test.go && git commit -m "feat(billing): thread usingGroup through ConsumeCommission hook + add L1 ratio-markup crediting"
```

---

### Task 14: Tier-based earning switch in the commission hook (no-double-credit)

Wires Tasks 12+13 together: `creditConsumeCommission` becomes a thin dispatcher on `params.Level`, extracting the existing L0 body into `creditL0Commission` (symmetric with `creditRatioMarkup`) so the switch itself is a 5-line diff, not a re-derivation. This is the task that makes the mutual-exclusion in §9.3 ("计费入账二选一…不重复给") a tested invariant rather than an assumption.

**Operational note (flagged, not a code change here):** Task 2 of this plan backfills every *existing* agent's `agent_profiles.level` to `1` (independent) as part of deleting the `type` column. Combined with this task, that means: the moment Tasks 1-10 **and** 11-14 are both deployed, every pre-existing agent tenant instantly stops earning `consume_commission` and starts earning `ratio_markup` instead — which is `0` for any agent that hasn't configured a model-group override via `HandleAgentSetGroupRatio` (most won't have, since that's a newer opt-in feature). This is the *intended* behavior per spec §9.3 ("二选一"), not a bug, but it is a real, immediate earnings-drop for any already-live agent relying on `commission_ratio`. Confirm this is acceptable before deploying Part 2 to a server with real agents on it — no task in this plan changes that outcome, since it's what the spec explicitly asks for.

**Files:**
- `internal/mtwire/agent.go` (`creditConsumeCommission` ~175-212 — full restructure into a dispatcher + `creditL0Commission`).
- `internal/mtwire/agent_tiering_test.go` (NEW).

**Interfaces:**
- Changes: `creditConsumeCommission`'s body (signature unchanged from Task 13).
- Produces: `(a *App) creditL0Commission(ctx, tenantID, userID, quotaUnits int64, requestID, billingSource string, commissionRatio float64)` (extracted, same logic Task 12 wrote inline).

- [ ] **Step 1:** Write the failing tests. Create `internal/mtwire/agent_tiering_test.go`:
```go
package mtwire

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/modelgroup"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// newTieringHookTestApp 在 newCreditCommissionTestApp（tiering_billing_test.go，Task 12）基础上
// 加 model_groups + tenant_groups（L1 差价入账用例需要）。同 modelgroup_test.go
// newModelGroup2DTenantApp「基础 app 上加表」的套路。
func newTieringHookTestApp(t *testing.T) *App {
	t.Helper()
	app := newCreditCommissionTestApp(t)
	if err := modelgroup.AutoMigrate(app.DB); err != nil {
		t.Fatalf("modelgroup migrate: %v", err)
	}
	if err := tenantrepo.AutoMigrate(app.DB); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	app.ModelGroupRepo = modelgroup.New(app.DB)
	app.TenantRepo = tenantrepo.New(app.DB)
	return app
}

// TestCreditConsumeCommission_TierSwitch_NeverBothSources 是核心不变量测试（防双发）：同一次
// creditConsumeCommission 调用，L0 租户只产生 consume_commission、L1 租户只产生 ratio_markup，
// 两个 source 绝不同时出现在同一 (tenant_id, source_id) 下 —— 即便 L1 租户也配了 commission_ratio
// （刻意保留非零值：证明 L1 分支根本不读这个字段，而不只是恰好为 0）。
func TestCreditConsumeCommission_TierSwitch_NeverBothSources(t *testing.T) {
	ctx := context.Background()
	app := newTieringHookTestApp(t)
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
	seedCommissionUser(t, app, 100, 5, 1000)

	// L1 租户 9：也配了 commission_ratio=0.2（刻意），且设了 claude-kiro 覆盖倍率 0.45（基准 0.3）；用户 200。
	if err := app.AgentRepo.SetAgentType(ctx, 9, agent.AgentParams{Level: 1, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L1 agent: %v", err)
	}
	if err := app.TenantRepo.UpsertGroup(ctx, 9, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}
	seedCommissionUser(t, app, 200, 9, 1000)

	quota := int64(1200)
	app.creditConsumeCommission(100, quota, "req-tier-l0", "wallet", "claude-kiro")
	app.creditConsumeCommission(200, quota, "req-tier-l1", "wallet", "claude-kiro")

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

	// 9 折只作用于 L0 邀请的用户；L1 站用户按 L1 倍率付、无 9 折（§9.3）。
	var q100, q200 int64
	app.DB.Raw(`SELECT quota FROM users WHERE id = 100`).Scan(&q100)
	app.DB.Raw(`SELECT quota FROM users WHERE id = 200`).Scan(&q200)
	if q100 <= 1000 {
		t.Fatalf("L0 user must receive invite discount refund, quota = %d", q100)
	}
	if q200 != 1000 {
		t.Fatalf("L1 user must NOT receive invite discount refund, quota = %d, want unchanged 1000", q200)
	}
}

// TestCreditConsumeCommission_RetrySameRequestID_NoDoubleCredit 覆盖红线要求的「强幂等」：同一
// requestID 被重复调用（模拟钩子被意外重放）——L1 钱包余额与台账行数都不应二次变动。
func TestCreditConsumeCommission_RetrySameRequestID_NoDoubleCredit(t *testing.T) {
	ctx := context.Background()
	app := newTieringHookTestApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register model group: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios

	if err := app.AgentRepo.SetAgentType(ctx, 9, agent.AgentParams{Level: 1, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L1 agent: %v", err)
	}
	if err := app.TenantRepo.UpsertGroup(ctx, 9, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}
	seedCommissionUser(t, app, 200, 9, 1000)

	for i := 0; i < 3; i++ { // 重放 3 次，同 requestID。
		app.creditConsumeCommission(200, 1200, "req-retry-1", "wallet", "claude-kiro")
	}
	w, err := app.AgentRepo.GetWallet(ctx, 9)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	wantCNY := consumeCommissionCNY(400, 1, operation_setting.USDExchangeRate) // charged=1200,baseline=0.3,tenant=0.45 → markup=400
	if math.Abs(w.WithdrawableBalance-wantCNY) > 1e-9 {
		t.Fatalf("after 3x replay: withdrawable = %v, want %v (must credit exactly once)", w.WithdrawableBalance, wantCNY)
	}
	var count int64
	app.DB.Table("agent_earning_logs").Where("tenant_id = ? AND source_id = ?", 9, "req-retry-1").Count(&count)
	if count != 1 {
		t.Fatalf("earning rows for req-retry-1 = %d, want exactly 1", count)
	}
}

// TestCreditConsumeCommission_NoTenant_NoAttribution_Unaffected 锁定不回归：主站用户 / 未归属用户
// （tenant_id=0）— 无论重复调用多少次 — 既不产生任何 earning 行，也不 panic、不阻断。
func TestCreditConsumeCommission_NoTenant_NoAttribution_Unaffected(t *testing.T) {
	app := newTieringHookTestApp(t)
	seedCommissionUser(t, app, 300, 0, 1000) // tenant_id=0：主站用户

	app.creditConsumeCommission(300, 1200, "req-main-1", "wallet", "claude-kiro")

	var count int64
	app.DB.Table("agent_earning_logs").Count(&count)
	if count != 0 {
		t.Fatalf("main-site user must never produce an earning row, got %d", count)
	}
	var q int64
	app.DB.Raw(`SELECT quota FROM users WHERE id = 300`).Scan(&q)
	if q != 1000 {
		t.Fatalf("main-site user quota must be untouched, got %d", q)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (the tier switch doesn't exist yet: `creditConsumeCommission` still unconditionally uses the L0/`consume_commission` path, so `TestCreditConsumeCommission_TierSwitch_NeverBothSources`'s L1 assertions fail — tenant 9 gets `consume_commission` instead of `ratio_markup`).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'TierSwitch|NoDoubleCredit|NoTenant_NoAttribution'
```

- [ ] **Step 3:** Restructure `creditConsumeCommission` into a dispatcher + extract `creditL0Commission`. In `internal/mtwire/agent.go`, replace the body (~175-212, the Task 12 version):
```go
// creditConsumeCommission 是 agenthook.ConsumeCommission 实现：userId→users.tenant_id→agent
// level→按档二选一计佣入账（§9.3）。幂等键 = requestID。best-effort：失败不阻断扣费。
func (a *App) creditConsumeCommission(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string) {
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
	// 按档二选一（spec §9.3）：level==0 → L0 提成通路（9 折 + consume_commission）；
	// level≥1 → L1 差价通路（ratio_markup）。NEVER both —— 两条路径在此分支互斥，绝不重叠调用。
	if params.Level == 0 {
		a.creditL0Commission(ctx, tenantID, userID, quotaUnits, requestID, billingSource, params.CommissionRatio)
		return
	}
	a.creditRatioMarkup(ctx, tenantID, userID, quotaUnits, usingGroup, requestID, billingSource)
}

// creditL0Commission 是 L0（普通档）计费通路：邀请 9 折退返（钱包桶，与提成是否 >0 无关）+
// 官方原价提成（commission_ratio × quotaUnits，基数不受折扣影响，红线要求）。从
// creditConsumeCommission 抽出以保持按档分支清晰；panic 由调用方的 defer 统一兜底。
func (a *App) creditL0Commission(ctx context.Context, tenantID, userID, quotaUnits int64, requestID, billingSource string, commissionRatio float64) {
	// 邀请 9 折：仅钱包桶（订阅桶另有额度池，见 grantInviteDiscount 注释）。
	if billingSource != "subscription" {
		a.grantInviteDiscount(ctx, userID, quotaUnits, requestID)
	}
	if commissionRatio <= 0 {
		return // 分润比例为 0：无提成（9 折已在上面独立处理，不受影响）
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
cd /Users/cc/newapi628 && git add internal/mtwire/agent.go internal/mtwire/agent_tiering_test.go && git commit -m "feat(billing): tier-based earning switch in creditConsumeCommission (L0 commission XOR L1 markup)"
```

---

### Task 15: Gate `HandleAgentSetGroupRatio` to level≥1 (L0 → 403)

Closes §9.3's "HandleAgentSetGroupRatio → 要求 level≥1（L0 → 403）" — the last piece that makes "L0 cannot set model multipliers" actually true (today any agent, regardless of level, can call this endpoint). Same one-liner-at-the-top-of-the-handler pattern as Task 3's `custom_domain.go`/`siteconfig.go` depth checks, reusing `ensureAgentLevel` (already exists from Task 3 by the time this task runs). Deliberately scoped to only the **setter** (`HandleAgentSetGroupRatio`) — `HandleAgentListGroups` (read-only) stays open to L0 agents, since seeing the platform baseline isn't a capability leak and the task list only names the setter.

**Files:**
- `internal/mtwire/distribution.go` (`HandleAgentSetGroupRatio` ~397-435).
- `internal/mtwire/distribution_test.go` (`newGroupRatioApp` ~142-160 — extend; `TestHandleAgentSetGroupRatio` ~162-203 — update seed so it keeps passing under the new gate).

**Interfaces:**
- Consumes: `App.ensureAgentLevel(c, min int) bool` (Task 3, `internal/mtwire/agent.go`).

- [ ] **Step 1:** Write the failing test + extend the shared test harness (both needed together since the new test needs `AgentService` on the harness to compile). In `internal/mtwire/distribution_test.go`, add `"github.com/QuantumNous/new-api/internal/agent"` to the import block, then update `newGroupRatioApp` (~142-160):
```go
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
	return &App{
		DB: db, ModelGroupRepo: modelgroup.New(db), TenantRepo: tenantrepo.New(db),
		// AgentService 供 ensureAgentLevel（level 门禁，Task 15）用；独立 MemRepo，与本 harness 的
		// sqlite 表无关（HandleAgentSetGroupRatio 只经 AgentService 读 level，不碰 AgentRepo/agent_profiles）。
		AgentService: agent.NewService(agent.NewMemRepo(), nil),
	}
}
```
  Update the existing `TestHandleAgentSetGroupRatio` (~162-165) so tenant 7 stays level≥1 under the new gate (add right after `const tenantID = int64(7)`):
```go
func TestHandleAgentSetGroupRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(7)
	if err := app.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{Level: 1}); err != nil { // Task 15 门禁：需 L1
		t.Fatalf("set L1: %v", err)
	}
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	// ... rest of the function is unchanged ...
```
  Append the new test:
```go
// TestHandleAgentSetGroupRatio_RequiresLevel1 覆盖分层门禁（§9.3）：L0（普通档）设模型分组倍率 →
// 403 AGENT_LEVEL_LOCKED，且不落 tenant_groups；L1（独立档）→ 200 放行（既有校验——下限/仅模型
// 分组——照常生效，同 Task 3 的门禁模式）。
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

	// L0 → 403 AGENT_LEVEL_LOCKED，且不落 tenant_groups。
	c, rec := newAgentCtx(70, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_LEVEL_LOCKED" {
		t.Fatalf("L0 must be locked, got %+v", r)
	}
	if _, found, _ := app.TenantRepo.LookupEnabledGroupRatio(ctx, 70, "claude-kiro"); found {
		t.Fatal("L0 must not have persisted a group override")
	}

	// L1 → 200 放行（组合下限校验照常生效）。
	c, rec = newAgentCtx(71, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("L1 should be allowed, got %+v", r)
	}
}
```

- [ ] **Step 2:** Run — expect FAIL (`TestHandleAgentSetGroupRatio_RequiresLevel1`'s L0 case gets 200 instead of 403 AGENT_LEVEL_LOCKED, since the handler doesn't gate on level yet; the harness/import changes themselves should already compile).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run TestHandleAgentSetGroupRatio
```

- [ ] **Step 3:** Add the gate. In `internal/mtwire/distribution.go`, insert as the first line of `HandleAgentSetGroupRatio`'s body (~400):
```go
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
	// ... rest of the function body is unchanged ...
```

- [ ] **Step 4:** Run — expect PASS (both the new gate test and the pre-existing `TestHandleAgentSetGroupRatio`/`TestHandleAgentListGroups`).
```
cd /Users/cc/newapi628 && go test ./internal/mtwire/ -run 'TestHandleAgentSetGroupRatio|TestHandleAgentListGroups'
cd /Users/cc/newapi628 && go build ./internal/... ./service/... ./router/... ./model/... ./setting/... ./controller/... && go test ./internal/mtwire/... ./internal/agent/...
```

- [ ] **Step 5:** Commit.
```
cd /Users/cc/newapi628 && git add internal/mtwire/distribution.go internal/mtwire/distribution_test.go && git commit -m "feat(billing): gate HandleAgentSetGroupRatio to level>=1 (L0 -> 403 AGENT_LEVEL_LOCKED)"
```

---

## Self-review notes (coverage of the spec)

- Spec §5.1 (data model): `can_api` added (Task 1); `type` column + `AgentType` enum + frontend `agentTypeValues` deleted (Task 2 / Task 6); migration backfills level=1 then drops `type` (Task 2).
- Spec §5.2 (backend): `AgentLevel` helper (Task 1); typeless `SetAgentType`/`GetAgentType` (Task 2); subdomain gate on create + promotion (Task 4); `RequireAgentLevel(1)` route gate + in-handler depth (Task 3); migration (Task 2); L0 promotion-link attribution path is unchanged (`attributeByChannel`/`attributeByHost` never depended on a subdomain — verified, no code touched).
- Spec §5.3 (frontend): level selector + badge + deleted enum (Task 6); sidebar + route guards on level>=1 (Task 7); `/api/tenant/agent-context` extended (Task 5) and consumed (Task 7).
- Spec §6 (tests): level gate L0 reject / L1 pass (Task 3 unit + Task 8 E2E); L0 create no subdomain + promotion provisions (Task 4 unit + server); migration level=1 (Task 8 server); `can_api` placeholder does not gate (Task 1 round-trip, never read by any gate); create/edit works after `type` removed (Task 2/6).
- Deviations flagged: (a) DB `type`-drop co-located with code removal in Task 2 (compile-safety) rather than Task 1; (b) `SetAgentType`/`GetAgentType` names kept; (c) `SkipSubdomain` (default-provision) chosen over `ProvisionSubdomain` to keep existing tenant tests green; (d) new-agent create defaults to level 0 per "base tier" intent; (e) the drawer "key metrics for promotion decision" (spec §5.3.1) is delivered by Task 10 (per-agent promotion metrics 总充值/分润收益/下级用户数, reusing reportrepo aggregates + one thin downstream-count query); (f) the "not unlocked" page reuses `/403` (existing pattern) rather than a bespoke page.

**Part 2 (money model, spec §9 — Tasks 11-15):**

- §9.1 (L0 提成+邀请9折): discount config (Task 11); 9折 refund at consumption, base unaffected (Task 12); commission math untouched, still `commission_ratio × quotaUnits` (Task 12/14, `creditL0Commission`).
- §9.2 (L1 差价入账): markup base = official price via `resolveGroupFactors`/`ratioMarkupQuotaUnits`, reusing the real pricing lookups (`modelGroupBaseline`, `resolveTenantGroupRatio`) rather than approximating them (Task 13).
- §9.3 (按档 gate): `HandleAgentSetGroupRatio` → level≥1 (Task 15); commission vs markup mutually exclusive in `creditConsumeCommission`'s dispatch, tested directly (Task 14 `TestCreditConsumeCommission_TierSwitch_NeverBothSources`); 9折 scoped to L0-attributed users only, tested (Task 14).
- §9.5 (红线): every new write path (`grantInviteDiscount`, `creditRatioMarkup`, `creditL0Commission`) is `requestID`-idempotent (Tasks 12/13, retested end-to-end in Task 14's `TestCreditConsumeCommission_RetrySameRequestID_NoDoubleCredit`) and wrapped by `creditConsumeCommission`'s existing `defer recover()` (never blocks the request); base is official price everywhere (9折 never shrinks the L0 commission base; markup is computed off the platform baseline, not the discounted price).
- §9.6 (tests): 9折 applies only to L0-attributed users / rate from config (Task 11/12); L0 commission unaffected by discount (Task 12); L1 markup formula + no-override-no-markup (Task 13); gate L0 403 / L1 pass (Task 15); tier switch on/off (Task 14); idempotency (Tasks 12/13/14).
- Deviations flagged (Part 2, not silently assumed): (a) the `ConsumeCommission` hook signature gained a 5th parameter (`usingGroup`) — required because the hook previously carried no way to know which model group was billed, which the L1 markup math needs (Task 13); (b) the 9折 refund is scoped to `billingSource=="wallet"` only — subscription-bucket consumption is metered against a different pool (`PostConsumeUserSubscriptionDelta`) that `users.quota` refunds can't reach without a further signature change the task list didn't ask for (Task 12); (c) L1 markup uses a single `SourceRatioMarkup`, not split by wallet/subscription bucket the way L0's `consume_commission`/`tokenplan_commission` is — kept to the one source type the task list specified; `TotalEarnedCNY`/wallet balance still include it correctly (`AppendEarning` sums by tenant regardless of source), only the finance report's per-source breakdown (`reportrepo.go` `earningsByTenant`) doesn't bucket it separately — the **same** already-accepted gap that `tokenplan_commission` has today (verified: `earningsByTenant`'s switch only special-cases `consume_commission`/`tokenplan_spread`/`manual_adjustment`), so this isn't a new class of gap; (d) `InviteDiscountRate` is admin-settable via the existing generic `PUT /api/option/` only — not yet wired into the admin frontend settings form (§9 doesn't ask for a UI, `USDExchangeRate` is the cited precedent and *is* hand-wired in the frontend, so a labeled field is a natural but unrequested follow-up); (e) deploying Part 2 on top of this plan's Task 2 (which backfills all existing agents to level=1) switches every pre-existing agent from commission to markup-delta earning immediately, dropping to $0 new earnings until they configure a group-ratio override — intentional per spec §9.3, flagged operationally in Task 14.
