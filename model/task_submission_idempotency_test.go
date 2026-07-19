package model

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func taskSubmissionClaimFixture(requestId string, key string, fingerprint string, userId int) TaskSubmissionClaimSpec {
	return TaskSubmissionClaimSpec{
		RequestId: requestId, Kind: TaskSubmissionKindTask, IdempotencyKey: key,
		RequestFingerprint: fingerprint, UserId: userId, TokenId: 9, TenantId: 3,
		Host: "tenant.example.com", Route: "/v1/videos", Method: "POST", PublicTaskId: "task_public_claim",
	}
}

func TestTaskSubmissionTenantIDHandlesLegacyAndNullableSchemas(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
	})
	tests := []struct {
		name       string
		definition string
		value      interface{}
		want       int64
		wantError  bool
	}{
		{name: "column absent fails closed", definition: "", value: nil, wantError: true},
		{name: "nullable tenant", definition: ", tenant_id BIGINT NULL", value: nil, want: 0},
		{name: "tenant value", definition: ", tenant_id BIGINT NULL", value: int64(17), want: 17},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:tenant-id-%d?mode=memory&cache=shared", index)), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY"+test.definition+")").Error)
			if test.definition == "" {
				require.NoError(t, db.Exec("INSERT INTO users (id) VALUES (1)").Error)
			} else {
				require.NoError(t, db.Exec("INSERT INTO users (id, tenant_id) VALUES (?, ?)", 1, test.value).Error)
			}
			DB = db
			LOG_DB = db
			got, err := TaskSubmissionTenantID(1)
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestTaskSubmissionClaimBindsPayloadAndAuthenticatedScope(t *testing.T) {
	truncateTables(t)
	fingerprintA := fmt.Sprintf("%064x", 1)
	fingerprintB := fmt.Sprintf("%064x", 2)
	first, err := ClaimTaskSubmission(taskSubmissionClaimFixture("claim-owner", "stable-key", fingerprintA, 7001))
	require.NoError(t, err)
	assert.True(t, first.Owned)
	require.NotNil(t, first.Recovery.IdempotencyFingerprint)
	assert.NotContains(t, *first.Recovery.IdempotencyFingerprint, "stable-key")

	lookup, found, err := LookupTaskSubmissionClaim(taskSubmissionClaimFixture("different-request-id", "stable-key", fingerprintA, 7001))
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, first.Recovery.RequestId, lookup.RequestId)

	_, found, err = LookupTaskSubmissionClaim(taskSubmissionClaimFixture("mismatch", "stable-key", fingerprintB, 7001))
	assert.True(t, found)
	require.ErrorIs(t, err, ErrTaskSubmissionIdempotencyPayloadMismatch)

	crossUser, err := ClaimTaskSubmission(taskSubmissionClaimFixture("cross-user", "stable-key", fingerprintA, 7002))
	require.NoError(t, err)
	assert.True(t, crossUser.Owned)
	assert.NotEqual(t, *first.Recovery.IdempotencyFingerprint, *crossUser.Recovery.IdempotencyFingerprint)

	crossHostSpec := taskSubmissionClaimFixture("cross-host", "stable-key", fingerprintA, 7001)
	crossHostSpec.Host = "other.example.com"
	crossHost, err := ClaimTaskSubmission(crossHostSpec)
	require.NoError(t, err)
	assert.True(t, crossHost.Owned)
	assert.NotEqual(t, *first.Recovery.IdempotencyFingerprint, *crossHost.Recovery.IdempotencyFingerprint)
}

func TestTaskSubmissionConcurrentClaimHasOneProviderOwner(t *testing.T) {
	truncateTables(t)
	const workers = 12
	start := make(chan struct{})
	results := make(chan TaskSubmissionClaimResult, workers)
	errorsSeen := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			spec := taskSubmissionClaimFixture(fmt.Sprintf("claim-race-%d", index), "concurrent-key", fmt.Sprintf("%064x", 3), 7101)
			result, err := ClaimTaskSubmission(spec)
			results <- result
			errorsSeen <- err
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}
	owners := 0
	requestId := ""
	for result := range results {
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
	require.NoError(t, DB.Model(&TaskSubmissionRecovery{}).Count(&rows).Error)
	assert.Equal(t, int64(1), rows)
}

