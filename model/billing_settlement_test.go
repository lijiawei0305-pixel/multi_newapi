package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedSettlementWallet(t *testing.T, userId int, tokenId int, quota int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: userId, Username: "settlement-user", Quota: quota, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&Token{Id: tokenId, UserId: userId, Key: "settlement-token", Name: "settlement", Status: common.TokenStatusEnabled, RemainQuota: quota}).Error)
}

func walletSettlementSpec(requestId string, userId int, tokenId int, reserved int) BillingSettlementSpec {
	return BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: userId, TokenId: tokenId,
		FundingSource: "wallet", UsingGroup: "default", ChargedGroupRatio: 1, ReservedQuota: reserved,
	}
}

func walletReserveAdjustment(requestId string, operation string, userId int, tokenId int, delta int) BillingAdjustmentSpec {
	return BillingAdjustmentSpec{
		RequestId: requestId, Operation: operation, UserId: userId, TokenId: tokenId,
		UserQuotaDelta: -delta, TokenQuotaDelta: -delta,
	}
}

func noAttributionCommission(requestId string) *BillingCommissionSnapshot {
	return &BillingCommissionSnapshot{
		SourceId: BillingCommissionSourceId(requestId, "request"), OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

type dailyResetSettlementFixture struct {
	requestId string
	oldTime   int64
	sub       *UserSubscription
	token     *Token
}

func seedDailyResetSettlement(t *testing.T, requestId string) dailyResetSettlementFixture {
	t.Helper()
	truncateTables(t)
	now := time.Unix(GetDBTimestamp(), 0)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	oldTime := today.Add(-time.Hour).Unix()
	const planId, userId, subscriptionId, tokenId = 9901, 9902, 9903, 9904
	plan := &SubscriptionPlan{
		Id: planId, Title: "daily-reset", TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetDaily,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	sub := &UserSubscription{
		Id: subscriptionId, UserId: userId, PlanId: planId, AmountTotal: 1000, AmountUsed: 100,
		StartTime: today.AddDate(0, 0, -2).Add(12 * time.Hour).Unix(), EndTime: today.AddDate(0, 1, 0).Unix(),
		Status: "active", Source: "order", BenefitSnapshotVersion: SubscriptionOrderSnapshotVersion,
		PlanTitle: plan.Title, QuotaResetPeriod: SubscriptionResetDaily,
	}
	token := &Token{
		Id: tokenId, UserId: userId, Key: "never-to-daily-token", Name: "never-to-daily",
		Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100,
	}
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: userId, TokenId: tokenId,
		SubscriptionId: subscriptionId, SubscriptionOccurredAt: oldTime,
		FundingSource: "subscription", ReservedQuota: 100,
	}))
	res, err := PreConsumeUserSubscription(requestId+"-new-period", userId, "gpt-4o", 0, 10)
	require.NoError(t, err)
	require.Equal(t, today.Unix(), res.ResetEpoch)
	require.Greater(t, res.ResetEpoch, oldTime)
	require.Positive(t, res.OccurredAt)
	require.Equal(t, int64(10), meterUsed(t, subscriptionId))
	return dailyResetSettlementFixture{
		requestId: requestId, oldTime: oldTime, sub: sub, token: token,
	}
}

func TestBillingSettlementReservationGrowthAndFinalization(t *testing.T) {
	truncateTables(t)
	const userId, tokenId = 9501, 9502
	const requestId = "settlement-grow-final"
	seedSettlementWallet(t, userId, tokenId, 1000)

	require.NoError(t, ReserveBillingSettlementImmediate(
		walletReserveAdjustment(requestId, "preconsume", userId, tokenId, 100),
		walletSettlementSpec(requestId, userId, tokenId, 100),
	))
	additional := walletReserveAdjustment(requestId, "reserve_150", userId, tokenId, 50)
	require.NoError(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, additional))
	require.NoError(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, additional), "exact replay must be a no-op")
	require.Error(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 140, additional), "a stale caller must not silently accept a lower target")

	wrongKey := additional
	wrongKey.Operation = "reserve_150_wrong"
	require.Error(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, wrongKey))
	wrongPayload := additional
	wrongPayload.UserQuotaDelta = -49
	require.Error(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, wrongPayload))

	commission := noAttributionCommission(requestId)
	require.NoError(t, FinalizeBillingSettlement(BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 120, ReleaseCommission: true, Commission: commission,
	}, &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "settle", UserId: userId, TokenId: tokenId,
		UserQuotaDelta: 30, TokenQuotaDelta: 30,
	}))

	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", requestId, "request").First(&event).Error)
	assert.Equal(t, 100, event.InitialReservedQuota)
	assert.Equal(t, 150, event.ReservedQuota)
	assert.Equal(t, 120, event.FinalQuota)
	assert.Equal(t, BillingSettlementStatusFinalized, event.Status)
	require.NotNil(t, event.CommissionOccurredAt)
	var user User
	var token Token
	require.NoError(t, DB.First(&user, userId).Error)
	require.NoError(t, DB.First(&token, tokenId).Error)
	assert.Equal(t, 880, user.Quota)
	assert.Equal(t, 880, token.RemainQuota)
	assert.Equal(t, 120, token.UsedQuota)
}

