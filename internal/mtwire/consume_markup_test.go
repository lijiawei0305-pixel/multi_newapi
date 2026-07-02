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
