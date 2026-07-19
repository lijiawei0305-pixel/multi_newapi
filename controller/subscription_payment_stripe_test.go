package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	stripewebhook "github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
)

func TestNewStripeSubscriptionSessionParamsUsesOneTimeCheckoutAndSnapshotMetadata(t *testing.T) {
	params := newStripeSubscriptionSessionParams(
		"sub_ref_123",
		"",
		"buyer@example.test",
		"price_subscription_123",
		"snapshot_hash_123",
	)

	require.NotNil(t, params.Mode)
	require.Equal(t, string(stripe.CheckoutSessionModePayment), *params.Mode)
	require.NotNil(t, params.ClientReferenceID)
	require.Equal(t, "sub_ref_123", *params.ClientReferenceID)
	require.Len(t, params.LineItems, 1)
	require.NotNil(t, params.LineItems[0].Price)
	require.Equal(t, "price_subscription_123", *params.LineItems[0].Price)
	require.NotNil(t, params.LineItems[0].Quantity)
	require.EqualValues(t, 1, *params.LineItems[0].Quantity)
	require.Equal(t, "price_subscription_123", params.Metadata[stripeSubscriptionProductMetadataKey])
	require.Equal(t, "snapshot_hash_123", params.Metadata[stripeSubscriptionSnapshotMetadataKey])
	require.NotNil(t, params.CustomerEmail)
	require.Equal(t, "buyer@example.test", *params.CustomerEmail)
	require.NotNil(t, params.CustomerCreation)
	require.Equal(t, string(stripe.CheckoutSessionCustomerCreationAlways), *params.CustomerCreation)
}

func TestNewStripeSubscriptionSessionParamsUsesExistingCustomer(t *testing.T) {
	params := newStripeSubscriptionSessionParams(
		"sub_ref_123",
		"cus_123",
		"buyer@example.test",
		"price_subscription_123",
		"snapshot_hash_123",
	)

	require.NotNil(t, params.Customer)
	require.Equal(t, "cus_123", *params.Customer)
	require.Nil(t, params.CustomerEmail)
	require.Nil(t, params.CustomerCreation)
}

func TestStripeSubscriptionPaymentFactUsesSignedCheckoutSessionFields(t *testing.T) {
	event := stripe.Event{
		ID:      "evt_123",
		Created: 1_725_000_000,
		Type:    stripe.EventTypeCheckoutSessionCompleted,
		Data: &stripe.EventData{Object: map[string]any{
			"id":           "cs_123",
			"amount_total": float64(1234),
			"currency":     "usd",
			"mode":         string(stripe.CheckoutSessionModePayment),
			"metadata": map[string]any{
				stripeSubscriptionProductMetadataKey:  "price_subscription_123",
				stripeSubscriptionSnapshotMetadataKey: "snapshot_hash_123",
			},
		}},
	}

	fact, err := stripeSubscriptionPaymentFact(event, "sub_ref_123", "payload_hash_123")
	require.NoError(t, err)
	require.Equal(t, model.VerifiedSubscriptionPaymentFact{
		TradeNo:               "sub_ref_123",
		Provider:              model.PaymentProviderStripe,
		ProviderEventId:       "evt_123",
		ProviderTransactionId: "cs_123",
		ProviderCheckoutId:    "cs_123",
		Amount:                "12.34",
		PaidAmount:            "12.34",
		Currency:              "USD",
		CurrencySource:        model.SubscriptionCurrencySourceProviderCallback,
		ProductId:             "price_subscription_123",
		PaymentMethod:         model.PaymentMethodStripe,
		CheckoutMode:          model.SubscriptionCheckoutModeOneTime,
		SnapshotHash:          "snapshot_hash_123",
		PayloadHash:           "payload_hash_123",
		PaidAt:                1_725_000_000,
	}, fact)
}

func TestStripeSubscriptionPaymentFactRejectsNonIntegerMinorAmount(t *testing.T) {
	event := stripe.Event{
		ID: "evt_123",
		Data: &stripe.EventData{Object: map[string]any{
			"id":           "cs_123",
			"amount_total": "12.5",
		}},
	}

	_, err := stripeSubscriptionPaymentFact(event, "sub_ref_123", "payload_hash_123")
	require.Error(t, err)
}

func TestStripeSubscriptionPaymentFactHandlesMissingMetadata(t *testing.T) {
	event := stripe.Event{
		ID: "evt_123",
		Data: &stripe.EventData{Object: map[string]any{
			"id":           "cs_123",
			"amount_total": float64(100),
			"currency":     "usd",
			"mode":         string(stripe.CheckoutSessionModePayment),
		}},
	}

	fact, err := stripeSubscriptionPaymentFact(event, "sub_ref_123", "payload_hash_123")
	require.NoError(t, err)
	require.Empty(t, fact.ProductId)
	require.Empty(t, fact.SnapshotHash)
}

