package model

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedTaskSubmissionRoot(t *testing.T, requestId string, userId int, tokenId int, quota int) {
	t.Helper()
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: userId, TokenId: tokenId, FundingSource: "wallet",
		UsingGroup: "default", ReservedQuota: quota, DeferCommission: true,
	}))
}

func taskSubmissionProjection(requestId string, userId int, channelId int, quota int, suffix string) *BillingProjectionSpec {
	return &BillingProjectionSpec{
		ProjectionKey:       BillingProjectionKey(requestId, "request", suffix),
		DependencyRequestId: requestId, DependencyOperation: "request",
		UserId: userId, UserUsedQuotaDelta: quota, UserRequestDelta: 1,
		ChannelId: channelId, ChannelQuotaDelta: quota,
	}
}

func TestAcceptedTaskSubmissionHardCommitReconcilesExactlyOnce(t *testing.T) {
	truncateTables(t)
	const userId, channelId, quota = 9701, 9702, 42
	const requestId = "task-submission-hard-commit"
	require.NoError(t, DB.Create(&User{Id: userId, Username: "submission-user"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "submission-channel"}).Error)
	seedTaskSubmissionRoot(t, requestId, userId, 0, quota)

	row, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusPreparing, row.Status)
	require.NoError(t, MarkTaskSubmissionUncertain(requestId, TaskSubmissionKindTask))
	payload := TaskSubmissionCommitPayload{
		Task: &Task{
			TaskID: "task_public_recovery", Platform: constant.TaskPlatformSuno, UserId: userId,
			Group: "default", ChannelId: channelId, Quota: quota, Status: TaskStatusSubmitted,
			Progress: "0%", SubmitTime: time.Now().Unix(),
		},
		TaskPrivateData: &TaskPrivateData{UpstreamTaskID: "upstream-recovery", BillingRequestId: requestId, Key: "existing-task-private-key"},
		Transition:      &BillingSettlementTransition{RequestId: requestId, Operation: "request", FinalQuota: quota},
		Projection:      taskSubmissionProjection(requestId, userId, channelId, quota, "task_submission"),
	}
	require.NoError(t, FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload))

	const callbackName = "test:fail_recovered_task_create"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Task" {
			tx.AddError(errors.New("injected recovered task create failure"))
		}
	}))
	_, err = ApplyAcceptedTaskSubmission(requestId, TaskSubmissionKindTask)
	require.ErrorContains(t, err, "injected recovered task create failure")
	require.NoError(t, DB.Callback().Create().Remove(callbackName))

	row, err = GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusAccepted, row.Status)
	assert.Equal(t, 1, row.AttemptCount)
	var taskCount, projectionCount int64
	require.NoError(t, DB.Model(&Task{}).Count(&taskCount).Error)
	require.NoError(t, DB.Model(&BillingProjectionOutbox{}).Count(&projectionCount).Error)
	assert.Zero(t, taskCount)
	assert.Zero(t, projectionCount)

	committed, err := ReconcileAcceptedTaskSubmissions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, committed)
	committed, err = ReconcileAcceptedTaskSubmissions(10)
	require.NoError(t, err)
	assert.Zero(t, committed)

	require.NoError(t, DB.Model(&Task{}).Count(&taskCount).Error)
	require.NoError(t, DB.Model(&BillingProjectionOutbox{}).Count(&projectionCount).Error)
	assert.Equal(t, int64(1), taskCount)
	assert.Equal(t, int64(1), projectionCount)
	var stored Task
	require.NoError(t, DB.First(&stored).Error)
	assert.Equal(t, "upstream-recovery", stored.PrivateData.UpstreamTaskID)
	assert.Equal(t, "existing-task-private-key", stored.PrivateData.Key)
	row, err = GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusCommitted, row.Status)
	assert.Equal(t, stored.ID, row.LocalTaskId)

	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, quota, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
}

