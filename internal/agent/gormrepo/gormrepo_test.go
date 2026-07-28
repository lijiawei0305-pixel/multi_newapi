package gormrepo

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

func newConcurrentSQLiteTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "agent-concurrency.db") +
		"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true, PrepareStmt: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, AutoMigrate(db))
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

func TestAppendEarning_FailsClosedWhenPersistedIdentityHashIsCorrupt(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	entry := agent.EarningEntry{
		TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment,
		SourceID: "corrupt-hash-source", Amount: 12.5,
	}
	applied, err := r.AppendEarning(ctx, entry)
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, r.db.Model(&earningRow{}).Where("tenant_id = ?", 1).
		Update("idem_key_hash", strings.Repeat("0", 64)).Error)

	applied, err = r.AppendEarning(ctx, entry)
	assert.False(t, applied)
	require.ErrorIs(t, err, agent.ErrWalletInvariant)
	var count int64
	require.NoError(t, r.db.Model(&earningRow{}).Where("tenant_id = ?", 1).Count(&count).Error)
	assert.Equal(t, int64(1), count, "the correct-hash insert must roll back when the raw identity already exists")
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 12.5, wallet.WithdrawableBalance)
	assert.Equal(t, 12.5, wallet.TotalEarned)
}

func TestSQLiteMoneyArithmeticUsesFixedPointUnits(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	for index, amount := range []float64{0.1, 0.7} {
		applied, err := r.AppendEarning(ctx, agent.EarningEntry{
			TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment,
			SourceID: fmt.Sprintf("decimal-%d", index), Amount: amount,
		})
		require.NoError(t, err)
		assert.True(t, applied)
	}

	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0.8, wallet.WithdrawableBalance)
	assert.Equal(t, 0.8, wallet.TotalEarned)

	var stored struct {
		WithdrawableUnits int64  `gorm:"column:withdrawable_balance_units"`
		TotalEarnedUnits  int64  `gorm:"column:total_earned_units"`
		StorageType       string `gorm:"column:storage_type"`
	}
	require.NoError(t, r.db.Raw(`SELECT withdrawable_balance_units, total_earned_units,
		typeof(withdrawable_balance_units) AS storage_type
		FROM agent_wallets WHERE tenant_id = ?`, 1).Scan(&stored).Error)
	assert.Equal(t, int64(80_000_000), stored.WithdrawableUnits)
	assert.Equal(t, int64(80_000_000), stored.TotalEarnedUnits)
	assert.Equal(t, "integer", stored.StorageType)

	wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 0.8}
	require.NoError(t, r.CreateWithdrawal(ctx, wd), "0.1 + 0.7 must fund an exact 0.8 withdrawal")
	wallet, err = r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Zero(t, wallet.WithdrawableBalance)
	assert.Equal(t, 0.8, wallet.FrozenWithdrawAmount)

	require.NoError(t, r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawRejected, "refund"))
	wallet, err = r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0.8, wallet.WithdrawableBalance)
	assert.Zero(t, wallet.FrozenWithdrawAmount)

	var units struct {
		Withdrawable int64 `gorm:"column:withdrawable_balance_units"`
		Frozen       int64 `gorm:"column:frozen_withdraw_amount_units"`
	}
	require.NoError(t, r.db.Table("agent_wallets").Select(
		"withdrawable_balance_units, frozen_withdraw_amount_units",
	).Where("tenant_id = ?", 1).Take(&units).Error)
	assert.Equal(t, int64(80_000_000), units.Withdrawable)
	assert.Zero(t, units.Frozen)
}

func TestWithdrawalPaginationIndexesExist(t *testing.T) {
	r := newTestRepo(t)
	for _, indexName := range []string{
		"idx_agent_withdrawals_tenant_created_id",
		"idx_agent_withdrawals_status_created_id",
		"idx_agent_withdrawals_created_id",
	} {
		assert.True(t, r.db.Migrator().HasIndex(&withdrawalRow{}, indexName), indexName)
	}
	assert.True(t, r.db.Migrator().HasIndex(&earningRow{}, "idx_agent_earnings_source_lookup"))
	assert.True(t, r.db.Migrator().HasIndex(&earningRow{}, "idx_agent_earnings_idem_hash"))
	assert.False(t, r.db.Migrator().HasIndex(&earningRow{}, legacyEarningSourceIndex))
	assert.False(t, r.db.Migrator().HasIndex(&earningRow{}, legacyEarningIdemIndex))
}

func TestAppendEarningsBatch_SourceIDsUseExactCrossDatabaseSemantics(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	entries := []agent.EarningEntry{
		{TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment, SourceID: "Req-A", Amount: 0.1},
		{TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment, SourceID: "req-a", Amount: 0.7},
	}

	applied, err := r.AppendEarningsBatch(ctx, entries)
	require.NoError(t, err)
	assert.Equal(t, 2, applied)
	replayed, err := r.AppendEarningsBatch(ctx, entries)
	require.NoError(t, err)
	assert.Zero(t, replayed)

	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0.8, wallet.WithdrawableBalance)
	assert.Equal(t, 0.8, wallet.TotalEarned)
	var hashes []string
	require.NoError(t, r.db.Model(&earningRow{}).Order("id ASC").Pluck("idem_key_hash", &hashes).Error)
	require.Len(t, hashes, 2)
	assert.NotEqual(t, hashes[0], hashes[1])
}