func TestBillingSettlementSubscriptionCancelRefundsInitialAndExtra(t *testing.T) {
	truncateTables(t)
	const requestId = "settlement-sub-cancel"
	sub := &UserSubscription{Id: 9601, UserId: 9600, AmountTotal: 1000, AmountUsed: 100, Status: "active"}
	token := &Token{Id: 9602, UserId: 9600, Key: "settlement-sub-token", Name: "sub", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(&SubscriptionPreConsumeRecord{
		RequestId: requestId, UserId: sub.UserId, UserSubscriptionId: sub.Id, PreConsumed: 100, Status: "consumed",
	}).Error)
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: sub.UserId, TokenId: token.Id, SubscriptionId: sub.Id,
		SubscriptionPreConsumeRequestId: requestId, SubscriptionOccurredAt: common.GetTimestamp(), FundingSource: "subscription", ReservedQuota: 100, DeferCommission: true,
	}))
	additional := BillingAdjustmentSpec{
		RequestId: requestId, Operation: "reserve_150", SubscriptionId: sub.Id, TokenId: token.Id,
		SubscriptionQuotaDelta: 50, TokenQuotaDelta: -50,
	}
	require.NoError(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, additional))
	require.NoError(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, additional), "same-period reserve replay must be a no-op")
	transition := BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 0, Cancel: true,
	}
	cancel := &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "cancel", SubscriptionId: sub.Id, SubscriptionRequestId: requestId,
		TokenId: token.Id, SubscriptionQuotaDelta: -50, TokenQuotaDelta: 150,
	}
	require.NoError(t, FinalizeBillingSettlement(transition, cancel))
	require.NoError(t, FinalizeBillingSettlement(transition, cancel), "same-period cancellation replay must be a no-op")

	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, 1000, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", requestId).First(&record).Error)
	assert.Equal(t, "refunded", record.Status)
}