func TestAcceptedMidjourneySubmissionHardCommitReconcilesExactlyOnce(t *testing.T) {
	truncateTables(t)
	const userId, channelId, quota = 9711, 9712, 60
	const requestId = "midjourney-submission-hard-commit"
	require.NoError(t, DB.Create(&User{Id: userId, Username: "mj-submission-user"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: channelId, Name: "mj-submission-channel"}).Error)
	seedTaskSubmissionRoot(t, requestId, userId, 44, quota)
	require.NoError(t, func() error {
		_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindMidjourney)
		return err
	}())
	require.NoError(t, MarkTaskSubmissionUncertain(requestId, TaskSubmissionKindMidjourney))
	payload := TaskSubmissionCommitPayload{
		Midjourney: &Midjourney{
			UserId: userId, MjId: "upstream-mj-recovery", Action: "IMAGINE", Status: "SUBMITTED",
			Progress: "0%", SubmitTime: time.Now().UnixMilli(), Quota: quota, ChannelId: channelId,
		},
		MidjourneyBilling: &MidjourneySubmissionBilling{
			TokenId: 44, Group: "default", ChargedGroupRatio: 1, BillingSource: "wallet", BillingRequestId: requestId,
		},
		Transition: &BillingSettlementTransition{RequestId: requestId, Operation: "request", FinalQuota: quota},
		Projection: taskSubmissionProjection(requestId, userId, channelId, quota, "midjourney_submission"),
	}
	require.NoError(t, FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindMidjourney, payload))

	const callbackName = "test:fail_recovered_midjourney_create"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Midjourney" {
			tx.AddError(errors.New("injected recovered midjourney create failure"))
		}
	}))
	_, err := ApplyAcceptedTaskSubmission(requestId, TaskSubmissionKindMidjourney)
	require.ErrorContains(t, err, "injected recovered midjourney create failure")
	require.NoError(t, DB.Callback().Create().Remove(callbackName))

	committed, err := ReconcileAcceptedTaskSubmissions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, committed)
	result, err := ApplyAcceptedTaskSubmission(requestId, TaskSubmissionKindMidjourney)
	require.NoError(t, err)
	assert.True(t, result.Committed)

	var tasks []Midjourney
	require.NoError(t, DB.Find(&tasks).Error)
	require.Len(t, tasks, 1)
	assert.Equal(t, "upstream-mj-recovery", tasks[0].MjId)
	assert.Equal(t, 44, tasks[0].TokenId)
	assert.Equal(t, requestId, tasks[0].BillingRequestId)
	var projectionCount int64
	require.NoError(t, DB.Model(&BillingProjectionOutbox{}).Count(&projectionCount).Error)
	assert.Equal(t, int64(1), projectionCount)
}

func TestUncertainTaskSubmissionCannotAbortOrAutoRefund(t *testing.T) {
	truncateTables(t)
	const requestId = "task-submission-uncertain"
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, MarkTaskSubmissionUncertain(requestId, TaskSubmissionKindTask))

	err = AbortTaskSubmission(requestId, TaskSubmissionKindTask)
	require.ErrorContains(t, err, "current status is uncertain")
	row, err := GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusUncertain, row.Status)

	require.NoError(t, MarkTaskSubmissionRejected(requestId, TaskSubmissionKindTask))
	require.NoError(t, AbortTaskSubmission(requestId, TaskSubmissionKindTask))
	row, err = GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusAborted, row.Status)
}

func TestAcceptedTaskSubmissionRejectsPayloadDrift(t *testing.T) {
	truncateTables(t)
	const requestId = "task-submission-payload-drift"
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	seedTaskSubmissionRoot(t, requestId, 1, 0, 10)
	payload := TaskSubmissionCommitPayload{
		Task:            &Task{TaskID: "task_payload_drift", UserId: 1, Quota: 10, Status: TaskStatusSubmitted},
		TaskPrivateData: &TaskPrivateData{UpstreamTaskID: "upstream-payload-drift"},
		Transition:      &BillingSettlementTransition{RequestId: requestId, Operation: "request", FinalQuota: 10},
		Projection: &BillingProjectionSpec{
			ProjectionKey:       BillingProjectionKey(requestId, "request", "payload_drift"),
			DependencyRequestId: requestId, DependencyOperation: "request", UserId: 1, UserRequestDelta: 1,
		},
	}
	require.NoError(t, FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload))
	payload.Transition.FinalQuota++
	payload.Task.Quota++
	err = FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload)
	require.ErrorContains(t, err, "does not match persisted payload")
}

