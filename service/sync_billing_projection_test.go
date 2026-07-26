package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newSynchronousWalletBillingFixture(t *testing.T, userId int, tokenId int, channelId int, requestId string, reserve int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	seedUser(t, userId, 1000)
	seedToken(t, tokenId, userId, "sync-projection-token", 1000)
	seedChannel(t, channelId)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("username", "sync-user")
	c.Set("token_name", "sync-token")
	c.Set(common.RequestIdKey, requestId+"-log")
	c.Set(common.UpstreamRequestIdKey, requestId+"-upstream")
	info := &relaycommon.RelayInfo{
		UserId: userId, TokenId: tokenId, TokenKey: "sync-projection-token", RequestId: requestId,
		OriginModelName: "sync-model", UsingGroup: "default",
		StartTime: time.Now().Add(-3 * time.Second), FirstResponseTime: time.Now().Add(-2 * time.Second),
		IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId, UpstreamModelName: "sync-upstream-model"},
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	info.PriceData.ModelRatio = 1
	info.PriceData.CompletionRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	require.Nil(t, PreConsumeBilling(c, reserve, info))
	return c, info
}

func TestSynchronousSettlementHardFactFailureReplaysFrozenTerminalIntent(t *testing.T) {
	truncate(t)
	c, info := newSynchronousWalletBillingFixture(t, 801, 802, 803, "sync-hard-fact", 60)
	const callbackName = "test:fail_sync_terminal_fact"
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "BillingSettlementEvent" {
			tx.AddError(errors.New("injected synchronous terminal fact failure"))
		}
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			_ = model.DB.Callback().Update().Remove(callbackName)
		}
	})

	apiErr := PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 40, TotalTokens: 40}, []string{"sync"})
	require.Nil(t, apiErr, "a durable exact terminal intent keeps the accepted upstream response successful")
	assert.False(t, info.Billing.NeedsRefund(), "durable accepted work must not be refunded while terminal replay is pending")
	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusReserved, settlement.Status)
	var recovery model.BillingTerminalRecovery
	require.NoError(t, model.DB.Where("recovery_key = ?", model.BillingTerminalRecoveryKey(info.RequestId, billingSettlementOperation)).First(&recovery).Error)
	assert.Equal(t, model.BillingTerminalRecoveryStatusPending, recovery.Status)
	assert.NotEmpty(t, recovery.PayloadHash)
	var payload model.BillingTerminalRecoveryPayload
	require.NoError(t, common.UnmarshalJsonStr(recovery.Payload, &payload))
	assert.Equal(t, 40, payload.Transition.FinalQuota)
	require.NotNil(t, payload.Adjustment)
	assert.Equal(t, 20, payload.Adjustment.UserQuotaDelta)
	require.NotNil(t, payload.Projection)
	assert.Equal(t, 40, payload.Projection.LogQuota)
	assert.Equal(t, 40, payload.Projection.LogPromptTokens)
	var outboxCount int64
	require.NoError(t, model.DB.Model(&model.BillingProjectionOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 940, user.Quota)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	assert.Zero(t, countLogs(t))
	require.NoError(t, info.Billing.Refund(c))
	assert.Equal(t, 940, getUserQuota(t, info.UserId), "a deferred refund must not erase accepted usage")

	require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	callbackRegistered = false
	recovered, err := model.ReconcilePendingBillingTerminalRecoveries(10)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	recovered, err = model.ReconcilePendingBillingTerminalRecoveries(10)
	require.NoError(t, err)
	assert.Zero(t, recovered)
	assert.Equal(t, 960, getUserQuota(t, info.UserId))
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 40, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, info.ChannelId).Error)
	assert.Equal(t, int64(40), channel.UsedQuota)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, 40, log.Quota)
	assert.Equal(t, 40, log.PromptTokens)
	require.NoError(t, model.DB.Where("recovery_key = ?", recovery.RecoveryKey).First(&recovery).Error)
	assert.Equal(t, model.BillingTerminalRecoveryStatusApplied, recovery.Status)
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusFinalized, settlement.Status)
	assert.Equal(t, model.BillingSettlementFinancialApplied, settlement.FinancialStatus)
}

