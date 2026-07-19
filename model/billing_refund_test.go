package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefundPendingSurvivesFailureAndRestart(t *testing.T) {
	truncateTables(t)

	spec := BillingRefundSpec{
		RequestId: "refund-restart",
		Operation: "request_refund",
		UserId:    909,
		UserQuota: 25,
	}
	require.NoError(t, EnsureBillingRefundPending(spec))
	require.Error(t, ApplyBillingRefund(spec.RequestId, spec.Operation))

	var pending BillingRefundIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).First(&pending).Error)
	assert.Equal(t, BillingRefundStatusPending, pending.Status)
	assert.NotEmpty(t, pending.LastError)

	require.NoError(t, DB.Create(&User{Id: spec.UserId, Username: "refund-restart", Password: "password"}).Error)
	applied, err := ReconcilePendingBillingRefunds(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)

	var user User
	require.NoError(t, DB.First(&user, spec.UserId).Error)
	assert.Equal(t, spec.UserQuota, user.Quota)
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).First(&pending).Error)
	assert.Equal(t, BillingRefundStatusRefunded, pending.Status)

	// A new process replaying the same durable request observes the terminal
	// state and must not add the quota again.
	require.NoError(t, RefundBillingQuotaOnce(spec))
	require.NoError(t, DB.First(&user, spec.UserId).Error)
	assert.Equal(t, spec.UserQuota, user.Quota)
}

func TestRefundSameRequestExactlyOnce(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "refund-once", Password: "password", Quota: 40}
	require.NoError(t, DB.Create(user).Error)
	token := &Token{UserId: user.Id, Key: "refund-once-token", Name: "refund", RemainQuota: 40, UsedQuota: 60}
	require.NoError(t, DB.Create(token).Error)
	spec := BillingRefundSpec{
		RequestId:  "refund-once",
		Operation:  "request_refund",
		UserId:     user.Id,
		TokenId:    token.Id,
		UserQuota:  60,
		TokenQuota: 60,
	}
	require.NoError(t, EnsureBillingRefundPending(spec))

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			<-start
			results <- ApplyBillingRefund(spec.RequestId, spec.Operation)
		}()
	}
	ready.Wait()
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)

	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestRefundRetryRejectsDifferentAmounts(t *testing.T) {
	truncateTables(t)

	spec := BillingRefundSpec{RequestId: "refund-mismatch", Operation: "request_refund", UserId: 1, UserQuota: 10}
	require.NoError(t, EnsureBillingRefundPending(spec))
	spec.UserQuota = 11
	assert.Error(t, EnsureBillingRefundPending(spec))
}

func TestLegacyPendingSubscriptionRefundSkipsPeriodAfterReset(t *testing.T) {
	truncateTables(t)

	subscription := &UserSubscription{
		Id:            9101,
		UserId:        9100,
		AmountTotal:   100,
		AmountUsed:    40,
		LastResetTime: 100,
		Status:        "active",
	}
	require.NoError(t, DB.Create(subscription).Error)
	spec := BillingRefundSpec{
		RequestId:         "refund-subscription-old-period",
		Operation:         "request_refund",
		SubscriptionId:    subscription.Id,
		SubscriptionQuota: 25,
	}
	require.NoError(t, EnsureBillingRefundPending(spec))

	// Hooks assign wall-clock timestamps. Pin the legacy intent to the old
	// period, then emulate a reset that establishes fresh current-period usage.
	require.NoError(t, DB.Model(&BillingRefundIntent{}).
		Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).
		Update("created_at", int64(200)).Error)
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", subscription.Id).
		Updates(map[string]interface{}{
			"last_reset_time": int64(300),
			"amount_used":     int64(7),
		}).Error)

	require.NoError(t, ApplyBillingRefund(spec.RequestId, spec.Operation))
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(7), subscription.AmountUsed, "old-period refund must not reduce current-period usage")

	var intent BillingRefundIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).First(&intent).Error)
	assert.Equal(t, BillingRefundStatusRefunded, intent.Status)

	// Terminal replay remains idempotent after the expired-period decision.
	require.NoError(t, ApplyBillingRefund(spec.RequestId, spec.Operation))
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(7), subscription.AmountUsed)
}

func TestLegacyPendingSubscriptionRefundAppliesWithinSamePeriod(t *testing.T) {
	truncateTables(t)

	subscription := &UserSubscription{
		Id:            9201,
		UserId:        9200,
		AmountTotal:   100,
		AmountUsed:    40,
		LastResetTime: 100,
		Status:        "active",
	}
	require.NoError(t, DB.Create(subscription).Error)
	spec := BillingRefundSpec{
		RequestId:         "refund-subscription-same-period",
		Operation:         "request_refund",
		SubscriptionId:    subscription.Id,
		SubscriptionQuota: 25,
	}
	require.NoError(t, EnsureBillingRefundPending(spec))
	require.NoError(t, DB.Model(&BillingRefundIntent{}).
		Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).
		Update("created_at", int64(200)).Error)

	require.NoError(t, ApplyBillingRefund(spec.RequestId, spec.Operation))
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(15), subscription.AmountUsed)

	var intent BillingRefundIntent
	require.NoError(t, DB.Where("request_id = ? AND operation = ?", spec.RequestId, spec.Operation).First(&intent).Error)
	assert.Equal(t, BillingRefundStatusRefunded, intent.Status)
}
