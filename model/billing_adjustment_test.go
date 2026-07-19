package model

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingAdjustmentImmediateRollbackOnTokenInsufficient(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 9101, Username: "adjustment-user", Quota: 100, Status: common.UserStatusEnabled}
	token := &Token{Id: 9102, UserId: user.Id, Key: "adjustment-token", Name: "adjustment", Status: common.TokenStatusEnabled, RemainQuota: 10}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)

	err := ApplyBillingAdjustmentImmediateOnce(BillingAdjustmentSpec{
		RequestId:       "adjustment-reserve-insufficient",
		Operation:       "reserve",
		UserId:          user.Id,
		TokenId:         token.Id,
		UserQuotaDelta:  -50,
		TokenQuotaDelta: -50,
	})
	require.ErrorIs(t, err, ErrTokenQuotaInsufficient)

	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Equal(t, 10, token.RemainQuota)
	var count int64
	require.NoError(t, DB.Model(&BillingAdjustmentIntent{}).
		Where("request_id = ?", "adjustment-reserve-insufficient").Count(&count).Error)
	assert.Zero(t, count, "failed immediate reservations must not leave a future debt")
}

func TestBillingAdjustmentRejectsIncompatibleFundingSpecs(t *testing.T) {
	truncateTables(t)
	cases := []BillingAdjustmentSpec{
		{RequestId: "bad-both-funding", Operation: "apply", UserId: 1, SubscriptionId: 2, UserQuotaDelta: 1, SubscriptionQuotaDelta: -1},
		{RequestId: "bad-wallet-token", Operation: "apply", UserId: 1, TokenId: 2, UserQuotaDelta: 1, TokenQuotaDelta: -1},
		{RequestId: "bad-sub-token", Operation: "apply", SubscriptionId: 1, TokenId: 2, SubscriptionQuotaDelta: 1, TokenQuotaDelta: 1},
	}
	for _, spec := range cases {
		require.Error(t, EnsureBillingAdjustmentPending(spec))
	}
	var count int64
	require.NoError(t, DB.Model(&BillingAdjustmentIntent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestBillingAdjustmentRefundInvariantRollsBackAndReconciles(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 9201, Username: "refund-user", Quota: 10, Status: common.UserStatusEnabled}
	token := &Token{Id: 9202, UserId: user.Id, Key: "refund-token", Name: "refund", Status: common.TokenStatusEnabled, RemainQuota: 20, UsedQuota: 5}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)
	spec := BillingAdjustmentSpec{
		RequestId:       "adjustment-refund-reconcile",
		Operation:       "refund",
		UserId:          user.Id,
		TokenId:         token.Id,
		UserQuotaDelta:  10,
		TokenQuotaDelta: 10,
	}

	require.Error(t, ApplyBillingAdjustmentOnce(spec))
	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 10, user.Quota, "token invariant failure must roll back the wallet refund")
	assert.Equal(t, 20, token.RemainQuota)

	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Update("used_quota", 10).Error)
	applied, err := ReconcilePendingBillingAdjustments(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 20, user.Quota)
	assert.Equal(t, 30, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)

	applied, err = ReconcilePendingBillingAdjustments(10)
	require.NoError(t, err)
	assert.Zero(t, applied)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 20, user.Quota)
}

