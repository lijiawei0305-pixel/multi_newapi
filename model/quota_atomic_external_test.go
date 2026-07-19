package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type legacyTaskSubmissionRecoveryText struct {
	Id           int
	RequestId    string `gorm:"type:varchar(128);not null;uniqueIndex:idx_task_submission_request_kind,priority:1"`
	Kind         string `gorm:"type:varchar(16);not null;uniqueIndex:idx_task_submission_request_kind,priority:2"`
	Status       string `gorm:"type:varchar(16);not null;index"`
	PayloadHash  string `gorm:"type:varchar(64);not null"`
	Payload      string `gorm:"type:text;not null"`
	LocalTaskId  int64  `gorm:"type:bigint;not null"`
	AttemptCount int    `gorm:"not null"`
	LastError    string `gorm:"type:varchar(512);not null"`
	AcceptedAt   *time.Time
	CommittedAt  *time.Time
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null;index"`
}

type legacyBillingTerminalRecoveryText struct {
	Id      int
	Payload string `gorm:"type:text;not null"`
}

func (legacyBillingTerminalRecoveryText) TableName() string {
	return "billing_terminal_recoveries"
}

type legacyBillingProjectionText struct {
	Id         int
	LogContent string `gorm:"type:text"`
	LogOther   string `gorm:"type:text"`
}

func (legacyBillingProjectionText) TableName() string {
	return "billing_projection_outboxes"
}

func (legacyTaskSubmissionRecoveryText) TableName() string {
	return "task_submission_recoveries"
}