func TestTaskSubmissionRecoveryUsesCommonJSONContract(t *testing.T) {
	private := TaskPrivateData{Key: "private-key", UpstreamTaskID: "upstream"}
	encoded, err := common.Marshal(private)
	require.NoError(t, err)
	var decoded TaskPrivateData
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	assert.Equal(t, private, decoded)
}

func freeTaskSubmissionProjection(requestId string, kind string, userId int, channelId int) *BillingProjectionSpec {
	return &BillingProjectionSpec{
		ProjectionKey:  BillingProjectionKey(requestId, kind, "free_initial"),
		DependencyType: BillingProjectionDependencyTaskSubmission, DependencyRequestId: requestId, DependencyOperation: kind,
		UserId: userId, UserRequestDelta: 1, ChannelId: channelId,
	}
}

func TestFreeTaskSubmissionsCommitWithoutBillingLedger(t *testing.T) {
	tests := []struct {
		name string
		kind string
		make func(requestId string, userId int, channelId int) TaskSubmissionCommitPayload
	}{
		{
			name: "generic task", kind: TaskSubmissionKindTask,
			make: func(requestId string, userId int, channelId int) TaskSubmissionCommitPayload {
				return TaskSubmissionCommitPayload{
					Task:            &Task{TaskID: "task_free", UserId: userId, ChannelId: channelId, Status: TaskStatusSubmitted},
					TaskPrivateData: &TaskPrivateData{UpstreamTaskID: "upstream-free", BillingSource: "free", BillingRequestId: requestId},
					Projection:      freeTaskSubmissionProjection(requestId, TaskSubmissionKindTask, userId, channelId),
				}
			},
		},
		{
			name: "midjourney", kind: TaskSubmissionKindMidjourney,
			make: func(requestId string, userId int, channelId int) TaskSubmissionCommitPayload {
				return TaskSubmissionCommitPayload{
					Midjourney:        &Midjourney{MjId: "mj-free", UserId: userId, ChannelId: channelId, Status: "SUBMITTED"},
					MidjourneyBilling: &MidjourneySubmissionBilling{BillingSource: "free", BillingRequestId: requestId},
					Projection:        freeTaskSubmissionProjection(requestId, TaskSubmissionKindMidjourney, userId, channelId),
				}
			},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId, channelId := 9800+index, 9900+index
			requestId := "free-submission-" + test.kind
			require.NoError(t, DB.Create(&User{Id: userId, Username: requestId}).Error)
			require.NoError(t, DB.Create(&Channel{Id: channelId, Name: requestId}).Error)
			_, err := EnsureTaskSubmissionPreparing(requestId, test.kind)
			require.NoError(t, err)
			require.NoError(t, MarkTaskSubmissionUncertain(requestId, test.kind))
			require.NoError(t, FreezeAcceptedTaskSubmission(requestId, test.kind, test.make(requestId, userId, channelId)))
			result, err := ApplyAcceptedTaskSubmission(requestId, test.kind)
			require.NoError(t, err)
			assert.True(t, result.Committed)

			var settlementCount, adjustmentCount int64
			require.NoError(t, DB.Model(&BillingSettlementEvent{}).Count(&settlementCount).Error)
			require.NoError(t, DB.Model(&BillingAdjustmentIntent{}).Count(&adjustmentCount).Error)
			assert.Zero(t, settlementCount)
			assert.Zero(t, adjustmentCount)
			var user User
			require.NoError(t, DB.First(&user, userId).Error)
			assert.Zero(t, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			var projection BillingProjectionOutbox
			require.NoError(t, DB.First(&projection).Error)
			assert.Equal(t, BillingProjectionStatusApplied, projection.Status)
		})
	}
}

