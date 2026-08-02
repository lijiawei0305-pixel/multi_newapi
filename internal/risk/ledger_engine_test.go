package risk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckPurchaseLimit_LegacyRedisOnlyStillBlocks 上线前仅 Redis 占过的键，DB 空时仍拒（迁移兼容）。
func TestCheckPurchaseLimit_LegacyRedisOnlyStillBlocks(t *testing.T) {
	kv := NewMemKVCache(nil)
	// 模拟旧版只写了 Redis
	ok, err := kv.SetNX(context.Background(), trialKey("user", "99"), "99", 0)
	require.NoError(t, err)
	require.True(t, ok)
	e := NewEngine(kv, WithPurchaseLedger(NewMemPurchaseLedger()))
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 99, trialPlan()), CodePurchaseLimitExceeded)
}

// TestCheckPurchaseLimit_LedgerSurvivesKVWipe 证 C4 根治：DB 台账权威——
// 首购占台账后即便 KV 全清（模拟 Redis 抹掉），再购仍拒；后台释放台账后才可再领。
func TestCheckPurchaseLimit_LedgerSurvivesKVWipe(t *testing.T) {
	kv := NewMemKVCache(nil)
	ledger := NewMemPurchaseLedger()
	e := NewEngine(kv, WithPurchaseLedger(ledger))

	require.NoError(t, e.CheckPurchaseLimit(context.Background(), 42, trialPlan()))
	// 模拟 Redis 全量丢失
	kv2 := NewMemKVCache(nil)
	e2 := NewEngine(kv2, WithPurchaseLedger(ledger))
	err := e2.CheckPurchaseLimit(context.Background(), 42, trialPlan())
	assertCode(t, err, CodePurchaseLimitExceeded)

	// 后台释放（DB + 新 KV）
	res, err := e2.ReleaseTrialLimit(context.Background(), 42, PurchaseIdentity{}, false)
	require.NoError(t, err)
	require.Contains(t, res.Released, "user")
	assertCode(t, e2.CheckPurchaseLimit(context.Background(), 42, trialPlan()), "")
}

// TestCheckPurchaseLimit_LedgerDeviceTTLSelfHeals 台账 device 维有界过期后可被他人占用。
func TestCheckPurchaseLimit_LedgerDeviceTTLSelfHeals(t *testing.T) {
	clk := newManualClock()
	kv := NewMemKVCache(clk)
	ledger := NewMemPurchaseLedger()
	e := NewEngine(kv, WithClock(clk), WithPurchaseLedger(ledger),
		WithConfig(Config{DeviceDedupTTL: time.Hour}))

	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-shared"), 1001, trialPlan()), "")
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-shared"), 1002, trialPlan()), CodePurchaseLimitExceeded)
	clk.Advance(2 * time.Hour)
	// 新引擎共享同一 ledger（台账按 now 参数过期）
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-shared"), 1003, trialPlan()), "")
}

// TestCheckPurchaseLimit_LedgerUserDimLifetime 用户维不随 device TTL 放行。
func TestCheckPurchaseLimit_LedgerUserDimLifetime(t *testing.T) {
	clk := newManualClock()
	e := NewEngine(NewMemKVCache(clk), WithClock(clk), WithPurchaseLedger(NewMemPurchaseLedger()),
		WithConfig(Config{DeviceDedupTTL: time.Hour}))
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-x"), 1001, trialPlan()), "")
	clk.Advance(100 * time.Hour)
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-x"), 1001, trialPlan()), CodePurchaseLimitExceeded)
}

// TestReleasePurchaseLimit_LedgerAndKV 非 Trial 计数台账+KV 双释放。
func TestReleasePurchaseLimit_LedgerAndKV(t *testing.T) {
	kv := NewMemKVCache(nil)
	ledger := NewMemPurchaseLedger()
	e := NewEngine(kv, WithPurchaseLedger(ledger))
	plan := Plan{ID: 9, Code: "mini", PerUserLimit: 1}
	require.NoError(t, e.CheckPurchaseLimit(context.Background(), 1, plan))
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 1, plan), CodePurchaseLimitExceeded)
	require.NoError(t, e.ReleasePurchaseLimit(context.Background(), 9, 1))
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 1, plan), "")
}

// TestRollbackPurchaseLimit_Ledger 失败下单补偿：台账 -1 + KV Decr。
func TestRollbackPurchaseLimit_Ledger(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil), WithPurchaseLedger(NewMemPurchaseLedger()))
	plan := Plan{ID: 3, Code: "pro", PerUserLimit: 1}
	require.NoError(t, e.CheckPurchaseLimit(context.Background(), 5, plan))
	require.NoError(t, e.RollbackPurchaseLimit(context.Background(), 3, 5))
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 5, plan), "")
}

// TestCheckPurchaseLimit_LedgerCrossAccountDevice 跨账号同设备单赢家（台账路径）。
func TestCheckPurchaseLimit_LedgerCrossAccountDevice(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil), WithPurchaseLedger(NewMemPurchaseLedger()))
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 7, trialPlan()), "")
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 8, trialPlan()), CodePurchaseLimitExceeded)
	// 败者换设备可买
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-2"), 8, trialPlan()), "")
}

func TestMemLedger_ForceDoesNotStealOtherUser(t *testing.T) {
	ledger := NewMemPurchaseLedger()
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, ledger.ClaimTrial(ctx, 1, PurchaseIdentity{DeviceID: "d"}, time.Hour, now))
	// user 2 force 释放 device 应 skip（另一真实用户）
	res, err := ledger.ReleaseTrial(ctx, 2, PurchaseIdentity{DeviceID: "d"}, true)
	require.NoError(t, err)
	assert.Contains(t, res.Skipped, "device")
	// user 1 仍被拒
	require.ErrorIs(t, ledger.ClaimTrial(ctx, 3, PurchaseIdentity{DeviceID: "d"}, time.Hour, now), ErrPurchaseLimitExceeded)
}
