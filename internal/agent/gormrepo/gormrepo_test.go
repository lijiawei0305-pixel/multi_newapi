package gormrepo

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
)

// newTestRepo 用纯 Go sqlite(:memory:) 建一个隔离的 agent 仓储。
// 单连接：:memory: 每连接独立库，限 1 连接保证同一库（对齐 tokenplan/payment 仓储测试约定）。
func newTestRepo(t *testing.T) *Repo {
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
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}

// TestSetAgentType_Upsert 确认按 tenant_id upsert：二次写覆盖参数。
func TestSetAgentType_Upsert(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	p1 := agent.AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 1}
	if err := r.SetAgentType(ctx, 7, agent.AgentTypeNormal, p1); err != nil {
		t.Fatalf("set1: %v", err)
	}
	p2 := agent.AgentParams{CostPrice: 20, PackageDiscount: 0.8, CommissionRatio: 0.3, Level: 2}
	if err := r.SetAgentType(ctx, 7, agent.AgentTypeOEM, p2); err != nil {
		t.Fatalf("set2: %v", err)
	}
	typ, got, found, err := r.GetAgentType(ctx, 7)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if typ != agent.AgentTypeOEM || got != p2 {
		t.Fatalf("got (%v,%+v), want (oem,%+v)", typ, got, p2)
	}
	// 未设代理的租户 found=false。
	if _, _, found, _ := r.GetAgentType(ctx, 99); found {
		t.Fatal("tenant 99 must not be an agent")
	}
}

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

// TestAppendEarning_IdempotentAndAccrues 是分润幂等核心用例：
// 同 (tenant, source_type, source_id) 重复入账只动一次钱包；不同来源各自累加。
func TestAppendEarning_IdempotentAndAccrues(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	e := agent.EarningEntry{TenantID: 1, UserID: 5, SourceType: agent.SourceTokenplanSpread, SourceID: "ord-1", Amount: 40}

	applied, err := r.AppendEarning(ctx, e)
	if err != nil || !applied {
		t.Fatalf("first append: applied=%v err=%v", applied, err)
	}
	// 重复同来源：applied=false，钱包不变。
	applied, err = r.AppendEarning(ctx, e)
	if err != nil || applied {
		t.Fatalf("dup append: applied=%v err=%v (want false/nil)", applied, err)
	}
	// 另一来源累加。
	if _, err := r.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, UserID: 5, SourceType: agent.SourceConsumeCommission, SourceID: "req-9", Amount: 10,
	}); err != nil {
		t.Fatalf("second source: %v", err)
	}

	w, err := r.GetWallet(ctx, 1)
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if w.WithdrawableBalance != 50 || w.TotalEarned != 50 {
		t.Fatalf("wallet = (withdrawable %v, total %v), want 50/50", w.WithdrawableBalance, w.TotalEarned)
	}
	if w.UserID != 5 {
		t.Fatalf("wallet UserID = %d, want 5", w.UserID)
	}

	logs, err := r.ListEarningsByTenant(ctx, 1)
	if err != nil || len(logs) != 2 {
		t.Fatalf("earning logs = %d (err %v), want 2", len(logs), err)
	}
}

// TestWithdrawal_FreezeApproveConservesMoney 验证提现「申请冻结 → 通过打款」金额守恒。
func TestWithdrawal_FreezeApproveConservesMoney(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	if err := r.CreateWithdrawal(ctx, wd); err != nil {
		t.Fatalf("create withdrawal: %v", err)
	}
	if wd.ID == 0 || wd.Status != agent.WithdrawPending {
		t.Fatalf("withdrawal not pending: %+v", wd)
	}
	// 申请后：可提现 100→70，冻结 0→30（守恒：70+30=100）。
	w, _ := r.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 70 || w.FrozenWithdrawAmount != 30 {
		t.Fatalf("after request: withdrawable=%v frozen=%v, want 70/30", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}

	if err := r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawApproved, "paid"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// 通过：扣冻结（资金离开），可提现保持 70，冻结回 0。
	w, _ = r.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 70 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("after approve: withdrawable=%v frozen=%v, want 70/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
	// 终态不可再审。
	if err := r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawRejected, "x"); err != agent.ErrWithdrawNotPending {
		t.Fatalf("re-review = %v, want ErrWithdrawNotPending", err)
	}
}

// TestWithdrawal_RejectRefunds 验证拒绝时冻结解冻退回可提现（金额守恒复原）。
func TestWithdrawal_RejectRefunds(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 40}
	if err := r.CreateWithdrawal(ctx, wd); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawRejected, "no"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	w, _ := r.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 100 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("after reject: withdrawable=%v frozen=%v, want 100/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

// TestWithdrawal_InsufficientRejected 验证超额提现被拒、钱包不动。
func TestWithdrawal_InsufficientRejected(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 20)

	if err := r.CreateWithdrawal(ctx, &agent.Withdrawal{TenantID: 1, Amount: 50}); err != agent.ErrWithdrawInsufficient {
		t.Fatalf("over-withdraw = %v, want ErrWithdrawInsufficient", err)
	}
	// 非正金额同样被拒。
	if err := r.CreateWithdrawal(ctx, &agent.Withdrawal{TenantID: 1, Amount: 0}); err != agent.ErrWithdrawInsufficient {
		t.Fatalf("zero-withdraw = %v, want ErrWithdrawInsufficient", err)
	}
	w, _ := r.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 20 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("wallet moved: %+v", w)
	}
}

// TestGetWithdrawal_NotFound 验证不存在提现单的错误码。
func TestGetWithdrawal_NotFound(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if _, err := r.GetWithdrawal(ctx, 123); err != agent.ErrWithdrawNotFound {
		t.Fatalf("get missing = %v, want ErrWithdrawNotFound", err)
	}
}

// seedBalance 借 AppendEarning 给某租户钱包注入初始可提现余额。
func seedBalance(t *testing.T, r *Repo, tenantID, userID int64, amount float64) {
	t.Helper()
	if _, err := r.AppendEarning(context.Background(), agent.EarningEntry{
		TenantID: tenantID, UserID: userID, SourceType: agent.SourceManualAdjustment,
		SourceID: "seed", Amount: amount,
	}); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
}
