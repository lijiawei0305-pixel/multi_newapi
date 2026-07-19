package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedUncertainTaskSubmission(t *testing.T, requestId string, kind string, userId int, tokenId int, channelId int, publicTaskId string, provider string, action string, quota int) {
	t.Helper()
	claim, err := model.ClaimTaskSubmission(model.TaskSubmissionClaimSpec{
		RequestId: requestId, Kind: kind, IdempotencyKey: requestId + "-key",
		RequestFingerprint: fmt.Sprintf("%064x", userId), UserId: userId, TokenId: tokenId,
		TenantId: 7, Host: "tenant.example.com", Route: "/v1/tasks", Method: "POST", PublicTaskId: publicTaskId,
	})
	require.NoError(t, err)
	require.True(t, claim.Owned)
	require.NoError(t, model.ReserveTaskSubmissionBillingSettlementImmediate(kind,
		model.BillingAdjustmentSpec{
			RequestId: requestId, Operation: "request_preconsume", UserId: userId, TokenId: tokenId,
			UserQuotaDelta: -quota, TokenQuotaDelta: -quota,
		},
		model.BillingSettlementSpec{
			RequestId: requestId, Operation: "request", UserId: userId, TokenId: tokenId,
			FundingSource: BillingSourceWallet, UsingGroup: "default", ChargedGroupRatio: 1,
			ReservedQuota: quota, DeferCommission: true,
		},
	))
	require.NoError(t, model.MarkTaskSubmissionUncertainWithMetadata(requestId, kind, model.TaskSubmissionAttemptMetadata{
		UserId: userId, TokenId: tokenId, ChannelId: channelId, Provider: provider, Model: "manual-model",
		PublicTaskId: publicTaskId, Action: action, UsingGroup: "default", InitialQuota: quota,
	}))
}

func assertManualResolutionProjectionOnce(t *testing.T, userId int, channelId int, quota int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, quota, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelId).Error)
	assert.Equal(t, int64(quota), channel.UsedQuota)
	var projectionCount, logCount, quotaDataCount int64
	require.NoError(t, model.DB.Model(&model.BillingProjectionOutbox{}).Count(&projectionCount).Error)
	require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logCount).Error)
	require.NoError(t, model.DB.Model(&model.QuotaData{}).Count(&quotaDataCount).Error)
	assert.Equal(t, int64(1), projectionCount)
	assert.Equal(t, int64(1), logCount)
	assert.Equal(t, int64(1), quotaDataCount)
}

func TestResolveUncertainGenericAcceptanceIsExactlyOnce(t *testing.T) {
	truncate(t)
	common.LogConsumeEnabled = true
	common.DataExportEnabled = true
	const userId, tokenId, channelId = 9911, 9912, 9913
	const requestId = "manual-generic-accepted"
	const publicTaskId = "task_manual_generic"
	seedUser(t, userId, 100)
	seedToken(t, tokenId, userId, "manual-generic-token", 100)
	seedChannel(t, channelId)
	seedUncertainTaskSubmission(t, requestId, model.TaskSubmissionKindTask, userId, tokenId, channelId,
		publicTaskId, string(constant.TaskPlatform("sora")), "generate", 40)
	input := TaskSubmissionAcceptedResolution{
		ProviderTaskId: "provider-generic-123", FinalQuota: 30,
		PublicResponse: model.TaskSubmissionPublicResponse{
			Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body: `{"id":"task_manual_generic","task_id":"task_manual_generic"}`,
		},
	}
	result, err := ResolveUncertainTaskSubmissionAccepted(requestId, model.TaskSubmissionKindTask, 8001, input)
	require.NoError(t, err)
	assert.True(t, result.Committed)
	result, err = ResolveUncertainTaskSubmissionAccepted(requestId, model.TaskSubmissionKindTask, 8001, input)
	require.NoError(t, err)
	assert.True(t, result.Committed)

	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, 70, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenId).Error)
	assert.Equal(t, 70, token.RemainQuota)
	var tasks int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&tasks).Error)
	assert.Equal(t, int64(1), tasks)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, publicTaskId, task.TaskID)
	assert.Equal(t, input.ProviderTaskId, task.PrivateData.UpstreamTaskID)
	assertManualResolutionProjectionOnce(t, userId, channelId, 30)
	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ?", requestId).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusDeferred, settlement.Status)
	assert.Equal(t, 30, settlement.FinalQuota)

	conflicting := input
	conflicting.FinalQuota = 31
	_, err = ResolveUncertainTaskSubmissionAccepted(requestId, model.TaskSubmissionKindTask, 8001, conflicting)
	require.ErrorContains(t, err, "final quota conflicts")
}

func TestResolveUncertainSwapFaceAcceptanceFinalizesCommissionOnce(t *testing.T) {
	truncate(t)
	common.LogConsumeEnabled = true
	common.DataExportEnabled = true
	const userId, tokenId, channelId = 9921, 9922, 9923
	const requestId = "manual-swapface-accepted"
	seedUser(t, userId, 100)
	seedToken(t, tokenId, userId, "manual-swapface-token", 100)
	seedChannel(t, channelId)
	seedUncertainTaskSubmission(t, requestId, model.TaskSubmissionKindMidjourney, userId, tokenId, channelId,
		"task_manual_swapface", "midjourney-swap-face", constant.MjActionSwapFace, 20)
	input := TaskSubmissionAcceptedResolution{
		ProviderTaskId: "mj-provider-swap-123", FinalQuota: 20,
		PublicResponse: model.TaskSubmissionPublicResponse{
			Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body: `{"code":1,"result":"mj-provider-swap-123"}`,
		},
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := ResolveUncertainTaskSubmissionAccepted(requestId, model.TaskSubmissionKindMidjourney, 8002, input)
		require.NoError(t, err)
		assert.True(t, result.Committed)
	}
	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ?", requestId).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusFinalized, settlement.Status)
	assert.Equal(t, model.BillingCommissionStatusDispatched, settlement.CommissionStatus)
	assert.NotEqual(t, model.BillingSettlementStatusReserved, settlement.Status)
	assert.NotEqual(t, model.BillingSettlementStatusDeferred, settlement.Status)
	var tasks int64
	require.NoError(t, model.DB.Model(&model.Midjourney{}).Count(&tasks).Error)
	assert.Equal(t, int64(1), tasks)
	assertManualResolutionProjectionOnce(t, userId, channelId, 20)
}
