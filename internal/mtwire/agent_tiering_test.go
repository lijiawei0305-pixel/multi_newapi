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
