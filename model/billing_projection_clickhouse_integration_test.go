package model

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
)

// TestBillingProjectionRealClickHouseCrashReplay is intentionally gated: the
// configured database is disposable because the test drops/recreates `logs`.
// Example:
//
//	TEST_CLICKHOUSE_DSN=clickhouse://default@127.0.0.1:9000/projection_test TEST_CLICKHOUSE_ALLOW_DROP=1 go test ./model -run TestBillingProjectionRealClickHouseCrashReplay -count=1
func TestBillingProjectionRealClickHouseCrashReplay(t *testing.T) {
	dsn := os.Getenv("TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TEST_CLICKHOUSE_DSN is not set to a disposable ClickHouse database")
	}
	if os.Getenv("TEST_CLICKHOUSE_ALLOW_DROP") != "1" {
		t.Skip("TEST_CLICKHOUSE_ALLOW_DROP=1 is required for the destructive ClickHouse fixture")
	}
	normalizedDSN, err := normalizeClickHouseDSN(dsn)
	require.NoError(t, err)
	clickHouseDB, err := gorm.Open(clickhouse.Open(normalizedDSN), &gorm.Config{PrepareStmt: false})
	require.NoError(t, err)
	var databaseName string
	require.NoError(t, clickHouseDB.Raw("SELECT currentDatabase()").Scan(&databaseName).Error)
	require.True(t, strings.HasSuffix(databaseName, "_test"), "destructive ClickHouse fixture requires an isolated *_test database, got %q", databaseName)
	require.NoError(t, clickHouseDB.Exec("DROP TABLE IF EXISTS logs").Error)
	t.Cleanup(func() { _ = clickHouseDB.Exec("DROP TABLE IF EXISTS logs").Error })

	mainDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	mainSQL, err := mainDB.DB()
	require.NoError(t, err)
	mainSQL.SetMaxOpenConns(1)
	require.NoError(t, mainDB.AutoMigrate(&User{}, &Channel{}, &BillingSettlementEvent{}, &BillingAdjustmentIntent{}, &BillingProjectionOutbox{}, &QuotaData{}))

	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = mainDB, clickHouseDB
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeClickHouse)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		_ = mainSQL.Close()
	})
	require.NoError(t, migrateClickHouseLogDB())

	var projectionColumnCount int64
	require.NoError(t, LOG_DB.Raw(`SELECT count() FROM system.columns WHERE database = currentDatabase() AND table = 'logs' AND name = 'projection_key'`).Scan(&projectionColumnCount).Error)
	assert.Equal(t, int64(1), projectionColumnCount)
	var createTableSQL string
	require.NoError(t, LOG_DB.Raw("SHOW CREATE TABLE logs").Scan(&createTableSQL).Error)
	assert.Contains(t, createTableSQL, "non_replicated_deduplication_window = 1000000")
	assert.Contains(t, createTableSQL, "idx_logs_projection_key")
	assert.Contains(t, createTableSQL, "bloom_filter(0.001)")

	// Treat the first successful call as a lost acknowledgement and retry the
	// exact insert with the same stable token. This deliberately bypasses the
	// application's read-before-insert check so the assertion proves that the
	// ClickHouse sink itself rejects the duplicate block.
	lostAckKey := BillingProjectionKey("projection-real-clickhouse-lost-ack", "request", "initial")
	lostAckLog := &Log{
		UserId: 504, Username: "clickhouse-user", CreatedAt: 7_201, Type: LogTypeConsume,
		Content: "lost acknowledgement", ModelName: "clickhouse-model", Quota: 7,
		RequestId: "clickhouse-lost-ack", ProjectionKey: &lostAckKey, Other: "{}",
	}
	require.NoError(t, createClickHouseBillingProjectionLog(LOG_DB, lostAckLog, lostAckKey).Error)
	require.NoError(t, createClickHouseBillingProjectionLog(LOG_DB, lostAckLog, lostAckKey).Error)
	var lostAckCount int64
	require.NoError(t, LOG_DB.Raw("SELECT count() FROM logs WHERE projection_key = ?", lostAckKey).Scan(&lostAckCount).Error)
	assert.Equal(t, int64(1), lostAckCount, "stable insertion token must make an ambiguous ClickHouse retry idempotent")
	lookup := &BillingProjectionOutbox{LogCreatedAt: lostAckLog.CreatedAt, ProjectionKey: lostAckKey}
	var lookedUp []Log
	require.NoError(t, billingProjectionLogLookup(LOG_DB, lookup).Find(&lookedUp).Error)
	require.Len(t, lookedUp, 1)
	assert.True(t, billingProjectionLogMatches(&lookedUp[0], &BillingProjectionOutbox{
		LogUserId: lostAckLog.UserId, LogUsername: lostAckLog.Username, LogCreatedAt: lostAckLog.CreatedAt,
		LogType: lostAckLog.Type, LogContent: lostAckLog.Content, LogModelName: lostAckLog.ModelName,
		LogQuota: lostAckLog.Quota, LogRequestId: lostAckLog.RequestId, LogOther: lostAckLog.Other,
	}))
	var explainRows []struct {
		Explain string `gorm:"column:explain"`
	}
	require.NoError(t, LOG_DB.Raw(
		"EXPLAIN indexes = 1 SELECT projection_key FROM logs WHERE created_at = ? AND projection_key = ? LIMIT 1",
		lostAckLog.CreatedAt, "missing-projection-key",
	).Scan(&explainRows).Error)
	explainPlan := make([]string, 0, len(explainRows))
	for i := range explainRows {
		explainPlan = append(explainPlan, explainRows[i].Explain)
	}
	joinedExplain := strings.Join(explainPlan, "\n")
	assert.Contains(t, joinedExplain, "PrimaryKey")
	assert.Contains(t, joinedExplain, "idx_logs_projection_key")

	// The connection default disables block deduplication for ordinary logs;
	// only projection inserts opt in with a stable token. Preserve two
	// intentionally repeated non-projection audit rows.
	normalLog := &Log{
		UserId: 504, Username: "clickhouse-user", CreatedAt: 7_202, Type: LogTypeManage,
		Content: "intentional repeated audit", RequestId: "clickhouse-normal-repeat", Other: "{}",
	}
	require.NoError(t, LOG_DB.Create(normalLog).Error)
	normalLog.Id = 0
	require.NoError(t, LOG_DB.Create(normalLog).Error)
	var normalLogCount int64
	require.NoError(t, LOG_DB.Raw("SELECT count() FROM logs WHERE request_id = ?", normalLog.RequestId).Scan(&normalLogCount).Error)
	assert.Equal(t, int64(2), normalLogCount, "ordinary log writes must not inherit projection deduplication")

	const userId, channelId = 504, 604
	require.NoError(t, DB.Create(&User{Id: userId, Username: "clickhouse-user"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "clickhouse-channel"}).Error)
	require.NoError(t, DB.Create(&BillingSettlementEvent{
		RequestId: "projection-real-clickhouse", Operation: "request", UserId: userId, FundingSource: "wallet",
		Status: BillingSettlementStatusFinalized, FinancialStatus: BillingSettlementFinancialApplied,
		CommissionStatus: BillingCommissionStatusNotApplicable, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error)
	spec := billingProjectionTestSpec(BillingProjectionKey("projection-real-clickhouse", "request", "initial"), "projection-real-clickhouse", userId, channelId)
	spec.QuotaDataEnabled = false
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, ensureErr := ensureBillingProjectionPendingTx(tx, spec)
		return ensureErr
	}))

	const callbackName = "test:real_clickhouse_projection_main_crash"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			tx.AddError(errors.New("injected main crash after ClickHouse insert"))
		}
	}))
	require.ErrorContains(t, ApplyBillingProjection(spec.ProjectionKey), "injected main crash after ClickHouse insert")
	require.NoError(t, DB.Callback().Update().Remove(callbackName))

	var logCount int64
	require.NoError(t, LOG_DB.Raw("SELECT count() FROM logs WHERE projection_key = ?", spec.ProjectionKey).Scan(&logCount).Error)
	assert.Equal(t, int64(1), logCount, "synchronous insert must be immediately visible after main rollback")
	require.NoError(t, ApplyBillingProjection(spec.ProjectionKey))
	require.NoError(t, ApplyBillingProjection(spec.ProjectionKey))
	require.NoError(t, LOG_DB.Raw("SELECT count() FROM logs WHERE projection_key = ?", spec.ProjectionKey).Scan(&logCount).Error)
	assert.Equal(t, int64(1), logCount)

	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 42, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var outbox BillingProjectionOutbox
	require.NoError(t, DB.Where("projection_key = ?", spec.ProjectionKey).First(&outbox).Error)
	assert.Equal(t, BillingProjectionStatusApplied, outbox.Status)
}
