package model

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func billingProjectionTestSpec(key string, requestId string, userId int, channelId int) BillingProjectionSpec {
	return BillingProjectionSpec{
		ProjectionKey: key, DependencyRequestId: requestId, DependencyOperation: "request",
		LogEnabled: true, LogUserId: userId, LogUsername: "projection-user", LogCreatedAt: 7201,
		LogType: LogTypeConsume, LogContent: "projection", LogTokenName: "projection-token",
		LogModelName: "projection-model", LogQuota: 42, LogChannelId: channelId, LogTokenId: 9,
		LogGroup: "default", LogRequestId: requestId, LogOther: "{}",
		QuotaDataEnabled: true, QuotaDataNodeName: "projection-node",
		UserId: userId, UserUsedQuotaDelta: 42, UserRequestDelta: 1,
		ChannelId: channelId, ChannelQuotaDelta: 42,
	}
}

func TestBillingProjectionLogLookupUsesTimeAndStableKey(t *testing.T) {
	assert.Equal(t, "created_at = ? AND projection_key = ?", billingProjectionLogLookupCondition)
	dryRunDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
	require.NoError(t, err)
	row := &BillingProjectionOutbox{LogCreatedAt: 7201, ProjectionKey: "stable-projection-key"}
	var logRow Log
	statement := billingProjectionLogLookup(dryRunDB, row).Limit(1).Find(&logRow).Statement
	assert.Contains(t, statement.SQL.String(), "created_at")
	assert.Contains(t, statement.SQL.String(), "projection_key")
	require.Len(t, statement.Vars, 2)
	assert.EqualValues(t, row.LogCreatedAt, statement.Vars[0])
	assert.Equal(t, row.ProjectionKey, statement.Vars[1])
}

func seedAppliedProjectionDependency(t *testing.T, requestId string, userId int) {
	t.Helper()
	require.NoError(t, DB.Create(&BillingSettlementEvent{
		RequestId: requestId, Operation: "request", UserId: userId, FundingSource: "wallet",
		Status: BillingSettlementStatusFinalized, FinancialStatus: BillingSettlementFinancialApplied,
		CommissionStatus: BillingCommissionStatusNotApplicable, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error)
}

func TestSettlementProjectionWaitsForDeferredOrTerminalLifecycle(t *testing.T) {
	truncateTables(t)
	const requestId = "projection-lifecycle-gate"
	const userId, channelId = 507, 607
	require.NoError(t, DB.Create(&User{Id: userId, Username: "projection-lifecycle"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "projection-lifecycle"}).Error)
	require.NoError(t, DB.Create(&BillingSettlementEvent{
		RequestId: requestId, Operation: "request", UserId: userId, FundingSource: "wallet",
		Status: BillingSettlementStatusReserved, FinancialStatus: BillingSettlementFinancialApplied,
		CommissionStatus: BillingCommissionStatusBlocked, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error)
	spec := billingProjectionTestSpec(BillingProjectionKey(requestId, "request", "lifecycle"), requestId, userId, channelId)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := ensureBillingProjectionPendingTx(tx, spec)
		return err
	}))
	require.ErrorContains(t, ApplyBillingProjection(spec.ProjectionKey), "lifecycle is not terminal or deferred")
	require.NoError(t, DB.Model(&BillingSettlementEvent{}).Where("request_id = ?", requestId).
		Update("status", BillingSettlementStatusDeferred).Error)
	require.NoError(t, ApplyBillingProjection(spec.ProjectionKey))
}

func TestBillingProjectionRejectsPayloadDriftForStableKey(t *testing.T) {
	truncateTables(t)
	spec := billingProjectionTestSpec(BillingProjectionKey("projection-drift", "request", "initial"), "projection-drift", 501, 601)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := ensureBillingProjectionPendingTx(tx, spec)
		return err
	}))

	drifted := spec
	drifted.LogQuota++
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, ensureErr := ensureBillingProjectionPendingTx(tx, drifted)
		return ensureErr
	})
	require.ErrorContains(t, err, "does not match persisted payload")

	var persisted BillingProjectionOutbox
	require.NoError(t, DB.Where("projection_key = ?", spec.ProjectionKey).First(&persisted).Error)
	assert.Equal(t, spec.LogQuota, persisted.LogQuota)
}