// TestAtomicQuotaReserveExternalDialect proves the production conditional
// UPDATE/RowsAffected contract on the two networked database dialects. It is
// intentionally opt-in because it owns and drops its account/settlement tables
// in a dedicated disposable test database.
func TestAtomicQuotaReserveExternalDialect(t *testing.T) {
	if os.Getenv("TEST_BILLING_DB_ALLOW_DROP") != "1" {
		t.Skip("set TEST_BILLING_DB_ALLOW_DROP=1 with a disposable *_test database")
	}

	dialect := os.Getenv("TEST_BILLING_DB_DIALECT")
	dsn := os.Getenv("TEST_BILLING_DB_DSN")
	require.NotEmpty(t, dsn, "TEST_BILLING_DB_DSN is required for the external dialect fixture")

	var (
		dbType    common.DatabaseType
		dialector gorm.Dialector
	)
	switch dialect {
	case "mysql":
		dbType = common.DatabaseTypeMySQL
		dialector = mysql.Open(dsn)
	case "postgres":
		dbType = common.DatabaseTypePostgreSQL
		dialector = postgres.Open(dsn)
	default:
		t.Fatalf("unsupported TEST_BILLING_DB_DIALECT %q", dialect)
	}

	testDB, err := gorm.Open(dialector, &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	databaseName := strings.ToLower(testDB.Migrator().CurrentDatabase())
	require.True(t, strings.HasSuffix(databaseName, "_test"), "refusing destructive fixture against database %q; name must end in _test", databaseName)

	sqlDB, err := testDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	originalDB, originalLogDB := DB, LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	DB, LOG_DB = testDB, testDB
	common.SetDatabaseTypes(dbType, dbType)
	common.RedisEnabled = false
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.RedisEnabled = originalRedisEnabled
		initCol()
	})

	require.NoError(t, testDB.Migrator().DropTable(&Task{}, &BillingProjectionOutbox{}, &QuotaData{}, &TaskSubmissionRecovery{}, &BillingTerminalRecovery{}, &BillingSettlementEvent{}, &BillingAdjustmentIntent{}, &Token{}, &User{}))
	require.NoError(t, testDB.AutoMigrate(&User{}, &Token{}, &BillingAdjustmentIntent{}, &BillingSettlementEvent{}, &Task{}, &QuotaData{}, &legacyTaskSubmissionRecoveryText{}))
	legacyTaskRow := &legacyTaskSubmissionRecoveryText{
		RequestId: "legacy-task-submission-existing", Kind: TaskSubmissionKindTask,
		Status: TaskSubmissionStatusAborted, Payload: `{}`,
	}
	require.NoError(t, testDB.Create(legacyTaskRow).Error)
	if dialect == "mysql" {
		require.NoError(t, testDB.AutoMigrate(&legacyBillingTerminalRecoveryText{}, &legacyBillingProjectionText{}))
	}
	require.NoError(t, testDB.AutoMigrate(&TaskSubmissionRecovery{}, &BillingTerminalRecovery{}, &BillingProjectionOutbox{}))
	require.NoError(t, testDB.AutoMigrate(&TaskSubmissionRecovery{}), "idempotent repeated migration must succeed")
	require.NoError(t, EnsureTaskSubmissionIdempotencyUniqueIndex(testDB))
	require.NoError(t, EnsureTaskSubmissionIdempotencyUniqueIndex(testDB), "idempotent repeated unique-index migration must succeed")
	var migratedLegacy TaskSubmissionRecovery
	require.NoError(t, testDB.Where("request_id = ? AND kind = ?", legacyTaskRow.RequestId, legacyTaskRow.Kind).First(&migratedLegacy).Error)
	assert.Nil(t, migratedLegacy.IdempotencyFingerprint)
	assert.Zero(t, migratedLegacy.UserId)
	assert.Zero(t, migratedLegacy.TenantId)
	assert.Empty(t, migratedLegacy.Host)
	t.Cleanup(func() {
		require.NoError(t, testDB.Migrator().DropTable(&Task{}, &BillingProjectionOutbox{}, &QuotaData{}, &TaskSubmissionRecovery{}, &BillingTerminalRecovery{}, &BillingSettlementEvent{}, &BillingAdjustmentIntent{}, &Token{}, &User{}))
	})
	if dialect == "mysql" {
		columnTypes, columnErr := testDB.Migrator().ColumnTypes(&TaskSubmissionRecovery{})
		require.NoError(t, columnErr)
		payloadType := ""
		for _, column := range columnTypes {
			if strings.EqualFold(column.Name(), "payload") {
				payloadType = strings.ToUpper(column.DatabaseTypeName())
			}
		}
		assert.Contains(t, payloadType, "LONGTEXT")
		assertMySQLLongTextColumn(t, testDB, "billing_terminal_recoveries", "payload")
		assertMySQLLongTextColumn(t, testDB, "billing_projection_outboxes", "log_content")
		assertMySQLLongTextColumn(t, testDB, "billing_projection_outboxes", "log_other")
	}
	assertBillingDurableLargeTextRoundTrip(t, testDB, dialect)
	assertExternalQuotaDataBucketMigrationAndRace(t, testDB)
	assertExternalTaskSubmissionIdempotencyClaimRace(t, testDB)
	assertExternalTaskSubmissionRetentionDependency(t, testDB)

	user := &User{Username: "external-atomic-user", Password: "test-password", AffCode: "external-atomic", Quota: 100}
	require.NoError(t, testDB.Create(user).Error)
	assertExternalReserveRace(t, ErrUserQuotaInsufficient, func() error {
		return decreaseUserQuota(user.Id, 60)
	})
	var storedUser User
	require.NoError(t, testDB.Select("id", "quota").First(&storedUser, user.Id).Error)
	assert.Equal(t, 40, storedUser.Quota)

	token := &Token{UserId: user.Id, Key: "external-atomic-token", Name: "external", RemainQuota: 100}
	require.NoError(t, testDB.Create(token).Error)
	assertExternalReserveRace(t, ErrTokenQuotaInsufficient, func() error {
		return decreaseTokenQuota(token.Id, 60)
	})
	var storedToken Token
	require.NoError(t, testDB.Select("id", "remain_quota", "used_quota").First(&storedToken, token.Id).Error)
	assert.Equal(t, 40, storedToken.RemainQuota)
	assert.Equal(t, 60, storedToken.UsedQuota)

	settlementUser := &User{Username: "external-settlement-user", Password: "test-password", AffCode: "external-settlement", Quota: 100}
	require.NoError(t, testDB.Create(settlementUser).Error)
	settlementToken := &Token{UserId: settlementUser.Id, Key: "external-settlement-token", Name: "settlement", RemainQuota: 100}
	require.NoError(t, testDB.Create(settlementToken).Error)

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := range 2 {
		go func(attempt int) {
			ready.Done()
			<-start
			requestID := fmt.Sprintf("external-settlement-%d", attempt)
			results <- ReserveBillingSettlementImmediate(BillingAdjustmentSpec{
				RequestId: requestID, Operation: "preconsume", UserId: settlementUser.Id, TokenId: settlementToken.Id,
				UserQuotaDelta: -60, TokenQuotaDelta: -60,
			}, BillingSettlementSpec{
				RequestId: requestID, Operation: "request", UserId: settlementUser.Id, TokenId: settlementToken.Id,
				FundingSource: "wallet", UsingGroup: "default", ChargedGroupRatio: 1, ReservedQuota: 60,
			})
		}(i)
	}
	ready.Wait()
	close(start)

	var settlementSuccesses, settlementInsufficient int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			settlementSuccesses++
		case errors.Is(err, ErrUserQuotaInsufficient):
			settlementInsufficient++
		default:
			require.NoError(t, err)
		}
	}
	assert.Equal(t, 1, settlementSuccesses)
	assert.Equal(t, 1, settlementInsufficient)
	require.NoError(t, testDB.First(settlementUser, settlementUser.Id).Error)
	require.NoError(t, testDB.First(settlementToken, settlementToken.Id).Error)
	assert.Equal(t, 40, settlementUser.Quota)
	assert.Equal(t, 40, settlementToken.RemainQuota)
	assert.Equal(t, 60, settlementToken.UsedQuota)
	var adjustmentCount, settlementCount int64
	require.NoError(t, testDB.Model(&BillingAdjustmentIntent{}).Count(&adjustmentCount).Error)
	require.NoError(t, testDB.Model(&BillingSettlementEvent{}).Count(&settlementCount).Error)
	assert.Equal(t, int64(1), adjustmentCount, "the failed reserve transaction must leave no intent")
	assert.Equal(t, int64(1), settlementCount, "the failed reserve transaction must leave no lifecycle root")
	assertExternalTaskSubmissionRecovery(t, testDB)
}