func TestBillingSettlementSubscriptionDeferredThenCancelRefundsFinalUsage(t *testing.T) {
	truncateTables(t)
	const requestId = "settlement-sub-deferred-cancel"
	sub := &UserSubscription{Id: 9651, UserId: 9650, AmountTotal: 1000, AmountUsed: 100, Status: "active"}
	token := &Token{Id: 9652, UserId: 9650, Key: "settlement-sub-deferred-token", Name: "sub", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(&SubscriptionPreConsumeRecord{
		RequestId: requestId, UserId: sub.UserId, UserSubscriptionId: sub.Id, PreConsumed: 100, Status: "consumed",
	}).Error)
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: sub.UserId, TokenId: token.Id, SubscriptionId: sub.Id,
		SubscriptionPreConsumeRequestId: requestId, SubscriptionOccurredAt: common.GetTimestamp(), FundingSource: "subscription", ReservedQuota: 100, DeferCommission: true,
	}))
	require.NoError(t, ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, BillingAdjustmentSpec{
		RequestId: requestId, Operation: "reserve_150", SubscriptionId: sub.Id, TokenId: token.Id,
		SubscriptionQuotaDelta: 50, TokenQuotaDelta: -50,
	}))
	deferredTransition := BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 120, ReleaseCommission: false,
	}
	deferredAdjustment := &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "defer", SubscriptionId: sub.Id, TokenId: token.Id,
		SubscriptionQuotaDelta: -30, TokenQuotaDelta: 30,
	}
	require.NoError(t, FinalizeBillingSettlement(deferredTransition, deferredAdjustment))
	require.NoError(t, FinalizeBillingSettlement(deferredTransition, deferredAdjustment), "same deferred phase replay must be exact and idempotent")
	cancelTransition := BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 0, Cancel: true,
	}
	cancelAdjustment := &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "cancel", SubscriptionId: sub.Id, SubscriptionRequestId: requestId,
		TokenId: token.Id, SubscriptionQuotaDelta: -20, TokenQuotaDelta: 120,
	}
	require.NoError(t, FinalizeBillingSettlement(cancelTransition, cancelAdjustment))
	require.NoError(t, FinalizeBillingSettlement(cancelTransition, cancelAdjustment), "same cancellation phase replay must be exact and idempotent")
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, 1000, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	deferredKey := BillingTerminalRecoveryKey(requestId, "request", BillingTerminalRecoveryPhaseDeferred)
	cancelledKey := BillingTerminalRecoveryKey(requestId, "request", BillingTerminalRecoveryPhaseCancelled)
	assert.NotEqual(t, deferredKey, cancelledKey)
	var recoveries []BillingTerminalRecovery
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", requestId, "request").Order("id").Find(&recoveries).Error)
	require.Len(t, recoveries, 2)
	assert.Equal(t, deferredKey, recoveries[0].RecoveryKey)
	assert.Equal(t, BillingTerminalRecoveryPhaseDeferred, recoveries[0].Phase)
	assert.Equal(t, BillingTerminalRecoveryStatusApplied, recoveries[0].Status)
	assert.Equal(t, cancelledKey, recoveries[1].RecoveryKey)
	assert.Equal(t, BillingTerminalRecoveryPhaseCancelled, recoveries[1].Phase)
	assert.Equal(t, BillingTerminalRecoveryStatusApplied, recoveries[1].Status)
}

func TestBillingSettlementDeferredThenFinalizedUsesDistinctRecoveryPhases(t *testing.T) {
	truncateTables(t)
	const userId, tokenId = 9671, 9672
	const requestId = "settlement-deferred-finalized-phases"
	seedSettlementWallet(t, userId, tokenId, 1000)
	settlement := walletSettlementSpec(requestId, userId, tokenId, 100)
	settlement.DeferCommission = true
	require.NoError(t, ReserveBillingSettlementImmediate(
		walletReserveAdjustment(requestId, "preconsume", userId, tokenId, 100), settlement,
	))
	deferred := BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 100, ReleaseCommission: false,
	}
	require.NoError(t, FinalizeBillingSettlement(deferred, nil))
	require.NoError(t, FinalizeBillingSettlement(deferred, nil), "same deferred phase replay must be exact and idempotent")
	finalized := BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 120, ReleaseCommission: true,
		Commission: noAttributionCommission(requestId),
	}
	finalAdjustment := &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "settle", UserId: userId, TokenId: tokenId,
		UserQuotaDelta: -20, TokenQuotaDelta: -20,
	}
	require.NoError(t, FinalizeBillingSettlement(finalized, finalAdjustment))
	require.NoError(t, FinalizeBillingSettlement(finalized, finalAdjustment), "same finalized phase replay must be exact and idempotent")

	deferredKey := BillingTerminalRecoveryKey(requestId, "request", BillingTerminalRecoveryPhaseDeferred)
	finalizedKey := BillingTerminalRecoveryKey(requestId, "request", BillingTerminalRecoveryPhaseFinalized)
	assert.NotEqual(t, deferredKey, finalizedKey)
	var recoveries []BillingTerminalRecovery
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", requestId, "request").Order("id").Find(&recoveries).Error)
	require.Len(t, recoveries, 2)
	assert.Equal(t, deferredKey, recoveries[0].RecoveryKey)
	assert.Equal(t, BillingTerminalRecoveryPhaseDeferred, recoveries[0].Phase)
	assert.Equal(t, BillingTerminalRecoveryStatusApplied, recoveries[0].Status)
	assert.Equal(t, finalizedKey, recoveries[1].RecoveryKey)
	assert.Equal(t, BillingTerminalRecoveryPhaseFinalized, recoveries[1].Phase)
	assert.Equal(t, BillingTerminalRecoveryStatusApplied, recoveries[1].Status)
	var user User
	var token Token
	require.NoError(t, DB.First(&user, userId).Error)
	require.NoError(t, DB.First(&token, tokenId).Error)
	assert.Equal(t, 880, user.Quota)
	assert.Equal(t, 880, token.RemainQuota)
	assert.Equal(t, 120, token.UsedQuota)
}

