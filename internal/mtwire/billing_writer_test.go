package mtwire

// 覆盖自研计费 hook 的异步批量落库路径（billing_writer.go）：开启后 creditConsumeCommission 的两类写
// （mt_wallet_consume_log 台账 + agent_earning_logs/agent_wallets 收益）先进缓冲、flush 才落库；跨 flush
// 幂等不双计；关闭时逐请求同步落库（无回归）。直接驱动 flush()（不起 goroutine/信号），保测试确定性。

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
)

// withAsyncBilling 在测试期装配 writer 并开启 AGENT_HOOK_ASYNC_ENABLED，返回还原函数（全局标志，务必 defer 还原）。
func withAsyncBilling(t *testing.T, app *App) func() {
	t.Helper()
	app.billing = newBillingWriter(app.DB, app.AgentRepo)
	prev := common.AgentHookAsyncEnabled
	common.AgentHookAsyncEnabled = true
	return func() { common.AgentHookAsyncEnabled = prev }
}

// earningAgg 返回某租户在 agent_earning_logs 的行数与金额合计。
func earningAgg(t *testing.T, app *App, tenantID int64) (rows int64, sum float64) {
	t.Helper()
	var out struct {
		N int64
		S float64
	}
	if err := app.DB.Table("agent_earning_logs").
		Select("COUNT(*) AS n, COALESCE(SUM(amount),0) AS s").
		Where("tenant_id = ?", tenantID).Scan(&out).Error; err != nil {
		t.Fatalf("query earnings (tenant=%d): %v", tenantID, err)
	}
	return out.N, out.S
}

// TestBillingWriter_AsyncFlush_LandsConsumeAndEarning：三笔同租户钱包桶消耗——flush 前只在缓冲（两账皆 0），
// flush 后台账 3 行、收益 3 行且钱包按 3 笔合并累加（3 条 earning 日志但只 1 次钱包 UPSERT 的结果=合计）。
func TestBillingWriter_AsyncFlush_LandsConsumeAndEarning(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	restore := withAsyncBilling(t, app)
	defer restore()

	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L0 agent: %v", err)
	}
	seedUser(t, app, 100, 5)

	app.creditConsumeCommission(100, 3000, "req-a", "wallet", "claude-kiro", 0.45)
	app.creditConsumeCommission(100, 3000, "req-b", "wallet", "claude-kiro", 0.45)
	app.creditConsumeCommission(100, 3000, "req-c", "wallet", "claude-kiro", 0.45)

	// flush 前：全在进程内缓冲，DB 两账皆空。
	if n, _ := walletConsumeAgg(t, app, 5); n != 0 {
		t.Fatalf("flush 前台账行 = %d, want 0（缓冲中）", n)
	}
	if n, _ := earningAgg(t, app, 5); n != 0 {
		t.Fatalf("flush 前收益行 = %d, want 0（缓冲中）", n)
	}
	if w, _ := app.AgentRepo.GetWallet(ctx, 5); w.WithdrawableBalance != 0 {
		t.Fatalf("flush 前钱包 = %v, want 0（缓冲中）", w.WithdrawableBalance)
	}

	app.billing.flush()

	// flush 后：台账 3 行合计 9000；收益 3 行；钱包可提现 = 收益合计（合并累加，方向正确）。
	if n, sum := walletConsumeAgg(t, app, 5); n != 3 || sum != 9000 {
		t.Fatalf("flush 后台账 = (%d 行, %d quota), want (3, 9000)", n, sum)
	}
	eRows, eSum := earningAgg(t, app, 5)
	if eRows != 3 {
		t.Fatalf("flush 后收益行 = %d, want 3", eRows)
	}
	if eSum <= 0 {
		t.Fatalf("flush 后收益合计 = %v, want >0（L0 提成入账）", eSum)
	}
	w, _ := app.AgentRepo.GetWallet(ctx, 5)
	if w.WithdrawableBalance != eSum || w.TotalEarned != eSum {
		t.Fatalf("钱包 = (%v, %v), want (%v, %v)＝3 笔收益合并累加", w.WithdrawableBalance, w.TotalEarned, eSum, eSum)
	}
}

// TestBillingWriter_AsyncFlush_IdempotentAcrossFlushes：同一消耗事件（同 request_id）跨两次 flush 重放，
// 收益/台账各仍只 1 行、钱包不双计——异步不破幂等。
func TestBillingWriter_AsyncFlush_IdempotentAcrossFlushes(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	restore := withAsyncBilling(t, app)
	defer restore()

	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L0 agent: %v", err)
	}
	seedUser(t, app, 100, 5)

	app.creditConsumeCommission(100, 3000, "req-dup", "wallet", "claude-kiro", 0.45)
	app.billing.flush()
	w1, _ := app.AgentRepo.GetWallet(ctx, 5)

	app.creditConsumeCommission(100, 3000, "req-dup", "wallet", "claude-kiro", 0.45) // 重放同一事件
	app.billing.flush()
	w2, _ := app.AgentRepo.GetWallet(ctx, 5)

	if w1.WithdrawableBalance != w2.WithdrawableBalance {
		t.Fatalf("重放双计：%v -> %v", w1.WithdrawableBalance, w2.WithdrawableBalance)
	}
	if n, _ := earningAgg(t, app, 5); n != 1 {
		t.Fatalf("收益行 = %d, want 1（幂等）", n)
	}
	if n, _ := walletConsumeAgg(t, app, 5); n != 1 {
		t.Fatalf("台账行 = %d, want 1（幂等）", n)
	}
}

// TestBillingWriter_Disabled_StaysSynchronous：装了 writer 但 flag 关闭——两类写立即同步落库（无需 flush），
// 守卫「关闭时逐字节等价于优化前」不回归。
func TestBillingWriter_Disabled_StaysSynchronous(t *testing.T) {
	ctx := context.Background()
	app := newRatioMarkupTestApp(t)
	app.billing = newBillingWriter(app.DB, app.AgentRepo) // writer 在位，但 flag 默认关

	if err := app.AgentRepo.SetAgentType(ctx, 5, agent.AgentParams{Level: 0, CommissionRatio: 0.2}); err != nil {
		t.Fatalf("set L0 agent: %v", err)
	}
	seedUser(t, app, 100, 5)

	app.creditConsumeCommission(100, 3000, "req-sync", "wallet", "claude-kiro", 0.45)

	if n, _ := walletConsumeAgg(t, app, 5); n != 1 {
		t.Fatalf("同步路径：台账行 = %d（未 flush 即应落库）, want 1", n)
	}
	if n, _ := earningAgg(t, app, 5); n != 1 {
		t.Fatalf("同步路径：收益行 = %d（未 flush 即应落库）, want 1", n)
	}
}