func TestBillingDurableLargeTextSQLiteRoundTrip(t *testing.T) {
	truncateTables(t)
	assertBillingDurableLargeTextRoundTrip(t, DB, "sqlite")
}

func TestTaskSubmissionIdempotencyLegacySQLiteMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task_submission_legacy_migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&legacyTaskSubmissionRecoveryText{}))
	require.NoError(t, db.Create(&legacyTaskSubmissionRecoveryText{
		RequestId: "legacy-sqlite-task", Kind: TaskSubmissionKindTask, Status: TaskSubmissionStatusAborted, Payload: `{}`,
	}).Error)
	require.NoError(t, db.AutoMigrate(&TaskSubmissionRecovery{}))
	require.NoError(t, db.AutoMigrate(&TaskSubmissionRecovery{}))
	require.NoError(t, EnsureTaskSubmissionIdempotencyUniqueIndex(db))
	require.NoError(t, EnsureTaskSubmissionIdempotencyUniqueIndex(db))
	var row TaskSubmissionRecovery
	require.NoError(t, db.Where("request_id = ?", "legacy-sqlite-task").First(&row).Error)
	assert.Nil(t, row.IdempotencyFingerprint)
	assert.Zero(t, row.TenantId)
	assert.Empty(t, row.RequestFingerprint)
	assert.Empty(t, row.Host)
}

func assertMySQLLongTextColumn(t *testing.T, db *gorm.DB, table string, column string) {
	t.Helper()
	var dataType string
	require.NoError(t, db.Raw(
		"SELECT DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?",
		table, column,
	).Scan(&dataType).Error)
	assert.Equal(t, "longtext", strings.ToLower(dataType), "%s.%s must be widened from legacy TEXT", table, column)
}

