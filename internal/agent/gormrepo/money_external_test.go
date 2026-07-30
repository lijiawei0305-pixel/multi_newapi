package gormrepo

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	financialreportrepo "github.com/QuantumNous/new-api/internal/report/reportrepo"
)

type externalLegacyWalletRow struct {
	TenantID             int64     `gorm:"column:tenant_id;primaryKey"`
	UserID               int64     `gorm:"column:user_id;not null;default:0"`
	APIBalance           float64   `gorm:"column:api_balance;type:decimal(20,8);not null;default:0"`
	WithdrawableBalance  float64   `gorm:"column:withdrawable_balance;type:decimal(20,8);not null;default:0"`
	FrozenWithdrawAmount float64   `gorm:"column:frozen_withdraw_amount;type:decimal(20,8);not null;default:0"`
	TotalEarned          float64   `gorm:"column:total_earned;type:decimal(20,8);not null;default:0"`
	UpdatedAt            time.Time `gorm:"column:updated_at"`
}

func (externalLegacyWalletRow) TableName() string { return "agent_wallets" }

type externalLegacyEarningRow struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID   int64     `gorm:"column:tenant_id;not null;index:idx_agent_earnings_tenant;uniqueIndex:idx_agent_earnings_source,priority:1"`
	UserID     int64     `gorm:"column:user_id;not null;default:0"`
	SourceType string    `gorm:"column:source_type;type:varchar(32);not null;uniqueIndex:idx_agent_earnings_source,priority:2"`
	SourceID   string    `gorm:"column:source_id;type:varchar(128);not null;uniqueIndex:idx_agent_earnings_source,priority:3"`
	IdemKey    string    `gorm:"column:idem_key;type:varchar(200);not null;uniqueIndex:idx_agent_earnings_idem"`
	ClaimID    string    `gorm:"column:claim_id;type:varchar(64);not null;default:''"`
	Amount     float64   `gorm:"column:amount;type:decimal(20,8);not null"`
	Remark     string    `gorm:"column:remark;type:varchar(255);not null;default:''"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (externalLegacyEarningRow) TableName() string { return "agent_earning_logs" }

type externalLegacyWithdrawalRow struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID      int64      `gorm:"column:tenant_id;not null;index:idx_agent_withdrawals_tenant"`
	UserID        int64      `gorm:"column:user_id;not null;default:0"`
	Amount        float64    `gorm:"column:amount;type:decimal(20,8);not null"`
	Status        string     `gorm:"column:status;type:varchar(16);not null;default:pending;index:idx_agent_withdrawals_status"`
	Remark        string     `gorm:"column:remark;type:varchar(255);not null;default:''"`
	PayoutMethod  string     `gorm:"column:payout_method;type:varchar(16);not null;default:''"`
	PayoutAccount string     `gorm:"column:payout_account;type:varchar(128);not null;default:''"`
	PayoutName    string     `gorm:"column:payout_name;type:varchar(64);not null;default:''"`
	PayoutBank    string     `gorm:"column:payout_bank;type:varchar(128);not null;default:''"`
	PayoutRef     string     `gorm:"column:payout_ref;type:varchar(128);not null;default:''"`
	PaidAt        *time.Time `gorm:"column:paid_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
	ReviewedAt    *time.Time `gorm:"column:reviewed_at"`
}

func (externalLegacyWithdrawalRow) TableName() string { return "agent_withdrawals" }