func TestStripeSubscriptionPaymentFactDoesNotAcceptSubscriptionModeAsOneTime(t *testing.T) {
	event := stripe.Event{
		ID: "evt_123",
		Data: &stripe.EventData{Object: map[string]any{
			"id":           "cs_123",
			"amount_total": float64(100),
			"currency":     "usd",
			"mode":         string(stripe.CheckoutSessionModeSubscription),
		}},
	}

	fact, err := stripeSubscriptionPaymentFact(event, "sub_ref_123", "payload_hash_123")
	require.NoError(t, err)
	require.Equal(t, string(stripe.CheckoutSessionModeSubscription), fact.CheckoutMode)
	require.NotEqual(t, model.SubscriptionCheckoutModeOneTime, fact.CheckoutMode)
}

func TestStripeWebhookReturnsRetryableStatusWhenVerifiedEventProcessingFails(t *testing.T) {
	originalWebhookSecret := setting.StripeWebhookSecret
	setting.StripeWebhookSecret = "whsec_retry_test"
	t.Cleanup(func() {
		setting.StripeWebhookSecret = originalWebhookSecret
	})

	payload := []byte(`{
		"id":"evt_retry_123",
		"object":"event",
		"created":1725000000,
		"type":"checkout.session.completed",
		"data":{"object":{
			"id":"cs_retry_123",
			"client_reference_id":"sub_ref_retry_123",
			"status":"complete",
			"payment_status":"paid",
			"amount_total":"invalid",
			"currency":"usd",
			"mode":"payment",
			"metadata":{}
		}}
	}`)
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: payload,
		Secret:  setting.StripeWebhookSecret,
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", bytes.NewReader(payload))
	c.Request.Header.Set("Stripe-Signature", signed.Header)

	StripeWebhook(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestStripeWebhookAcknowledgesPaidMismatchAfterPersistingReconciliation(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf(
		"file:stripe-webhook-reconciliation-%d?mode=memory&cache=shared",
		time.Now().UnixNano(),
	)), &gorm.Config{})
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
		sqlDB, sqlErr := database.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	const (
		userId        = 9_501
		planId        = 9_502
		tradeNo       = "sub_ref_reconciliation_123"
		checkoutId    = "cs_reconciliation_123"
		productId     = "price_reconciliation_123"
		webhookSecret = "whsec_reconciliation_test"
	)
	require.NoError(t, database.Create(&model.User{
		Id:       userId,
		Username: "stripe_reconciliation_user",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, database.Create(&model.SubscriptionPlan{
		Id:            planId,
		Title:         "Stripe reconciliation plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   10_000,
		StripePriceId: productId,
	}).Error)
	order, err := model.CreatePendingSubscriptionOrder(
		userId,
		planId,
		tradeNo,
		model.PaymentMethodStripe,
		model.PaymentProviderStripe,
		model.SubscriptionCheckoutPolicy{
			Currency:         "USD",
			CurrencySource:   model.SubscriptionCurrencySourceProviderCallback,
			AmountMultiplier: "1",
			CheckoutMode:     model.SubscriptionCheckoutModeOneTime,
		},
	)
	require.NoError(t, err)
	require.NoError(t, model.SetSubscriptionOrderCheckoutId(tradeNo, model.PaymentProviderStripe, checkoutId))

	originalWebhookSecret := setting.StripeWebhookSecret
	setting.StripeWebhookSecret = webhookSecret
	t.Cleanup(func() {
		setting.StripeWebhookSecret = originalWebhookSecret
	})
	payload, err := common.Marshal(map[string]any{
		"id":      "evt_reconciliation_123",
		"object":  "event",
		"created": int64(1_725_000_000),
		"type":    "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{
			"id":                  checkoutId,
			"client_reference_id": tradeNo,
			"status":              "complete",
			"payment_status":      "paid",
			"amount_total":        1099,
			"currency":            "usd",
			"mode":                "payment",
			"metadata": map[string]string{
				stripeSubscriptionProductMetadataKey:  productId,
				stripeSubscriptionSnapshotMetadataKey: order.SnapshotHash,
			},
		}},
	})
	require.NoError(t, err)
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: payload,
		Secret:  setting.StripeWebhookSecret,
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", bytes.NewReader(payload))
	c.Request.Header.Set("Stripe-Signature", signed.Header)

	StripeWebhook(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	persisted := model.GetSubscriptionOrderByTradeNo(tradeNo)
	require.NotNil(t, persisted)
	require.Equal(t, model.SubscriptionOrderStatusReconciliationRequired, persisted.Status)
	require.NotEmpty(t, persisted.PaymentFact)
	require.NotEmpty(t, persisted.ReconciliationReason)
	var receiptCount int64
	require.NoError(t, database.Model(&model.SubscriptionPaymentReceipt{}).Count(&receiptCount).Error)
	require.EqualValues(t, 1, receiptCount)
	var entitlementCount int64
	require.NoError(t, database.Model(&model.UserSubscription{}).Count(&entitlementCount).Error)
	require.Zero(t, entitlementCount)
}
