package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionPlanCacheInvalidationPreventsStaleFill(t *testing.T) {
	truncateTables(t)
	const planID = 8701
	require.NoError(t, DB.Create(&SubscriptionPlan{
		Id:               planID,
		Title:            "old title",
		PriceAmount:      1,
		Currency:         "USD",
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          true,
		AllowBalancePay:  common.GetPointer(true),
		QuotaResetPeriod: SubscriptionResetNever,
	}).Error)
	require.NoError(t, InvalidateSubscriptionPlanCache(planID))

	readFinished := make(chan struct{})
	releaseFill := make(chan struct{})
	var once sync.Once
	subscriptionPlanCacheBeforeFillHook = func(id int) {
		if id != planID {
			return
		}
		once.Do(func() {
			close(readFinished)
			<-releaseFill
		})
	}
	t.Cleanup(func() {
		subscriptionPlanCacheBeforeFillHook = nil
	})

	readerResult := make(chan *SubscriptionPlan, 1)
	readerErr := make(chan error, 1)
	go func() {
		plan, err := GetSubscriptionPlanById(planID)
		readerResult <- plan
		readerErr <- err
	}()

	<-readFinished
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", planID).Updates(map[string]interface{}{
		"title":      "new title",
		"updated_at": common.GetTimestamp() + 1,
	}).Error)
	require.NoError(t, InvalidateSubscriptionPlanCache(planID))
	subscriptionPlanCacheBeforeFillHook = nil
	close(releaseFill)

	require.NoError(t, <-readerErr)
	staleRead := <-readerResult
	require.NotNil(t, staleRead)
	assert.Equal(t, "old title", staleRead.Title, "the already-started read may return its snapshot")

	latest, err := GetSubscriptionPlanById(planID)
	require.NoError(t, err)
	assert.Equal(t, "new title", latest.Title, "the old read must not refill the invalidated generation")
}

func TestSubscriptionPlanInvalidationReportsRedisFailure(t *testing.T) {
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalEnabled
		common.RDB = originalClient
	})

	err := InvalidateSubscriptionPlanCache(8702)
	require.ErrorContains(t, err, "client is not initialized")
}

func TestPurchaseSubscriptionWithBalanceReadsCurrentPlanInsideTransaction(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
	})

	testCases := []struct {
		name           string
		planID         int
		userID         int
		updates        map[string]interface{}
		wantError      string
		wantQuota      int
		wantOrderMoney float64
	}{
		{
			name:      "latest disabled status rejects purchase",
			planID:    8711,
			userID:    8811,
			updates:   map[string]interface{}{"enabled": false},
			wantError: "套餐未启用",
			wantQuota: 1000,
		},
		{
			name:      "latest balance payment policy rejects purchase",
			planID:    8712,
			userID:    8812,
			updates:   map[string]interface{}{"allow_balance_pay": false},
			wantError: "不允许使用余额",
			wantQuota: 1000,
		},
		{
			name:           "latest price determines wallet charge",
			planID:         8713,
			userID:         8813,
			updates:        map[string]interface{}{"price_amount": 3.25},
			wantQuota:      675,
			wantOrderMoney: 3.25,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			plan := &SubscriptionPlan{
				Id:               testCase.planID,
				Title:            "cached plan",
				PriceAmount:      1,
				Currency:         "USD",
				DurationUnit:     SubscriptionDurationMonth,
				DurationValue:    1,
				Enabled:          true,
				AllowBalancePay:  common.GetPointer(true),
				QuotaResetPeriod: SubscriptionResetNever,
			}
			require.NoError(t, DB.Create(plan).Error)
			require.NoError(t, DB.Create(&User{
				Id:       testCase.userID,
				Username: "subscription-current-plan-user",
				Status:   common.UserStatusEnabled,
				Quota:    1000,
				Group:    "default",
			}).Error)
			require.NoError(t, InvalidateSubscriptionPlanCache(testCase.planID))

			cached, err := GetSubscriptionPlanById(testCase.planID)
			require.NoError(t, err)
			assert.Equal(t, float64(1), cached.PriceAmount)
			assert.True(t, cached.Enabled)
			require.NotNil(t, cached.AllowBalancePay)
			assert.True(t, *cached.AllowBalancePay)

			// Deliberately do not invalidate: the purchase path must not consult this
			// cached copy for security- or money-sensitive decisions.
			require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", testCase.planID).Updates(testCase.updates).Error)
			err = PurchaseSubscriptionWithBalance(testCase.userID, testCase.planID)
			if testCase.wantError != "" {
				require.ErrorContains(t, err, testCase.wantError)
			} else {
				require.NoError(t, err)
			}

			var user User
			require.NoError(t, DB.Select("quota").First(&user, testCase.userID).Error)
			assert.Equal(t, testCase.wantQuota, user.Quota)

			var subscriptionCount int64
			require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", testCase.userID).Count(&subscriptionCount).Error)
			if testCase.wantError != "" {
				assert.Zero(t, subscriptionCount)
				return
			}
			assert.Equal(t, int64(1), subscriptionCount)
			var order SubscriptionOrder
			require.NoError(t, DB.Where("user_id = ?", testCase.userID).First(&order).Error)
			assert.Equal(t, testCase.wantOrderMoney, order.Money)
		})
	}
}