func TestSynchronousTerminalJournalFreezeFailureIsProtectedAndSkipRetry(t *testing.T) {
	truncate(t)
	c, info := newSynchronousWalletBillingFixture(t, 806, 807, 808, "sync-journal-freeze-failure", 60)
	const callbackName = "test:fail_sync_terminal_journal_freeze"
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "BillingTerminalRecovery" {
			tx.AddError(errors.New("injected terminal journal freeze failure"))
		}
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			_ = model.DB.Callback().Create().Remove(callbackName)
		}
	})

	apiErr := PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 40, TotalTokens: 40}, nil)
	require.NotNil(t, apiErr)
	require.ErrorContains(t, apiErr, "upstream accepted but billing terminal state is unknown")
	assert.NotContains(t, apiErr.Error(), "injected terminal journal freeze failure", "internal persistence errors must not reach clients")
	assert.Equal(t, types.ErrorCodeUpdateDataError, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, IsUpstreamAccepted(c), "controller must preserve the reservation after upstream acceptance")
	assert.Equal(t, 940, getUserQuota(t, info.UserId))
	var recoveryCount int64
	require.NoError(t, model.DB.Model(&model.BillingTerminalRecovery{}).Count(&recoveryCount).Error)
	assert.Zero(t, recoveryCount)
	var outboxCount int64
	require.NoError(t, model.DB.Model(&model.BillingProjectionOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
	assert.Zero(t, countLogs(t))

	require.NoError(t, model.DB.Callback().Create().Remove(callbackName))
	callbackRegistered = false
}

func TestFinalizeAcceptedBillingFailureSettlesReservedQuotaDurably(t *testing.T) {
	truncate(t)
	c, info := newSynchronousWalletBillingFixture(t, 851, 852, 853, "sync-accepted-malformed", 60)
	serviceErr := types.NewErrorWithStatusCode(
		errors.New("upstream response was malformed: private-provider-canary"),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
		types.ErrOptionWithSkipRetry(),
	)
	MarkUpstreamAccepted(c)

	result := FinalizeAcceptedBillingFailure(c, info, serviceErr)
	require.NotNil(t, result)
	assert.NotSame(t, serviceErr, result)
	assert.Equal(t, http.StatusBadGateway, result.StatusCode)
	assert.True(t, types.IsSkipRetryError(result))
	assert.NotContains(t, result.Error(), "private-provider-canary")
	assert.Equal(t, "upstream accepted; response unavailable; do not retry/contact admin", result.Error())
	assert.True(t, IsBillingTerminalAttempted(c))
	assert.False(t, info.Billing.NeedsRefund())
	assert.Equal(t, 940, getUserQuota(t, info.UserId), "the immutable reservation is the conservative final charge")

	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusFinalized, settlement.Status)
	assert.Equal(t, 60, settlement.FinalQuota)
	var recovery model.BillingTerminalRecovery
	require.NoError(t, model.DB.Where("recovery_key = ?", model.BillingTerminalRecoveryKey(info.RequestId, billingSettlementOperation)).First(&recovery).Error)
	assert.Equal(t, model.BillingTerminalRecoveryStatusApplied, recovery.Status)

	var outbox model.BillingProjectionOutbox
	require.NoError(t, model.DB.Where("projection_key = ?", model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "sync_accepted_failure")).First(&outbox).Error)
	assert.Equal(t, model.BillingProjectionStatusApplied, outbox.Status)
	assert.Equal(t, 60, outbox.LogQuota)
	assert.Contains(t, outbox.LogOther, `"billing_fallback":"reserved_quota"`)
}