func TestBillingAdjustmentConcurrentReplayAppliesOnce(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 9301, Username: "concurrent-user", Quota: 100, Status: common.UserStatusEnabled}
	token := &Token{Id: 9302, UserId: user.Id, Key: "concurrent-token", Name: "concurrent", Status: common.TokenStatusEnabled, RemainQuota: 100}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)
	spec := BillingAdjustmentSpec{
		RequestId:       "adjustment-concurrent",
		Operation:       "charge",
		UserId:          user.Id,
		TokenId:         token.Id,
		UserQuotaDelta:  -30,
		TokenQuotaDelta: -30,
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- ApplyBillingAdjustmentOnce(spec)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 70, user.Quota)
	assert.Equal(t, 70, token.RemainQuota)
	assert.Equal(t, 30, token.UsedQuota)
	var count int64
	require.NoError(t, DB.Model(&BillingAdjustmentIntent{}).
		Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestBillingAdjustmentSubscriptionUnderflowRollsBackToken(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{Id: 9401, UserId: 9400, AmountTotal: 100, AmountUsed: 5, Status: "active"}
	token := &Token{Id: 9402, UserId: 9400, Key: "sub-refund-token", Name: "sub-refund", Status: common.TokenStatusEnabled, RemainQuota: 20, UsedQuota: 10}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	spec := BillingAdjustmentSpec{
		RequestId:              "adjustment-sub-underflow",
		Operation:              "refund",
		SubscriptionId:         sub.Id,
		SubscriptionOccurredAt: 100,
		TokenId:                token.Id,
		SubscriptionQuotaDelta: -10,
		TokenQuotaDelta:        10,
	}

	require.ErrorContains(t, ApplyBillingAdjustmentOnce(spec), "would become negative")
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, int64(5), sub.AmountUsed)
	assert.Equal(t, 20, token.RemainQuota, "subscription underflow must roll back token refund")
	assert.Equal(t, 10, token.UsedQuota)

	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("amount_used", 10).Error)
	applied, err := ReconcilePendingBillingAdjustments(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, 30, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestBillingAdjustmentOldSubscriptionPeriodSkipsCurrentPeriod(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{Id: 9451, UserId: 9450, AmountTotal: 100, AmountUsed: 0, LastResetTime: 200, Status: "active"}
	token := &Token{Id: 9452, UserId: 9450, Key: "sub-old-period-token", Name: "sub-old", Status: common.TokenStatusEnabled, RemainQuota: 20, UsedQuota: 10}
	record := &SubscriptionPreConsumeRecord{
		RequestId: "sub-old-period", UserId: sub.UserId, UserSubscriptionId: sub.Id,
		PreConsumed: 10, ResetEpoch: 100, Status: "consumed",
	}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(record).Error)

	require.NoError(t, ApplyBillingAdjustmentOnce(BillingAdjustmentSpec{
		RequestId: "sub-old-period", Operation: "cancel", SubscriptionId: sub.Id,
		SubscriptionRequestId: "sub-old-period", SubscriptionResetEpoch: 100,
		TokenId: token.Id, TokenQuotaDelta: 10,
	}))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Zero(t, sub.AmountUsed, "old-period refund must not reduce the new period")
	assert.Equal(t, 30, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	var intent BillingAdjustmentIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", "sub-old-period", "cancel").First(&intent).Error)
	assert.Equal(t, "expired_period_skipped", intent.SubscriptionEpochOutcome)
	var persistedRecord SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "sub-old-period").First(&persistedRecord).Error)
	assert.Equal(t, "refunded", persistedRecord.Status)
}

func TestBillingAdjustmentRejectsFutureSubscriptionEpochAtomically(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{Id: 9461, UserId: 9460, AmountTotal: 100, AmountUsed: 10, LastResetTime: 100, Status: "active"}
	token := &Token{Id: 9462, UserId: 9460, Key: "sub-future-period-token", Name: "sub-future", Status: common.TokenStatusEnabled, RemainQuota: 20, UsedQuota: 0}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)

	err := ApplyBillingAdjustmentOnce(BillingAdjustmentSpec{
		RequestId: "sub-future-period", Operation: "settle", SubscriptionId: sub.Id,
		SubscriptionResetEpoch: 200, SubscriptionQuotaDelta: 5,
		TokenId: token.Id, TokenQuotaDelta: -5,
	})
	require.ErrorContains(t, err, "ahead of persisted subscription")
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, int64(10), sub.AmountUsed)
	assert.Equal(t, 20, token.RemainQuota, "future epoch failure must roll back token charge")
}

func TestLegacySubscriptionPreConsumeRecordUsesCreatedAtResetFallback(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{Id: 9471, UserId: 9470, AmountTotal: 100, AmountUsed: 0, LastResetTime: 300, Status: "active"}
	record := &SubscriptionPreConsumeRecord{
		RequestId: "sub-legacy-period", UserId: sub.UserId, UserSubscriptionId: sub.Id,
		PreConsumed: 10, ResetEpoch: 0, Status: "consumed", CreatedAt: 200, UpdatedAt: 200,
	}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(record).Error)
	// Hooks overwrite timestamps on Create; emulate an upgraded legacy row.
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", record.RequestId).
		Updates(map[string]interface{}{"reset_epoch": 0, "created_at": 200, "updated_at": 200}).Error)

	require.NoError(t, ApplyBillingAdjustmentOnce(BillingAdjustmentSpec{
		RequestId: "sub-legacy-period", Operation: "cancel", SubscriptionId: sub.Id,
		SubscriptionRequestId: "sub-legacy-period",
	}))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	assert.Zero(t, sub.AmountUsed)
	var intent BillingAdjustmentIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", "sub-legacy-period", "cancel").First(&intent).Error)
	assert.Equal(t, "expired_period_skipped", intent.SubscriptionEpochOutcome)
}

