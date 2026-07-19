package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/epay"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func validCreemSubscriptionEvent() *CreemWebhookEvent {
	event := &CreemWebhookEvent{
		Id:        "evt_creem_paid_1",
		EventType: "checkout.completed",
		CreatedAt: 1_750_000_000,
	}
	event.Object.Id = "checkout_creem_1"
	event.Object.RequestId = "sub_ref_1"
	event.Object.Order.Id = "order_creem_1"
	event.Object.Order.Product = "prod_subscription_1"
	event.Object.Order.Amount = 1300
	event.Object.Order.SubTotal = 1234
	event.Object.Order.AmountPaid = 1200
	event.Object.Order.Currency = "usd"
	event.Object.Order.Status = "paid"
	event.Object.Order.Type = "onetime"
	event.Object.Order.Transaction = "txn_creem_1"
	event.Object.Order.UpdatedAt = "2026-07-19T08:30:00Z"
	event.Object.Product.Id = "prod_subscription_1"
	event.Object.Product.Currency = "USD"
	event.Object.Metadata = map[string]string{
		"new_api_subscription_product_id":    "prod_subscription_1",
		"new_api_subscription_snapshot_hash": strings.Repeat("a", 64),
	}
	return event
}

func TestCreemSubscriptionPaymentFactPreservesVerifiedProviderFacts(t *testing.T) {
	event := validCreemSubscriptionEvent()

	fact, consistent := creemSubscriptionPaymentFact(event, strings.Repeat("b", 64))

	require.True(t, consistent)
	assert.Equal(t, "sub_ref_1", fact.TradeNo)
	assert.Equal(t, model.PaymentProviderCreem, fact.Provider)
	assert.Equal(t, "evt_creem_paid_1", fact.ProviderEventId)
	assert.Equal(t, "txn_creem_1", fact.ProviderTransactionId)
	assert.Equal(t, "checkout_creem_1", fact.ProviderCheckoutId)
	assert.Equal(t, "12.34", fact.Amount)
	assert.Equal(t, "12.00", fact.PaidAmount)
	assert.Equal(t, "USD", fact.Currency)
	assert.Equal(t, model.SubscriptionCurrencySourceProviderCallback, fact.CurrencySource)
	assert.Equal(t, "prod_subscription_1", fact.ProductId)
	assert.Equal(t, model.PaymentMethodCreem, fact.PaymentMethod)
	assert.Equal(t, strings.Repeat("a", 64), fact.SnapshotHash)
	assert.Equal(t, strings.Repeat("b", 64), fact.PayloadHash)
	assert.Equal(t, model.SubscriptionCheckoutModeOneTime, fact.CheckoutMode)
	assert.Equal(t, time.Date(2026, time.July, 19, 8, 30, 0, 0, time.UTC).Unix(), fact.PaidAt)
}

func TestCreemSubscriptionPaymentFactFallsBackToOrderIdentity(t *testing.T) {
	event := validCreemSubscriptionEvent()
	event.Object.Order.Transaction = ""

	fact, consistent := creemSubscriptionPaymentFact(event, strings.Repeat("b", 64))

	require.True(t, consistent)
	assert.Equal(t, event.Object.Order.Id, fact.ProviderTransactionId)
}

func TestCreemSubscriptionPaymentFactFailsClosedOnInconsistentCheckout(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CreemWebhookEvent)
		check  func(*testing.T, model.VerifiedSubscriptionPaymentFact)
	}{
		{
			name: "order product differs from checkout product",
			mutate: func(event *CreemWebhookEvent) {
				event.Object.Order.Product = "prod_other"
			},
			check: func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) {
				assert.Empty(t, fact.ProductId)
			},
		},
		{
			name: "provider currencies differ",
			mutate: func(event *CreemWebhookEvent) {
				event.Object.Product.Currency = "CNY"
			},
			check: func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) {
				assert.Empty(t, fact.Currency)
			},
		},
		{
			name: "recurring checkout is not normalized to one time",
			mutate: func(event *CreemWebhookEvent) {
				event.Object.Order.Type = "recurring"
			},
			check: func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) {
				assert.Equal(t, "recurring", fact.CheckoutMode)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := validCreemSubscriptionEvent()
			test.mutate(event)

			fact, consistent := creemSubscriptionPaymentFact(event, strings.Repeat("c", 64))

			assert.False(t, consistent)
			test.check(t, fact)
		})
	}
}