func TestEarningRoundsOnceAtEightDecimalBoundary(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	applied, err := r.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment,
		SourceID: "one-unit", Amount: 0.000000006,
	})
	require.NoError(t, err)
	assert.True(t, applied)
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0.00000001, wallet.WithdrawableBalance)

	applied, err = r.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment,
		SourceID: "below-one-unit", Amount: 0.000000004,
	})
	assert.False(t, applied)
	assert.ErrorIs(t, err, agent.ErrEarningInvalid)
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
	// The dropped/recreated legacy table represents a pre-money-units database.
	// Remove both durable markers so the next startup performs the one-time
	// fixed-point and exact-idempotency imports.
	require.NoError(t, db.Delete(&agentSchemaMigrationRow{}, "key = ?", moneyUnitsMigrationKey).Error)
	require.NoError(t, db.Delete(&agentSchemaMigrationRow{}, "key = ?", exactKeyHashesMigrationKey).Error)

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
	var migratedEarning earningRow
	require.NoError(t, db.Take(&migratedEarning, "tenant_id = ?", 42).Error)
	require.NotNil(t, migratedEarning.IdemKeyHash)
	assert.Equal(t, entry.IdempotencyKey(), *migratedEarning.IdemKeyHash)
	assert.False(t, db.Migrator().HasIndex(&earningRow{}, legacyEarningSourceIndex))
	assert.False(t, db.Migrator().HasIndex(&earningRow{}, legacyEarningIdemIndex))
	wallet, err := repo.GetWallet(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, 1.25, wallet.WithdrawableBalance)
	assert.Equal(t, 1.25, wallet.TotalEarned)
	var migrated struct {
		WalletUnits int64 `gorm:"column:wallet_units"`
		EarnedUnits int64 `gorm:"column:earned_units"`
		LogUnits    int64 `gorm:"column:log_units"`
	}
	require.NoError(t, db.Raw(`SELECT
		w.withdrawable_balance_units AS wallet_units,
		w.total_earned_units AS earned_units,
		e.amount_units AS log_units
		FROM agent_wallets w JOIN agent_earning_logs e ON e.tenant_id = w.tenant_id
		WHERE w.tenant_id = ?`, 42).Scan(&migrated).Error)
	assert.Equal(t, int64(125_000_000), migrated.WalletUnits)
	assert.Equal(t, int64(125_000_000), migrated.EarnedUnits)
	assert.Equal(t, int64(125_000_000), migrated.LogUnits)

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

func TestAutoMigrateCopiesLegacyPayoutClaimsWithoutRawHashNamespaceCollision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&withdrawalRow{}, &legacyPayoutRefClaimRow{}))

	rawRef := "foo"
	rawHashLookingRef := exactStringHash(rawRef)
	createdAt := time.Now()
	require.NoError(t, db.Create(&withdrawalRow{
		ID: 10, TenantID: 1, Amount: 1, AmountUnits: moneyScaleUnits,
		Status: string(agent.WithdrawPaid), PayoutRef: rawRef, CreatedAt: createdAt,
	}).Error)
	require.NoError(t, db.Create(&withdrawalRow{
		ID: 11, TenantID: 1, Amount: 1, AmountUnits: moneyScaleUnits,
		Status: string(agent.WithdrawPaid), PayoutRef: rawHashLookingRef, CreatedAt: createdAt,
	}).Error)
	require.NoError(t, db.Create(&legacyPayoutRefClaimRow{
		PayoutRef: rawRef, WithdrawalID: 10, CreatedAt: createdAt,
	}).Error)
	require.NoError(t, db.Create(&legacyPayoutRefClaimRow{
		PayoutRef: rawHashLookingRef, WithdrawalID: 11, CreatedAt: createdAt,
	}).Error)

	require.NoError(t, AutoMigrate(db))
	var claims []payoutRefClaimRow
	require.NoError(t, db.Order("withdrawal_id ASC").Find(&claims).Error)
	require.Len(t, claims, 2)
	assert.Equal(t, rawRef, claims[0].PayoutRef)
	assert.Equal(t, exactStringHash(rawRef), claims[0].PayoutRefHash)
	assert.Equal(t, rawHashLookingRef, claims[1].PayoutRef)
	assert.Equal(t, exactStringHash(rawHashLookingRef), claims[1].PayoutRefHash)
	assert.NotEqual(t, claims[0].PayoutRefHash, claims[1].PayoutRefHash)
}

func TestAutoMigrateV4CanonicalizesWithdrawalHashesAndV2Claims(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	withdrawal := &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 10, Remark: "canonical migration", RequestKey: "request-key",
	}
	require.NoError(t, r.CreateWithdrawal(ctx, withdrawal))
	require.NoError(t, r.ResolveWithdrawal(ctx, withdrawal.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, r.MarkWithdrawalPaid(ctx, withdrawal.ID, "BANK-123"))

	wrongHash := strings.Repeat("0", 64)
	require.NoError(t, r.db.Model(&withdrawalRow{}).Where("id = ?", withdrawal.ID).
		Updates(map[string]interface{}{
			"request_key":      " request-key ",
			"request_key_hash": wrongHash,
			"payout_ref":       " BANK-123 ",
			"payout_ref_hash":  wrongHash,
		}).Error)
	require.NoError(t, r.db.Where("withdrawal_id = ?", withdrawal.ID).Delete(&payoutRefClaimRow{}).Error)
	require.NoError(t, r.db.AutoMigrate(&v2PayoutRefClaimRow{}))
	require.NoError(t, r.db.Create(&v2PayoutRefClaimRow{
		PayoutRefHash: wrongHash,
		PayoutRef:     " BANK-123 ",
		WithdrawalID:  withdrawal.ID,
		CreatedAt:     time.Now(),
	}).Error)
	require.NoError(t, r.db.Delete(&agentSchemaMigrationRow{}, "key = ?", exactKeyHashesMigrationKey).Error)

	require.NoError(t, AutoMigrate(r.db))
	require.NoError(t, AutoMigrate(r.db), "the completed v4 migration must be restart-idempotent")

	var stored withdrawalRow
	require.NoError(t, r.db.Take(&stored, "id = ?", withdrawal.ID).Error)
	require.NotNil(t, stored.RequestKey)
	require.NotNil(t, stored.RequestKeyHash)
	require.NotNil(t, stored.PayoutRefHash)
	assert.Equal(t, "request-key", *stored.RequestKey)
	assert.Equal(t, exactStringHash("request-key"), *stored.RequestKeyHash)
	assert.Equal(t, "BANK-123", stored.PayoutRef)
	assert.Equal(t, exactStringHash("BANK-123"), *stored.PayoutRefHash)

	var claim payoutRefClaimRow
	require.NoError(t, r.db.Take(&claim, "withdrawal_id = ?", withdrawal.ID).Error)
	assert.Equal(t, "BANK-123", claim.PayoutRef)
	assert.Equal(t, exactStringHash("BANK-123"), claim.PayoutRefHash)
	assert.True(t, r.db.Migrator().HasIndex(&payoutRefClaimRow{}, "idx_agent_payout_ref_claims_v3_withdrawal"))

	replay := &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 10, Remark: "canonical migration", RequestKey: " request-key ",
	}
	require.NoError(t, r.CreateWithdrawal(ctx, replay))
	assert.Equal(t, withdrawal.ID, replay.ID)

	seedBalance(t, r, 2, 6, 20)
	second := &agent.Withdrawal{TenantID: 2, UserID: 6, Amount: 10}
	require.NoError(t, r.CreateWithdrawal(ctx, second))
	require.NoError(t, r.ResolveWithdrawal(ctx, second.ID, agent.WithdrawApproved, "approved"))
	require.ErrorIs(t, r.MarkWithdrawalPaid(ctx, second.ID, " BANK-123 "), agent.ErrPayoutRefDuplicate)
}