func TestFinalizeAcceptedBillingFailureCountsZeroReservedRequest(t *testing.T) {
	truncate(t)
	c, info := newSynchronousWalletBillingFixture(t, 861, 862, 863, "sync-accepted-zero-reserve", 0)
	MarkUpstreamAccepted(c)

	result := FinalizeAcceptedBillingFailure(c, info, types.NewError(errors.New("malformed"), types.ErrorCodeBadResponseBody))
	require.NotNil(t, result)
	assert.Equal(t, http.StatusBadGateway, result.StatusCode)

	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusFinalized, settlement.Status)
	assert.Zero(t, settlement.FinalQuota)
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount, "accepted zero-quota work still counts as a completed request")
	var recovery model.BillingTerminalRecovery
	require.NoError(t, model.DB.Where("recovery_key = ?", model.BillingTerminalRecoveryKey(info.RequestId, billingSettlementOperation)).First(&recovery).Error)
	assert.Equal(t, model.BillingTerminalRecoveryStatusApplied, recovery.Status)
}

func TestFinalizeAcceptedBillingFailureNilContextIsSafe(t *testing.T) {
	truncate(t)
	_, info := newSynchronousWalletBillingFixture(t, 866, 867, 868, "sync-accepted-nil-context", 10)
	apiErr := types.NewError(errors.New("malformed"), types.ErrorCodeBadResponseBody)

	require.NotPanics(t, func() {
		assert.Same(t, apiErr, FinalizeAcceptedBillingFailure(nil, info, apiErr))
	})
}

func TestFinalizeAcceptedBillingFailureDoesNotOverwriteActualSettlementAttempt(t *testing.T) {
	truncate(t)
	c, info := newSynchronousWalletBillingFixture(t, 856, 857, 858, "sync-actual-attempt", 60)
	MarkUpstreamAccepted(c)
	require.NoError(t, SettleBilling(c, info, 40))
	assert.True(t, IsBillingTerminalAttempted(c))

	serviceErr := types.NewErrorWithStatusCode(errors.New("delivery failed"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	require.Same(t, serviceErr, FinalizeAcceptedBillingFailure(c, info, serviceErr))

	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, 40, settlement.FinalQuota, "fallback quota must not overwrite an actual-quota terminal attempt")
	assert.Equal(t, 960, getUserQuota(t, info.UserId))
}

func TestFinalizeAcceptedBillingFailureHidesCauseForFreeModel(t *testing.T) {
	truncate(t)
	seedUser(t, 871, 1000)
	seedToken(t, 872, 871, "free-fallback-token", 1000)
	seedChannel(t, 873)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 871, TokenId: 872, RequestId: "free-accepted-fallback", OriginModelName: "free-model", UsingGroup: "default",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 873},
	}
	MarkUpstreamAccepted(c)
	privateErr := types.NewError(errors.New("provider-private-malformed-payload"), types.ErrorCodeBadResponseBody)

	result := FinalizeAcceptedBillingFailure(c, info, privateErr)

	require.NotNil(t, result)
	assert.Equal(t, http.StatusBadGateway, result.StatusCode)
	assert.True(t, types.IsSkipRetryError(result))
	assert.NotContains(t, result.Error(), "provider-private")
	assert.True(t, IsBillingTerminalAttempted(c))
	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusFinalized, settlement.Status)
	assert.Zero(t, settlement.FinalQuota)
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 1, user.RequestCount)
	assert.Zero(t, user.UsedQuota)
}

