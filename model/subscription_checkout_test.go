package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type legacySubscriptionPaymentReceiptSchema struct {
	Id                    int
	OrderId               int    `gorm:"not null;uniqueIndex"`
	ReceiptKey            string `gorm:"type:varchar(191);not null;uniqueIndex"`
	Provider              string `gorm:"type:varchar(50);not null"`
	ProviderTransactionId string `gorm:"type:varchar(128);not null"`
	ProviderEventId       string `gorm:"type:varchar(128)"`
	ProviderCheckoutId    string `gorm:"type:varchar(128)"`
	Amount                string `gorm:"type:varchar(32);not null"`
	PaidAmount            string `gorm:"type:varchar(32);not null"`
	Currency              string `gorm:"type:varchar(8);not null"`
	CurrencySource        string `gorm:"type:varchar(32);not null"`
	ProductId             string `gorm:"type:varchar(191);not null"`
	PaymentMethod         string `gorm:"type:varchar(50);not null"`
	CheckoutMode          string `gorm:"type:varchar(32);not null"`
	SnapshotHash          string `gorm:"type:varchar(64)"`
	PayloadHash           string `gorm:"type:varchar(64)"`
	PaidAt                int64  `gorm:"type:bigint;not null"`
	CreatedAt             int64  `gorm:"type:bigint;not null"`
}

func (legacySubscriptionPaymentReceiptSchema) TableName() string {
	return "subscription_payment_receipts"
}

func seedSubscriptionCheckoutUserAndPlan(t *testing.T, userId int, planId int) *SubscriptionPlan {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id: userId, Username: fmt.Sprintf("subscription-checkout-user-%d", userId),
		AffCode: fmt.Sprintf("checkout-aff-%d", userId), Status: common.UserStatusEnabled,
	}).Error)
	plan := &SubscriptionPlan{
		Id: planId, Title: "Original plan", PriceAmount: 9.99, Currency: "USD",
		DurationUnit: SubscriptionDurationDay, DurationValue: 3, Enabled: true,
		StripePriceId: "price_original", CreemProductId: "prod_original",
		WaffoPancakeProductId: "waffo_original", MaxPurchasePerUser: 1,
		TotalAmount: 100, QuotaResetPeriod: SubscriptionResetDaily,
		AllowBalancePay: common.GetPointer(true), AllowWalletOverflow: common.GetPointer(false),
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func createStripeSubscriptionCheckoutOrder(t *testing.T, userId int, planId int, tradeNo string) *SubscriptionOrder {
	t.Helper()
	order, err := CreatePendingSubscriptionOrder(userId, planId, tradeNo, PaymentMethodStripe, PaymentProviderStripe, SubscriptionCheckoutPolicy{
		Currency:         "USD",
		CurrencySource:   SubscriptionCurrencySourceProviderCallback,
		AmountMultiplier: "1",
		CheckoutMode:     SubscriptionCheckoutModeOneTime,
	})
	require.NoError(t, err)
	require.NotNil(t, order)
	require.NoError(t, SetSubscriptionOrderCheckoutId(tradeNo, PaymentProviderStripe, "cs_"+tradeNo))
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(order).Error)
	return order
}

func stripeSubscriptionFact(order *SubscriptionOrder, transactionId string) VerifiedSubscriptionPaymentFact {
	return VerifiedSubscriptionPaymentFact{
		TradeNo:               order.TradeNo,
		Provider:              PaymentProviderStripe,
		ProviderEventId:       "evt_" + transactionId,
		ProviderTransactionId: transactionId,
		ProviderCheckoutId:    order.ProviderCheckoutId,
		Amount:                order.ExpectedAmount,
		PaidAmount:            order.ExpectedAmount,
		Currency:              order.ExpectedCurrency,
		CurrencySource:        order.CurrencySource,
		ProductId:             order.ExpectedProductId,
		PaymentMethod:         order.PaymentMethod,
		CheckoutMode:          order.ExpectedMode,
		SnapshotHash:          order.SnapshotHash,
		PayloadHash:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PaidAt:                common.GetTimestamp(),
	}
}