func TestLegacyBillingAdjustmentUsesSettlementCreatedAtBoundary(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{Id: 9481, UserId: 9480, AmountTotal: 100, AmountUsed: 0, LastResetTime: 300, Status: "active"}
	require.NoError(t, DB.Create(sub).Error)
	intent := &BillingAdjustmentIntent{
		RequestId: "legacy-settlement-boundary", Operation: "settle", SubscriptionId: sub.Id,
		SubscriptionQuotaDelta: 10, Status: BillingAdjustmentStatusPending,
	}
	require.NoError(t, DB.Create(intent).Error)
	require.NoError(t, DB.Create(&BillingSettlementEvent{
		RequestId: "legacy-settlement-boundary", Operation: "request", UserId: sub.UserId,
		SubscriptionId: sub.Id, FundingSource: "subscription",
		Status: BillingSettlementStatusFinalized, FinancialStatus: BillingSettlementFinancialPending,
		AdjustmentRequestId: intent.RequestId, AdjustmentOperation: intent.Operation,
		AdjustmentSubscriptionId: sub.Id, AdjustmentSubscriptionQuotaDelta: 10,
		CreatedAt: time.Unix(200, 0), UpdatedAt: time.Unix(200, 0),
	}).Error)

	require.NoError(t, ApplyBillingAdjustment(intent.RequestId, intent.Operation))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	assert.Zero(t, sub.AmountUsed, "migration fallback must use the old lifecycle root instead of adjustment retry time")
	require.NoError(t, DB.First(intent, intent.Id).Error)
	assert.Equal(t, "expired_period_debt_absorbed", intent.SubscriptionEpochOutcome)
}

func TestLegacyBillingAdjustmentUnknownOccurrenceFailsClosedAfterDailyReset(t *testing.T) {
	fixture := seedDailyResetSettlement(t, "legacy-unknown-root")
	intent := &BillingAdjustmentIntent{
		RequestId: "legacy-unknown-occurrence", Operation: "settle", SubscriptionId: fixture.sub.Id,
		TokenId: fixture.token.Id, SubscriptionQuotaDelta: 20, TokenQuotaDelta: -20,
		Status: BillingAdjustmentStatusPending,
	}
	require.NoError(t, DB.Create(intent).Error)

	err := ApplyBillingAdjustment(intent.RequestId, intent.Operation)
	require.ErrorIs(t, err, errLegacySubscriptionOccurrenceUnknown)
	require.NoError(t, DB.First(fixture.sub, fixture.sub.Id).Error)
	require.NoError(t, DB.First(fixture.token, fixture.token.Id).Error)
	assert.Equal(t, int64(10), fixture.sub.AmountUsed, "an unknown legacy request must never mutate the new subscription period")
	assert.Equal(t, 900, fixture.token.RemainQuota, "the fail-closed transaction must roll back the paired token charge")
	assert.Equal(t, 100, fixture.token.UsedQuota)
	require.NoError(t, DB.First(intent, intent.Id).Error)
	assert.Equal(t, BillingAdjustmentStatusPending, intent.Status)
	assert.Zero(t, intent.SubscriptionOccurredAt)
	assert.Equal(t, errLegacySubscriptionOccurrenceUnknown.Error(), intent.LastError)

	applied, err := ReconcilePendingBillingAdjustments(10)
	require.ErrorIs(t, err, errLegacySubscriptionOccurrenceUnknown)
	assert.Zero(t, applied)
	require.NoError(t, DB.First(fixture.sub, fixture.sub.Id).Error)
	require.NoError(t, DB.First(fixture.token, fixture.token.Id).Error)
	assert.Equal(t, int64(10), fixture.sub.AmountUsed)
	assert.Equal(t, 900, fixture.token.RemainQuota)
}

