package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func submissionTestContext(path string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set("username", "submission-user")
	c.Set("token_name", "submission-token")
	return c
}

func TestTaskSubmissionIdempotencyRetention(t *testing.T) {
	t.Setenv("TASK_SUBMISSION_IDEMPOTENCY_RETENTION_HOURS", "168")
	assert.Equal(t, 7*24*time.Hour, taskSubmissionIdempotencyRetention())

	t.Setenv("TASK_SUBMISSION_IDEMPOTENCY_RETENTION_HOURS", "1")
	assert.Equal(t, 24*time.Hour, taskSubmissionIdempotencyRetention())
}

func TestFreeSubmissionLifecycleHasNoBillingLedger(t *testing.T) {
	t.Run("generic task", func(t *testing.T) {
		truncate(t)
		const userId, channelId = 9811, 9812
		const requestId = "service-free-task"
		seedUser(t, userId, 100)
		seedChannel(t, channelId)
		_, err := model.EnsureTaskSubmissionPreparing(requestId, model.TaskSubmissionKindTask)
		require.NoError(t, err)
		require.NoError(t, model.MarkTaskSubmissionUncertain(requestId, model.TaskSubmissionKindTask))
		info := &relaycommon.RelayInfo{
			UserId: userId, RequestId: requestId, OriginModelName: "free-task", UsingGroup: "default",
			ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"},
		}
		task := makeTask(userId, channelId, 0, 0, "", 0)
		task.TaskID = "task_service_free"
		outcome, err := CommitTaskSubmissionWithRecovery(submissionTestContext("/v1/videos"), info, task, 0)
		require.NoError(t, err)
		assert.Equal(t, TaskSubmissionCommitOutcome{Durable: true, Committed: true}, outcome)
		assert.Equal(t, BillingSourceFree, task.PrivateData.BillingSource)

		fromStatus := task.Status
		task.Status = model.TaskStatusSuccess
		task.Progress = "100%"
		won, err := TransitionTaskWithBilling(context.Background(), task, fromStatus, 0, "success")
		require.NoError(t, err)
		assert.True(t, won)
		assertFreeSubmissionLedger(t, userId)
	})

	t.Run("midjourney", func(t *testing.T) {
		truncate(t)
		const userId, channelId = 9821, 9822
		const requestId = "service-free-midjourney"
		seedUser(t, userId, 100)
		seedChannel(t, channelId)
		_, err := model.EnsureTaskSubmissionPreparing(requestId, model.TaskSubmissionKindMidjourney)
		require.NoError(t, err)
		require.NoError(t, model.MarkTaskSubmissionUncertain(requestId, model.TaskSubmissionKindMidjourney))
		info := &relaycommon.RelayInfo{
			UserId: userId, RequestId: requestId, OriginModelName: "mj_free", UsingGroup: "default",
			ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
		}
		task := &model.Midjourney{
			UserId: userId, ChannelId: channelId, MjId: "mj_service_free", Action: "INPAINT",
			Status: "SUBMITTED", Progress: "0%", SubmitTime: time.Now().UnixMilli(),
		}
		outcome, err := CommitMidjourneySubmissionWithRecovery(submissionTestContext("/mj/submit/inpaint"), info, task, true)
		require.NoError(t, err)
		assert.Equal(t, TaskSubmissionCommitOutcome{Durable: true, Committed: true}, outcome)
		assert.Equal(t, BillingSourceFree, task.BillingSource)

		fromStatus := task.Status
		task.Status = "SUCCESS"
		task.Progress = "100%"
		won, err := TransitionMidjourneyWithBilling(context.Background(), task, fromStatus, "success")
		require.NoError(t, err)
		assert.True(t, won)
		assertFreeSubmissionLedger(t, userId)
	})
}

func assertFreeSubmissionLedger(t *testing.T, userId int) {
	t.Helper()
	var settlementCount, adjustmentCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlementEvent{}).Count(&settlementCount).Error)
	require.NoError(t, model.DB.Model(&model.BillingAdjustmentIntent{}).Count(&adjustmentCount).Error)
	assert.Zero(t, settlementCount)
	assert.Zero(t, adjustmentCount)
	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var projection model.BillingProjectionOutbox
	require.NoError(t, model.DB.First(&projection).Error)
	assert.Equal(t, model.BillingProjectionStatusApplied, projection.Status)
}