func TestBillingSettlementPendingTerminalReplayApplies(t *testing.T) {
	truncateTables(t)
	const userId, tokenId = 9701, 9702
	const requestId = "settlement-pending-replay"
	seedSettlementWallet(t, userId, tokenId, 1000)
	require.NoError(t, ReserveBillingSettlementImmediate(
		walletReserveAdjustment(requestId, "preconsume", userId, tokenId, 100),
		walletSettlementSpec(requestId, userId, tokenId, 100),
	))
	transition := BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 80, ReleaseCommission: true,
		Commission: noAttributionCommission(requestId),
	}
	adjustment := &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "settle", UserId: userId, TokenId: tokenId,
		UserQuotaDelta: 20, TokenQuotaDelta: 20,
	}
	const callbackName = "test:billing_settlement_pending_replay"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Token" {
			tx.AddError(errors.New("injected token update failure"))
		}
	}))
	var pending *BillingSettlementApplyPendingError
	require.ErrorAs(t, FinalizeBillingSettlement(transition, adjustment), &pending)
	require.NoError(t, DB.Callback().Update().Remove(callbackName))

	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", requestId, "request").First(&event).Error)
	assert.Equal(t, BillingSettlementStatusFinalized, event.Status)
	assert.Equal(t, BillingSettlementFinancialPending, event.FinancialStatus)
	require.NoError(t, FinalizeBillingSettlement(transition, adjustment), "same terminal payload must retry the pending apply")
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", requestId, "request").First(&event).Error)
	assert.Equal(t, BillingSettlementFinancialApplied, event.FinancialStatus)
}

func TestBillingSettlementReservedHasNullableCommissionTime(t *testing.T) {
	truncateTables(t)
	seedSettlementWallet(t, 9801, 9802, 100)
	require.NoError(t, ReserveBillingSettlementImmediate(
		walletReserveAdjustment("settlement-null-time", "preconsume", 9801, 9802, 10),
		walletSettlementSpec("settlement-null-time", 9801, 9802, 10),
	))
	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ?", "settlement-null-time").First(&event).Error)
	assert.Nil(t, event.CommissionOccurredAt)
}

func TestBillingSettlementRejectsAdditionalReserveAfterSubscriptionReset(t *testing.T) {
	truncateTables(t)
	const requestId = "settlement-sub-reset-reserve"
	sub := &UserSubscription{Id: 9851, UserId: 9850, AmountTotal: 1000, AmountUsed: 0, LastResetTime: 200, Status: "active"}
	token := &Token{Id: 9852, UserId: 9850, Key: "settlement-reset-token", Name: "sub", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: sub.UserId, TokenId: token.Id, SubscriptionId: sub.Id,
		SubscriptionResetEpoch: 100, SubscriptionOccurredAt: 100, FundingSource: "subscription", ReservedQuota: 100,
	}))
	err := ReserveBillingSettlementAdditionalImmediate(requestId, "request", 150, BillingAdjustmentSpec{
		RequestId: requestId, Operation: "reserve_150", SubscriptionId: sub.Id, SubscriptionResetEpoch: 100,
		TokenId: token.Id, SubscriptionQuotaDelta: 50, TokenQuotaDelta: -50,
	})
	require.ErrorContains(t, err, "expired subscription period")
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, 900, token.RemainQuota)
	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ?", requestId).First(&event).Error)
	assert.Equal(t, 100, event.ReservedQuota)
	var count int64
	require.NoError(t, DB.Model(&BillingAdjustmentIntent{}).Where("request_id = ? AND operation = ?", requestId, "reserve_150").Count(&count).Error)
	assert.Zero(t, count)
}