func TestAutoMigrateV4RejectsCanonicalPayoutReferenceCollisionAtomically(t *testing.T) {
	r := newTestRepo(t)
	createdAt := time.Now()
	rows := []withdrawalRow{
		{TenantID: 1, Amount: 1, AmountUnits: moneyScaleUnits, Status: string(agent.WithdrawPaid), PayoutRef: "R", CreatedAt: createdAt},
		{TenantID: 2, Amount: 1, AmountUnits: moneyScaleUnits, Status: string(agent.WithdrawPaid), PayoutRef: " R ", CreatedAt: createdAt},
	}
	require.NoError(t, r.db.Create(&rows).Error)
	require.NoError(t, r.db.Delete(&agentSchemaMigrationRow{}, "key = ?", exactKeyHashesMigrationKey).Error)

	err := AutoMigrate(r.db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already associated")

	var stored []withdrawalRow
	require.NoError(t, r.db.Where("id IN ?", []int64{rows[0].ID, rows[1].ID}).Order("id ASC").Find(&stored).Error)
	require.Len(t, stored, 2)
	assert.Equal(t, "R", stored[0].PayoutRef)
	assert.Equal(t, " R ", stored[1].PayoutRef, "failed migration must roll back canonicalization")
	assert.Nil(t, stored[0].PayoutRefHash)
	assert.Nil(t, stored[1].PayoutRefHash)
	var markers int64
	require.NoError(t, r.db.Model(&agentSchemaMigrationRow{}).Where("key = ?", exactKeyHashesMigrationKey).Count(&markers).Error)
	assert.Zero(t, markers)
	var claims int64
	require.NoError(t, r.db.Model(&payoutRefClaimRow{}).Count(&claims).Error)
	assert.Zero(t, claims, "failed migration must roll back v3 claims")
}

func TestAutoMigrateV4RejectsOrphanV2PayoutClaim(t *testing.T) {
	r := newTestRepo(t)
	require.NoError(t, r.db.AutoMigrate(&v2PayoutRefClaimRow{}))
	require.NoError(t, r.db.Create(&v2PayoutRefClaimRow{
		PayoutRefHash: exactStringHash("orphan-ref"),
		PayoutRef:     "orphan-ref",
		WithdrawalID:  999,
		CreatedAt:     time.Now(),
	}).Error)
	require.NoError(t, r.db.Delete(&agentSchemaMigrationRow{}, "key = ?", exactKeyHashesMigrationKey).Error)

	err := AutoMigrate(r.db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no paid withdrawal 999")
	var markers int64
	require.NoError(t, r.db.Model(&agentSchemaMigrationRow{}).Where("key = ?", exactKeyHashesMigrationKey).Count(&markers).Error)
	assert.Zero(t, markers)
}

func TestCompatibilityTriggerKeepsLegacyMirrorWritesAuthoritative(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	applied, err := r.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment,
		SourceID: "authoritative-units", Amount: 0.8,
	})
	require.NoError(t, err)
	require.True(t, applied)

	// Simulate a rollback binary that knows only the legacy decimal column.
	// The compatibility trigger must convert that write into authoritative units
	// before the old statement commits.
	require.NoError(t, r.db.Model(&walletRow{}).Where("tenant_id = ?", 1).
		UpdateColumn("withdrawable_balance", 99.99).Error)

	require.NoError(t, AutoMigrate(r.db))
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 99.99, wallet.WithdrawableBalance)

	var stored walletRow
	require.NoError(t, r.db.Take(&stored, "tenant_id = ?", 1).Error)
	assert.Equal(t, int64(9_999_000_000), stored.WithdrawableBalanceUnits)
	assert.Equal(t, 99.99, stored.WithdrawableBalance)
}