func TestCompleteSubscriptionOrderUsesImmutableCheckoutSnapshot(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7201, 7202
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "snapshot-order")

	// Simulate a catalog edit after the customer has entered checkout. None of
	// these new values may alter the paid entitlement.
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", planId).Updates(map[string]interface{}{
		"title": "Changed plan", "price_amount": 88.88, "duration_value": 30,
		"total_amount": 9999, "quota_reset_period": SubscriptionResetNever,
		"max_purchase_per_user": 0, "stripe_price_id": "price_changed",
	}).Error)

	fact := stripeSubscriptionFact(order, "pi_snapshot")
	require.NoError(t, CompleteSubscriptionOrder(fact))
	require.NoError(t, CompleteSubscriptionOrder(fact), "an exact provider replay must be idempotent")

	var subscriptions []UserSubscription
	require.NoError(t, DB.Where("user_id = ?", userId).Find(&subscriptions).Error)
	require.Len(t, subscriptions, 1)
	subscription := subscriptions[0]
	assert.Equal(t, int64(100), subscription.AmountTotal)
	assert.Equal(t, "Original plan", subscription.PlanTitle)
	assert.Equal(t, SubscriptionResetDaily, subscription.QuotaResetPeriod)
	assert.Equal(t, SubscriptionOrderSnapshotVersion, subscription.BenefitSnapshotVersion)
	assert.False(t, subscription.AllowWalletOverflow)
	assert.Equal(t, int64(3*24*time.Hour/time.Second), subscription.EndTime-subscription.StartTime)
	planInfo, err := GetSubscriptionPlanInfoByUserSubscriptionId(subscription.Id)
	require.NoError(t, err)
	assert.Equal(t, "Original plan", planInfo.PlanTitle)

	var persistedOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&persistedOrder).Error)
	assert.Equal(t, common.TopUpStatusSuccess, persistedOrder.Status)
	assert.Empty(t, persistedOrder.ProviderPayload)
	assert.NotEmpty(t, persistedOrder.PaymentFact)
	var receiptCount int64
	require.NoError(t, DB.Model(&SubscriptionPaymentReceipt{}).Where("order_id = ?", persistedOrder.Id).Count(&receiptCount).Error)
	assert.Equal(t, int64(1), receiptCount)
}

func TestCompleteSubscriptionOrderPersistsMismatchWithoutGrant(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7211, 7212
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "mismatch-order")
	fact := stripeSubscriptionFact(order, "pi_mismatch")
	fact.Amount = "10.99"
	fact.PaidAmount = "10.99"

	err := CompleteSubscriptionOrder(fact)
	require.ErrorIs(t, err, ErrSubscriptionOrderReconciliationRequired)
	require.ErrorIs(t, CompleteSubscriptionOrder(fact), ErrSubscriptionOrderReconciliationRequired)

	var persistedOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&persistedOrder).Error)
	assert.Equal(t, SubscriptionOrderStatusReconciliationRequired, persistedOrder.Status)
	assert.NotEmpty(t, persistedOrder.PaymentFact)
	assert.Contains(t, persistedOrder.ReconciliationReason, ErrSubscriptionPaymentFactMismatch.Error())
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userId).Count(&subscriptionCount).Error)
	assert.Zero(t, subscriptionCount)
	var receiptCount int64
	require.NoError(t, DB.Model(&SubscriptionPaymentReceipt{}).Where("order_id = ?", persistedOrder.Id).Count(&receiptCount).Error)
	assert.Equal(t, int64(1), receiptCount)
	secondFact := fact
	secondFact.ProviderTransactionId = "pi_mismatch_second"
	secondFact.ProviderEventId = "evt_pi_mismatch_second"
	secondFact.PayloadHash = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	require.ErrorIs(t, CompleteSubscriptionOrder(secondFact), ErrSubscriptionOrderReconciliationRequired)
	require.NoError(t, DB.Model(&SubscriptionPaymentReceipt{}).Where("order_id = ?", persistedOrder.Id).Count(&receiptCount).Error)
	assert.Equal(t, int64(2), receiptCount, "a second verified payment must remain visible for manual reconciliation")
}

func TestCompleteSubscriptionOrderRejectsUnderpayment(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7215, 7216
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "underpaid-order")
	fact := stripeSubscriptionFact(order, "pi_underpaid")
	fact.PaidAmount = "8.99"

	err := CompleteSubscriptionOrder(fact)
	require.ErrorIs(t, err, ErrSubscriptionOrderReconciliationRequired)
	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	assert.Equal(t, SubscriptionOrderStatusReconciliationRequired, persisted.Status)
	assert.Contains(t, persisted.ReconciliationReason, "paid amount is below")
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userId).Count(&subscriptionCount).Error)
	assert.Zero(t, subscriptionCount)
}

func TestCompleteSubscriptionOrderRejectsFrozenPaymentFactMismatches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*VerifiedSubscriptionPaymentFact)
	}{
		{name: "currency", mutate: func(fact *VerifiedSubscriptionPaymentFact) { fact.Currency = "EUR" }},
		{name: "currency source", mutate: func(fact *VerifiedSubscriptionPaymentFact) {
			fact.CurrencySource = SubscriptionCurrencySourceMerchantContract
		}},
		{name: "product", mutate: func(fact *VerifiedSubscriptionPaymentFact) { fact.ProductId = "price_other" }},
		{name: "payment method", mutate: func(fact *VerifiedSubscriptionPaymentFact) { fact.PaymentMethod = "alipay" }},
		{name: "snapshot hash", mutate: func(fact *VerifiedSubscriptionPaymentFact) { fact.SnapshotHash = strings.Repeat("e", 64) }},
		{name: "checkout id", mutate: func(fact *VerifiedSubscriptionPaymentFact) { fact.ProviderCheckoutId = "cs_other" }},
		{name: "checkout mode", mutate: func(fact *VerifiedSubscriptionPaymentFact) { fact.CheckoutMode = "recurring" }},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 7270 + index*2
			planId := userId + 1
			seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
			order := createStripeSubscriptionCheckoutOrder(t, userId, planId, fmt.Sprintf("frozen-mismatch-%d", index))
			fact := stripeSubscriptionFact(order, fmt.Sprintf("pi_frozen_mismatch_%d", index))
			test.mutate(&fact)

			err := CompleteSubscriptionOrder(fact)
			require.ErrorIs(t, err, ErrSubscriptionOrderReconciliationRequired)
			var persisted SubscriptionOrder
			require.NoError(t, DB.First(&persisted, order.Id).Error)
			assert.Equal(t, SubscriptionOrderStatusReconciliationRequired, persisted.Status)
			var subscriptionCount int64
			require.NoError(t, DB.Model(&UserSubscription{}).Count(&subscriptionCount).Error)
			assert.Zero(t, subscriptionCount)
		})
	}
}