func TestBillingSettlementPositiveDeltaAfterSubscriptionResetIsExplicitlyAbsorbed(t *testing.T) {
	truncateTables(t)
	const requestId = "settlement-sub-reset-positive-final"
	sub := &UserSubscription{Id: 9861, UserId: 9860, AmountTotal: 1000, AmountUsed: 0, LastResetTime: 200, Status: "active"}
	token := &Token{Id: 9862, UserId: 9860, Key: "settlement-reset-final-token", Name: "sub", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: sub.UserId, TokenId: token.Id, SubscriptionId: sub.Id,
		SubscriptionResetEpoch: 100, SubscriptionOccurredAt: 100, FundingSource: "subscription", ReservedQuota: 100,
	}))
	err := FinalizeBillingSettlement(BillingSettlementTransition{
		RequestId: requestId, Operation: "request", FinalQuota: 120, ReleaseCommission: true,
	}, &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "settle", SubscriptionId: sub.Id, SubscriptionResetEpoch: 100,
		TokenId: token.Id, SubscriptionQuotaDelta: 20, TokenQuotaDelta: -20,
	})
	require.NoError(t, err)
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, 880, token.RemainQuota, "token accounting remains exact while old subscription-period debt is absorbed")
	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ?", requestId).First(&event).Error)
	assert.Equal(t, BillingSettlementStatusFinalized, event.Status)
	assert.Equal(t, BillingSettlementFinancialApplied, event.FinancialStatus)
	var intent BillingAdjustmentIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", requestId, "settle").First(&intent).Error)
	assert.Equal(t, "expired_period_debt_absorbed", intent.SubscriptionEpochOutcome)
}

func TestBillingSettlementDailyResetSkipsOldSettleAndReplays(t *testing.T) {
	fixture := seedDailyResetSettlement(t, "settlement-daily-settle")
	adjustment := &BillingAdjustmentSpec{
		RequestId: fixture.requestId, Operation: "settle", SubscriptionId: fixture.sub.Id,
		TokenId: fixture.token.Id, SubscriptionQuotaDelta: 20, TokenQuotaDelta: -20,
	}
	transition := BillingSettlementTransition{
		RequestId: fixture.requestId, Operation: "request", FinalQuota: 120, ReleaseCommission: true,
	}
	require.NoError(t, FinalizeBillingSettlement(transition, adjustment))
	require.NoError(t, FinalizeBillingSettlement(transition, adjustment), "exact terminal replay must be a no-op")

	require.Equal(t, int64(10), meterUsed(t, fixture.sub.Id), "old request debt must not enter the new daily period")
	require.NoError(t, DB.First(fixture.token, fixture.token.Id).Error)
	assert.Equal(t, 880, fixture.token.RemainQuota)
	assert.Equal(t, 120, fixture.token.UsedQuota)
	var intent BillingAdjustmentIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", fixture.requestId, "settle").First(&intent).Error)
	assert.Equal(t, fixture.oldTime, intent.SubscriptionOccurredAt)
	assert.Equal(t, "expired_period_debt_absorbed", intent.SubscriptionEpochOutcome)
	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", fixture.requestId, "request").First(&event).Error)
	assert.Equal(t, fixture.oldTime, event.SubscriptionOccurredAt)
	assert.Equal(t, fixture.oldTime, event.AdjustmentSubscriptionOccurredAt)
}

func TestBillingSettlementDailyResetRollsBackAdditionalReserve(t *testing.T) {
	fixture := seedDailyResetSettlement(t, "settlement-daily-reserve")
	err := ReserveBillingSettlementAdditionalImmediate(fixture.requestId, "request", 150, BillingAdjustmentSpec{
		RequestId: fixture.requestId, Operation: "reserve_150", SubscriptionId: fixture.sub.Id,
		TokenId: fixture.token.Id, SubscriptionQuotaDelta: 50, TokenQuotaDelta: -50,
	})
	require.ErrorContains(t, err, "expired subscription period")
	require.Equal(t, int64(10), meterUsed(t, fixture.sub.Id))
	require.NoError(t, DB.First(fixture.token, fixture.token.Id).Error)
	assert.Equal(t, 900, fixture.token.RemainQuota, "token mutation must roll back with the expired reserve")
	assert.Equal(t, 100, fixture.token.UsedQuota)
	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", fixture.requestId, "request").First(&event).Error)
	assert.Equal(t, 100, event.ReservedQuota)
	var count int64
	require.NoError(t, DB.Model(&BillingAdjustmentIntent{}).
		Where("request_id = ? AND operation = ?", fixture.requestId, "reserve_150").Count(&count).Error)
	assert.Zero(t, count)
}