// TestAgentMoneyExternalDialect owns only the agent_* tables in a disposable
// *_test database. CI runs it against MySQL with both affected-row modes and
// once against PostgreSQL, protecting the real trigger/DDL/RowsAffected rules
// that a SQLite-only test cannot model.
func TestAgentMoneyExternalDialect(t *testing.T) {
	if os.Getenv("TEST_AGENT_DB_ALLOW_DROP") != "1" {
		t.Skip("set TEST_AGENT_DB_ALLOW_DROP=1 with a disposable *_test database")
	}
	dialect := os.Getenv("TEST_AGENT_DB_DIALECT")
	dsn := os.Getenv("TEST_AGENT_DB_DSN")
	require.NotEmpty(t, dsn)

	var dialector gorm.Dialector
	switch dialect {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "postgres":
		dialector = postgres.Open(dsn)
	default:
		t.Fatalf("unsupported TEST_AGENT_DB_DIALECT %q", dialect)
	}
	previousMainDatabaseType := common.MainDatabaseType()
	if dialect == "mysql" {
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	} else {
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	}
	t.Cleanup(func() { common.SetMainDatabaseType(previousMainDatabaseType) })
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true, PrepareStmt: true})
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(strings.ToLower(db.Migrator().CurrentDatabase()), "_test"),
		"refusing destructive fixture outside a *_test database")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	dropTables := func() {
		require.NoError(t, db.Migrator().DropTable(
			&payoutRefClaimRow{}, &v2PayoutRefClaimRow{}, &legacyPayoutRefClaimRow{}, &withdrawalRow{}, &earningRow{}, &walletRow{}, &profileRow{}, &agentSchemaMigrationRow{},
		))
	}
	dropTables()
	t.Cleanup(dropTables)

	require.NoError(t, db.AutoMigrate(
		&profileRow{}, &externalLegacyWalletRow{}, &externalLegacyEarningRow{}, &externalLegacyWithdrawalRow{},
	))
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&externalLegacyWalletRow{
		TenantID: 1, UserID: 11, WithdrawableBalance: 0.5, FrozenWithdrawAmount: 0.3, TotalEarned: 0.8, UpdatedAt: now,
	}).Error)
	for index, amount := range []float64{0.1, 0.7} {
		require.NoError(t, db.Create(&externalLegacyEarningRow{
			TenantID: 1, UserID: 11, SourceType: string(agent.SourceManualAdjustment),
			SourceID: "legacy-decimal-" + string(rune('a'+index)), IdemKey: "legacy-idem-" + string(rune('a'+index)),
			Amount: amount, CreatedAt: now,
		}).Error)
	}
	require.NoError(t, db.Create(&externalLegacyWithdrawalRow{
		TenantID: 1, UserID: 11, Amount: 0.3, Status: string(agent.WithdrawPending), CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&externalLegacyWalletRow{
		TenantID: 2, UserID: 22, WithdrawableBalance: 12_345.67890123, TotalEarned: 12_345.67890123, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&externalLegacyEarningRow{
		TenantID: 2, UserID: 22, SourceType: string(agent.SourceManualAdjustment), SourceID: "legacy-large",
		IdemKey: "legacy-large-idem", Amount: 12_345.67890123, CreatedAt: now,
	}).Error)

	require.NoError(t, AutoMigrate(db))
	repo := New(db)
	ctx := context.Background()
	wallet, err := repo.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0.5, wallet.WithdrawableBalance)
	assert.Equal(t, 0.3, wallet.FrozenWithdrawAmount)
	assert.Equal(t, 0.8, wallet.TotalEarned)
	largeWallet, err := repo.GetWallet(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, 12_345.67890123, largeWallet.TotalEarned)

	var migrated struct {
		EarningSum int64 `gorm:"column:earning_sum"`
		LargeUnits int64 `gorm:"column:large_units"`
		Withdraw   int64 `gorm:"column:withdraw_units"`
	}
	require.NoError(t, db.Raw(`SELECT
		(SELECT SUM(amount_units) FROM agent_earning_logs WHERE tenant_id=1) AS earning_sum,
		(SELECT total_earned_units FROM agent_wallets WHERE tenant_id=2) AS large_units,
		(SELECT amount_units FROM agent_withdrawals WHERE tenant_id=1) AS withdraw_units`).Scan(&migrated).Error)
	assert.Equal(t, int64(80_000_000), migrated.EarningSum)
	assert.Equal(t, int64(1_234_567_890_123), migrated.LargeUnits)
	assert.Equal(t, int64(30_000_000), migrated.Withdraw)

	// Exercise reportrepo against the real server dialect after the migration
	// marker commits. These assertions protect SQL SUM/scanning of BIGINT units,
	// not merely the repository's single-row conversion path. Deliberately
	// diverge the legacy mirrors first so a silent fallback cannot pass.
	reportEarningTrigger := "trg_agent_earning_logs_money_" + moneyTriggerVersion
	reportWalletTrigger := "trg_agent_wallets_money_" + moneyTriggerVersion
	if dialect == "mysql" {
		reportEarningTrigger += "_bu"
		reportWalletTrigger += "_bu"
		require.NoError(t, execMoneyDDL(db, "DROP TRIGGER "+reportEarningTrigger))
		require.NoError(t, execMoneyDDL(db, "DROP TRIGGER "+reportWalletTrigger))
	} else {
		reportEarningTrigger += "_biu"
		reportWalletTrigger += "_biu"
		require.NoError(t, execMoneyDDL(db, "DROP TRIGGER "+reportEarningTrigger+" ON agent_earning_logs"))
		require.NoError(t, execMoneyDDL(db, "DROP TRIGGER "+reportWalletTrigger+" ON agent_wallets"))
	}
	require.NoError(t, db.Exec(`UPDATE agent_earning_logs SET amount=amount+100 WHERE tenant_id=1`).Error)
	require.NoError(t, db.Exec(`UPDATE agent_wallets SET api_balance=91, withdrawable_balance=92,
		frozen_withdraw_amount=93, total_earned=94 WHERE tenant_id=1`).Error)
	reports := financialreportrepo.New(db)
	tenantID := int64(1)
	start, end := now.Add(-time.Hour).Unix(), now.Add(time.Hour).Unix()
	earnings, err := reports.SummaryEarnings(ctx, &tenantID, start, end)
	require.NoError(t, err)
	require.Len(t, earnings, 1)
	assert.Equal(t, 0.8, earnings[0].AmountCNY)
	trend, err := reports.TrendEarnings(ctx, &tenantID, start, end, "day")
	require.NoError(t, err)
	require.Len(t, trend, 1)
	assert.Equal(t, 0.8, trend[0].AmountCNY)
	walletTotals, err := reports.WalletTotals(ctx, &tenantID)
	require.NoError(t, err)
	assert.Equal(t, 0.5, walletTotals.WithdrawableCNY)
	assert.Equal(t, 0.3, walletTotals.FrozenCNY)
	assert.Equal(t, 0.8, walletTotals.TotalEarnedCNY)
	assert.Zero(t, walletTotals.APIBalanceUSD)
	require.NoError(t, AutoMigrate(db), "repeat migration must restore the deliberately removed triggers")

	// Runtime fixed-point behavior: the MySQL case specifically proves that a
	// 1e-8 update changes the decimal mirror and is not rolled back as a no-op.
	for index, amount := range []float64{0.1, 0.7, 0.00000001} {
		applied, appendErr := repo.AppendEarning(ctx, agent.EarningEntry{
			TenantID: 3, UserID: 33, SourceType: agent.SourceManualAdjustment,
			SourceID: "runtime-" + string(rune('a'+index)), Amount: amount,
		})
		require.NoError(t, appendErr)
		assert.True(t, applied)
	}
	runtimeWallet, err := repo.GetWallet(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, 0.80000001, runtimeWallet.TotalEarned)
	var runtimeStored walletRow
	require.NoError(t, db.Take(&runtimeStored, "tenant_id = ?", 3).Error)
	assert.Equal(t, int64(80_000_001), runtimeStored.TotalEarnedUnits)
	assert.Equal(t, 0.80000001, runtimeStored.TotalEarned)

	// Old-binary statements omit every unit column. Triggers must make them
	// visible to the current repository immediately.
	require.NoError(t, db.Exec(`INSERT INTO agent_earning_logs
		(tenant_id,user_id,source_type,source_id,idem_key,claim_id,amount,remark,created_at)
		VALUES (3,33,'manual_adjustment','old-runtime','old-runtime-idem','old-runtime-claim',0.20,'',?)`, now).Error)
	require.NoError(t, db.Exec(`UPDATE agent_wallets SET
		withdrawable_balance=withdrawable_balance+0.20,
		total_earned=total_earned+0.20 WHERE tenant_id=3`).Error)
	require.NoError(t, db.Exec(`INSERT INTO agent_withdrawals
		(tenant_id,user_id,amount,status,created_at,updated_at)
		VALUES (3,33,0.20,'pending',?,?)`, now, now).Error)
	runtimeWallet, err = repo.GetWallet(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, 1.00000001, runtimeWallet.TotalEarned)
	var oldUnits int64
	require.NoError(t, db.Table("agent_withdrawals").Select("amount_units").Where("tenant_id = ?", 3).Scan(&oldUnits).Error)
	assert.Equal(t, int64(20_000_000), oldUnits)

	// Remove only the wallet UPDATE trigger and create a deliberate mismatch.
	// Repeated migration must reinstall the trigger but must not re-import or
	// rewrite history, even with MySQL clientFoundRows=true.
	triggerName := "trg_agent_wallets_money_" + moneyTriggerVersion
	if dialect == "mysql" {
		triggerName += "_bu"
		require.NoError(t, execMoneyDDL(db, "DROP TRIGGER "+triggerName))
	} else {
		triggerName += "_biu"
		require.NoError(t, execMoneyDDL(db, "DROP TRIGGER "+triggerName+" ON agent_wallets"))
	}
	require.NoError(t, db.Exec(`UPDATE agent_wallets SET withdrawable_balance=99.99 WHERE tenant_id=3`).Error)
	beforeUnits := runtimeStored.WithdrawableBalanceUnits
	require.NoError(t, db.Table("agent_wallets").Select("withdrawable_balance_units").Where("tenant_id = ?", 3).Scan(&beforeUnits).Error)
	runtimeTenantID := int64(3)
	walletTotals, err = reports.WalletTotals(ctx, &runtimeTenantID)
	require.NoError(t, err)
	assert.Equal(t, moneyAmount(beforeUnits), walletTotals.WithdrawableCNY,
		"reports must remain on authoritative units when a legacy mirror diverges")
	require.NoError(t, AutoMigrate(db))
	var after walletRow
	require.NoError(t, db.Take(&after, "tenant_id = ?", 3).Error)
	assert.Equal(t, beforeUnits, after.WithdrawableBalanceUnits)
	assert.Equal(t, 99.99, after.WithdrawableBalance, "existing marker must not launch a full-table repair")
	require.NoError(t, db.Exec(`UPDATE agent_wallets SET withdrawable_balance_units=withdrawable_balance_units WHERE tenant_id=3`).Error)
	require.NoError(t, db.Take(&after, "tenant_id = ?", 3).Error)
	assert.Equal(t, moneyAmount(after.WithdrawableBalanceUnits), after.WithdrawableBalance)

	// Exact hashes make request/payout idempotency independent of MySQL's
	// commonly case-insensitive VARCHAR collation while retaining byte-exact
	// semantics on PostgreSQL.
	_, err = repo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 4, UserID: 44, SourceType: agent.SourceManualAdjustment, SourceID: "withdraw-seed", Amount: 100,
	})
	require.NoError(t, err)
	upperKey := &agent.Withdrawal{TenantID: 4, UserID: 44, Amount: 10, RequestKey: "Key-A"}
	lowerKey := &agent.Withdrawal{TenantID: 4, UserID: 44, Amount: 10, RequestKey: "key-a"}
	require.NoError(t, repo.CreateWithdrawal(ctx, upperKey))
	require.NoError(t, repo.CreateWithdrawal(ctx, lowerKey))
	assert.NotEqual(t, upperKey.ID, lowerKey.ID)
	require.NoError(t, repo.ResolveWithdrawal(ctx, upperKey.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, repo.ResolveWithdrawal(ctx, lowerKey.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, repo.MarkWithdrawalPaid(ctx, upperKey.ID, "REF-A"))
	require.NoError(t, repo.MarkWithdrawalPaid(ctx, lowerKey.ID, "ref-a"))

	// Real multi-connection request-key contention must freeze exactly once and
	// replay the same row on every loser.
	_, err = repo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 5, UserID: 55, SourceType: agent.SourceManualAdjustment, SourceID: "idem-seed", Amount: 100,
	})
	require.NoError(t, err)
	const workers = 8
	startConcurrent := make(chan struct{})
	requestIDs := make(chan int64, workers)
	requestErrors := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-startConcurrent
			withdrawal := &agent.Withdrawal{TenantID: 5, UserID: 55, Amount: 10, RequestKey: "external-concurrent-key"}
			if createErr := repo.CreateWithdrawal(ctx, withdrawal); createErr != nil {
				requestErrors <- createErr
				return
			}
			requestIDs <- withdrawal.ID
		}()
	}
	close(startConcurrent)
	waitGroup.Wait()
	close(requestErrors)
	close(requestIDs)
	for requestErr := range requestErrors {
		require.NoError(t, requestErr)
	}
	var firstRequestID int64
	requestCount := 0
	for requestID := range requestIDs {
		if firstRequestID == 0 {
			firstRequestID = requestID
		}
		assert.Equal(t, firstRequestID, requestID)
		requestCount++
	}
	assert.Equal(t, workers, requestCount)
	idempotentWallet, err := repo.GetWallet(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, 90.0, idempotentWallet.WithdrawableBalance)
	assert.Equal(t, 10.0, idempotentWallet.FrozenWithdrawAmount)

	// Claim contention exercises PostgreSQL READ COMMITTED and MySQL REPEATABLE
	// READ/current-lock behavior rather than SQLite's single-writer shortcut.
	_, err = repo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 6, UserID: 66, SourceType: agent.SourceManualAdjustment, SourceID: "claim-seed", Amount: 100,
	})
	require.NoError(t, err)
	claimA := &agent.Withdrawal{TenantID: 6, UserID: 66, Amount: 20}
	claimB := &agent.Withdrawal{TenantID: 6, UserID: 66, Amount: 20}
	require.NoError(t, repo.CreateWithdrawal(ctx, claimA))
	require.NoError(t, repo.CreateWithdrawal(ctx, claimB))
	require.NoError(t, repo.ResolveWithdrawal(ctx, claimA.ID, agent.WithdrawApproved, "approved"))
	require.NoError(t, repo.ResolveWithdrawal(ctx, claimB.ID, agent.WithdrawApproved, "approved"))
	startConcurrent = make(chan struct{})
	claimErrors := make(chan error, 2)
	for _, withdrawalID := range []int64{claimA.ID, claimB.ID} {
		go func(id int64) {
			<-startConcurrent
			claimErrors <- repo.MarkWithdrawalPaid(ctx, id, "external-shared-ref")
		}(withdrawalID)
	}
	close(startConcurrent)
	firstClaimErr, secondClaimErr := <-claimErrors, <-claimErrors
	assert.True(t, (firstClaimErr == nil && errors.Is(secondClaimErr, agent.ErrPayoutRefDuplicate)) ||
		(secondClaimErr == nil && errors.Is(firstClaimErr, agent.ErrPayoutRefDuplicate)),
		"claim race must have one winner and one duplicate: %v / %v", firstClaimErr, secondClaimErr)

	claimC := &agent.Withdrawal{TenantID: 6, UserID: 66, Amount: 20}
	require.NoError(t, repo.CreateWithdrawal(ctx, claimC))
	require.NoError(t, repo.ResolveWithdrawal(ctx, claimC.ID, agent.WithdrawApproved, "approved"))
	startConcurrent = make(chan struct{})
	claimErrors = make(chan error, 2)
	for _, payoutRef := range []string{"external-ref-a", "external-ref-b"} {
		go func(ref string) {
			<-startConcurrent
			claimErrors <- repo.MarkWithdrawalPaid(ctx, claimC.ID, ref)
		}(payoutRef)
	}
	close(startConcurrent)
	firstClaimErr, secondClaimErr = <-claimErrors, <-claimErrors
	assert.True(t, (firstClaimErr == nil && errors.Is(secondClaimErr, agent.ErrWithdrawNotApproved)) ||
		(secondClaimErr == nil && errors.Is(firstClaimErr, agent.ErrWithdrawNotApproved)),
		"same-withdrawal race must have one winner and one state conflict: %v / %v", firstClaimErr, secondClaimErr)

	_, err = repo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 7, UserID: 77, SourceType: agent.SourceManualAdjustment, SourceID: "review-seed", Amount: 100,
	})
	require.NoError(t, err)
	reviewed := &agent.Withdrawal{TenantID: 7, UserID: 77, Amount: 40}
	require.NoError(t, repo.CreateWithdrawal(ctx, reviewed))
	startConcurrent = make(chan struct{})
	reviewErrors := make(chan error, 2)
	for range 2 {
		go func() {
			<-startConcurrent
			reviewErrors <- repo.ResolveWithdrawal(ctx, reviewed.ID, agent.WithdrawRejected, "concurrent reject")
		}()
	}
	close(startConcurrent)
	firstReviewErr, secondReviewErr := <-reviewErrors, <-reviewErrors
	assert.True(t, (firstReviewErr == nil && errors.Is(secondReviewErr, agent.ErrWithdrawNotPending)) ||
		(secondReviewErr == nil && errors.Is(firstReviewErr, agent.ErrWithdrawNotPending)),
		"review race must have one winner and one state conflict: %v / %v", firstReviewErr, secondReviewErr)

	for index, sourceID := range []string{"Req-A", "req-a"} {
		applied, appendErr := repo.AppendEarning(ctx, agent.EarningEntry{
			TenantID: 8, UserID: 88, SourceType: agent.SourceManualAdjustment,
			SourceID: sourceID, Amount: []float64{0.1, 0.7}[index],
		})
		require.NoError(t, appendErr)
		assert.True(t, applied)
	}
	exactEarningWallet, err := repo.GetWallet(ctx, 8)
	require.NoError(t, err)
	assert.Equal(t, 0.8, exactEarningWallet.TotalEarned,
		"case-distinct source IDs must not collapse under the database collation")
	assert.False(t, db.Migrator().HasIndex(&earningRow{}, legacyEarningSourceIndex))
	assert.False(t, db.Migrator().HasIndex(&earningRow{}, legacyEarningIdemIndex))
	assert.True(t, db.Migrator().HasIndex(&earningRow{}, "idx_agent_earnings_idem_hash"))

	columnTypes, err := db.Migrator().ColumnTypes(&walletRow{})
	require.NoError(t, err)
	foundUnits := false
	for _, column := range columnTypes {
		if column.Name() != "withdrawable_balance_units" {
			continue
		}
		foundUnits = true
		nullable, ok := column.Nullable()
		require.True(t, ok)
		assert.True(t, nullable, "unit columns must stay nullable for PostgreSQL 9.6 fast-add compatibility")
	}
	assert.True(t, foundUnits)
}