func TestSuccessfulSubscriptionOrderPersistsSecondPaymentForReview(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7217, 7218
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "duplicate-payment-order")
	firstFact := stripeSubscriptionFact(order, "pi_first")
	require.NoError(t, CompleteSubscriptionOrder(firstFact))
	require.NoError(t, CompleteSubscriptionOrder(firstFact), "the fulfilled transaction replay remains idempotent")

	secondFact := stripeSubscriptionFact(order, "pi_second")
	secondFact.ProviderCheckoutId = "cs_second_payment"
	secondFact.ProviderEventId = "evt_second_payment"
	secondFact.PayloadHash = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	require.ErrorIs(t, CompleteSubscriptionOrder(secondFact), ErrSubscriptionOrderReconciliationRequired)
	require.ErrorIs(t, CompleteSubscriptionOrder(secondFact), ErrSubscriptionOrderReconciliationRequired)

	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, persisted.Status, "an already-granted entitlement remains recorded as successful")
	assert.Equal(t, SubscriptionOrderReviewRequired, persisted.ReviewStatus)
	assert.Positive(t, persisted.ReviewTime)
	assert.Contains(t, persisted.ReconciliationReason, "additional verified payment")
	var subscriptions int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userId).Count(&subscriptions).Error)
	assert.Equal(t, int64(1), subscriptions)
	var receipts []SubscriptionPaymentReceipt
	require.NoError(t, DB.Where("order_id = ?", order.Id).Order("id asc").Find(&receipts).Error)
	require.Len(t, receipts, 2)
	assert.Equal(t, SubscriptionReceiptDispositionFulfilled, receipts[0].Disposition)
	assert.Equal(t, SubscriptionReceiptDispositionDuplicatePayment, receipts[1].Disposition)
}

func TestCompleteSubscriptionOrderRejectsProviderTransactionReuse(t *testing.T) {
	truncateTables(t)
	const planId = 7220
	plan := seedSubscriptionCheckoutUserAndPlan(t, 7221, planId)
	require.NoError(t, DB.Create(&User{Id: 7222, Username: "subscription-checkout-user-2", AffCode: "checkout-aff-7222", Status: common.UserStatusEnabled}).Error)
	plan.MaxPurchasePerUser = 0
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", planId).Update("max_purchase_per_user", 0).Error)
	first := createStripeSubscriptionCheckoutOrder(t, 7221, planId, "receipt-first")
	second := createStripeSubscriptionCheckoutOrder(t, 7222, planId, "receipt-second")

	require.NoError(t, CompleteSubscriptionOrder(stripeSubscriptionFact(first, "pi_reused")))
	err := CompleteSubscriptionOrder(stripeSubscriptionFact(second, "pi_reused"))
	require.ErrorIs(t, err, ErrSubscriptionOrderReconciliationRequired)

	var secondOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", second.TradeNo).First(&secondOrder).Error)
	assert.Equal(t, SubscriptionOrderStatusReconciliationRequired, secondOrder.Status)
	assert.Contains(t, secondOrder.ReconciliationReason, ErrSubscriptionPaymentReceiptConflict.Error())
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Count(&subscriptionCount).Error)
	assert.Equal(t, int64(1), subscriptionCount)
	var receiptCount int64
	require.NoError(t, DB.Model(&SubscriptionPaymentReceipt{}).Count(&receiptCount).Error)
	assert.Equal(t, int64(1), receiptCount)
}

func TestCompleteLegacyPendingSubscriptionOrderRequiresReconciliation(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7231, 7232
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	legacy := &SubscriptionOrder{
		UserId: userId, PlanId: planId, Money: 9.99, TradeNo: "legacy-pending",
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(legacy).Error)
	fact := VerifiedSubscriptionPaymentFact{
		TradeNo: legacy.TradeNo, Provider: PaymentProviderStripe,
		ProviderTransactionId: "pi_legacy", Amount: "9.99", PaidAmount: "9.99",
		Currency: "USD", CurrencySource: SubscriptionCurrencySourceProviderCallback,
		ProductId: "price_original", PaymentMethod: PaymentMethodStripe,
		CheckoutMode: SubscriptionCheckoutModeOneTime,
		PayloadHash:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}

	err := CompleteSubscriptionOrder(fact)
	require.ErrorIs(t, err, ErrSubscriptionOrderReconciliationRequired)
	require.ErrorIs(t, err, ErrSubscriptionOrderSnapshotMissing)
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Count(&subscriptionCount).Error)
	assert.Zero(t, subscriptionCount)
}