func TestBillingSettlementDailyResetSkipsOldCancelAndReplays(t *testing.T) {
	fixture := seedDailyResetSettlement(t, "settlement-daily-cancel")
	adjustment := &BillingAdjustmentSpec{
		RequestId: fixture.requestId, Operation: "cancel", SubscriptionId: fixture.sub.Id,
		TokenId: fixture.token.Id, SubscriptionQuotaDelta: -100, TokenQuotaDelta: 100,
	}
	transition := BillingSettlementTransition{
		RequestId: fixture.requestId, Operation: "request", FinalQuota: 0, Cancel: true,
	}
	require.NoError(t, FinalizeBillingSettlement(transition, adjustment))
	require.NoError(t, FinalizeBillingSettlement(transition, adjustment), "exact cancellation replay must be a no-op")

	require.Equal(t, int64(10), meterUsed(t, fixture.sub.Id), "old cancellation must not refund from the new daily period")
	require.NoError(t, DB.First(fixture.token, fixture.token.Id).Error)
	assert.Equal(t, 1000, fixture.token.RemainQuota)
	assert.Zero(t, fixture.token.UsedQuota)
	var intent BillingAdjustmentIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", fixture.requestId, "cancel").First(&intent).Error)
	assert.Equal(t, fixture.oldTime, intent.SubscriptionOccurredAt)
	assert.Equal(t, "expired_period_skipped", intent.SubscriptionEpochOutcome)
}

func TestBillingSettlementEpochZeroSamePeriodAppliesAndReplays(t *testing.T) {
	truncateTables(t)
	const requestId = "settlement-epoch-zero-same-period"
	sub := &UserSubscription{Id: 9911, UserId: 9910, AmountTotal: 1000, AmountUsed: 100, LastResetTime: 200, Status: "active"}
	token := &Token{Id: 9912, UserId: 9910, Key: "same-period-token", Name: "same-period", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: requestId, Operation: "request", UserId: sub.UserId, TokenId: token.Id,
		SubscriptionId: sub.Id, SubscriptionOccurredAt: 250, FundingSource: "subscription", ReservedQuota: 100,
	}))
	adjustment := &BillingAdjustmentSpec{
		RequestId: requestId, Operation: "settle", SubscriptionId: sub.Id, TokenId: token.Id,
		SubscriptionQuotaDelta: 20, TokenQuotaDelta: -20,
	}
	transition := BillingSettlementTransition{RequestId: requestId, Operation: "request", FinalQuota: 120, ReleaseCommission: true}
	require.NoError(t, FinalizeBillingSettlement(transition, adjustment))
	require.NoError(t, FinalizeBillingSettlement(transition, adjustment))
	assert.Equal(t, int64(120), meterUsed(t, sub.Id))
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 880, token.RemainQuota)
	assert.Equal(t, 120, token.UsedQuota)
}

func TestStaleBillingSettlementUsesLifecycleAgeNotRetryUpdatedAt(t *testing.T) {
	truncateTables(t)
	now := time.Now().UTC()
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: "stale-reserved", Operation: "request", UserId: 9901, FundingSource: "wallet", ReservedQuota: 0,
	}))
	require.NoError(t, DB.Model(&BillingSettlementEvent{}).Where("request_id = ?", "stale-reserved").
		Updates(map[string]interface{}{"created_at": now.Add(-2 * time.Hour), "updated_at": now}).Error)
	require.NoError(t, EnsureBillingSettlementReserved(BillingSettlementSpec{
		RequestId: "stale-deferred", Operation: "request", UserId: 9902, FundingSource: "wallet", ReservedQuota: 0, DeferCommission: true,
	}))
	require.NoError(t, FinalizeBillingSettlement(BillingSettlementTransition{
		RequestId: "stale-deferred", Operation: "request", FinalQuota: 0, ReleaseCommission: false,
	}, nil))
	require.NoError(t, DB.Model(&BillingSettlementEvent{}).Where("request_id = ?", "stale-deferred").
		Updates(map[string]interface{}{"terminal_at": now.Add(-25 * time.Hour), "updated_at": now}).Error)

	counts, err := CountStaleBillingSettlements(time.Hour, 24*time.Hour, 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), counts.Reserved)
	assert.Equal(t, int64(1), counts.Deferred)
}