func TestSubscriptionEpayPaymentFactUsesOnlySignedPaymentFields(t *testing.T) {
	snapshotHash := strings.Repeat("d", 64)
	verified := &epay.VerifyResult{
		Type:           "alipay",
		TradeNo:        "provider_tx_1",
		ServiceTradeNo: "SUBUSR1NO123",
		Name:           "SUBPLAN:42:" + snapshotHash,
		Money:          "72.30",
		TradeStatus:    epay.StatusTradeSuccess,
		VerifyStatus:   true,
	}
	paramsA := map[string]string{"trade_no": "provider_tx_1", "money": "72.30", "name": verified.Name}
	paramsB := map[string]string{"name": verified.Name, "money": "72.30", "trade_no": "provider_tx_1"}

	fact, err := subscriptionEpayPaymentFact(verified, paramsA)
	require.NoError(t, err)
	factWithDifferentMapOrder, err := subscriptionEpayPaymentFact(verified, paramsB)
	require.NoError(t, err)

	assert.Equal(t, "SUBUSR1NO123", fact.TradeNo)
	assert.Equal(t, model.PaymentProviderEpay, fact.Provider)
	assert.Equal(t, "provider_tx_1", fact.ProviderEventId)
	assert.Equal(t, "provider_tx_1", fact.ProviderTransactionId)
	assert.Equal(t, "72.30", fact.Amount)
	assert.Equal(t, "72.30", fact.PaidAmount)
	assert.Equal(t, "CNY", fact.Currency)
	assert.Equal(t, model.SubscriptionCurrencySourceMerchantContract, fact.CurrencySource)
	assert.Equal(t, "plan:42", fact.ProductId)
	assert.Equal(t, "alipay", fact.PaymentMethod)
	assert.Equal(t, snapshotHash, fact.SnapshotHash)
	assert.Equal(t, model.SubscriptionCheckoutModeOneTime, fact.CheckoutMode)
	assert.Len(t, fact.PayloadHash, 64)
	assert.Equal(t, fact.PayloadHash, factWithDifferentMapOrder.PayloadHash)
	assert.Zero(t, fact.PaidAt, "Epay does not provide a signed payment timestamp")
}

func TestSubscriptionEpayPaymentFactPreservesAmbiguousSignedFieldsForReconciliation(t *testing.T) {
	valid := epay.VerifyResult{
		Type:           "alipay",
		TradeNo:        "provider_tx_1",
		ServiceTradeNo: "SUBUSR1NO123",
		Name:           "SUBPLAN:42:" + strings.Repeat("e", 64),
		Money:          "72.30",
	}

	tests := []struct {
		name   string
		mutate func(*epay.VerifyResult)
		check  func(*testing.T, model.VerifiedSubscriptionPaymentFact)
	}{
		{
			name:   "legacy product name",
			mutate: func(result *epay.VerifyResult) { result.Name = "SUB:Plan" },
			check:  func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) { assert.Empty(t, fact.ProductId) },
		},
		{
			name:   "invalid plan",
			mutate: func(result *epay.VerifyResult) { result.Name = "SUBPLAN:0:" + strings.Repeat("e", 64) },
			check:  func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) { assert.Empty(t, fact.ProductId) },
		},
		{
			name:   "invalid snapshot hash",
			mutate: func(result *epay.VerifyResult) { result.Name = "SUBPLAN:42:not-a-hash" },
			check: func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) {
				assert.Equal(t, "not-a-hash", fact.SnapshotHash)
			},
		},
		{
			name:   "sub-cent precision",
			mutate: func(result *epay.VerifyResult) { result.Money = "72.301" },
			check:  func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) { assert.Equal(t, "72.301", fact.Amount) },
		},
		{
			name:   "missing payment method",
			mutate: func(result *epay.VerifyResult) { result.Type = "" },
			check:  func(t *testing.T, fact model.VerifiedSubscriptionPaymentFact) { assert.Empty(t, fact.PaymentMethod) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := valid
			test.mutate(&result)

			fact, err := subscriptionEpayPaymentFact(&result, map[string]string{"signed": "value"})

			require.NoError(t, err)
			test.check(t, fact)
		})
	}
}