func TestTaskSubmissionFreezeRejectsMissingRootAndExtraneousPayload(t *testing.T) {
	truncateTables(t)
	const requestId = "submission-missing-root"
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	payload := TaskSubmissionCommitPayload{
		Task:            &Task{TaskID: "task-missing-root", UserId: 1, Quota: 2},
		TaskPrivateData: &TaskPrivateData{BillingSource: "wallet"},
		Transition:      &BillingSettlementTransition{RequestId: requestId, Operation: "request", FinalQuota: 2},
	}
	err = FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload)
	require.ErrorContains(t, err, "settlement root is unavailable")
	payload.Transition = nil
	payload.MidjourneyBilling = &MidjourneySubmissionBilling{}
	err = FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload)
	require.ErrorContains(t, err, "task payload is invalid")
}

func TestTaskSubmissionPayloadHashCorruptionCannotReplay(t *testing.T) {
	truncateTables(t)
	const requestId = "submission-corrupt-payload"
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, MarkTaskSubmissionUncertain(requestId, TaskSubmissionKindTask))
	payload := TaskSubmissionCommitPayload{
		Task: &Task{TaskID: "task-corrupt", UserId: 1}, TaskPrivateData: &TaskPrivateData{BillingSource: "free"},
	}
	require.NoError(t, FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload))
	require.NoError(t, DB.Model(&TaskSubmissionRecovery{}).Where("request_id = ?", requestId).Update("payload", "{}").Error)
	_, err = ApplyAcceptedTaskSubmission(requestId, TaskSubmissionKindTask)
	require.ErrorContains(t, err, "payload hash mismatch")
	row, err := GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusAccepted, row.Status)
	assert.Equal(t, 1, row.AttemptCount)
}

func TestPreparingSubmissionAbortAndReserveOrdersAreCompensated(t *testing.T) {
	tests := []struct {
		name         string
		reserveFirst bool
	}{
		{name: "reserve wins row lock", reserveFirst: true},
		{name: "abort wins row lock", reserveFirst: false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			requestId := fmt.Sprintf("submission-abort-order-%d", index)
			userId := 9950 + index
			require.NoError(t, DB.Create(&User{Id: userId, Username: requestId, Quota: 100}).Error)
			_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
			require.NoError(t, err)
			adjustment := BillingAdjustmentSpec{RequestId: requestId, Operation: "request_preconsume", UserId: userId, UserQuotaDelta: -40}
			settlement := BillingSettlementSpec{
				RequestId: requestId, Operation: "request", UserId: userId, FundingSource: "wallet",
				UsingGroup: "default", ReservedQuota: 40, DeferCommission: true,
			}
			reserve := func() error {
				return ReserveTaskSubmissionBillingSettlementImmediate(TaskSubmissionKindTask, adjustment, settlement)
			}
			if test.reserveFirst {
				require.NoError(t, reserve())
				require.NoError(t, AbortPreparingTaskSubmission(requestId, TaskSubmissionKindTask))
			} else {
				require.NoError(t, AbortPreparingTaskSubmission(requestId, TaskSubmissionKindTask))
				require.ErrorContains(t, reserve(), "not preparing")
			}
			var user User
			require.NoError(t, DB.First(&user, userId).Error)
			assert.Equal(t, 100, user.Quota)
			row, err := GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
			require.NoError(t, err)
			assert.Equal(t, TaskSubmissionStatusAborted, row.Status)
			var reserved int64
			require.NoError(t, DB.Model(&BillingSettlementEvent{}).Where("status = ?", BillingSettlementStatusReserved).Count(&reserved).Error)
			assert.Zero(t, reserved)
		})
	}
}