func TestBillingProjectionRejectsPoisonedSinkRow(t *testing.T) {
	truncateTables(t)
	const userId, channelId = 506, 606
	require.NoError(t, DB.Create(&User{Id: userId, Username: "projection-user"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "projection-channel"}).Error)
	seedAppliedProjectionDependency(t, "projection-poison", userId)
	spec := billingProjectionTestSpec(BillingProjectionKey("projection-poison", "request", "initial"), "projection-poison", userId, channelId)
	spec.QuotaDataEnabled = false
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, ensureErr := ensureBillingProjectionPendingTx(tx, spec)
		return ensureErr
	}))
	key := spec.ProjectionKey
	require.NoError(t, DB.Create(&Log{
		UserId: spec.LogUserId, Username: spec.LogUsername, CreatedAt: spec.LogCreatedAt, Type: spec.LogType,
		Content: spec.LogContent, TokenName: spec.LogTokenName, ModelName: spec.LogModelName, Quota: spec.LogQuota,
		ChannelId: spec.LogChannelId, TokenId: spec.LogTokenId, Group: spec.LogGroup, RequestId: spec.LogRequestId,
		Other: spec.LogOther, PromptTokens: 1, ProjectionKey: &key,
	}).Error)

	require.ErrorContains(t, ApplyBillingProjection(spec.ProjectionKey), "log payload mismatch")
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	var outbox BillingProjectionOutbox
	require.NoError(t, DB.Where("projection_key = ?", spec.ProjectionKey).First(&outbox).Error)
	assert.Equal(t, BillingProjectionStatusPending, outbox.Status)
	assert.Equal(t, 1, outbox.AttemptCount)
}

func TestBillingProjectionSplitLogCrashReplaysExactlyOnce(t *testing.T) {
	truncateTables(t)
	oldLogDB := LOG_DB
	oldLogType := common.LogDatabaseType()
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	LOG_DB = logDB
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = oldLogDB
		common.SetLogDatabaseType(oldLogType)
	})

	const userId, channelId = 502, 602
	require.NoError(t, DB.Create(&User{Id: userId, Username: "projection-user"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "projection-channel"}).Error)
	seedAppliedProjectionDependency(t, "projection-split-crash", userId)
	spec := billingProjectionTestSpec(BillingProjectionKey("projection-split-crash", "request", "initial"), "projection-split-crash", userId, channelId)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, ensureErr := ensureBillingProjectionPendingTx(tx, spec)
		return ensureErr
	}))

	const callbackName = "test:projection_main_stats_crash"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			tx.AddError(errors.New("injected main projection crash"))
		}
	}))
	require.ErrorContains(t, ApplyBillingProjection(spec.ProjectionKey), "injected main projection crash")
	require.NoError(t, DB.Callback().Update().Remove(callbackName))

	var logCount int64
	require.NoError(t, logDB.Model(&Log{}).Where("projection_key = ?", spec.ProjectionKey).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount, "external log commit survives the main transaction rollback")
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	var quotaDataCount int64
	require.NoError(t, DB.Model(&QuotaData{}).Count(&quotaDataCount).Error)
	assert.Zero(t, quotaDataCount)
	var outbox BillingProjectionOutbox
	require.NoError(t, DB.Where("projection_key = ?", spec.ProjectionKey).First(&outbox).Error)
	assert.Equal(t, 1, outbox.AttemptCount)

	require.NoError(t, ApplyBillingProjection(spec.ProjectionKey))
	require.NoError(t, ApplyBillingProjection(spec.ProjectionKey))
	require.NoError(t, logDB.Model(&Log{}).Where("projection_key = ?", spec.ProjectionKey).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 42, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel Channel
	require.NoError(t, DB.First(&channel, channelId).Error)
	assert.Equal(t, int64(42), channel.UsedQuota)
	var quotaData QuotaData
	require.NoError(t, DB.First(&quotaData).Error)
	assert.Equal(t, 1, quotaData.Count)
	assert.Equal(t, 42, quotaData.Quota)
	require.NoError(t, DB.Where("projection_key = ?", spec.ProjectionKey).First(&outbox).Error)
	assert.Equal(t, 2, outbox.AttemptCount, "failed attempt and successful replay are both observable; applied no-op is not double-counted")
}