func TestSubscriptionSubmissionProjectionFreezesFinalUsage(t *testing.T) {
	truncate(t)
	const userId, channelId, subscriptionId, tokenId = 9831, 9832, 9833, 9834
	const requestId = "service-subscription-task-projection"
	seedUser(t, userId, 100)
	seedChannel(t, channelId)
	seedToken(t, tokenId, userId, "subscription-task-token", 900, 100)
	seedSubscription(t, subscriptionId, userId, 1000, 300)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionId).Update("last_reset_time", 123).Error)
	_, err := model.EnsureTaskSubmissionPreparing(requestId, model.TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, model.MarkTaskSubmissionUncertain(requestId, model.TaskSubmissionKindTask))
	require.NoError(t, model.EnsureBillingSettlementReserved(model.BillingSettlementSpec{
		RequestId: requestId, Operation: billingSettlementOperation, UserId: userId, TokenId: tokenId, SubscriptionId: subscriptionId,
		SubscriptionPreConsumeRequestId: requestId, SubscriptionResetEpoch: 123, SubscriptionOccurredAt: 200,
		FundingSource: BillingSourceSubscription, UsingGroup: "default", ReservedQuota: 100, DeferCommission: true,
	}))
	info := &relaycommon.RelayInfo{
		UserId: userId, TokenId: tokenId, RequestId: requestId, OriginModelName: "subscription-task", UsingGroup: "default",
		BillingSource: BillingSourceSubscription, SubscriptionId: subscriptionId, SubscriptionResetEpoch: 123,
		SubscriptionOccurredAt: 200, SubscriptionPreConsumed: 100,
		SubscriptionAmountTotal: 1000, SubscriptionAmountUsedAfterPreConsume: 300,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"},
	}
	funding := &SubscriptionFunding{
		requestId: requestId, userId: userId, subscriptionId: subscriptionId, preConsumed: 100,
		resetEpoch: 123, occurredAt: 200, AmountTotal: 1000, AmountUsedAfter: 300,
	}
	info.Billing = &BillingSession{
		relayInfo: info, funding: funding, preConsumedQuota: 100, billingRequestId: requestId,
	}
	task := makeTask(userId, channelId, 80, tokenId, BillingSourceSubscription, subscriptionId)
	task.TaskID = "task_subscription_projection"
	task.PrivateData.SubscriptionResetEpoch = 123
	task.PrivateData.SubscriptionOccurredAt = 200
	outcome, err := CommitTaskSubmissionWithRecovery(submissionTestContext("/v1/videos"), info, task, 80)
	require.NoError(t, err)
	assert.True(t, outcome.Durable)
	assert.True(t, outcome.Committed)

	var projection model.BillingProjectionOutbox
	require.NoError(t, model.DB.First(&projection).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(projection.LogOther, &other))
	assert.EqualValues(t, -20, other["subscription_post_delta"])
	assert.EqualValues(t, 280, other["subscription_used"])
	assert.EqualValues(t, 720, other["subscription_remain"])
	assert.EqualValues(t, 80, other["subscription_consumed"])

	before := projection.LogOther
	_, err = model.ApplyAcceptedTaskSubmission(requestId, model.TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(&projection, projection.Id).Error)
	assert.Equal(t, before, projection.LogOther)
	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, 1, user.RequestCount)
}

func TestTransitionTaskWithBillingRejectsNilTask(t *testing.T) {
	_, err := TransitionTaskWithBilling(context.Background(), nil, model.TaskStatusSubmitted, 0, "nil")
	require.ErrorContains(t, err, "task is nil")
}