func TestSubscriptionEpayPaymentFactRejectsMissingSignedTransactionIdentity(t *testing.T) {
	verified := &epay.VerifyResult{
		Type:           "alipay",
		ServiceTradeNo: "SUBUSR1NO123",
		Name:           "SUBPLAN:42:" + strings.Repeat("f", 64),
		Money:          "72.30",
	}

	_, err := subscriptionEpayPaymentFact(verified, map[string]string{"signed": "value"})

	require.Error(t, err)
}

func TestHandleCheckoutCompletedAcknowledgesPaidSubscriptionMismatchWithoutGrantingBenefits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CreemWebhookEvent)
	}{
		{
			name: "recurring checkout",
			mutate: func(event *CreemWebhookEvent) {
				event.Object.Order.Type = "recurring"
			},
		},
		{
			name: "inconsistent product",
			mutate: func(event *CreemWebhookEvent) {
				event.Object.Order.Product = "prod_other"
			},
		},
		{
			name: "inconsistent currency",
			mutate: func(event *CreemWebhookEvent) {
				event.Object.Product.Currency = "CNY"
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalDB := model.DB
			originalLogDB := model.LOG_DB
			originalMainDatabaseType := common.MainDatabaseType()
			originalLogDatabaseType := common.LogDatabaseType()
			database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:subscription-creem-mismatch-%d?mode=memory&cache=shared", index)), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, database.AutoMigrate(
				&model.User{},
				&model.SubscriptionPlan{},
				&model.SubscriptionOrder{},
				&model.SubscriptionPaymentReceipt{},
				&model.SubscriptionPaymentEvidence{},
				&model.UserSubscription{},
			))
			model.DB = database
			model.LOG_DB = database
			common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
			t.Cleanup(func() {
				model.DB = originalDB
				model.LOG_DB = originalLogDB
				common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
			})

			userId := 9200 + index
			planId := 9300 + index
			tradeNo := fmt.Sprintf("sub_ref_mismatch_%d", index)
			require.NoError(t, database.Create(&model.User{
				Id: userId, Username: fmt.Sprintf("creem-mismatch-%d", index), AffCode: fmt.Sprintf("creem-%d", index),
				Status: common.UserStatusEnabled, Group: "default",
			}).Error)
			require.NoError(t, database.Create(&model.SubscriptionPlan{
				Id: planId, Title: "Creem snapshot plan", PriceAmount: 12.34, Currency: "USD",
				DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
				TotalAmount: 1000, CreemProductId: "prod_subscription_1",
			}).Error)
			order, err := model.CreatePendingSubscriptionOrder(userId, planId, tradeNo, model.PaymentMethodCreem, model.PaymentProviderCreem, model.SubscriptionCheckoutPolicy{
				Currency:         "USD",
				CurrencySource:   model.SubscriptionCurrencySourceProviderCallback,
				AmountMultiplier: "1",
				CheckoutMode:     model.SubscriptionCheckoutModeOneTime,
			})
			require.NoError(t, err)
			require.NoError(t, model.SetSubscriptionOrderCheckoutId(tradeNo, model.PaymentProviderCreem, "checkout_creem_1"))

			event := validCreemSubscriptionEvent()
			event.Object.RequestId = tradeNo
			event.Object.Order.AmountPaid = 1234
			event.Object.Metadata["new_api_subscription_snapshot_hash"] = order.SnapshotHash
			event.Object.Customer.Email = "private-customer@example.test"
			event.Object.Customer.Name = "Private Customer"
			test.mutate(event)

			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, "/api/creem/webhook", nil)
			handleCheckoutCompleted(context, event, strings.Repeat("f", 64))

			assert.Equal(t, http.StatusOK, recorder.Code)
			storedOrder := model.GetSubscriptionOrderByTradeNo(tradeNo)
			require.NotNil(t, storedOrder)
			assert.Equal(t, model.SubscriptionOrderStatusReconciliationRequired, storedOrder.Status)
			assert.Empty(t, storedOrder.ProviderPayload)
			assert.NotEmpty(t, storedOrder.PaymentFact)
			assert.NotContains(t, storedOrder.PaymentFact, "private-customer@example.test")
			assert.NotContains(t, storedOrder.PaymentFact, "Private Customer")
			var subscriptionCount int64
			require.NoError(t, database.Model(&model.UserSubscription{}).Where("user_id = ?", userId).Count(&subscriptionCount).Error)
			assert.Zero(t, subscriptionCount)
		})
	}
}