func TestAdjustmentDependentProjectionReconcilesOnlyAfterFinancialIntent(t *testing.T) {
	truncateTables(t)
	const userId, channelId = 505, 605
	require.NoError(t, DB.Create(&User{Id: userId, Username: "projection-user", Quota: 100}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "projection-channel"}).Error)
	adjustment := BillingAdjustmentSpec{
		RequestId: "projection-adjustment-order", Operation: "legacy-adjustment",
		UserId: userId, UserQuotaDelta: 10,
	}
	spec := billingProjectionTestSpec(BillingProjectionKey(adjustment.RequestId, adjustment.Operation, "legacy"), adjustment.RequestId, userId, channelId)
	spec.DependencyType = BillingProjectionDependencyAdjustment
	spec.DependencyOperation = adjustment.Operation
	spec.QuotaDataEnabled = false
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		if _, err := ensureBillingAdjustmentPendingTx(tx, adjustment); err != nil {
			return err
		}
		_, err := ensureBillingProjectionPendingTx(tx, spec)
		return err
	}))

	projected, err := ReconcilePendingBillingProjections(10)
	require.ErrorContains(t, err, "adjustment financial application is pending")
	assert.Zero(t, projected)
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Zero(t, user.UsedQuota)

	adjusted, err := ReconcilePendingBillingAdjustments(10)
	require.NoError(t, err)
	assert.Equal(t, 1, adjusted)
	projected, err = ReconcilePendingBillingProjections(10)
	require.NoError(t, err)
	assert.Equal(t, 1, projected)
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 110, user.Quota)
	assert.Equal(t, 42, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	adjusted, err = ReconcilePendingBillingAdjustments(10)
	require.NoError(t, err)
	assert.Zero(t, adjusted)
	projected, err = ReconcilePendingBillingProjections(10)
	require.NoError(t, err)
	assert.Zero(t, projected)
}

func TestBillingProjectionConcurrentClaimProtectsNonUniqueLogSink(t *testing.T) {
	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	mainDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "main.db")+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "clickhouse-sim.db")+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, mainDB.AutoMigrate(&User{}, &Channel{}, &BillingSettlementEvent{}, &BillingAdjustmentIntent{}, &BillingProjectionOutbox{}, &QuotaData{}))
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	require.NoError(t, logDB.Migrator().DropIndex(&Log{}, "idx_logs_projection_key"))
	mainSQL, err := mainDB.DB()
	require.NoError(t, err)
	mainSQL.SetMaxOpenConns(4)
	logSQL, err := logDB.DB()
	require.NoError(t, err)
	logSQL.SetMaxOpenConns(4)
	DB, LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeClickHouse)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		require.NoError(t, mainSQL.Close())
		require.NoError(t, logSQL.Close())
	})

	const userId, channelId = 503, 603
	require.NoError(t, DB.Create(&User{Id: userId, Username: "projection-user"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "projection-channel"}).Error)
	seedAppliedProjectionDependency(t, "projection-concurrent", userId)
	spec := billingProjectionTestSpec(BillingProjectionKey("projection-concurrent", "request", "initial"), "projection-concurrent", userId, channelId)
	spec.QuotaDataEnabled = false
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, ensureErr := ensureBillingProjectionPendingTx(tx, spec)
		return ensureErr
	}))

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- ApplyBillingProjection(spec.ProjectionKey)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for applyErr := range errs {
		if applyErr == nil {
			successes++
		}
	}
	assert.GreaterOrEqual(t, successes, 1)
	require.NoError(t, ApplyBillingProjection(spec.ProjectionKey), "a lock loser must be safely replayable")

	var logCount int64
	require.NoError(t, logDB.Model(&Log{}).Where("projection_key = ?", spec.ProjectionKey).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount, "the main claim serializes a ClickHouse-compatible sink even without a unique index")
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 42, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel Channel
	require.NoError(t, DB.First(&channel, channelId).Error)
	assert.Equal(t, int64(42), channel.UsedQuota)
}
