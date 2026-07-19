package gormrepo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type legacyEarningRow struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID   int64     `gorm:"column:tenant_id;not null;index:idx_agent_earnings_tenant"`
	UserID     int64     `gorm:"column:user_id;not null;default:0"`
	SourceType string    `gorm:"column:source_type;type:varchar(32);not null"`
	SourceID   string    `gorm:"column:source_id;type:varchar(128);not null"`
	IdemKey    string    `gorm:"column:idem_key;type:varchar(200);not null;uniqueIndex:idx_agent_earnings_idem"`
	Amount     float64   `gorm:"column:amount;type:decimal(20,8);not null"`
	Remark     string    `gorm:"column:remark;type:varchar(255);not null;default:''"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (legacyEarningRow) TableName() string { return "agent_earning_logs" }

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

func TestAutoMigratePreservesLegacyEarningIdempotency(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, AutoMigrate(db))
	require.NoError(t, AutoMigrate(db))
	require.NoError(t, db.Migrator().DropTable(&earningRow{}))

	// This is the table shape produced before claim_id and the composite source
	// uniqueness constraint were added. Its idem_key is the old raw NUL-delimited
	// value rather than the current bounded SHA-256 encoding.
	require.NoError(t, db.Migrator().CreateTable(&legacyEarningRow{}))

	createdAt := time.Date(2026, time.July, 19, 6, 7, 8, 0, time.UTC)
	legacyKey := "42\x00consume_commission\x00legacy-request"
	require.NoError(t, db.Exec(`INSERT INTO agent_earning_logs
		(tenant_id, user_id, source_type, source_id, idem_key, amount, remark, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		42, 99, string(agent.SourceConsumeCommission), "legacy-request", legacyKey, 1.25, "legacy", createdAt).Error)
	require.NoError(t, db.Exec(`INSERT INTO agent_wallets
		(tenant_id, user_id, withdrawable_balance, total_earned, updated_at)
		VALUES (?, ?, ?, ?, ?)`, 42, 99, 1.25, 1.25, createdAt).Error)

	require.NoError(t, AutoMigrate(db))
	repo := New(db)
	entry := agent.EarningEntry{
		TenantID:   42,
		UserID:     99,
		SourceType: agent.SourceConsumeCommission,
		SourceID:   "legacy-request",
		Amount:     1.25,
		Remark:     "legacy",
	}

	applied, err := repo.AppendEarning(context.Background(), entry)
	require.NoError(t, err)
	assert.False(t, applied, "a legacy row must remain an exact idempotent replay after migration")
	wallet, err := repo.GetWallet(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, 1.25, wallet.WithdrawableBalance)
	assert.Equal(t, 1.25, wallet.TotalEarned)

	mismatched := entry
	mismatched.Amount = 2.50
	applied, err = repo.AppendEarning(context.Background(), mismatched)
	assert.False(t, applied)
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrEarningInvalid))

	var count int64
	require.NoError(t, db.Table("agent_earning_logs").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

// TestWithdrawal_ApproveMovesNoMoney 验证提现闭环补强 #2 的钱流调整：「申请冻结 → 通过」
// 只翻状态，approve 绝不动钱（钱仍留在 frozen，等 mark-paid 才真正出账）。
func TestWithdrawal_ApproveMovesNoMoney(t *testing.T) {
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
	// 通过：不动钱——可提现仍 70，冻结仍 30（等 mark-paid 才真正出账）。
	w, _ = r.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 70 || w.FrozenWithdrawAmount != 30 {
		t.Fatalf("after approve: withdrawable=%v frozen=%v, want 70/30 (approve must not move money)",
			w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
	// approved 只能迁去 paid，不能再迁去 rejected/pending。
	if err := r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawRejected, "x"); err != agent.ErrWithdrawNotPending {
		t.Fatalf("re-review = %v, want ErrWithdrawNotPending", err)
	}
}

// ---- mark-paid（提现闭环补强 #2）----

// TestMarkWithdrawalPaid_DebitsFrozenAndRecordsRef 验证标记已打款：扣减冻结（资金真正出账）+
// 记录打款单号/时间；金额守恒（withdrawable+frozen 复原到「打款前」状态）。
func TestMarkWithdrawalPaid_DebitsFrozenAndRecordsRef(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	if err := r.CreateWithdrawal(ctx, wd); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawApproved, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := r.MarkWithdrawalPaid(ctx, wd.ID, "WX20260704001"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	got, err := r.GetWithdrawal(ctx, wd.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != agent.WithdrawPaid || got.PayoutRef != "WX20260704001" || got.PaidAt.IsZero() {
		t.Fatalf("withdrawal after mark-paid = %+v", got)
	}
	// mark-paid 真正出账：冻结清零，可提现维持 70（早在申请时已冻结，approve 未动过）。
	w, _ := r.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 70 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("after mark-paid: withdrawable=%v frozen=%v, want 70/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

// TestMarkWithdrawalPaid_OnlyFromApproved 验证 CAS：仅 approved→paid；pending 直接标记 / 重复
// 标记均被拒（ErrWithdrawNotApproved），且被拒的调用绝不重复扣款。
func TestMarkWithdrawalPaid_OnlyFromApproved(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	if err := r.CreateWithdrawal(ctx, wd); err != nil {
		t.Fatalf("create: %v", err)
	}
	// 仍是 pending：mark-paid 必须拒绝。
	if err := r.MarkWithdrawalPaid(ctx, wd.ID, "ref-1"); err != agent.ErrWithdrawNotApproved {
		t.Fatalf("mark-paid on pending = %v, want ErrWithdrawNotApproved", err)
	}

	if err := r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawApproved, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := r.MarkWithdrawalPaid(ctx, wd.ID, "ref-1"); err != nil {
		t.Fatalf("first mark-paid: %v", err)
	}
	// 已是 paid（终态）：重复标记必须拒绝。
	if err := r.MarkWithdrawalPaid(ctx, wd.ID, "ref-2"); err != agent.ErrWithdrawNotApproved {
		t.Fatalf("second mark-paid = %v, want ErrWithdrawNotApproved", err)
	}

	w, _ := r.GetWallet(ctx, 1)
	if w.FrozenWithdrawAmount != 0 || w.WithdrawableBalance != 70 {
		t.Fatalf("double mark-paid moved money again: withdrawable=%v frozen=%v", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
	got, _ := r.GetWithdrawal(ctx, wd.ID)
	if got.PayoutRef != "ref-1" {
		t.Fatalf("payout_ref = %q, want unchanged %q", got.PayoutRef, "ref-1")
	}
}

// TestMarkWithdrawalPaid_NotFound 验证提现单不存在时的错误码。
func TestMarkWithdrawalPaid_NotFound(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if err := r.MarkWithdrawalPaid(ctx, 404, "ref"); err != agent.ErrWithdrawNotFound {
		t.Fatalf("mark-paid missing = %v, want ErrWithdrawNotFound", err)
	}
}

// TestMarkWithdrawalPaid_ConcurrentOnlyOneWins 验证 -race 下并发 mark-paid 同一张 approved 单，
// CAS(WHERE status='approved') 只放行一个赢家，冻结只扣一次（不重复出账）。
func TestMarkWithdrawalPaid_ConcurrentOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	if err := r.CreateWithdrawal(ctx, wd); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawApproved, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}

	var wg sync.WaitGroup
	var wins int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.MarkWithdrawalPaid(ctx, wd.ID, "ref"); err == nil {
				atomic.AddInt64(&wins, 1)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("concurrent mark-paid winners = %d, want exactly 1", wins)
	}
	w, _ := r.GetWallet(ctx, 1)
	if w.FrozenWithdrawAmount != 0 || w.WithdrawableBalance != 70 {
		t.Fatalf("after concurrent mark-paid: withdrawable=%v frozen=%v, want 70/0 (must debit exactly once)",
			w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

// ---- 收款账户（提现闭环补强 #1）----

func TestSetPayoutAccount_RoundTrips(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	want := agent.PayoutAccount{Method: agent.PayoutBank, Account: "6222000000", Name: "Alice", Bank: "ICBC"}
	if err := r.SetPayoutAccount(ctx, 7, want); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, found, err := r.GetPayoutAccount(ctx, 7)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestGetPayoutAccount_NotFoundWhenUnset(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	// tenant 7 无 profile 行。
	if _, found, err := r.GetPayoutAccount(ctx, 7); err != nil || found {
		t.Fatalf("GetPayoutAccount(no profile): found=%v err=%v, want false/nil", found, err)
	}
	// tenant 5 有 profile 行（已设代理），但从未设置收款账户。
	if err := r.SetAgentType(ctx, 5, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set agent type: %v", err)
	}
	if _, found, err := r.GetPayoutAccount(ctx, 5); err != nil || found {
		t.Fatalf("GetPayoutAccount(unset payout): found=%v err=%v, want false/nil", found, err)
	}
}

// TestSetPayoutAccount_DoesNotClobberAgentParams 确认收款账户与 AgentParams 是同一 profile 行
// 里互不干扰的两组列：无论先后顺序写入，都不清空对方（镜像 SetAgentType 对 payout_* 的同等保护）。
func TestSetPayoutAccount_DoesNotClobberAgentParams(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	params := agent.AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 1}
	if err := r.SetAgentType(ctx, 7, params); err != nil {
		t.Fatalf("set agent type: %v", err)
	}
	payout := agent.PayoutAccount{Method: agent.PayoutAlipay, Account: "a@example.com", Name: "Alice"}
	if err := r.SetPayoutAccount(ctx, 7, payout); err != nil {
		t.Fatalf("set payout account: %v", err)
	}

	gotParams, _, err := r.GetAgentType(ctx, 7)
	if err != nil || gotParams != params {
		t.Fatalf("AgentParams clobbered by SetPayoutAccount: got %+v, want %+v (err %v)", gotParams, params, err)
	}
	gotPayout, found, err := r.GetPayoutAccount(ctx, 7)
	if err != nil || !found || gotPayout != payout {
		t.Fatalf("payout account not persisted: got %+v found=%v err=%v", gotPayout, found, err)
	}

	// 反向：再次 SetAgentType 不得清空已设置的 payout。
	params2 := agent.AgentParams{CostPrice: 20, Level: 2}
	if err := r.SetAgentType(ctx, 7, params2); err != nil {
		t.Fatalf("set agent type again: %v", err)
	}
	gotPayout2, found2, err := r.GetPayoutAccount(ctx, 7)
	if err != nil || !found2 || gotPayout2 != payout {
		t.Fatalf("payout account clobbered by second SetAgentType: got %+v found=%v err=%v", gotPayout2, found2, err)
	}
}

// TestCreateWithdrawal_PersistsPayoutSnapshot 验证 CreateWithdrawal 把调用方已填好的收款快照
// 字段（PayoutMethod/PayoutAccount/PayoutName/PayoutBank）如实持久化并原样读回。
func TestCreateWithdrawal_PersistsPayoutSnapshot(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	wd := &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 30,
		PayoutMethod: agent.PayoutAlipay, PayoutAccount: "alice@example.com", PayoutName: "Alice",
	}
	if err := r.CreateWithdrawal(ctx, wd); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := r.GetWithdrawal(ctx, wd.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PayoutMethod != agent.PayoutAlipay || got.PayoutAccount != "alice@example.com" || got.PayoutName != "Alice" || got.PayoutBank != "" {
		t.Fatalf("withdrawal payout snapshot = %+v, want alipay/alice@example.com/Alice/''", got)
	}
	// 列表接口同样带出快照字段（管理员/代理列表页据此显示打款目标）。
	byTenant, err := r.ListWithdrawalsByTenant(ctx, 1)
	if err != nil || len(byTenant) != 1 || byTenant[0].PayoutAccount != "alice@example.com" {
		t.Fatalf("ListWithdrawalsByTenant = %+v (err %v), want 1 row with payout snapshot", byTenant, err)
	}
	all, err := r.ListWithdrawals(ctx, "")
	if err != nil || len(all) != 1 || all[0].PayoutAccount != "alice@example.com" {
		t.Fatalf("ListWithdrawals = %+v (err %v), want 1 row with payout snapshot", all, err)
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