func TestSynchronousProjectionLogFailureReplaysCompletePayloadExactlyOnce(t *testing.T) {
	truncate(t)
	oldDataExport, oldNodeName := common.DataExportEnabled, common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "sync-node"
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExport
		common.NodeName = oldNodeName
	})
	c, info := newSynchronousWalletBillingFixture(t, 811, 812, 813, "sync-log-replay", 80)
	const callbackName = "test:fail_sync_projection_log"
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Log" {
			tx.AddError(errors.New("injected synchronous log sink failure"))
		}
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			_ = model.DB.Callback().Create().Remove(callbackName)
		}
	})

	apiErr := PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 40, CompletionTokens: 10, TotalTokens: 50}, []string{"complete payload"})
	require.Nil(t, apiErr, "sink failure leaves a durable outbox and must not undo the terminal fact")
	require.NoError(t, model.DB.Callback().Create().Remove(callbackName))
	callbackRegistered = false
	assert.Equal(t, 950, getUserQuota(t, info.UserId), "actual quota below reserve refunds the exact delta")
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	assert.Zero(t, countLogs(t))

	projectionKey := model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "sync_consume")
	var outbox model.BillingProjectionOutbox
	require.NoError(t, model.DB.Where("projection_key = ?", projectionKey).First(&outbox).Error)
	assert.Equal(t, model.BillingProjectionStatusPending, outbox.Status)
	assert.Equal(t, 50, outbox.LogQuota)
	assert.Equal(t, 40, outbox.LogPromptTokens)
	assert.Equal(t, 10, outbox.LogCompletionTokens)
	// use_time 是 Unix() 整秒截断差（text_quota.go）：夹具 StartTime 在 3s 前，夹具建库到
	// 扣费之间一旦跨过墙钟整秒边界结果即为 4，与耗时长短无关。契约是「重放逐字保真快照」，
	// 精确断言放在重放侧，这里只锚定快照确实源自 StartTime。
	snapshotUseTime := outbox.LogUseTime
	assert.GreaterOrEqual(t, snapshotUseTime, 3)
	assert.Less(t, snapshotUseTime, 60)
	assert.True(t, outbox.LogIsStream)
	assert.Equal(t, 1, outbox.AttemptCount)

	projected, err := model.ReconcilePendingBillingProjections(10)
	require.NoError(t, err)
	assert.Equal(t, 1, projected)
	projected, err = model.ReconcilePendingBillingProjections(10)
	require.NoError(t, err)
	assert.Zero(t, projected)
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 50, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, info.ChannelId).Error)
	assert.Equal(t, int64(50), channel.UsedQuota)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, 40, log.PromptTokens)
	assert.Equal(t, 10, log.CompletionTokens)
	assert.Equal(t, snapshotUseTime, log.UseTime)
	assert.True(t, log.IsStream)
	assert.Equal(t, info.RequestId+"-upstream", log.UpstreamRequestId)
	var quotaData model.QuotaData
	require.NoError(t, model.DB.First(&quotaData).Error)
	assert.Equal(t, 1, quotaData.Count)
	assert.Equal(t, 50, quotaData.Quota)
	assert.Equal(t, 50, quotaData.TokenUsed)
	assert.Equal(t, "sync-node", quotaData.NodeName)
}

func TestSynchronousFinancialApplyFailureKeepsSuccessfulResponseReplayable(t *testing.T) {
	truncate(t)
	c, info := newSynchronousWalletBillingFixture(t, 816, 817, 818, "sync-financial-replay", 80)
	const callbackName = "test:fail_sync_financial_apply"
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Token" {
			tx.AddError(errors.New("injected synchronous financial apply failure"))
		}
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			_ = model.DB.Callback().Update().Remove(callbackName)
		}
	})

	require.Nil(t, PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 50, TotalTokens: 50}, nil), "durable terminal fact must not turn an accepted upstream response into a retry")
	assert.False(t, info.Billing.NeedsRefund())
	var outbox model.BillingProjectionOutbox
	require.NoError(t, model.DB.Where("projection_key = ?", model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "sync_consume")).First(&outbox).Error)
	assert.Equal(t, model.BillingProjectionStatusPending, outbox.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)

	require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	callbackRegistered = false
	settled, err := model.ReconcilePendingBillingSettlements(10)
	require.NoError(t, err)
	assert.Equal(t, 1, settled)
	projected, err := model.ReconcilePendingBillingProjections(10)
	require.NoError(t, err)
	assert.Equal(t, 1, projected)
	assert.Equal(t, 950, getUserQuota(t, info.UserId))
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 50, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
}

func TestSynchronousSettlementChargesAboveReserveAndCountsOnce(t *testing.T) {
	truncate(t)
	c, info := newSynchronousWalletBillingFixture(t, 821, 822, 823, "sync-above-reserve", 20)
	if apiErr := PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 50, TotalTokens: 50}, nil); apiErr != nil {
		t.Fatal(apiErr.Error())
	}
	assert.Equal(t, 950, getUserQuota(t, info.UserId))
	var user model.User
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 50, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	require.NoError(t, model.ApplyBillingProjection(model.BillingProjectionKey(info.RequestId, billingSettlementOperation, "sync_consume")))
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	assert.Equal(t, 50, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
}