func TestPreparingSubmissionAbortReserveRaceIsSafe(t *testing.T) {
	truncateTables(t)
	const requestId = "submission-abort-reserve-race"
	const userId = 9961
	require.NoError(t, DB.Create(&User{Id: userId, Username: requestId, Quota: 100}).Error)
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	adjustment := BillingAdjustmentSpec{RequestId: requestId, Operation: "request_preconsume", UserId: userId, UserQuotaDelta: -40}
	settlement := BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: userId, FundingSource: "wallet",
		UsingGroup: "default", ReservedQuota: 40, DeferCommission: true,
	}
	start := make(chan struct{})
	reserveResult := make(chan error, 1)
	abortResult := make(chan error, 1)
	go func() {
		<-start
		reserveResult <- ReserveTaskSubmissionBillingSettlementImmediate(TaskSubmissionKindTask, adjustment, settlement)
	}()
	go func() {
		<-start
		abortResult <- AbortPreparingTaskSubmission(requestId, TaskSubmissionKindTask)
	}()
	close(start)
	reserveErr := <-reserveResult
	abortErr := <-abortResult
	require.NoError(t, abortErr)
	if reserveErr != nil {
		require.ErrorContains(t, reserveErr, "not preparing")
	}
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 100, user.Quota)
	row, err := GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	assert.Equal(t, TaskSubmissionStatusAborted, row.Status)
	var reserved int64
	require.NoError(t, DB.Model(&BillingSettlementEvent{}).Where("status = ?", BillingSettlementStatusReserved).Count(&reserved).Error)
	assert.Zero(t, reserved)
}

func TestPreparingSubmissionAbortFreezeRaceHasSingleWinner(t *testing.T) {
	truncateTables(t)
	const requestId = "submission-abort-freeze-race"
	const userId = 9962
	require.NoError(t, DB.Create(&User{Id: userId, Username: requestId, Quota: 100}).Error)
	_, err := EnsureTaskSubmissionPreparing(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, ReserveTaskSubmissionBillingSettlementImmediate(TaskSubmissionKindTask,
		BillingAdjustmentSpec{RequestId: requestId, Operation: "request_preconsume", UserId: userId, UserQuotaDelta: -40},
		BillingSettlementSpec{RequestId: requestId, Operation: "request", UserId: userId, FundingSource: "wallet", UsingGroup: "default", ReservedQuota: 40, DeferCommission: true},
	))
	payload := TaskSubmissionCommitPayload{
		Task:            &Task{TaskID: "task_abort_freeze_race", UserId: userId, Quota: 40},
		TaskPrivateData: &TaskPrivateData{BillingSource: "wallet", BillingRequestId: requestId},
		Transition:      &BillingSettlementTransition{RequestId: requestId, Operation: "request", FinalQuota: 40},
	}
	start := make(chan struct{})
	freezeResult := make(chan error, 1)
	abortResult := make(chan error, 1)
	go func() {
		<-start
		freezeResult <- FreezeAcceptedTaskSubmission(requestId, TaskSubmissionKindTask, payload)
	}()
	go func() {
		<-start
		abortResult <- AbortPreparingTaskSubmission(requestId, TaskSubmissionKindTask)
	}()
	close(start)
	freezeErr := <-freezeResult
	abortErr := <-abortResult
	assert.NotEqual(t, freezeErr == nil, abortErr == nil, "exactly one recovery transition may win")
	row, err := GetTaskSubmissionRecovery(requestId, TaskSubmissionKindTask)
	require.NoError(t, err)
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	if freezeErr == nil {
		assert.Equal(t, TaskSubmissionStatusAccepted, row.Status)
		assert.Equal(t, 60, user.Quota)
		var settlement BillingSettlementEvent
		require.NoError(t, DB.Where("request_id = ?", requestId).First(&settlement).Error)
		assert.Equal(t, BillingSettlementStatusReserved, settlement.Status)
	} else {
		assert.Equal(t, TaskSubmissionStatusAborted, row.Status)
		assert.Equal(t, 100, user.Quota)
	}
}