func TestSubscriptionCheckoutSupportsExistingSixDecimalPlanPrices(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7235, 7236
	require.NoError(t, DB.Create(&User{
		Id: userId, Username: "six-decimal-price-user", AffCode: "checkout-aff-7235", Status: common.UserStatusEnabled,
	}).Error)
	plan := &SubscriptionPlan{
		Id: planId, Title: "Six decimal plan", PriceAmount: 9.999999, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
		TotalAmount: 100, StripePriceId: "price_six_decimal",
	}
	require.NoError(t, DB.Create(plan).Error)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "six-decimal-order")
	assert.Equal(t, "10.00", order.ExpectedAmount)
	snapshot, err := decodeSubscriptionCheckoutSnapshot(order)
	require.NoError(t, err)
	assert.Equal(t, "9.999999", snapshot.Plan.PriceAmount)
	assert.Equal(t, "1", snapshot.Expectation.AmountMultiplier)
	assert.Equal(t, SubscriptionAmountPolicyV1, snapshot.Expectation.AmountPolicy)
	require.NoError(t, CompleteSubscriptionOrder(stripeSubscriptionFact(order, "pi_six_decimal")))
}

func TestBalanceSubscriptionRejectsPositivePriceThatRoundsToZero(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7239, 7240
	require.NoError(t, DB.Create(&User{
		Id: userId, Username: "subcent-balance-user", AffCode: "checkout-aff-7239",
		Status: common.UserStatusEnabled, Quota: 1000,
	}).Error)
	plan := &SubscriptionPlan{
		Id: planId, Title: "Sub-cent plan", PriceAmount: 0.004999, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
		TotalAmount: 100, AllowBalancePay: common.GetPointer(true),
	}
	require.NoError(t, DB.Create(plan).Error)

	err := PurchaseSubscriptionWithBalance(userId, planId)
	require.ErrorContains(t, err, "rounds to zero")
	var user User
	require.NoError(t, DB.Select("quota").First(&user, userId).Error)
	assert.Equal(t, 1000, user.Quota)
	var orderCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Count(&orderCount).Error)
	assert.Zero(t, orderCount)
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Count(&subscriptionCount).Error)
	assert.Zero(t, subscriptionCount)
}

func TestSetSubscriptionOrderCheckoutIdRejectsExpiredOrder(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7237, 7238
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order, err := CreatePendingSubscriptionOrder(userId, planId, "expired-unbound-order", PaymentMethodStripe, PaymentProviderStripe, SubscriptionCheckoutPolicy{
		Currency: "USD", CurrencySource: SubscriptionCurrencySourceProviderCallback,
		AmountMultiplier: "1", CheckoutMode: SubscriptionCheckoutModeOneTime,
	})
	require.NoError(t, err)
	require.NoError(t, ExpireSubscriptionOrder(order.TradeNo, PaymentProviderStripe))
	require.ErrorIs(t, SetSubscriptionOrderCheckoutId(order.TradeNo, PaymentProviderStripe, "cs_late"), ErrSubscriptionOrderStatusInvalid)
}

func TestSubscriptionResetPolicyDoesNotFollowCatalogEdits(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7241, 7242
	require.NoError(t, DB.Create(&User{Id: userId, Username: "immutable-reset-user", AffCode: "checkout-aff-7241", Status: common.UserStatusEnabled}).Error)
	plan := &SubscriptionPlan{
		Id: planId, Title: "Never reset", PriceAmount: 1, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
		TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, DB.Create(plan).Error)
	var subscription *UserSubscription
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		subscription, err = CreateUserSubscriptionFromPlanTx(tx, userId, plan, "admin")
		return err
	}))
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subscription.Id).Update("amount_used", 100).Error)
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", planId).Updates(map[string]interface{}{
		"quota_reset_period": SubscriptionResetDaily,
		"title":              "Edited daily plan",
	}).Error)

	result, err := PreConsumeUserSubscription("immutable-reset-request", userId, "gpt-4o", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(100), result.AmountUsedBefore)
	assert.Equal(t, int64(110), result.AmountUsedAfter)
	var persisted UserSubscription
	require.NoError(t, DB.First(&persisted, subscription.Id).Error)
	assert.Equal(t, SubscriptionResetNever, persisted.QuotaResetPeriod)
	assert.Equal(t, "Never reset", persisted.PlanTitle)
	assert.Equal(t, int64(110), persisted.AmountUsed)
	assert.Zero(t, persisted.NextResetTime)
}

func TestResetDueSubscriptionsClearsStaleNeverResetRows(t *testing.T) {
	truncateTables(t)
	subscription := &UserSubscription{
		Id: 7245, UserId: 7246, PlanId: 999999, AmountTotal: 100, AmountUsed: 50,
		StartTime: 1, EndTime: GetDBTimestamp() + 3600, Status: "active",
		QuotaResetPeriod: SubscriptionResetNever, NextResetTime: 1,
	}
	require.NoError(t, DB.Create(subscription).Error)

	processed, err := ResetDueSubscriptions(1)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Zero(t, subscription.NextResetTime)
	assert.Equal(t, int64(50), subscription.AmountUsed)
	processed, err = ResetDueSubscriptions(1)
	require.NoError(t, err)
	assert.Zero(t, processed, "the stale row must leave the due queue permanently")
}

