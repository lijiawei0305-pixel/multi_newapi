package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletBillingAlwaysReservesFiniteQuotaBeforeUpstream(t *testing.T) {
	truncate(t)

	const userID = 7001
	const tokenID = 7002
	totalQuota := 12 * int(common.QuotaPerUnit)
	reserveQuota := 7 * int(common.QuotaPerUnit)
	seedUser(t, userID, totalQuota)
	seedToken(t, tokenID, userID, "billing-reserve-token", totalQuota)

	type result struct {
		info   *relaycommon.RelayInfo
		apiErr error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for attempt := 0; attempt < 2; attempt++ {
		go func(attempt int) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("token_quota", totalQuota)
			info := &relaycommon.RelayInfo{
				UserId:          userID,
				TokenId:         tokenID,
				TokenKey:        "billing-reserve-token",
				OriginModelName: "test-model",
				RequestId:       fmt.Sprintf("billing-concurrent-%d", attempt),
				UserSetting: dto.UserSetting{
					BillingPreference: "wallet_only",
				},
			}
			ready.Done()
			<-start
			apiErr := PreConsumeBilling(c, reserveQuota, info)
			if apiErr != nil {
				results <- result{info: info, apiErr: apiErr}
				return
			}
			results <- result{info: info}
		}(attempt)
	}
	ready.Wait()
	close(start)

	successes := 0
	failures := 0
	for attempt := 0; attempt < 2; attempt++ {
		got := <-results
		if got.apiErr != nil {
			failures++
			continue
		}
		successes++
		require.NotNil(t, got.info.Billing)
		assert.Equal(t, reserveQuota, got.info.Billing.GetPreConsumedQuota())
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, failures)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, totalQuota-reserveQuota, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	assert.Equal(t, totalQuota-reserveQuota, token.RemainQuota)
	assert.Equal(t, reserveQuota, token.UsedQuota)
}

func TestBillingCancellationRetriesExactSettlementAndAppliesOnce(t *testing.T) {
	truncate(t)

	const userID = 7101
	const tokenID = 7102
	const quota = 60
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "billing-refund-token", 100)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("token_quota", 100)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "billing-refund-token",
		OriginModelName: "test-model",
		RequestId:       "billing-refund-retry",
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	require.Nil(t, PreConsumeBilling(c, quota, info))

	const callbackName = "test:fail_first_billing_cancel_intent"
	failedOnce := false
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "BillingAdjustmentIntent" || failedOnce {
			return
		}
		failedOnce = true
		tx.AddError(errors.New("injected first refund intent failure"))
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			require.NoError(t, model.DB.Callback().Create().Remove(callbackName))
		}
	})

	var recoveryPending *model.BillingTerminalRecoveryPendingError
	require.ErrorAs(t, info.Billing.Refund(c), &recoveryPending)
	assert.True(t, failedOnce)
	assert.False(t, info.Billing.NeedsRefund())
	require.NoError(t, info.Billing.Refund(c))
	require.NoError(t, model.DB.Callback().Create().Remove(callbackName))
	callbackRegistered = false
	recovered, err := model.ReconcilePendingBillingTerminalRecoveries(10)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	recovered, err = model.ReconcilePendingBillingTerminalRecoveries(10)
	require.NoError(t, err)
	assert.Zero(t, recovered)

	var intent model.BillingAdjustmentIntent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, "request_cancel").First(&intent).Error)
	assert.Equal(t, model.BillingAdjustmentStatusApplied, intent.Status)
	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusCancelled, settlement.Status)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 100, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestBillingCancellationTerminalUpdateFailureReplaysExactRefundOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, quota = 7111, 7112, 60
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "billing-refund-terminal-token", 100)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "billing-refund-terminal-token",
		OriginModelName: "test-model", RequestId: "billing-refund-terminal-update",
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	require.Nil(t, PreConsumeBilling(c, quota, info))

	const callbackName = "test:fail_billing_cancel_terminal_update"
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "BillingSettlementEvent" {
			tx.AddError(errors.New("injected refund terminal update failure"))
		}
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			_ = model.DB.Callback().Update().Remove(callbackName)
		}
	})

	var recoveryPending *model.BillingTerminalRecoveryPendingError
	require.ErrorAs(t, info.Billing.Refund(c), &recoveryPending)
	assert.False(t, info.Billing.NeedsRefund())
	assert.Equal(t, 40, getUserQuota(t, userID))
	var recovery model.BillingTerminalRecovery
	require.NoError(t, model.DB.Where("recovery_key = ?", model.BillingTerminalRecoveryKey(info.RequestId, billingSettlementOperation, model.BillingTerminalRecoveryPhaseCancelled)).First(&recovery).Error)
	assert.Equal(t, model.BillingTerminalRecoveryStatusPending, recovery.Status)
	var payload model.BillingTerminalRecoveryPayload
	require.NoError(t, common.UnmarshalJsonStr(recovery.Payload, &payload))
	assert.True(t, payload.Transition.Cancel)
	assert.Zero(t, payload.Transition.FinalQuota)
	require.NotNil(t, payload.Adjustment)
	assert.Equal(t, quota, payload.Adjustment.UserQuotaDelta)
	assert.Equal(t, quota, payload.Adjustment.TokenQuotaDelta)

	require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	callbackRegistered = false
	recovered, err := model.ReconcilePendingBillingTerminalRecoveries(10)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	recovered, err = model.ReconcilePendingBillingTerminalRecoveries(10)
	require.NoError(t, err)
	assert.Zero(t, recovered)
	assert.Equal(t, 100, getUserQuota(t, userID))
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	var settlement model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusCancelled, settlement.Status)
}