func assertBillingDurableLargeTextRoundTrip(t *testing.T, db *gorm.DB, suffix string) {
	t.Helper()
	largeValue := strings.Repeat("large-payload-", 6*1024)
	require.Greater(t, len(largeValue), 64*1024)
	now := time.Now().UTC()

	requestId := "large-terminal-" + suffix
	operation := "request"
	payload := `{"blob":"` + largeValue + `"}`
	payloadHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	recovery := &BillingTerminalRecovery{
		RecoveryKey: BillingTerminalRecoveryKey(requestId, operation), RequestId: requestId, Operation: operation,
		Phase: BillingTerminalRecoveryPhaseFinalized, PayloadHash: payloadHash, Payload: payload,
		Status: BillingTerminalRecoveryStatusApplied, LastError: "", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(recovery).Error)
	var storedRecovery BillingTerminalRecovery
	require.NoError(t, db.Where("recovery_key = ?", recovery.RecoveryKey).First(&storedRecovery).Error)
	assert.Equal(t, payload, storedRecovery.Payload)
	assert.Greater(t, len(storedRecovery.Payload), 64*1024)

	projectionRequestId := "large-projection-" + suffix
	projection := &BillingProjectionOutbox{
		ProjectionKey:  BillingProjectionKey(projectionRequestId, operation, "large"),
		DependencyType: BillingProjectionDependencySettlement, DependencyRequestId: projectionRequestId, DependencyOperation: operation,
		LogContent: largeValue, LogOther: `{"blob":"` + largeValue + `"}`,
		Status: BillingProjectionStatusApplied, LastError: "", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(projection).Error)
	var storedProjection BillingProjectionOutbox
	require.NoError(t, db.Where("projection_key = ?", projection.ProjectionKey).First(&storedProjection).Error)
	assert.Equal(t, projection.LogContent, storedProjection.LogContent)
	assert.Equal(t, projection.LogOther, storedProjection.LogOther)
	assert.Greater(t, len(storedProjection.LogContent), 64*1024)
	assert.Greater(t, len(storedProjection.LogOther), 64*1024)

	taskRequestId := "large-task-recovery-" + suffix
	taskRecovery := &TaskSubmissionRecovery{
		RequestId: taskRequestId, Kind: TaskSubmissionKindTask, Status: TaskSubmissionStatusAborted,
		PayloadHash: payloadHash, Payload: TaskSubmissionPayload(payload), LastError: "", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(taskRecovery).Error)
	var storedTaskRecovery TaskSubmissionRecovery
	require.NoError(t, db.Where("request_id = ? AND kind = ?", taskRequestId, TaskSubmissionKindTask).First(&storedTaskRecovery).Error)
	assert.Equal(t, taskRecovery.Payload, storedTaskRecovery.Payload)
	assert.Greater(t, len(storedTaskRecovery.Payload), 64*1024)
}