func TestCustomSubscriptionResetFastForwardsWithoutPerPeriodLoop(t *testing.T) {
	truncateTables(t)
	now := GetDBTimestamp()
	start := now - 365*24*60*60
	subscription := &UserSubscription{
		Id: 7247, UserId: 7248, PlanId: 999998, AmountTotal: 100, AmountUsed: 50,
		StartTime: start, EndTime: now + 3600, Status: "active",
		QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 1,
		LastResetTime: start, NextResetTime: start + 1,
	}
	require.NoError(t, DB.Create(subscription).Error)

	processed, err := ResetDueSubscriptions(1)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Zero(t, subscription.AmountUsed)
	assert.GreaterOrEqual(t, subscription.LastResetTime, now-1)
	assert.Greater(t, subscription.NextResetTime, now)
}

func TestBackfillUserSubscriptionBenefitSnapshots(t *testing.T) {
	truncateTables(t)
	const planId = 7251
	plan := &SubscriptionPlan{
		Id: planId, Title: "Legacy daily", Currency: "USD", DurationUnit: SubscriptionDurationMonth,
		DurationValue: 1, Enabled: true, QuotaResetPeriod: SubscriptionResetCustom,
		QuotaResetCustomSeconds: 3600,
	}
	require.NoError(t, DB.Create(plan).Error)
	legacy := &UserSubscription{Id: 7252, UserId: 7253, PlanId: planId, Status: "active"}
	missingPlan := &UserSubscription{Id: 7254, UserId: 7255, PlanId: 999999, Status: "active", NextResetTime: 1}
	require.NoError(t, DB.Create(legacy).Error)
	require.NoError(t, DB.Create(missingPlan).Error)

	require.NoError(t, backfillUserSubscriptionBenefitSnapshots())
	require.NoError(t, DB.First(legacy, legacy.Id).Error)
	assert.Equal(t, SubscriptionOrderSnapshotVersion, legacy.BenefitSnapshotVersion)
	assert.Equal(t, plan.Title, legacy.PlanTitle)
	assert.Equal(t, SubscriptionResetCustom, legacy.QuotaResetPeriod)
	assert.Equal(t, int64(3600), legacy.QuotaResetCustomSeconds)
	require.NoError(t, DB.First(missingPlan, missingPlan.Id).Error)
	assert.Zero(t, missingPlan.BenefitSnapshotVersion)
	assert.Zero(t, missingPlan.NextResetTime, "missing legacy plans must not starve the reset queue")
	require.NoError(t, backfillUserSubscriptionBenefitSnapshots(), "repeated backfill must be idempotent")
}

func TestBackfillSubscriptionPaymentReceiptMetadataAndCanonicalKey(t *testing.T) {
	truncateTables(t)
	order := &SubscriptionOrder{
		UserId: 7261, PlanId: 7262, TradeNo: "legacy-receipt-order",
		PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(order).Error)
	receipt := &SubscriptionPaymentReceipt{
		OrderId: order.Id, ReceiptKey: "stripe:cs_legacy", Provider: PaymentProviderStripe,
		ProviderTransactionId: "cs_legacy", Amount: "9.99", PaidAmount: "9.99",
		Currency: "USD", CurrencySource: SubscriptionCurrencySourceProviderCallback,
		ProductId: "price_legacy", PaymentMethod: PaymentMethodStripe,
		CheckoutMode: SubscriptionCheckoutModeOneTime,
	}
	require.NoError(t, DB.Create(receipt).Error)

	require.NoError(t, backfillSubscriptionPaymentReceiptMetadata())
	require.NoError(t, DB.First(receipt, receipt.Id).Error)
	expectedKey, err := subscriptionReceiptKey(PaymentProviderStripe, "cs_legacy")
	require.NoError(t, err)
	assert.Equal(t, expectedKey, receipt.ReceiptKey)
	assert.Equal(t, fmt.Sprintf("legacy-%d", receipt.Id), receipt.ClaimId)
	assert.Equal(t, SubscriptionReceiptDispositionFulfilled, receipt.Disposition)
}