func TestZeroQuotaMidjourneyRootIsClosed(t *testing.T) {
	tests := []struct {
		name            string
		deferCommission bool
		wantStatus      string
	}{
		{name: "normal deferred task", deferCommission: true, wantStatus: model.BillingSettlementStatusDeferred},
		{name: "swapface immediate task", deferCommission: false, wantStatus: model.BillingSettlementStatusFinalized},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			userId, channelId := 9840+index, 9850+index
			requestId := "zero-quota-mj-" + test.name
			seedUser(t, userId, 100)
			seedChannel(t, channelId)
			_, err := model.EnsureTaskSubmissionPreparing(requestId, model.TaskSubmissionKindMidjourney)
			require.NoError(t, err)
			require.NoError(t, model.EnsureTaskSubmissionBillingSettlementReserved(model.TaskSubmissionKindMidjourney, model.BillingSettlementSpec{
				RequestId: requestId, Operation: billingSettlementOperation, UserId: userId,
				FundingSource: BillingSourceWallet, UsingGroup: "default", ReservedQuota: 0,
				DeferCommission: test.deferCommission,
			}))
			require.NoError(t, model.MarkTaskSubmissionUncertain(requestId, model.TaskSubmissionKindMidjourney))
			info := &relaycommon.RelayInfo{
				UserId: userId, RequestId: requestId, BillingSource: BillingSourceWallet, UsingGroup: "default",
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
			}
			task := &model.Midjourney{
				UserId: userId, ChannelId: channelId, MjId: "mj-zero-root", Action: "IMAGINE",
				Status: "SUBMITTED", Progress: "0%", SubmitTime: time.Now().UnixMilli(),
			}
			outcome, err := CommitMidjourneySubmissionWithRecovery(submissionTestContext("/mj/submit"), info, task, test.deferCommission)
			require.NoError(t, err)
			assert.True(t, outcome.Durable)
			assert.True(t, outcome.Committed)
			assert.Equal(t, BillingSourceWallet, task.BillingSource)
			var settlement model.BillingSettlementEvent
			require.NoError(t, model.DB.Where("request_id = ?", requestId).First(&settlement).Error)
			assert.Equal(t, test.wantStatus, settlement.Status)
			assert.NotEqual(t, model.BillingSettlementStatusReserved, settlement.Status)
			var adjustments int64
			require.NoError(t, model.DB.Model(&model.BillingAdjustmentIntent{}).Count(&adjustments).Error)
			assert.Zero(t, adjustments)
		})
	}
}

func TestPaidPlaygroundSubmissionPersistsNoTokenBilling(t *testing.T) {
	truncate(t)
	const userId, channelId = 9861, 9862
	const requestId = "paid-playground-task"
	seedUser(t, userId, 100)
	seedChannel(t, channelId)
	_, err := model.EnsureTaskSubmissionPreparing(requestId, model.TaskSubmissionKindTask)
	require.NoError(t, err)
	require.NoError(t, model.EnsureTaskSubmissionBillingSettlementReserved(model.TaskSubmissionKindTask, model.BillingSettlementSpec{
		RequestId: requestId, Operation: billingSettlementOperation, UserId: userId, TokenId: 0,
		FundingSource: BillingSourceWallet, UsingGroup: "default", ReservedQuota: 10, DeferCommission: true,
	}))
	require.NoError(t, model.MarkTaskSubmissionUncertain(requestId, model.TaskSubmissionKindTask))
	info := &relaycommon.RelayInfo{
		UserId: userId, TokenId: 999, RequestId: requestId, BillingSource: BillingSourceWallet, IsPlayground: true,
		OriginModelName: "playground-task", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"},
	}
	info.Billing = &BillingSession{
		relayInfo: info, funding: &WalletFunding{userId: userId, consumed: 10}, preConsumedQuota: 10, billingRequestId: requestId,
	}
	task := makeTask(userId, channelId, 10, 999, BillingSourceWallet, 0)
	task.TaskID = "task_playground_no_token"
	outcome, err := CommitTaskSubmissionWithRecovery(submissionTestContext("/v1/videos"), info, task, 10)
	require.NoError(t, err)
	assert.True(t, outcome.Committed)
	assert.Zero(t, task.PrivateData.TokenId)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.Zero(t, stored.PrivateData.TokenId)
}