func TestCleanupTerminalTaskSubmissionRecoveriesHonorsRetentionAndStatus(t *testing.T) {
	truncateTables(t)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	young := time.Now().UTC().Add(-6 * 24 * time.Hour)
	fixtures := []struct {
		requestId string
		status    string
		updatedAt time.Time
	}{
		{requestId: "retention-old-committed", status: TaskSubmissionStatusCommitted, updatedAt: old},
		{requestId: "retention-old-aborted", status: TaskSubmissionStatusAborted, updatedAt: old},
		{requestId: "retention-old-preparing", status: TaskSubmissionStatusPreparing, updatedAt: old},
		{requestId: "retention-old-uncertain", status: TaskSubmissionStatusUncertain, updatedAt: old},
		{requestId: "retention-old-accepted", status: TaskSubmissionStatusAccepted, updatedAt: old},
		{requestId: "retention-young-committed", status: TaskSubmissionStatusCommitted, updatedAt: young},
		{requestId: "retention-young-aborted", status: TaskSubmissionStatusAborted, updatedAt: young},
	}
	for _, fixture := range fixtures {
		_, err := EnsureTaskSubmissionPreparing(fixture.requestId, TaskSubmissionKindTask)
		require.NoError(t, err)
		result := DB.Model(&TaskSubmissionRecovery{}).
			Where("request_id = ? AND kind = ?", fixture.requestId, TaskSubmissionKindTask).
			UpdateColumns(map[string]interface{}{"status": fixture.status, "updated_at": fixture.updatedAt})
		require.NoError(t, result.Error)
		require.Equal(t, int64(1), result.RowsAffected)
	}

	deleted, err := CleanupTerminalTaskSubmissionRecoveries(7*24*time.Hour, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	deleted, err = CleanupTerminalTaskSubmissionRecoveries(7*24*time.Hour, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	deleted, err = CleanupTerminalTaskSubmissionRecoveries(7*24*time.Hour, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)

	var remaining []string
	require.NoError(t, DB.Model(&TaskSubmissionRecovery{}).Order("request_id").Pluck("request_id", &remaining).Error)
	assert.Equal(t, []string{
		"retention-old-accepted",
		"retention-old-preparing",
		"retention-old-uncertain",
		"retention-young-aborted",
		"retention-young-committed",
	}, remaining)

	_, err = CleanupTerminalTaskSubmissionRecoveries(24*time.Hour-time.Second, 100)
	require.ErrorContains(t, err, "at least 24 hours")
}

func TestCleanupTerminalTaskSubmissionRecoveriesPreservesPendingProjectionAuthority(t *testing.T) {
	truncateTables(t)
	const requestId = "retention-pending-task-projection"
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	require.NoError(t, DB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ?", requestId, TaskSubmissionKindTask).
		UpdateColumns(map[string]interface{}{"status": TaskSubmissionStatusCommitted, "updated_at": old}).Error)
	require.NoError(t, DB.Create(&BillingProjectionOutbox{
		ProjectionKey:       BillingProjectionKey(requestId, TaskSubmissionKindTask, "retention-pending"),
		DependencyType:      BillingProjectionDependencyTaskSubmission,
		DependencyRequestId: requestId,
		DependencyOperation: TaskSubmissionKindTask,
		Status:              BillingProjectionStatusPending,
		CreatedAt:           old,
		UpdatedAt:           old,
	}).Error)

	deleted, err := CleanupTerminalTaskSubmissionRecoveries(7*24*time.Hour, 100)
	require.NoError(t, err)
	assert.Zero(t, deleted)
	var recoveryCount int64
	require.NoError(t, DB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ?", requestId, TaskSubmissionKindTask).Count(&recoveryCount).Error)
	assert.Equal(t, int64(1), recoveryCount, "pending projection replay still needs the committed recovery authority")

	require.NoError(t, DB.Model(&BillingProjectionOutbox{}).
		Where("dependency_request_id = ? AND dependency_operation = ?", requestId, TaskSubmissionKindTask).
		Updates(map[string]interface{}{"status": BillingProjectionStatusApplied, "updated_at": time.Now().UTC()}).Error)
	deleted, err = CleanupTerminalTaskSubmissionRecoveries(7*24*time.Hour, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
}

func TestTaskSubmissionPublicResponseRejectsHeaderLeaksAndFalseIdentity(t *testing.T) {
	valid := &TaskSubmissionPublicResponse{
		Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body: `{"id":"task_public"}`,
	}
	require.NoError(t, valid.Validate())
	assert.True(t, TaskSubmissionPublicResponseIdentifies(TaskSubmissionKindTask, "task_public", valid))

	falsePositive := *valid
	falsePositive.Body = `{"id":"different","prompt":"mentions task_public but is not the response identity"}`
	require.NoError(t, falsePositive.Validate())
	assert.False(t, TaskSubmissionPublicResponseIdentifies(TaskSubmissionKindTask, "task_public", &falsePositive))

	for name := range map[string]struct{}{"Authorization": {}, "Set-Cookie": {}, "Connection": {}} {
		leaky := *valid
		leaky.Headers = map[string][]string{name: {"secret"}}
		require.Error(t, leaky.Validate(), name)
	}
	duplicate := *valid
	duplicate.Headers = map[string][]string{"content-type": {"application/json"}, "Content-Type": {"application/json"}}
	require.ErrorContains(t, duplicate.Validate(), "duplicate canonical")
}

func TestResolveUncertainTaskSubmissionRejectedRefundsExactlyOnce(t *testing.T) {
	truncateTables(t)
	const userId = 7201
	const requestId = "manual-rejected-exact-refund"
	require.NoError(t, DB.Create(&User{Id: userId, Username: "manual-rejected", Quota: 100}).Error)
	require.NoError(t, DB.Create(&Token{Id: 9, UserId: userId, Name: "manual-rejected-token", Key: "manual-rejected-token-key", RemainQuota: 100}).Error)
	claim, err := ClaimTaskSubmission(taskSubmissionClaimFixture(requestId, "manual-rejected-key", fmt.Sprintf("%064x", 4), userId))
	require.NoError(t, err)
	require.True(t, claim.Owned)
	require.NoError(t, ReserveTaskSubmissionBillingSettlementImmediate(TaskSubmissionKindTask,
		BillingAdjustmentSpec{RequestId: requestId, Operation: "request_preconsume", UserId: userId, TokenId: 9, UserQuotaDelta: -40, TokenQuotaDelta: -40},
		BillingSettlementSpec{RequestId: requestId, Operation: "request", UserId: userId, TokenId: 9, FundingSource: "wallet", UsingGroup: "default", ReservedQuota: 40, DeferCommission: true},
	))
	require.NoError(t, MarkTaskSubmissionUncertainWithMetadata(requestId, TaskSubmissionKindTask, TaskSubmissionAttemptMetadata{
		UserId: userId, TokenId: 9, ChannelId: 15, Provider: "sora", Model: "sora", PublicTaskId: "task_public_claim",
		Action: "submit", UsingGroup: "default", InitialQuota: 40,
	}))
	require.NoError(t, ResolveUncertainTaskSubmissionRejected(requestId, TaskSubmissionKindTask, 9001))
	require.NoError(t, ResolveUncertainTaskSubmissionRejected(requestId, TaskSubmissionKindTask, 9001))
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 100, user.Quota)
	var token Token
	require.NoError(t, DB.First(&token, 9).Error)
	assert.Equal(t, 100, token.RemainQuota)
	row, err := GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusAborted, row.Status)
	assert.Equal(t, TaskSubmissionResolutionRejected, row.Resolution)
	var intents int64
	require.NoError(t, DB.Model(&BillingAdjustmentIntent{}).Where("request_id = ? AND operation = ?", requestId, "task_submission_cancel").Count(&intents).Error)
	assert.Equal(t, int64(1), intents)
}