func TestSubscriptionPaymentReceiptMigrationPreservesLegacyRows(t *testing.T) {
	originalDB := DB
	database, err := gorm.Open(sqlite.Open("file:legacy-subscription-receipt-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = database
	t.Cleanup(func() { DB = originalDB })
	require.NoError(t, database.AutoMigrate(&SubscriptionOrder{}, &legacySubscriptionPaymentReceiptSchema{}))
	order := &SubscriptionOrder{TradeNo: "legacy-schema-order", Status: common.TopUpStatusSuccess}
	require.NoError(t, database.Create(order).Error)
	legacy := &legacySubscriptionPaymentReceiptSchema{
		OrderId: order.Id, ReceiptKey: "stripe:cs_legacy_schema",
		Provider: PaymentProviderStripe, ProviderTransactionId: "cs_legacy_schema",
		Amount: "1.00", PaidAmount: "1.00", Currency: "USD",
		CurrencySource: SubscriptionCurrencySourceProviderCallback, ProductId: "price_legacy",
		PaymentMethod: PaymentMethodStripe, CheckoutMode: SubscriptionCheckoutModeOneTime,
	}
	require.NoError(t, database.Create(legacy).Error)

	require.NoError(t, database.AutoMigrate(&SubscriptionPaymentReceipt{}))
	require.NoError(t, ensureSubscriptionPaymentReceiptIndexes(database))
	require.NoError(t, backfillSubscriptionPaymentReceiptMetadata())
	var migrated SubscriptionPaymentReceipt
	require.NoError(t, database.First(&migrated, legacy.Id).Error)
	assert.Equal(t, SubscriptionReceiptDispositionFulfilled, migrated.Disposition)
	assert.NotEmpty(t, migrated.ClaimId)
	assert.NotContains(t, migrated.ReceiptKey, ":")

	second := &SubscriptionPaymentReceipt{
		OrderId: order.Id, ReceiptKey: strings.Repeat("d", 64), ClaimId: "second-claim",
		Provider: PaymentProviderStripe, ProviderTransactionId: "cs_second_schema",
		Amount: "1.00", PaidAmount: "1.00", Currency: "USD",
		CurrencySource: SubscriptionCurrencySourceProviderCallback, ProductId: "price_second",
		PaymentMethod: PaymentMethodStripe, CheckoutMode: SubscriptionCheckoutModeOneTime,
		Disposition: SubscriptionReceiptDispositionDuplicatePayment,
	}
	require.NoError(t, database.Create(second).Error, "order_id must be a non-unique lookup index after migration")
}

func TestResolveSubscriptionOrderReviewGrantIsIdempotent(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7281, 7282
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "manual-review-grant")
	fact := stripeSubscriptionFact(order, "pi_manual_review_grant")
	fact.Amount = "10.99"
	fact.PaidAmount = "10.99"
	require.ErrorIs(t, CompleteSubscriptionOrder(fact), ErrSubscriptionOrderReconciliationRequired)

	items, total, err := ListSubscriptionOrderReviews(0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.Len(t, items[0].Receipts, 1)
	require.Len(t, items[0].Evidence, 1)
	receiptId := items[0].Receipts[0].Id

	require.NoError(t, ResolveSubscriptionOrderReview(
		order.Id, userId, receiptId, SubscriptionReviewActionGrant, "provider dashboard confirms the captured payment",
	))
	// An exact client retry must not issue a second entitlement or decision.
	require.NoError(t, ResolveSubscriptionOrderReview(
		order.Id, userId, receiptId, SubscriptionReviewActionGrant, "provider dashboard confirms the captured payment",
	))
	require.ErrorIs(t, ResolveSubscriptionOrderReview(
		order.Id, userId, receiptId, SubscriptionReviewActionClose, "different resolution",
	), ErrSubscriptionReviewAlreadyResolved)

	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, persisted.Status)
	assert.Equal(t, SubscriptionOrderReviewResolved, persisted.ReviewStatus)
	assert.Equal(t, SubscriptionReviewActionGrant, persisted.ResolutionAction)
	assert.Equal(t, receiptId, persisted.ResolutionReceiptId)
	assert.Equal(t, userId, persisted.ResolvedBy)
	assert.Positive(t, persisted.ResolvedAt)

	var receipt SubscriptionPaymentReceipt
	require.NoError(t, DB.First(&receipt, receiptId).Error)
	assert.Equal(t, SubscriptionReceiptDispositionFulfilled, receipt.Disposition)
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userId).Count(&subscriptionCount).Error)
	assert.EqualValues(t, 1, subscriptionCount)
	var decisionCount int64
	require.NoError(t, DB.Model(&SubscriptionPaymentReviewDecision{}).Where("order_id = ?", order.Id).Count(&decisionCount).Error)
	assert.EqualValues(t, 1, decisionCount)
	_, remaining, err := ListSubscriptionOrderReviews(0, 20)
	require.NoError(t, err)
	assert.Zero(t, remaining)
}

func TestResolveDuplicatePaymentReviewClosesWithoutChangingEntitlement(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7283, 7284
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "manual-review-close")
	require.NoError(t, CompleteSubscriptionOrder(stripeSubscriptionFact(order, "pi_manual_review_primary")))
	duplicate := stripeSubscriptionFact(order, "pi_manual_review_duplicate")
	duplicate.PayloadHash = strings.Repeat("b", 64)
	require.ErrorIs(t, CompleteSubscriptionOrder(duplicate), ErrSubscriptionOrderReconciliationRequired)

	var receipt SubscriptionPaymentReceipt
	require.NoError(t, DB.Where("provider_transaction_id = ?", duplicate.ProviderTransactionId).First(&receipt).Error)
	require.NoError(t, ResolveSubscriptionOrderReview(
		order.Id, userId, receipt.Id, SubscriptionReviewActionClose, "duplicate charge handled by support",
	))

	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, persisted.Status)
	assert.Equal(t, SubscriptionOrderReviewResolved, persisted.ReviewStatus)
	require.NoError(t, DB.First(&receipt, receipt.Id).Error)
	assert.Equal(t, SubscriptionReceiptDispositionReviewClosed, receipt.Disposition)
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userId).Count(&subscriptionCount).Error)
	assert.EqualValues(t, 1, subscriptionCount)
}

