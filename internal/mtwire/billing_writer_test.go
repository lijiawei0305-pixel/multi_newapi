package mtwire

// 覆盖自研计费 hook 的异步批量落库路径（billing_writer.go）：开关只缓冲 display-only 的
// mt_wallet_consume_log；agent_earning_logs/agent_wallets 可提现收益必须在调用返回前同步落库。直接驱动
// flush()（不起 goroutine/信号），保持测试确定性。

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withAsyncBilling 在测试期装配 writer 并开启 AGENT_HOOK_ASYNC_ENABLED，返回还原函数（全局标志，务必 defer 还原）。
func withAsyncBilling(t *testing.T, app *App) func() {
	t.Helper()
	app.billing = newBillingWriter(app.DB)
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

// TestBillingWriter_AsyncFlush_OnlyBuffersDisplayLog：三笔同租户钱包桶消耗——flush 前仅展示台账在缓冲，
// 三笔真实收益及钱包余额均已同步提交；flush 后只新增三行展示台账，收益与钱包数值不再变化。
func TestBillingWriter_AsyncFlush_OnlyBuffersDisplayLog(t *testing.T) {
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

	// flush 前：只有 display-only 台账在缓冲；可提现收益已经同步持久化。
	if n, _ := walletConsumeAgg(t, app, 5); n != 0 {
		t.Fatalf("flush 前台账行 = %d, want 0（缓冲中）", n)
	}
	eRowsBefore, eSumBefore := earningAgg(t, app, 5)
	if eRowsBefore != 3 || eSumBefore <= 0 {
		t.Fatalf("flush 前收益 = (%d 行, %v), want (3, >0)（同步持久化）", eRowsBefore, eSumBefore)
	}
	walletBefore, err := app.AgentRepo.GetWallet(ctx, 5)
	if err != nil {
		t.Fatalf("flush 前查钱包: %v", err)
	}
	if walletBefore.WithdrawableBalance != eSumBefore || walletBefore.TotalEarned != eSumBefore {
		t.Fatalf("flush 前钱包 = (%v, %v), want (%v, %v)（同步持久化）", walletBefore.WithdrawableBalance, walletBefore.TotalEarned, eSumBefore, eSumBefore)
	}

	app.billing.flush()

	// flush 后：展示台账落三行；收益和钱包不受异步 flush 影响。
	if n, sum := walletConsumeAgg(t, app, 5); n != 3 || sum != 9000 {
		t.Fatalf("flush 后台账 = (%d 行, %d quota), want (3, 9000)", n, sum)
	}
	eRowsAfter, eSumAfter := earningAgg(t, app, 5)
	assert.Equal(t, eRowsBefore, eRowsAfter)
	assert.Equal(t, eSumBefore, eSumAfter)
	walletAfter, err := app.AgentRepo.GetWallet(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, walletBefore.WithdrawableBalance, walletAfter.WithdrawableBalance)
	assert.Equal(t, walletBefore.TotalEarned, walletAfter.TotalEarned)
}

type retryingEarningSink struct {
	calls     int
	failUntil int
	entry     agent.EarningEntry
}

func (s *retryingEarningSink) AddEarning(_ context.Context, entry agent.EarningEntry) error {
	s.calls++
	if s.calls <= s.failUntil {
		return errors.New("temporary database error")
	}
	s.entry = entry
	return nil
}

func TestCreditEarningRetriesSynchronously(t *testing.T) {
	sink := &retryingEarningSink{failUntil: 2}
	app := newRatioMarkupTestApp(t)
	app.AgentEarnings = sink
	entry := agent.EarningEntry{
		TenantID:   7,
		UserID:     8,
		SourceType: agent.SourceConsumeCommission,
		SourceID:   "req-sync-retry",
		Amount:     1.25,
	}

	require.NoError(t, app.creditEarning(context.Background(), entry))

	assert.Equal(t, 3, sink.calls)
	assert.Equal(t, entry, sink.entry)
}

func TestBillingWriter_RetriesFailedDisplayLogFlush(t *testing.T) {
	app := newRatioMarkupTestApp(t)
	app.billing = newBillingWriter(app.DB)
	row := walletConsumeRow{
		TenantID:    5,
		UserID:      100,
		WalletQuota: 3000,
		RequestID:   "req-display-retry",
	}
	app.billing.enqueueConsume(row)

	const callbackName = "test:fail_wallet_consume_flush"
	require.NoError(t, app.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == row.TableName() {
			tx.AddError(errors.New("temporary display log failure"))
		}
	}))
	app.billing.flush()
	require.NoError(t, app.DB.Callback().Create().Remove(callbackName))

	var beforeRetry int64
	require.NoError(t, app.DB.Model(&walletConsumeRow{}).Count(&beforeRetry).Error)
	assert.Zero(t, beforeRetry)
	app.billing.mu.Lock()
	buffered := len(app.billing.consumeRows)
	app.billing.mu.Unlock()
	assert.Equal(t, 1, buffered, "failed display log must remain buffered for retry")

	app.billing.flush()
	var afterRetry int64
	require.NoError(t, app.DB.Model(&walletConsumeRow{}).Count(&afterRetry).Error)
	assert.Equal(t, int64(1), afterRetry)
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
	app.billing = newBillingWriter(app.DB) // writer 在位，但 flag 默认关

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