func TestLegacyBillingAdjustmentBackfillsOccurrenceFromPreConsumeRecord(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{Id: 9491, UserId: 9490, AmountTotal: 100, AmountUsed: 10, LastResetTime: 300, Status: "active"}
	token := &Token{Id: 9492, UserId: sub.UserId, Key: "legacy-record-token", Name: "legacy-record", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	record := &SubscriptionPreConsumeRecord{
		RequestId: "legacy-record-occurrence", UserId: sub.UserId, UserSubscriptionId: sub.Id,
		PreConsumed: 10, Status: "consumed",
	}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(record).Error)
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("id = ?", record.Id).
		Updates(map[string]interface{}{"created_at": 200, "updated_at": 200}).Error)
	intent := &BillingAdjustmentIntent{
		RequestId: record.RequestId, Operation: "settle", SubscriptionId: sub.Id, TokenId: token.Id,
		SubscriptionQuotaDelta: 20, TokenQuotaDelta: -20, Status: BillingAdjustmentStatusPending,
	}
	require.NoError(t, DB.Create(intent).Error)

	require.NoError(t, ApplyBillingAdjustment(intent.RequestId, intent.Operation))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, int64(10), sub.AmountUsed)
	assert.Equal(t, 880, token.RemainQuota)
	require.NoError(t, DB.First(intent, intent.Id).Error)
	assert.Equal(t, int64(200), intent.SubscriptionOccurredAt)
	assert.Equal(t, "expired_period_debt_absorbed", intent.SubscriptionEpochOutcome)
}

func TestLegacyBillingAdjustmentBackfillsOccurrenceFromTask(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{Id: 9501, UserId: 9500, AmountTotal: 100, AmountUsed: 10, LastResetTime: 300, Status: "active"}
	token := &Token{Id: 9502, UserId: sub.UserId, Key: "legacy-task-token", Name: "legacy-task", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	task := &Task{
		CreatedAt: 190, UpdatedAt: 190, SubmitTime: 200,
		PrivateData: TaskPrivateData{BillingSource: "subscription", SubscriptionId: sub.Id},
	}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(task).Error)
	intent := &BillingAdjustmentIntent{
		RequestId: "task:" + strconv.FormatInt(task.ID, 10), Operation: "settle", SubscriptionId: sub.Id, TokenId: token.Id,
		SubscriptionQuotaDelta: 20, TokenQuotaDelta: -20, Status: BillingAdjustmentStatusPending,
	}
	require.NoError(t, DB.Create(intent).Error)

	require.NoError(t, ApplyBillingAdjustment(intent.RequestId, intent.Operation))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, int64(10), sub.AmountUsed)
	assert.Equal(t, 880, token.RemainQuota)
	require.NoError(t, DB.First(intent, intent.Id).Error)
	assert.Equal(t, int64(200), intent.SubscriptionOccurredAt)
	assert.Equal(t, "expired_period_debt_absorbed", intent.SubscriptionEpochOutcome)
}

func TestLegacyBillingAdjustmentBackfillsOccurrenceFromMidjourney(t *testing.T) {
	truncateTables(t)
	const submitTimeMillis int64 = 1_700_000_000_000
	sub := &UserSubscription{Id: 9511, UserId: 9510, AmountTotal: 100, AmountUsed: 10, LastResetTime: 1_700_000_100, Status: "active"}
	token := &Token{Id: 9512, UserId: sub.UserId, Key: "legacy-mj-token", Name: "legacy-mj", Status: common.TokenStatusEnabled, RemainQuota: 900, UsedQuota: 100}
	task := &Midjourney{
		UserId: sub.UserId, SubmitTime: submitTimeMillis, BillingSource: "subscription",
		SubscriptionId: sub.Id, BillingRequestId: "legacy-mj-occurrence",
	}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(task).Error)
	intent := &BillingAdjustmentIntent{
		RequestId: task.BillingRequestId, Operation: "settle", SubscriptionId: sub.Id, TokenId: token.Id,
		SubscriptionQuotaDelta: 20, TokenQuotaDelta: -20, Status: BillingAdjustmentStatusPending,
	}
	require.NoError(t, DB.Create(intent).Error)

	require.NoError(t, ApplyBillingAdjustment(intent.RequestId, intent.Operation))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, int64(10), sub.AmountUsed)
	assert.Equal(t, 880, token.RemainQuota)
	require.NoError(t, DB.First(intent, intent.Id).Error)
	assert.Equal(t, submitTimeMillis/1000, intent.SubscriptionOccurredAt)
	assert.Equal(t, "expired_period_debt_absorbed", intent.SubscriptionEpochOutcome)
}