func TestConflictingSubscriptionPaymentFactsPreserveEveryEvidenceRecord(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7285, 7286
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "conflicting-evidence")
	first := stripeSubscriptionFact(order, "pi_conflicting_evidence")
	require.NoError(t, CompleteSubscriptionOrder(first))

	conflict := first
	conflict.Amount = "10.99"
	conflict.PaidAmount = "10.99"
	conflict.ProviderEventId = "evt_conflicting_evidence_second"
	conflict.PayloadHash = strings.Repeat("c", 64)
	require.ErrorIs(t, CompleteSubscriptionOrder(conflict), ErrSubscriptionOrderReconciliationRequired)

	var receipts []SubscriptionPaymentReceipt
	require.NoError(t, DB.Where("order_id = ?", order.Id).Find(&receipts).Error)
	require.Len(t, receipts, 1, "the provider transaction remains a unique fulfillment boundary")
	var evidence []SubscriptionPaymentEvidence
	require.NoError(t, DB.Where("order_id = ?", order.Id).Order("id asc").Find(&evidence).Error)
	require.Len(t, evidence, 2, "conflicting authenticated callbacks must not overwrite one another")
	assert.NotEqual(t, evidence[0].EvidenceKey, evidence[1].EvidenceKey)
	assert.NotEqual(t, evidence[0].CanonicalFact, evidence[1].CanonicalFact)
	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, persisted.Status)
	assert.Equal(t, SubscriptionOrderReviewRequired, persisted.ReviewStatus)
	assert.Equal(t, receipts[0].Id, persisted.ReviewRelatedReceiptId)
}

func TestInvalidVerifiedSubscriptionFactIsStoredOnlyAsBoundedEvidence(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7287, 7288
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "bounded-invalid-evidence")
	fact := stripeSubscriptionFact(order, strings.Repeat("t", 500))
	fact.ProviderEventId = strings.Repeat("e", 500)
	fact.ProviderCheckoutId = strings.Repeat("c", 500)
	fact.Amount = strings.Repeat("9", 100)
	fact.PaidAmount = strings.Repeat("8", 100)
	fact.Currency = strings.Repeat("U", 100)
	fact.CurrencySource = strings.Repeat("s", 100)
	fact.ProductId = strings.Repeat("p", 500)
	fact.PaymentMethod = strings.Repeat("m", 100)
	fact.CheckoutMode = strings.Repeat("o", 100)

	require.ErrorIs(t, CompleteSubscriptionOrder(fact), ErrSubscriptionOrderReconciliationRequired)
	var receipt SubscriptionPaymentReceipt
	require.NoError(t, DB.Where("order_id = ?", order.Id).First(&receipt).Error)
	assert.LessOrEqual(t, len(receipt.ProviderTransactionId), 128)
	assert.LessOrEqual(t, len(receipt.ProviderEventId), 128)
	assert.LessOrEqual(t, len(receipt.ProviderCheckoutId), 128)
	assert.LessOrEqual(t, len(receipt.Amount), 32)
	assert.LessOrEqual(t, len(receipt.PaidAmount), 32)
	assert.LessOrEqual(t, len(receipt.Currency), 8)
	assert.LessOrEqual(t, len(receipt.CurrencySource), 32)
	assert.LessOrEqual(t, len(receipt.ProductId), 191)
	assert.LessOrEqual(t, len(receipt.PaymentMethod), 50)
	assert.LessOrEqual(t, len(receipt.CheckoutMode), 32)
	var evidence SubscriptionPaymentEvidence
	require.NoError(t, DB.Where("order_id = ?", order.Id).First(&evidence).Error)
	assert.Equal(t, "invalid", evidence.FactStatus)
	assert.NotContains(t, evidence.CanonicalFact, strings.Repeat("t", 500))
	assert.NotContains(t, evidence.CanonicalFact, strings.Repeat("p", 500))
}

func TestVerifiedSubscriptionFactRejectsVarcharOverflowBeforePersistence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*VerifiedSubscriptionPaymentFact)
	}{
		{
			name: "payment method",
			mutate: func(fact *VerifiedSubscriptionPaymentFact) {
				fact.PaymentMethod = strings.Repeat("m", 51)
			},
		},
		{
			name: "amount",
			mutate: func(fact *VerifiedSubscriptionPaymentFact) {
				fact.Amount = strings.Repeat("9", 33) + ".00"
			},
		},
		{
			name: "paid amount",
			mutate: func(fact *VerifiedSubscriptionPaymentFact) {
				fact.PaidAmount = strings.Repeat("9", 33) + ".00"
			},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 7301 + index*2
			planId := userId + 1
			seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
			order := createStripeSubscriptionCheckoutOrder(t, userId, planId, fmt.Sprintf("varchar-overflow-%d", index))
			fact := stripeSubscriptionFact(order, fmt.Sprintf("pi_varchar_overflow_%d", index))
			test.mutate(&fact)

			require.ErrorIs(t, CompleteSubscriptionOrder(fact), ErrSubscriptionOrderReconciliationRequired)
			var receipt SubscriptionPaymentReceipt
			require.NoError(t, DB.Where("order_id = ?", order.Id).First(&receipt).Error)
			assert.LessOrEqual(t, len(receipt.Amount), 32)
			assert.LessOrEqual(t, len(receipt.PaidAmount), 32)
			assert.LessOrEqual(t, len(receipt.PaymentMethod), 50)
			var evidence SubscriptionPaymentEvidence
			require.NoError(t, DB.Where("order_id = ?", order.Id).First(&evidence).Error)
			assert.Equal(t, "invalid", evidence.FactStatus)
		})
	}
}