func TestBillingSettlementBackfillsLegacySubscriptionOccurrenceOnce(t *testing.T) {
	truncateTables(t)
	spec := BillingSettlementSpec{
		RequestId: "legacy-root-occurrence", Operation: "request", UserId: 9951, TokenId: 9952,
		SubscriptionId: 9953, SubscriptionOccurredAt: 200, FundingSource: "subscription", ReservedQuota: 10,
	}
	require.NoError(t, DB.Create(&BillingSettlementEvent{
		RequestId: spec.RequestId, Operation: spec.Operation, UserId: spec.UserId, TokenId: spec.TokenId,
		SubscriptionId: spec.SubscriptionId, FundingSource: spec.FundingSource,
		InitialReservedQuota: spec.ReservedQuota, ReservedQuota: spec.ReservedQuota, FinalQuota: spec.ReservedQuota,
		Status: BillingSettlementStatusReserved, FinancialStatus: BillingSettlementFinancialApplied,
		CommissionStatus: BillingCommissionStatusBlocked, CreatedAt: time.Unix(100, 0), UpdatedAt: time.Unix(100, 0),
	}).Error)

	require.NoError(t, EnsureBillingSettlementReserved(spec))
	require.NoError(t, EnsureBillingSettlementReserved(spec), "the trusted backfill must replay exactly")
	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).First(&event).Error)
	assert.Equal(t, spec.SubscriptionOccurredAt, event.SubscriptionOccurredAt)

	drifted := spec
	drifted.SubscriptionOccurredAt++
	require.Error(t, EnsureBillingSettlementReserved(drifted))
	require.NoError(t, DB.First(&event, event.Id).Error)
	assert.Equal(t, spec.SubscriptionOccurredAt, event.SubscriptionOccurredAt, "a retry must not overwrite the first trusted occurrence")
}

func TestBillingSettlementLegacyOccurrenceConcurrentDriftUsesCAS(t *testing.T) {
	truncateTables(t)
	base := BillingSettlementSpec{
		RequestId: "legacy-root-occurrence-race", Operation: "request", UserId: 9961, TokenId: 9962,
		SubscriptionId: 9963, FundingSource: "subscription", ReservedQuota: 10,
	}
	require.NoError(t, DB.Create(&BillingSettlementEvent{
		RequestId: base.RequestId, Operation: base.Operation, UserId: base.UserId, TokenId: base.TokenId,
		SubscriptionId: base.SubscriptionId, FundingSource: base.FundingSource,
		InitialReservedQuota: base.ReservedQuota, ReservedQuota: base.ReservedQuota, FinalQuota: base.ReservedQuota,
		Status: BillingSettlementStatusReserved, FinancialStatus: BillingSettlementFinancialApplied,
		CommissionStatus: BillingCommissionStatusBlocked, CreatedAt: time.Unix(100, 0), UpdatedAt: time.Unix(100, 0),
	}).Error)

	type result struct {
		occurredAt int64
		err        error
	}
	results := make(chan result, 2)
	for _, occurredAt := range []int64{210, 220} {
		occurredAt := occurredAt
		go func() {
			spec := base
			spec.SubscriptionOccurredAt = occurredAt
			results <- result{occurredAt: occurredAt, err: EnsureBillingSettlementReserved(spec)}
		}()
	}
	first, second := <-results, <-results
	close(results)
	winners := make([]int64, 0, 1)
	for _, attempt := range []result{first, second} {
		if attempt.err == nil {
			winners = append(winners, attempt.occurredAt)
		}
	}
	require.Len(t, winners, 1, "exactly one conflicting trusted occurrence may win the zero-value CAS")

	var event BillingSettlementEvent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", base.RequestId, base.Operation).First(&event).Error)
	assert.Equal(t, winners[0], event.SubscriptionOccurredAt)
	winner := base
	winner.SubscriptionOccurredAt = winners[0]
	require.NoError(t, EnsureBillingSettlementReserved(winner), "the winning occurrence must replay")
	loser := base
	if winners[0] == 210 {
		loser.SubscriptionOccurredAt = 220
	} else {
		loser.SubscriptionOccurredAt = 210
	}
	require.Error(t, EnsureBillingSettlementReserved(loser))
}