func assertExternalTaskSubmissionRecovery(t *testing.T, testDB *gorm.DB) {
	t.Helper()
	large := strings.Repeat("x", 70*1024)
	requestId := "external-large-task-submission"
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, MarkTaskSubmissionUncertain(requestId, TaskSubmissionKindTask))
	payload := TaskSubmissionCommitPayload{
		Task: &Task{
			TaskID: "task_external_large", UserId: 1, Status: TaskStatusSubmitted,
			Data: []byte(`"` + large + `"`),
		},
		TaskPrivateData: &TaskPrivateData{BillingSource: "free", UpstreamTaskID: "upstream-large"},
	}
	require.NoError(t, FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload))
	result, err := ApplyAcceptedTaskSubmission(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.True(t, result.Committed)
	var stored Task
	require.NoError(t, testDB.Where("task_id = ?", "task_external_large").First(&stored).Error)
	assert.Greater(t, len(stored.Data), 64*1024)

	raceUser := &User{Username: "external-submission-race", Password: "test", AffCode: "external-submission-race", Quota: 100}
	require.NoError(t, testDB.Create(raceUser).Error)
	raceRequestId := "external-task-abort-reserve-race"
	_, err = EnsureTaskSubmissionPreparing(raceRequestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	start := make(chan struct{})
	reserveResult := make(chan error, 1)
	abortResult := make(chan error, 1)
	go func() {
		<-start
		reserveResult <- ReserveTaskSubmissionBillingSettlementImmediate(TaskSubmissionKindTask,
			BillingAdjustmentSpec{RequestId: raceRequestId, Operation: "request_preconsume", UserId: raceUser.Id, UserQuotaDelta: -40},
			BillingSettlementSpec{RequestId: raceRequestId, Operation: "request", UserId: raceUser.Id, FundingSource: "wallet", UsingGroup: "default", ReservedQuota: 40, DeferCommission: true},
		)
	}()
	go func() {
		<-start
		abortResult <- AbortPreparingTaskSubmission(raceRequestId, TaskSubmissionKindTask)
	}()
	close(start)
	reserveErr := <-reserveResult
	abortErr := <-abortResult
	require.NoError(t, abortErr)
	if reserveErr != nil {
		require.ErrorContains(t, reserveErr, "not preparing")
	}
	require.NoError(t, testDB.First(raceUser, raceUser.Id).Error)
	assert.Equal(t, 100, raceUser.Quota)

	freezeUser := &User{Username: "external-freeze-race", Password: "test", AffCode: "external-freeze-race", Quota: 100}
	require.NoError(t, testDB.Create(freezeUser).Error)
	freezeRequestId := "external-task-abort-freeze-race"
	_, err = EnsureTaskSubmissionPreparing(freezeRequestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, ReserveTaskSubmissionBillingSettlementImmediate(TaskSubmissionKindTask,
		BillingAdjustmentSpec{RequestId: freezeRequestId, Operation: "request_preconsume", UserId: freezeUser.Id, UserQuotaDelta: -40},
		BillingSettlementSpec{RequestId: freezeRequestId, Operation: "request", UserId: freezeUser.Id, FundingSource: "wallet", UsingGroup: "default", ReservedQuota: 40, DeferCommission: true},
	))
	freezePayload := TaskSubmissionCommitPayload{
		Task:            &Task{TaskID: "task_external_freeze_race", UserId: freezeUser.Id, Quota: 40},
		TaskPrivateData: &TaskPrivateData{BillingSource: "wallet", BillingRequestId: freezeRequestId},
		Transition:      &BillingSettlementTransition{RequestId: freezeRequestId, Operation: "request", FinalQuota: 40},
	}
	start = make(chan struct{})
	freezeResult := make(chan error, 1)
	abortResult = make(chan error, 1)
	go func() {
		<-start
		freezeResult <- FreezeAcceptedTaskSubmission(freezeRequestId, TaskSubmissionKindTask, freezePayload)
	}()
	go func() {
		<-start
		abortResult <- AbortPreparingTaskSubmission(freezeRequestId, TaskSubmissionKindTask)
	}()
	close(start)
	freezeErr := <-freezeResult
	abortErr = <-abortResult
	assert.NotEqual(t, freezeErr == nil, abortErr == nil)
	var freezeRow TaskSubmissionRecovery
	require.NoError(t, testDB.Where("request_id = ?", freezeRequestId).First(&freezeRow).Error)
	if freezeErr == nil {
		assert.Equal(t, TaskSubmissionStatusAccepted, freezeRow.Status)
	} else {
		assert.Equal(t, TaskSubmissionStatusAborted, freezeRow.Status)
	}
}

func assertExternalTaskSubmissionIdempotencyClaimRace(t *testing.T, testDB *gorm.DB) {
	t.Helper()
	start := make(chan struct{})
	results := make(chan TaskSubmissionClaimResult, 8)
	errorsSeen := make(chan error, 8)
	var ready sync.WaitGroup
	ready.Add(8)
	for index := 0; index < 8; index++ {
		go func(index int) {
			ready.Done()
			<-start
			result, err := ClaimTaskSubmission(TaskSubmissionClaimSpec{
				RequestId: fmt.Sprintf("external-idempotency-claim-%d", index), Kind: TaskSubmissionKindTask,
				IdempotencyKey: "external-concurrent-key", RequestFingerprint: fmt.Sprintf("%064x", 9876),
				UserId: 8801, TokenId: 0, TenantId: 17, Host: "tenant.example.com",
				Route: "/v1/videos", Method: "POST", PublicTaskId: "task_external_public",
			})
			results <- result
			errorsSeen <- err
		}(index)
	}
	ready.Wait()
	close(start)
	owners := 0
	requestId := ""
	for range 8 {
		require.NoError(t, <-errorsSeen)
		result := <-results
		if result.Owned {
			owners++
		}
		if requestId == "" {
			requestId = result.Recovery.RequestId
		}
		assert.Equal(t, requestId, result.Recovery.RequestId)
	}
	assert.Equal(t, 1, owners)
	var rows int64
	require.NoError(t, testDB.Model(&TaskSubmissionRecovery{}).
		Where("idempotency_fingerprint IS NOT NULL AND user_id = ?", 8801).Count(&rows).Error)
	assert.Equal(t, int64(1), rows)
	_, found, err := LookupTaskSubmissionClaim(TaskSubmissionClaimSpec{
		RequestId: "external-idempotency-mismatch", Kind: TaskSubmissionKindTask,
		IdempotencyKey: "external-concurrent-key", RequestFingerprint: fmt.Sprintf("%064x", 9877),
		UserId: 8801, TokenId: 0, TenantId: 17, Host: "tenant.example.com",
		Route: "/v1/videos", Method: "POST", PublicTaskId: "task_external_public",
	})
	assert.True(t, found)
	require.ErrorIs(t, err, ErrTaskSubmissionIdempotencyPayloadMismatch)
}

func assertExternalTaskSubmissionRetentionDependency(t *testing.T, testDB *gorm.DB) {
	t.Helper()
	const requestId = "external-retention-pending-projection"
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	require.NoError(t, testDB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ?", requestId, TaskSubmissionKindTask).
		UpdateColumns(map[string]interface{}{"status": TaskSubmissionStatusCommitted, "updated_at": old}).Error)
	require.NoError(t, testDB.Create(&BillingProjectionOutbox{
		ProjectionKey:       BillingProjectionKey(requestId, TaskSubmissionKindTask, "external-retention"),
		DependencyType:      BillingProjectionDependencyTaskSubmission,
		DependencyRequestId: requestId,
		DependencyOperation: TaskSubmissionKindTask,
		Status:              BillingProjectionStatusPending,
		CreatedAt:           old,
		UpdatedAt:           old,
	}).Error)

	_, err = CleanupTerminalTaskSubmissionRecoveries(7*24*time.Hour, 100)
	require.NoError(t, err)
	var count int64
	require.NoError(t, testDB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ?", requestId, TaskSubmissionKindTask).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, testDB.Model(&BillingProjectionOutbox{}).
		Where("dependency_request_id = ? AND dependency_operation = ?", requestId, TaskSubmissionKindTask).
		Update("status", BillingProjectionStatusApplied).Error)
	_, err = CleanupTerminalTaskSubmissionRecoveries(7*24*time.Hour, 100)
	require.NoError(t, err)
	require.NoError(t, testDB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ?", requestId, TaskSubmissionKindTask).Count(&count).Error)
	assert.Zero(t, count)
}

func assertExternalQuotaDataBucketMigrationAndRace(t *testing.T, testDB *gorm.DB) {
	t.Helper()
	legacy := QuotaData{
		UserID: 81, Username: "external-legacy", ModelName: "external-model", CreatedAt: 3600,
		UseGroup: "default", TokenID: 82, ChannelID: 83, NodeName: "external-node",
		Count: 1, Quota: 2, TokenUsed: 3,
	}
	require.NoError(t, testDB.Create(&legacy).Error)
	duplicate := legacy
	duplicate.Id = 0
	duplicate.Count = 4
	duplicate.Quota = 5
	duplicate.TokenUsed = 6
	require.NoError(t, testDB.Create(&duplicate).Error)
	t.Setenv("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", "1")
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(testDB, true))
	t.Setenv("QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED", "")
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(testDB, true), "external bucket migration must replay without a persistent acknowledgement")

	var legacyRows []QuotaData
	require.NoError(t, testDB.Where("bucket_key = ?", legacy.BucketKey).Find(&legacyRows).Error)
	require.Len(t, legacyRows, 1)
	assert.Equal(t, 5, legacyRows[0].Count)
	assert.Equal(t, 7, legacyRows[0].Quota)
	assert.Equal(t, 9, legacyRows[0].TokenUsed)

	const workers = 12
	start := make(chan struct{})
	results := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for i := range workers {
		i := i
		go func() {
			ready.Done()
			<-start
			results <- testDB.Transaction(func(tx *gorm.DB) error {
				return applyBillingProjectionQuotaDataTx(tx, &BillingProjectionOutbox{
					ProjectionKey: fmt.Sprintf("external-quota-projection-%d", i), QuotaDataEnabled: true,
					LogUserId: 91, LogUsername: "external-projection", LogModelName: "external-model",
					LogCreatedAt: 7201, LogGroup: "default", LogTokenId: 92, LogChannelId: 93,
					QuotaDataNodeName: "external-node", LogQuota: i + 1, QuotaDataTokenUsed: i,
				})
			})
		}()
	}
	ready.Wait()
	close(start)
	for range workers {
		require.NoError(t, <-results)
	}
	close(results)

	expected := normalizeQuotaDataBucket(QuotaData{
		UserID: 91, Username: "external-projection", ModelName: "external-model", CreatedAt: 7200,
		UseGroup: "default", TokenID: 92, ChannelID: 93, NodeName: "external-node",
	})
	var projectedRows []QuotaData
	require.NoError(t, testDB.Where("bucket_key = ?", expected.BucketKey).Find(&projectedRows).Error)
	require.Len(t, projectedRows, 1)
	assert.Equal(t, workers, projectedRows[0].Count)
	assert.Equal(t, workers*(workers+1)/2, projectedRows[0].Quota)
	assert.Equal(t, workers*(workers-1)/2, projectedRows[0].TokenUsed)
}

func assertExternalReserveRace(t *testing.T, insufficientError error, reserve func() error) {
	t.Helper()

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			results <- reserve()
		}()
	}
	ready.Wait()
	close(start)

	var successes, insufficient int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, insufficientError):
			insufficient++
		default:
			require.NoError(t, err)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, insufficient)
}