func TestCreatePendingSubscriptionOrderRejectsOversizedPaymentContract(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7311, 7312
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)

	_, err := CreatePendingSubscriptionOrder(
		userId, planId, "oversized-payment-method", strings.Repeat("m", 51), PaymentProviderStripe,
		SubscriptionCheckoutPolicy{
			Currency: "USD", CurrencySource: SubscriptionCurrencySourceProviderCallback,
			AmountMultiplier: "1", CheckoutMode: SubscriptionCheckoutModeOneTime,
		},
	)
	require.Error(t, err)

	_, err = CreatePendingSubscriptionOrder(
		userId, planId, "oversized-expected-amount", PaymentMethodStripe, PaymentProviderStripe,
		SubscriptionCheckoutPolicy{
			Currency: "USD", CurrencySource: SubscriptionCurrencySourceProviderCallback,
			AmountMultiplier: "1e40", CheckoutMode: SubscriptionCheckoutModeOneTime,
		},
	)
	require.ErrorContains(t, err, "too large")
	var orderCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Count(&orderCount).Error)
	assert.Zero(t, orderCount)
}

func TestExpiredSubscriptionOrderReviewPreservesTerminalState(t *testing.T) {
	truncateTables(t)
	const userId, planId = 7289, 7290
	seedSubscriptionCheckoutUserAndPlan(t, userId, planId)
	order := createStripeSubscriptionCheckoutOrder(t, userId, planId, "expired-review-state")
	require.NoError(t, ExpireSubscriptionOrder(order.TradeNo, PaymentProviderStripe))
	var expired SubscriptionOrder
	require.NoError(t, DB.First(&expired, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, expired.Status)
	require.Positive(t, expired.CompleteTime)

	fact := stripeSubscriptionFact(order, "pi_expired_review")
	require.ErrorIs(t, CompleteSubscriptionOrder(fact), ErrSubscriptionOrderReconciliationRequired)
	var reviewed SubscriptionOrder
	require.NoError(t, DB.First(&reviewed, order.Id).Error)
	assert.Equal(t, common.TopUpStatusExpired, reviewed.Status)
	assert.Equal(t, expired.CompleteTime, reviewed.CompleteTime)
	assert.Equal(t, common.TopUpStatusExpired, reviewed.ReviewPreviousStatus)
	assert.Equal(t, expired.CompleteTime, reviewed.ReviewPreviousCompleteTime)
	assert.Equal(t, SubscriptionOrderReviewRequired, reviewed.ReviewStatus)

	var receipt SubscriptionPaymentReceipt
	require.NoError(t, DB.Where("order_id = ?", order.Id).First(&receipt).Error)
	require.NoError(t, ResolveSubscriptionOrderReview(
		order.Id, userId, receipt.Id, SubscriptionReviewActionClose, "late payment was refunded",
	))
	require.NoError(t, DB.First(&reviewed, order.Id).Error)
	assert.Equal(t, common.TopUpStatusExpired, reviewed.Status)
	assert.Equal(t, expired.CompleteTime, reviewed.CompleteTime)
	assert.Equal(t, SubscriptionOrderReviewResolved, reviewed.ReviewStatus)
}

func TestBackfillSubscriptionOrderReviewMetadata(t *testing.T) {
	truncateTables(t)
	legacy := &SubscriptionOrder{
		UserId: 7291, PlanId: 7292, TradeNo: "legacy-review-metadata",
		Status: SubscriptionOrderStatusReconciliationRequired, CreateTime: 100, CompleteTime: 200,
	}
	require.NoError(t, DB.Create(legacy).Error)
	require.NoError(t, backfillSubscriptionOrderReviewMetadata())
	require.NoError(t, backfillSubscriptionOrderReviewMetadata(), "the migration must be idempotent")
	require.NoError(t, DB.First(legacy, legacy.Id).Error)
	assert.Equal(t, SubscriptionOrderReviewRequired, legacy.ReviewStatus)
	assert.Equal(t, int64(200), legacy.FirstReviewTime)
	assert.Equal(t, int64(200), legacy.ReviewTime)
	assert.Equal(t, "legacy_unknown", legacy.ReviewPreviousStatus)
	assert.Equal(t, int64(200), legacy.ReviewPreviousCompleteTime)
}