func TestSQLiteCompatibilityTriggersCoverLegacyInsertsAndUpdates(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	require.NoError(t, r.EnsureWallet(ctx, 9, 99))

	// These are the exact statements an old binary can issue after a release
	// rollback: the expanded unit columns are omitted entirely.
	require.NoError(t, r.db.Exec(`INSERT INTO agent_earning_logs
		(tenant_id,user_id,source_type,source_id,idem_key,claim_id,amount,remark,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		9, 99, string(agent.SourceManualAdjustment), "legacy-earning", "legacy-earning-key", "legacy-claim",
		0.1, "", time.Now()).Error)
	require.NoError(t, r.db.Exec(`UPDATE agent_wallets
		SET withdrawable_balance=withdrawable_balance+?, total_earned=total_earned+?
		WHERE tenant_id=?`, 0.1, 0.1, 9).Error)
	require.NoError(t, r.db.Exec(`INSERT INTO agent_withdrawals
		(tenant_id,user_id,amount,status,created_at,updated_at) VALUES (?,?,?,?,?,?)`,
		9, 99, 0.1, string(agent.WithdrawPending), time.Now(), time.Now()).Error)

	var stored struct {
		WithdrawableUnits int64 `gorm:"column:withdrawable_units"`
		TotalUnits        int64 `gorm:"column:total_units"`
		EarningUnits      int64 `gorm:"column:earning_units"`
		WithdrawalUnits   int64 `gorm:"column:withdrawal_units"`
	}
	require.NoError(t, r.db.Raw(`SELECT
		w.withdrawable_balance_units AS withdrawable_units,
		w.total_earned_units AS total_units,
		e.amount_units AS earning_units,
		x.amount_units AS withdrawal_units
		FROM agent_wallets w
		JOIN agent_earning_logs e ON e.tenant_id=w.tenant_id
		JOIN agent_withdrawals x ON x.tenant_id=w.tenant_id
		WHERE w.tenant_id=?`, 9).Scan(&stored).Error)
	assert.Equal(t, int64(10_000_000), stored.WithdrawableUnits)
	assert.Equal(t, int64(10_000_000), stored.TotalUnits)
	assert.Equal(t, int64(10_000_000), stored.EarningUnits)
	assert.Equal(t, int64(10_000_000), stored.WithdrawalUnits)

	// Repeated migration only observes the durable claim; it neither rewrites
	// history nor changes the trigger-synchronized values.
	require.NoError(t, AutoMigrate(r.db))
	wallet, err := r.GetWallet(ctx, 9)
	require.NoError(t, err)
	assert.Equal(t, 0.1, wallet.WithdrawableBalance)
}

func TestSQLiteLegacyMigrationRejectsAmbiguousInt64Boundary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.Exec(`CREATE TABLE agent_wallets (
		tenant_id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL DEFAULT 0,
		api_balance DECIMAL(20,8) NOT NULL DEFAULT 0,
		withdrawable_balance DECIMAL(20,8) NOT NULL DEFAULT 0,
		frozen_withdraw_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
		total_earned DECIMAL(20,8) NOT NULL DEFAULT 0,
		updated_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO agent_wallets
		(tenant_id,withdrawable_balance,total_earned) VALUES (1,?,?)`,
		90_000_000_001.0, 90_000_000_001.0).Error)

	err = AutoMigrate(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside safe fixed-point migration range")
	var markers int64
	require.NoError(t, db.Table("agent_schema_migrations").Where("key = ?", moneyUnitsMigrationKey).Count(&markers).Error)
	assert.Zero(t, markers, "failed import must roll back its ownership marker")
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
	seedBalance(t, r, 1, 5, 0.8)

	wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 0.3}
	require.NoError(t, r.CreateWithdrawal(ctx, wd))
	require.NoError(t, r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawApproved, ""))

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

	assert.Equal(t, int64(1), wins)
	w, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0.5, w.WithdrawableBalance)
	assert.Zero(t, w.FrozenWithdrawAmount)

	var stored walletRow
	require.NoError(t, r.db.Take(&stored, "tenant_id = ?", 1).Error)
	assert.Equal(t, int64(50_000_000), stored.WithdrawableBalanceUnits)
	assert.Zero(t, stored.FrozenWithdrawAmountUnits)
}

func TestMarkWithdrawalPaid_PayoutReferenceCanBelongToOnlyOneWithdrawal(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	first := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	second := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 20}
	require.NoError(t, r.CreateWithdrawal(ctx, first))
	require.NoError(t, r.CreateWithdrawal(ctx, second))
	require.NoError(t, r.ResolveWithdrawal(ctx, first.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, r.ResolveWithdrawal(ctx, second.ID, agent.WithdrawApproved, "approved"))

	require.NoError(t, r.MarkWithdrawalPaid(ctx, first.ID, "bank-transfer-1"))
	require.ErrorIs(t, r.MarkWithdrawalPaid(ctx, second.ID, "bank-transfer-1"), agent.ErrPayoutRefDuplicate)

	gotFirst, err := r.GetWithdrawal(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.WithdrawPaid, gotFirst.Status)
	assert.Equal(t, "bank-transfer-1", gotFirst.PayoutRef)
	gotSecond, err := r.GetWithdrawal(ctx, second.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.WithdrawApproved, gotSecond.Status)
	assert.Empty(t, gotSecond.PayoutRef)

	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 50.0, wallet.WithdrawableBalance)
	assert.Equal(t, 20.0, wallet.FrozenWithdrawAmount, "duplicate reference must not debit the second withdrawal")
	var claim payoutRefClaimRow
	require.NoError(t, r.db.Take(&claim, "payout_ref_hash = ?", exactStringHash("bank-transfer-1")).Error)
	assert.Equal(t, first.ID, claim.WithdrawalID)
	assert.Equal(t, "bank-transfer-1", claim.PayoutRef)
}

func TestMarkWithdrawalPaid_PayoutReferencesUseExactCrossDatabaseSemantics(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	upper := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 20}
	lower := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 20}
	require.NoError(t, r.CreateWithdrawal(ctx, upper))
	require.NoError(t, r.CreateWithdrawal(ctx, lower))
	require.NoError(t, r.ResolveWithdrawal(ctx, upper.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, r.ResolveWithdrawal(ctx, lower.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, r.MarkWithdrawalPaid(ctx, upper.ID, "REF-A"))
	require.NoError(t, r.MarkWithdrawalPaid(ctx, lower.ID, "ref-a"))

	var claims []payoutRefClaimRow
	require.NoError(t, r.db.Order("withdrawal_id ASC").Find(&claims).Error)
	require.Len(t, claims, 2)
	assert.NotEqual(t, claims[0].PayoutRefHash, claims[1].PayoutRefHash)
	assert.ElementsMatch(t, []string{"REF-A", "ref-a"}, []string{claims[0].PayoutRef, claims[1].PayoutRef})
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 60.0, wallet.WithdrawableBalance)
	assert.Zero(t, wallet.FrozenWithdrawAmount)
}

func TestMarkWithdrawalPaid_RejectsReferenceFromPreClaimHistory(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	legacyPaid := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	current := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 20}
	require.NoError(t, r.CreateWithdrawal(ctx, legacyPaid))
	require.NoError(t, r.CreateWithdrawal(ctx, current))
	require.NoError(t, r.ResolveWithdrawal(ctx, legacyPaid.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, r.ResolveWithdrawal(ctx, current.ID, agent.WithdrawApproved, "approved"))

	// Simulate a paid record written before agent_payout_ref_claims was added.
	require.NoError(t, r.db.Model(&withdrawalRow{}).Where("id = ?", legacyPaid.ID).Updates(map[string]interface{}{
		"status": string(agent.WithdrawPaid), "payout_ref": "legacy-transfer",
	}).Error)
	require.ErrorIs(t, r.MarkWithdrawalPaid(ctx, current.ID, "legacy-transfer"), agent.ErrPayoutRefDuplicate)

	var claims int64
	require.NoError(t, r.db.Model(&payoutRefClaimRow{}).Count(&claims).Error)
	assert.Zero(t, claims)
	got, err := r.GetWithdrawal(ctx, current.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.WithdrawApproved, got.Status)
}

func TestMarkWithdrawalPaid_ConcurrentSameReferenceOnlyOneWithdrawalWins(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	first := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	second := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
	require.NoError(t, r.CreateWithdrawal(ctx, first))
	require.NoError(t, r.CreateWithdrawal(ctx, second))
	require.NoError(t, r.ResolveWithdrawal(ctx, first.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, r.ResolveWithdrawal(ctx, second.ID, agent.WithdrawApproved, "approved"))

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []int64{first.ID, second.ID} {
		wg.Add(1)
		go func(withdrawalID int64) {
			defer wg.Done()
			<-start
			errs <- r.MarkWithdrawalPaid(ctx, withdrawalID, "shared-transfer")
		}(id)
	}
	close(start)
	wg.Wait()
	close(errs)

	var paid, duplicate int
	for err := range errs {
		switch {
		case err == nil:
			paid++
		case errors.Is(err, agent.ErrPayoutRefDuplicate):
			duplicate++
		default:
			require.NoError(t, err)
		}
	}
	assert.Equal(t, 1, paid)
	assert.Equal(t, 1, duplicate)

	rows, err := r.ListWithdrawalsByTenant(ctx, 1)
	require.NoError(t, err)
	var paidRows, approvedRows int
	for _, row := range rows {
		switch row.Status {
		case agent.WithdrawPaid:
			paidRows++
			assert.Equal(t, "shared-transfer", row.PayoutRef)
		case agent.WithdrawApproved:
			approvedRows++
			assert.Empty(t, row.PayoutRef)
		}
	}
	assert.Equal(t, 1, paidRows)
	assert.Equal(t, 1, approvedRows)
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 40.0, wallet.WithdrawableBalance)
	assert.Equal(t, 30.0, wallet.FrozenWithdrawAmount)
}

func TestMarkWithdrawalPaid_FileSQLiteConcurrentReferenceMatrix(t *testing.T) {
	ctx := context.Background()
	r := newConcurrentSQLiteTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	first := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 20}
	second := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 20}
	require.NoError(t, r.CreateWithdrawal(ctx, first))
	require.NoError(t, r.CreateWithdrawal(ctx, second))
	require.NoError(t, r.ResolveWithdrawal(ctx, first.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, r.ResolveWithdrawal(ctx, second.ID, agent.WithdrawApproved, "approved"))

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, id := range []int64{first.ID, second.ID} {
		go func(withdrawalID int64) {
			<-start
			errs <- r.MarkWithdrawalPaid(ctx, withdrawalID, "shared-file-ref")
		}(id)
	}
	close(start)
	firstErr, secondErr := <-errs, <-errs
	assert.True(t, (firstErr == nil && errors.Is(secondErr, agent.ErrPayoutRefDuplicate)) ||
		(secondErr == nil && errors.Is(firstErr, agent.ErrPayoutRefDuplicate)),
		"different withdrawals/same ref must have one winner and one duplicate: %v / %v", firstErr, secondErr)

	third := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 20}
	require.NoError(t, r.CreateWithdrawal(ctx, third))
	require.NoError(t, r.ResolveWithdrawal(ctx, third.ID, agent.WithdrawApproved, "approved"))
	start = make(chan struct{})
	errs = make(chan error, 2)
	for _, ref := range []string{"same-id-ref-a", "same-id-ref-b"} {
		go func(payoutRef string) {
			<-start
			errs <- r.MarkWithdrawalPaid(ctx, third.ID, payoutRef)
		}(ref)
	}
	close(start)
	firstErr, secondErr = <-errs, <-errs
	assert.True(t, (firstErr == nil && errors.Is(secondErr, agent.ErrWithdrawNotApproved)) ||
		(secondErr == nil && errors.Is(firstErr, agent.ErrWithdrawNotApproved)),
		"same withdrawal/different refs must have one winner and one state conflict: %v / %v", firstErr, secondErr)

	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 40.0, wallet.WithdrawableBalance)
	assert.Equal(t, 20.0, wallet.FrozenWithdrawAmount)
}

func TestResolveWithdrawal_FileSQLiteConcurrentRejectReturnsStateConflict(t *testing.T) {
	ctx := context.Background()
	r := newConcurrentSQLiteTestRepo(t)
	seedBalance(t, r, 1, 5, 100)
	withdrawal := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 40}
	require.NoError(t, r.CreateWithdrawal(ctx, withdrawal))

	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errs <- r.ResolveWithdrawal(ctx, withdrawal.ID, agent.WithdrawRejected, "duplicate reject")
		}()
	}
	close(start)
	firstErr, secondErr := <-errs, <-errs
	assert.True(t, (firstErr == nil && errors.Is(secondErr, agent.ErrWithdrawNotPending)) ||
		(secondErr == nil && errors.Is(firstErr, agent.ErrWithdrawNotPending)),
		"concurrent reject must have one winner and one state conflict: %v / %v", firstErr, secondErr)

	stored, err := r.GetWithdrawal(ctx, withdrawal.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.WithdrawRejected, stored.Status)
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 100.0, wallet.WithdrawableBalance)
	assert.Zero(t, wallet.FrozenWithdrawAmount)
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

func TestCreateWithdrawal_IdempotentReplayReturnsOriginalCompleteSnapshot(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	original := &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 30, Remark: "cashout", RequestKey: "withdraw-request-1",
		PayoutMethod: agent.PayoutAlipay, PayoutAccount: "alice@example.com", PayoutName: "Alice",
	}
	require.NoError(t, r.CreateWithdrawal(ctx, original))
	require.NoError(t, r.ResolveWithdrawal(ctx, original.ID, agent.WithdrawApproved, "reviewed"))

	retry := &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 30, Remark: "cashout", RequestKey: "withdraw-request-1",
		// A retry must receive the first request's immutable payout snapshot even
		// if the caller supplies a newer server-side account snapshot.
		PayoutMethod: agent.PayoutBank, PayoutAccount: "changed", PayoutName: "Changed", PayoutBank: "Changed Bank",
	}
	require.NoError(t, r.CreateWithdrawal(ctx, retry))

	persisted, err := r.GetWithdrawal(ctx, original.ID)
	require.NoError(t, err)
	assert.Equal(t, persisted, retry)
	assert.Equal(t, "withdraw-request-1", retry.RequestKey)
	assert.Equal(t, agent.WithdrawApproved, retry.Status)
	assert.Equal(t, "reviewed", retry.Remark)
	assert.False(t, retry.ReviewedAt.IsZero())
	assert.Equal(t, agent.PayoutAlipay, retry.PayoutMethod)
	assert.Equal(t, "alice@example.com", retry.PayoutAccount)

	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 70.0, wallet.WithdrawableBalance)
	assert.Equal(t, 30.0, wallet.FrozenWithdrawAmount)
	var count int64
	require.NoError(t, r.db.Model(&withdrawalRow{}).Where("tenant_id = ?", 1).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCreateWithdrawal_IdempotencyConflictAndTenantScope(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)
	seedBalance(t, r, 2, 5, 100)

	require.NoError(t, r.CreateWithdrawal(ctx, &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 30, Remark: "cashout", RequestKey: "shared-key",
	}))
	tests := []struct {
		name string
		wd   agent.Withdrawal
	}{
		{name: "different user", wd: agent.Withdrawal{TenantID: 1, UserID: 6, Amount: 30, Remark: "cashout", RequestKey: "shared-key"}},
		{name: "different amount", wd: agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 31, Remark: "cashout", RequestKey: "shared-key"}},
		{name: "different remark", wd: agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30, Remark: "other", RequestKey: "shared-key"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := r.CreateWithdrawal(ctx, &test.wd)
			require.ErrorIs(t, err, agent.ErrWithdrawIdempotencyConflict)
		})
	}

	// Keys are tenant-scoped, so another tenant may independently reuse one.
	otherTenant := &agent.Withdrawal{TenantID: 2, UserID: 5, Amount: 30, Remark: "cashout", RequestKey: "shared-key"}
	require.NoError(t, r.CreateWithdrawal(ctx, otherTenant))
	assert.NotZero(t, otherTenant.ID)

	wallet1, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 70.0, wallet1.WithdrawableBalance, "conflicting retries must not freeze again")
	assert.Equal(t, 30.0, wallet1.FrozenWithdrawAmount)
	var count int64
	require.NoError(t, r.db.Model(&withdrawalRow{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

func TestCreateWithdrawal_RequestKeysUseExactCrossDatabaseSemantics(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	upper := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 10, RequestKey: "Key-A"}
	lower := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 10, RequestKey: "key-a"}
	require.NoError(t, r.CreateWithdrawal(ctx, upper))
	require.NoError(t, r.CreateWithdrawal(ctx, lower))
	assert.NotEqual(t, upper.ID, lower.ID)

	var rows []withdrawalRow
	require.NoError(t, r.db.Order("id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.NotNil(t, rows[0].RequestKeyHash)
	require.NotNil(t, rows[1].RequestKeyHash)
	assert.NotEqual(t, *rows[0].RequestKeyHash, *rows[1].RequestKeyHash)
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 80.0, wallet.WithdrawableBalance)
	assert.Equal(t, 20.0, wallet.FrozenWithdrawAmount)
}

func TestCreateWithdrawal_ReplaysByExactRawKeyWhenPersistedHashIsCorrupt(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	first := &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 30, Remark: "corrupt hash replay", RequestKey: "request-raw-fallback",
	}
	require.NoError(t, r.CreateWithdrawal(ctx, first))
	require.NoError(t, r.db.Model(&withdrawalRow{}).Where("id = ?", first.ID).
		Update("request_key_hash", strings.Repeat("0", 64)).Error)

	retry := &agent.Withdrawal{
		TenantID: 1, UserID: 5, Amount: 30, Remark: "corrupt hash replay", RequestKey: " request-raw-fallback ",
	}
	require.NoError(t, r.CreateWithdrawal(ctx, retry))
	assert.Equal(t, first.ID, retry.ID)
	var count int64
	require.NoError(t, r.db.Model(&withdrawalRow{}).Where("tenant_id = ?", 1).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 70.0, wallet.WithdrawableBalance)
	assert.Equal(t, 30.0, wallet.FrozenWithdrawAmount)
}

func TestCreateWithdrawal_EmptyRequestKeyRemainsBackwardCompatible(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	first := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 10}
	second := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 10}
	require.NoError(t, r.CreateWithdrawal(ctx, first))
	require.NoError(t, r.CreateWithdrawal(ctx, second))
	assert.NotEqual(t, first.ID, second.ID)

	var nullKeys int64
	require.NoError(t, r.db.Model(&withdrawalRow{}).Where("tenant_id = ? AND request_key IS NULL", 1).Count(&nullKeys).Error)
	assert.Equal(t, int64(2), nullKeys, "legacy requests must persist NULL so the unique index permits multiple withdrawals")
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 80.0, wallet.WithdrawableBalance)
	assert.Equal(t, 20.0, wallet.FrozenWithdrawAmount)
}

func TestCreateWithdrawal_ConcurrentSameKeyFreezesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	const workers = 32
	start := make(chan struct{})
	ids := make(chan int64, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			wd := &agent.Withdrawal{
				TenantID: 1, UserID: 5, Amount: 10, Remark: "cashout", RequestKey: "concurrent-key",
			}
			if err := r.CreateWithdrawal(ctx, wd); err != nil {
				errs <- err
				return
			}
			ids <- wd.ID
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		require.NoError(t, err)
	}
	var firstID int64
	var successes int
	for id := range ids {
		if firstID == 0 {
			firstID = id
		}
		assert.Equal(t, firstID, id)
		successes++
	}
	assert.Equal(t, workers, successes)
	assert.NotZero(t, firstID)

	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 90.0, wallet.WithdrawableBalance)
	assert.Equal(t, 10.0, wallet.FrozenWithdrawAmount)
	var count int64
	require.NoError(t, r.db.Model(&withdrawalRow{}).Where("tenant_id = ?", 1).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCreateWithdrawal_FileSQLiteConcurrentSameKeyFreezesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	r := newConcurrentSQLiteTestRepo(t)
	seedBalance(t, r, 1, 5, 100)

	const workers = 16
	start := make(chan struct{})
	ids := make(chan int64, workers)
	errs := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			withdrawal := &agent.Withdrawal{
				TenantID: 1, UserID: 5, Amount: 10, Remark: "file concurrency", RequestKey: "file-concurrent-key",
			}
			if err := r.CreateWithdrawal(ctx, withdrawal); err != nil {
				errs <- err
				return
			}
			ids <- withdrawal.ID
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		require.NoError(t, err, "SQLite must wait rather than leak SQLITE_BUSY")
	}
	var originalID int64
	for id := range ids {
		if originalID == 0 {
			originalID = id
		}
		assert.Equal(t, originalID, id)
	}
	wallet, err := r.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 90.0, wallet.WithdrawableBalance)
	assert.Equal(t, 10.0, wallet.FrozenWithdrawAmount)
}

func TestWithdrawalListsAreDeterministicallyPaginatedAndBounded(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	seedBalance(t, r, 1, 5, 150)
	fixedTime := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return fixedTime }

	for i := 0; i < 125; i++ {
		require.NoError(t, r.CreateWithdrawal(ctx, &agent.Withdrawal{
			TenantID: 1,
			UserID:   5,
			Amount:   1,
		}))
	}

	page, total, err := r.ListWithdrawalsByTenantPage(ctx, 1, 2, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(125), total)
	require.Len(t, page, 20)
	assert.Equal(t, int64(105), page[0].ID)
	assert.Equal(t, int64(86), page[len(page)-1].ID)

	capped, cappedTotal, err := r.ListWithdrawalsPage(ctx, string(agent.WithdrawPending), 1, 1_000)
	require.NoError(t, err)
	assert.Equal(t, int64(125), cappedTotal)
	assert.Len(t, capped, maxWithdrawalPageSize)
	assert.Equal(t, int64(125), capped[0].ID)
	assert.Equal(t, int64(26), capped[len(capped)-1].ID)

	legacy, err := r.ListWithdrawalsByTenant(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, legacy, maxWithdrawalPageSize, "legacy list helpers must no longer perform an unbounded query")
}

func TestLegacyWithdrawalListsKeepOlderActiveRowsVisible(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	base := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 105; i++ {
		require.NoError(t, r.db.Create(&withdrawalRow{
			TenantID: 1, UserID: 5, Amount: 1, AmountUnits: moneyScaleUnits,
			Status: string(agent.WithdrawPaid), CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}).Error)
	}
	for index, status := range []agent.WithdrawStatus{agent.WithdrawPending, agent.WithdrawApproved} {
		require.NoError(t, r.db.Create(&withdrawalRow{
			TenantID: 1, UserID: 5, Amount: 1, AmountUnits: moneyScaleUnits,
			Status: string(status), CreatedAt: base.Add(-time.Duration(index+1) * time.Hour),
		}).Error)
	}

	byTenant, err := r.ListWithdrawalsByTenant(ctx, 1)
	require.NoError(t, err)
	require.Len(t, byTenant, maxWithdrawalPageSize)
	assert.Equal(t, agent.WithdrawPending, byTenant[0].Status)
	assert.Equal(t, agent.WithdrawApproved, byTenant[1].Status)

	all, err := r.ListWithdrawals(ctx, "")
	require.NoError(t, err)
	require.Len(t, all, maxWithdrawalPageSize)
	assert.Equal(t, agent.WithdrawPending, all[0].Status)
	assert.Equal(t, agent.WithdrawApproved, all[1].Status)

	page, total, err := r.ListWithdrawalsPage(ctx, "", 1, maxWithdrawalPageSize)
	require.NoError(t, err)
	assert.Equal(t, int64(107), total)
	assert.NotContains(t, []agent.WithdrawStatus{page[0].Status, page[len(page)-1].Status}, agent.WithdrawPending,
		"the normal paginated endpoint must retain strict created_at ordering")
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

func TestWithdrawalWalletMutationFailsClosedAndRollsBackStatus(t *testing.T) {
	tests := []struct {
		name         string
		approveFirst bool
		removeWallet bool
		wantStatus   agent.WithdrawStatus
		mutate       func(context.Context, *Repo, int64) error
		resolve      func(context.Context, *Repo, int64) error
	}{
		{
			name:         "approve missing wallet",
			removeWallet: true,
			wantStatus:   agent.WithdrawPending,
			resolve: func(ctx context.Context, r *Repo, id int64) error {
				return r.ResolveWithdrawal(ctx, id, agent.WithdrawApproved, "approve")
			},
		},
		{
			name:       "approve insufficient frozen balance",
			wantStatus: agent.WithdrawPending,
			mutate: func(ctx context.Context, r *Repo, tenantID int64) error {
				return r.db.WithContext(ctx).Model(&walletRow{}).Where("tenant_id = ?", tenantID).
					Updates(map[string]interface{}{
						"frozen_withdraw_amount":       29,
						"frozen_withdraw_amount_units": int64(2_900_000_000),
					}).Error
			},
			resolve: func(ctx context.Context, r *Repo, id int64) error {
				return r.ResolveWithdrawal(ctx, id, agent.WithdrawApproved, "approve")
			},
		},
		{
			name:         "reject missing wallet",
			removeWallet: true,
			wantStatus:   agent.WithdrawPending,
			resolve: func(ctx context.Context, r *Repo, id int64) error {
				return r.ResolveWithdrawal(ctx, id, agent.WithdrawRejected, "reject")
			},
		},
		{
			name:       "reject insufficient frozen balance",
			wantStatus: agent.WithdrawPending,
			mutate: func(ctx context.Context, r *Repo, tenantID int64) error {
				return r.db.WithContext(ctx).Model(&walletRow{}).Where("tenant_id = ?", tenantID).
					Updates(map[string]interface{}{
						"frozen_withdraw_amount":       29,
						"frozen_withdraw_amount_units": int64(2_900_000_000),
					}).Error
			},
			resolve: func(ctx context.Context, r *Repo, id int64) error {
				return r.ResolveWithdrawal(ctx, id, agent.WithdrawRejected, "reject")
			},
		},
		{
			name:         "mark paid missing wallet",
			approveFirst: true,
			removeWallet: true,
			wantStatus:   agent.WithdrawApproved,
			resolve: func(ctx context.Context, r *Repo, id int64) error {
				return r.MarkWithdrawalPaid(ctx, id, "payout-1")
			},
		},
		{
			name:         "mark paid insufficient frozen balance",
			approveFirst: true,
			wantStatus:   agent.WithdrawApproved,
			mutate: func(ctx context.Context, r *Repo, tenantID int64) error {
				return r.db.WithContext(ctx).Model(&walletRow{}).Where("tenant_id = ?", tenantID).
					Updates(map[string]interface{}{
						"frozen_withdraw_amount":       29,
						"frozen_withdraw_amount_units": int64(2_900_000_000),
					}).Error
			},
			resolve: func(ctx context.Context, r *Repo, id int64) error {
				return r.MarkWithdrawalPaid(ctx, id, "payout-1")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			r := newTestRepo(t)
			seedBalance(t, r, 1, 5, 100)
			wd := &agent.Withdrawal{TenantID: 1, UserID: 5, Amount: 30}
			require.NoError(t, r.CreateWithdrawal(ctx, wd))
			if test.approveFirst {
				require.NoError(t, r.ResolveWithdrawal(ctx, wd.ID, agent.WithdrawApproved, "approved"))
			}
			if test.removeWallet {
				require.NoError(t, r.db.WithContext(ctx).Delete(&walletRow{}, "tenant_id = ?", wd.TenantID).Error)
			} else {
				require.NoError(t, test.mutate(ctx, r, wd.TenantID))
			}

			err := test.resolve(ctx, r, wd.ID)
			require.Error(t, err)
			assert.ErrorIs(t, err, errWalletInvariant)

			persisted, getErr := r.GetWithdrawal(ctx, wd.ID)
			require.NoError(t, getErr)
			assert.Equal(t, test.wantStatus, persisted.Status, "status CAS must roll back with the wallet failure")
			assert.Empty(t, persisted.PayoutRef)
			assert.True(t, persisted.PaidAt.IsZero())
			if test.wantStatus == agent.WithdrawPending {
				assert.True(t, persisted.ReviewedAt.IsZero())
				assert.Empty(t, persisted.Remark)
			}

			if !test.removeWallet {
				var frozenUnits int64
				require.NoError(t, r.db.Table("agent_wallets").Select("frozen_withdraw_amount_units").
					Where("tenant_id = ?", wd.TenantID).Scan(&frozenUnits).Error)
				assert.Equal(t, int64(2_900_000_000), frozenUnits, "failed transition must not debit or refund a partial amount")
			}
		})
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
	if err := r.CreateWithdrawal(ctx, &agent.Withdrawal{TenantID: 1, Amount: 10.999}); !errors.Is(err, agent.ErrWithdrawAmountInvalid) {
		t.Fatalf("sub-cent withdrawal = %v, want ErrWithdrawAmountInvalid", err)
	}
	w, _ := r.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 20 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("wallet moved: %+v", w)
	}
	var withdrawals int64
	require.NoError(t, r.db.Model(&withdrawalRow{}).Count(&withdrawals).Error)
	assert.Zero(t, withdrawals, "invalid precision must not create a withdrawal row")
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
