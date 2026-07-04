package mtwire

// 覆盖钱包消耗台账（mt_wallet_consume_log）的写入侧：经 creditConsumeCommission（agenthook.ConsumeCommission
// 结算收口）落账。关键口径：只记钱包桶消耗（billingSource==wallet），套餐桶不记；无 agent_profile 的租户
// 也记（否则「主站钱包消耗」永远为 0）；(user_id, request_id) 幂等；未归属(tenant_id=0)不记。台账 display-only：
// 与代理分润(agent_earning_logs)正交——这里只查钱包台账，不断言分润。

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/agent"
)

// walletConsumeAgg 返回某租户在钱包台账里的行数与额度合计。
func walletConsumeAgg(t *testing.T, app *App, tenantID int64) (rows int64, sum int64) {
	t.Helper()
	var out struct {
		N   int64
		Sum int64
	}
	if err := app.DB.Table("mt_wallet_consume_log").
		Select("COUNT(*) AS n, COALESCE(SUM(wallet_quota),0) AS sum").
		Where("tenant_id = ?", tenantID).Scan(&out).Error; err != nil {
		t.Fatalf("query wallet consume (tenant=%d): %v", tenantID, err)
	}
	return out.N, out.Sum
}

// TestCreditConsumeCommission_WalletConsume_RecordsWalletOnly 断言：钱包桶消耗落台账（含无 agent_profile
// 的租户）、套餐桶消耗不落台账、未归属用户不落台账。
func TestCreditConsumeCommission_WalletConsume_RecordsWalletOnly(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)

	// 租户 5：设 L0 代理；用户 100 归属租户 5。
	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L0 agent: %v", err)
	}
	seedUser(t, app, 100, 5)
	// 租户 7：**无** agent_profile（模拟平台/无分润租户）；用户 108 归属租户 7。
	seedUser(t, app, 108, 7)

	// 钱包桶消耗：租户 5 记 3000，租户 7（无 profile）记 5000。
	app.creditConsumeCommission(100, 3000, "req-w-5", "wallet", "claude-kiro", 0.45)
	app.creditConsumeCommission(108, 5000, "req-w-7", "wallet", "claude-kiro", 0.45)
	// 套餐桶消耗：不记（租户 5 台账不应因此增加）。
	app.creditConsumeCommission(100, 9999, "req-sub-5", "subscription", "claude-kiro", 0.45)
	// 未归属用户 300（users 无此行 → tenant_id 解析为 0）：不记。
	app.creditConsumeCommission(300, 8888, "req-w-none", "wallet", "claude-kiro", 0.45)

	if n, sum := walletConsumeAgg(t, app, 5); n != 1 || sum != 3000 {
		t.Fatalf("tenant 5 wallet ledger = (%d rows, %d quota), want (1, 3000) —— 套餐桶消耗不得计入", n, sum)
	}
	if n, sum := walletConsumeAgg(t, app, 7); n != 1 || sum != 5000 {
		t.Fatalf("tenant 7 (无 agent_profile) wallet ledger = (%d rows, %d quota), want (1, 5000) —— 无分润租户仍须记钱包消耗", n, sum)
	}
	if n, _ := walletConsumeAgg(t, app, 0); n != 0 {
		t.Fatalf("tenant 0 (未归属) wallet ledger rows = %d, want 0", n)
	}
}

// TestCreditConsumeCommission_WalletConsume_Idempotent 断言：同一消耗事件（同 user_id+request_id）重放
// 不双计——(user_id, request_id) 唯一 + ON CONFLICT DO NOTHING。
func TestCreditConsumeCommission_WalletConsume_Idempotent(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L0 agent: %v", err)
	}
	seedUser(t, app, 100, 5)

	for i := 0; i < 3; i++ {
		app.creditConsumeCommission(100, 3000, "req-dup", "wallet", "claude-kiro", 0.45)
	}
	if n, sum := walletConsumeAgg(t, app, 5); n != 1 || sum != 3000 {
		t.Fatalf("after 3 replays, wallet ledger = (%d rows, %d quota), want (1, 3000)", n, sum)
	}
}
