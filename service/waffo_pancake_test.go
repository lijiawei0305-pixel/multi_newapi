package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pancake "github.com/waffo-com/waffo-pancake-sdk-go"
)

func TestBuildWaffoPancakeAuthenticatedCheckoutParamsPreservesMetadata(t *testing.T) {
	expiresInSeconds := 900
	params := &WaffoPancakeCreateSessionParams{
		ProductID:     "prod_plan",
		BuyerIdentity: "new-api-user-17",
		PriceSnapshot: &WaffoPancakePriceSnapshot{
			Amount:      "29.00",
			TaxCategory: "saas",
		},
		Metadata: map[string]string{
			"new_api_subscription_product_id":    "prod_plan",
			"new_api_subscription_snapshot_hash": "snapshot-hash",
		},
		BuyerEmail:              "buyer@example.com",
		ExpiresInSeconds:        &expiresInSeconds,
		OrderMerchantExternalID: "WAFFO_PANCAKE_SUB-17-1-token",
	}

	actual := buildWaffoPancakeAuthenticatedCheckoutParams(params)

	require.NotNil(t, actual.PriceSnapshot)
	assert.Equal(t, "prod_plan", actual.ProductID)
	assert.Equal(t, "USD", actual.Currency)
	assert.Equal(t, "29.00", actual.PriceSnapshot.Amount)
	assert.Equal(t, "prod_plan", actual.Metadata["new_api_subscription_product_id"])
	assert.Equal(t, "snapshot-hash", actual.Metadata["new_api_subscription_snapshot_hash"])
	require.NotNil(t, actual.OrderMerchantExternalID)
	assert.Equal(t, params.OrderMerchantExternalID, *actual.OrderMerchantExternalID)

	params.Metadata["new_api_subscription_snapshot_hash"] = "mutated"
	assert.Equal(t, "snapshot-hash", actual.Metadata["new_api_subscription_snapshot_hash"])
}

func TestConvertWaffoPancakeWebhookEventPreservesVerifiedPaymentFacts(t *testing.T) {
	identity := "new-api-user-17"
	externalID := "WAFFO_PANCAKE_SUB-17-1-token"
	subtotal := "29.00"
	total := "31.90"
	paymentID := "PAY_123"
	paymentStatus := "succeeded"
	paymentMethod := "card"
	paymentDate := "2026-07-19T10:11:12Z"
	event := &pancake.TypedWebhookEvent[pancake.WebhookEventData]{
		ID:        "delivery-1",
		Timestamp: "2026-07-19T10:11:12Z",
		EventType: "order.completed",
		EventID:   "business-event-1",
		StoreID:   "store-1",
		Mode:      pancake.Environment("prod"),
		Data: pancake.WebhookEventData{
			OrderID:                       "ORD_123",
			OrderMerchantExternalID:       &externalID,
			MerchantProvidedBuyerIdentity: &identity,
			Currency:                      "USD",
			Amount:                        "29.00",
			TaxAmount:                     "2.90",
			Subtotal:                      &subtotal,
			Total:                         &total,
			ProductName:                   "Plan",
			OrderMetadata: map[string]string{
				"new_api_subscription_product_id":    "prod_plan",
				"new_api_subscription_snapshot_hash": "snapshot-hash",
			},
			ProductMetadata: map[string]string{"catalog": "stable"},
			PaymentID:       &paymentID,
			PaymentStatus:   &paymentStatus,
			PaymentMethod:   &paymentMethod,
			PaymentDate:     &paymentDate,
		},
	}

	actual := convertWaffoPancakeWebhookEvent(event)

	require.NotNil(t, actual)
	assert.Equal(t, event.ID, actual.ID)
	assert.Equal(t, "ORD_123", actual.Data.OrderID)
	assert.Equal(t, externalID, actual.Data.OrderMerchantExternalID)
	assert.Equal(t, identity, actual.Data.MerchantProvidedBuyerIdentity)
	assert.Equal(t, subtotal, actual.Data.Subtotal)
	assert.Equal(t, total, actual.Data.Total)
	assert.Equal(t, paymentID, actual.Data.PaymentID)
	assert.Equal(t, paymentStatus, actual.Data.PaymentStatus)
	assert.Equal(t, paymentMethod, actual.Data.PaymentMethod)
	assert.Equal(t, paymentDate, actual.Data.PaymentDate)
	assert.Equal(t, "prod_plan", actual.Data.OrderMetadata["new_api_subscription_product_id"])
	assert.Equal(t, "stable", actual.Data.ProductMetadata["catalog"])

	event.Data.OrderMetadata["new_api_subscription_product_id"] = "mutated"
	event.Data.ProductMetadata["catalog"] = "mutated"
	assert.Equal(t, "prod_plan", actual.Data.OrderMetadata["new_api_subscription_product_id"])
	assert.Equal(t, "stable", actual.Data.ProductMetadata["catalog"])
}