func TestBillingSessionCommissionResolutionFailureKeepsFinalCharge(t *testing.T) {
	truncate(t)
	const userID, tokenID = 7151, 7152
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "billing-resolution-token", 100)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "billing-resolution-token", RequestId: "billing-resolution-pending",
		OriginModelName: "test-model", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	require.Nil(t, PreConsumeBilling(c, 60, info))
	previous := agenthook.MaterializeConsumeCommissionPolicy
	agenthook.MaterializeConsumeCommissionPolicy = func(agenthook.CommissionPolicy, int64, string) (agenthook.CommissionSnapshot, error) {
		return agenthook.CommissionSnapshot{}, errors.New("injected commission lookup failure")
	}
	t.Cleanup(func() { agenthook.MaterializeConsumeCommissionPolicy = previous })

	require.ErrorContains(t, info.Billing.Settle(50), "resolution pending")
	assert.False(t, info.Billing.NeedsRefund())
	require.NoError(t, info.Billing.Settle(50), "durable final state makes in-process retry a no-op")
	var event model.BillingSettlementEvent
	require.NoError(t, model.DB.Where("request_id = ? AND operation = ?", info.RequestId, billingSettlementOperation).First(&event).Error)
	assert.Equal(t, model.BillingSettlementStatusFinalized, event.Status)
	assert.Equal(t, model.BillingSettlementFinancialApplied, event.FinancialStatus)
	assert.Equal(t, model.BillingCommissionStatusResolutionPending, event.CommissionStatus)
	assert.Equal(t, 50, getUserQuota(t, userID))
}

func TestBillingSessionApplyPendingIsAlreadySettled(t *testing.T) {
	truncate(t)
	const userID, tokenID = 7161, 7162
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "billing-apply-pending-token", 100)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "billing-apply-pending-token", RequestId: "billing-apply-pending",
		OriginModelName: "test-model", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	require.Nil(t, PreConsumeBilling(c, 60, info))
	const callbackName = "test:billing_session_apply_pending"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Token" {
			tx.AddError(errors.New("injected token settlement failure"))
		}
	}))
	var pending *model.BillingSettlementApplyPendingError
	require.ErrorAs(t, info.Billing.Settle(40), &pending)
	assert.False(t, info.Billing.NeedsRefund())
	require.NoError(t, info.Billing.Settle(40))
	require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	applied, err := model.ReconcilePendingBillingSettlements(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	assert.Equal(t, 60, getUserQuota(t, userID))
}