func TestViolationFeeUsesAtomicDurableProjection(t *testing.T) {
	truncate(t)
	const userId, tokenId, channelId = 831, 832, 833
	seedUser(t, userId, 1000)
	seedToken(t, tokenId, userId, "violation-projection-token", 1000)
	seedChannel(t, channelId)
	settings := model_setting.GetGrokSettings()
	oldEnabled, oldAmount := settings.ViolationDeductionEnabled, settings.ViolationDeductionAmount
	settings.ViolationDeductionEnabled = true
	settings.ViolationDeductionAmount = 2 / common.QuotaPerUnit
	t.Cleanup(func() {
		settings.ViolationDeductionEnabled = oldEnabled
		settings.ViolationDeductionAmount = oldAmount
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("username", "violation-user")
	c.Set("token_name", "violation-token")
	info := &relaycommon.RelayInfo{
		UserId: userId, TokenId: tokenId, RequestId: "violation-projection", OriginModelName: "grok-test",
		UsingGroup: "default", BillingSource: BillingSourceWallet, StartTime: time.Now().Add(-time.Second),
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
	}
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	apiErr := types.NewError(errors.New(CSAMViolationMarker), types.ErrorCodeViolationFeeGrokCSAM)
	require.True(t, ChargeViolationFeeIfNeeded(c, info, apiErr))
	assert.Equal(t, 998, getUserQuota(t, userId))
	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, 2, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelId).Error)
	assert.Equal(t, int64(2), channel.UsedQuota)
	var outbox model.BillingProjectionOutbox
	key := model.BillingProjectionKey(info.RequestId, "violation_fee", "violation_fee")
	require.NoError(t, model.DB.Where("projection_key = ?", key).First(&outbox).Error)
	assert.Equal(t, model.BillingProjectionStatusApplied, outbox.Status)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestSynchronousSubscriptionProjectionFreezesFinalPeriodUsage(t *testing.T) {
	truncate(t)
	const userId, tokenId, channelId, subscriptionId = 841, 842, 843, 844
	seedUser(t, userId, 1000)
	seedToken(t, tokenId, userId, "subscription-projection-token", 1000)
	seedChannel(t, channelId)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: subscriptionId, Title: "projection-plan", TotalAmount: 1000,
		QuotaResetPeriod: model.SubscriptionResetNever, DurationUnit: model.SubscriptionDurationMonth,
		DurationValue: 1, Enabled: true,
	}).Error)
	seedSubscription(t, subscriptionId, userId, 1000, 0)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionId).Update("plan_id", subscriptionId).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("username", "subscription-user")
	c.Set("token_name", "subscription-token")
	info := &relaycommon.RelayInfo{
		UserId: userId, TokenId: tokenId, TokenKey: "subscription-projection-token", RequestId: "sync-subscription",
		OriginModelName: "subscription-model", UsingGroup: "default", StartTime: time.Now().Add(-time.Second),
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
	}
	info.PriceData.ModelRatio = 1
	info.PriceData.CompletionRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	if apiErr := PreConsumeBilling(c, 80, info); apiErr != nil {
		t.Fatal(apiErr.Error())
	}
	if apiErr := PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 50, TotalTokens: 50}, nil); apiErr != nil {
		t.Fatal(apiErr.Error())
	}
	assert.Equal(t, 1000, getUserQuota(t, userId), "subscription billing must not debit wallet quota")
	assert.Equal(t, int64(50), getSubscriptionUsed(t, subscriptionId))
	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, 50, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	log := getLastLog(t)
	require.NotNil(t, log)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	assert.EqualValues(t, -30, other["subscription_post_delta"])
	assert.EqualValues(t, 50, other["subscription_consumed"])
	assert.EqualValues(t, 50, other["subscription_used"])
	assert.EqualValues(t, 950, other["subscription_remain"])
	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Positive(t, settlement.SubscriptionOccurredAt)
}
