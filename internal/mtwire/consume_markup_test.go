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
	// 钱包消耗台账（creditConsumeCommission 旁路写入 mt_wallet_consume_log，见 wallet_consume_log.go）。
	if err := migrateWalletConsumeLog(db); err != nil {
		t.Fatalf("wallet consume migrate: %v", err)
	}
	if err := migratePayableEarningIntents(db); err != nil {
		t.Fatalf("payable earning migrate: %v", err)
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
	app.creditRatioMarkup(ctx, 5, 100, 1200, "claude-kiro", "req-md-1", "wallet", 0.45, 0, 0)

	w, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	wantCNY := consumeCommissionCNY(400, 1, operation_setting.USDExchangeRate)
	if math.Abs(w.WithdrawableBalance-wantCNY) > 1e-6 {
		t.Fatalf("withdrawable = %v, want %v", w.WithdrawableBalance, wantCNY)
	}

	// 幂等：同 requestID 重复调用不重复入账。
	app.creditRatioMarkup(ctx, 5, 100, 1200, "claude-kiro", "req-md-1", "wallet", 0.45, 0, 0)
	w2, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w2.WithdrawableBalance != w.WithdrawableBalance {
		t.Fatalf("idempotency broken: withdrawable = %v, want %v", w2.WithdrawableBalance, w.WithdrawableBalance)
	}

	// 无覆盖（租户 9 未设卖价）：不入账。
	seedUser(t, app, 200, 9)
	app.creditRatioMarkup(ctx, 9, 200, 1200, "claude-kiro", "req-md-2", "wallet", 0.3, 0, 0)
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
	app.creditRatioMarkup(ctx, 11, 300, 1200, "claude-kiro", "req-md-3", "wallet", 0.45, 0, 0.5)
	w11, err := app.AgentRepo.GetWallet(ctx, 11)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w11.WithdrawableBalance != 0 {
		t.Fatalf("chargedGroupRatio<=bottomRatio (drift) must not credit anything, got %v", w11.WithdrawableBalance)
	}

	// 非模型分组（层级名）：即便凑巧有 tenant_groups 行，也不产生差价（IsModelGroup 门禁）。
	seedUser(t, app, 400, 12)
	app.creditRatioMarkup(ctx, 12, 400, 1200, "vip", "req-md-4", "wallet", 0.36, 0, 0)
	w12, err := app.AgentRepo.GetWallet(ctx, 12)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	if w12.WithdrawableBalance != 0 {
		t.Fatalf("non-model-group usingGroup must not credit markup, got %v", w12.WithdrawableBalance)
	}

	// 折扣系数(discountRatio=0.8)：底价 = 平台基准 × 0.8 = 0.3×0.8 = 0.24（相对缩放，取代绝对底价，
	// 优先级高于 bottomPriceRatio）。charged=1200,chargedGroupRatio=0.45,rawUnits≈2666.67,
	// markup=(0.45-0.24)×2666.67=560。
	seedUser(t, app, 500, 13)
	if err := app.TenantRepo.UpsertGroup(ctx, 13, "claude-kiro", 0.45); err != nil {
		t.Fatalf("upsert override: %v", err)
	}
	app.creditRatioMarkup(ctx, 13, 500, 1200, "claude-kiro", "req-md-5", "wallet", 0.45, 0.8, 0)
	w13, err := app.AgentRepo.GetWallet(ctx, 13)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	wantCNY5 := consumeCommissionCNY(560, 1, operation_setting.USDExchangeRate)
	if math.Abs(w13.WithdrawableBalance-wantCNY5) > 1e-6 {
		t.Fatalf("discount-factor markup = %v, want %v (bottom=0.3*0.8=0.24)", w13.WithdrawableBalance, wantCNY5)
	}
}

// TestRatioMarkupQuotaUnits_NeverNegative_AcrossTierRange 是 MANDATORY safety 的结构性证明（**v3 改写**）：
// 无论层级优惠（tier，vip 力度深浅）取值如何，markup 恒 >= 0——即便 tier 深到让 chargedGroupRatio 跌破
// bottomRatio（guard 直接 clamp 到 0，不会算出负数）。**同时新增单调性断言（v3 新性质，取代 v2 的
// tier-无关断言）**：固定同样的「原始消耗」（rawUnits=1000），tier 越深（折扣越大），markup 非增——
// 代理自己给出的折扣越深，自己赚的差价只会越少或不变，绝不会更多。
func TestRatioMarkupQuotaUnits_NeverNegative_AcrossTierRange(t *testing.T) {
	const sellRatio = 0.9
	const bottomRatio = 0.7
	prev := int64(1 << 62) // 极大值起步，第一次比较必过
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
	vipCharged := int64(rawUnits * vipTier * sellRatio)         // 720，vip 少付

	seedUser(t, app, 100, 5) // default 用户
	seedUser(t, app, 101, 5) // vip 用户，同一 L1 租户
	if err := app.TenantRepo.UpsertGroup(ctx, 5, "claude-kiro", sellRatio); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	app.creditRatioMarkup(ctx, 5, 100, defaultCharged, "claude-kiro", "req-vip-default", "wallet", defaultTier*sellRatio, 0, bottomRatio)
	app.creditRatioMarkup(ctx, 5, 101, vipCharged, "claude-kiro", "req-vip-vip", "wallet", vipTier*sellRatio, 0, bottomRatio)

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
