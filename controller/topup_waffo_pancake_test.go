package controller

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFormatWaffoPancakeAmount_UsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   float64
		expected string
	}{
		{name: "whole amount", amount: 29, expected: "29.00"},
		{name: "decimal amount", amount: 29.9, expected: "29.90"},
		{name: "round half up to cents", amount: 29.999, expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, formatWaffoPancakeAmount(tc.amount))
		})
	}
}

func TestWaffoPancakePaymentSucceeded(t *testing.T) {
	testCases := []struct {
		status   string
		expected bool
	}{
		{status: "succeeded", expected: true},
		{status: " Succeeded ", expected: true},
		{status: "", expected: true},
		{status: "pending", expected: false},
		{status: "failed", expected: false},
		{status: "canceled", expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.status, func(t *testing.T) {
			require.Equal(t, testCase.expected, waffoPancakePaymentSucceeded(testCase.status))
		})
	}
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalUnitPrice := setting.WaffoPancakeUnitPrice
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	for k, v := range operation_setting.GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		setting.WaffoPancakeUnitPrice = originalUnitPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.WaffoPancakeUnitPrice = 2.5
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{
		10:                           0.8,
		int(common.QuotaPerUnit * 3): 0.5,
		20:                           0,
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{
			name:             "currency display applies unit price group ratio and discount",
			amount:           10,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         24,
		},
		{
			name:             "tokens display converts quota to display units before pricing",
			amount:           int64(common.QuotaPerUnit * 3),
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeTokens,
			expected:         4.5,
		},
		{
			name:             "non-positive discount falls back to no discount",
			amount:           20,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         50,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			actual := getWaffoPancakePayMoney(tc.amount, tc.group)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}

func TestBuildWaffoPancakeSubscriptionPaymentFact(t *testing.T) {
	order := &model.SubscriptionOrder{
		TradeNo:           "WAFFO_PANCAKE_SUB-17-1-token",
		ExpectedProductId: "prod_plan",
		ExpectedAmount:    "29.00",
		SnapshotHash:      strings.Repeat("a", 64),
	}
	event := &service.WaffoPancakeWebhookEvent{
		ID:        "delivery-1",
		Timestamp: "2026-07-19T10:11:12Z",
		EventType: "order.completed",
		Data: service.WaffoPancakeWebhookData{
			OrderID:                 "ORD_123",
			OrderMerchantExternalID: order.TradeNo,
			Currency:                "usd",
			Amount:                  "29.00",
			Subtotal:                "29.0",
			Total:                   "31.90",
			PaymentID:               "PAY_123",
			PaymentStatus:           "Succeeded",
			PaymentMethod:           "card",
			PaymentDate:             "2026-07-19T10:11:12Z",
			OrderMetadata: map[string]string{
				waffoPancakeSubscriptionProductMetadataKey:  order.ExpectedProductId,
				waffoPancakeSubscriptionSnapshotMetadataKey: order.SnapshotHash,
			},
		},
	}
	payloadHash := strings.Repeat("b", 64)

	fact, consistent, err := buildWaffoPancakeSubscriptionPaymentFact(event, order, payloadHash)

	require.NoError(t, err)
	require.True(t, consistent)
	require.Equal(t, order.TradeNo, fact.TradeNo)
	require.Equal(t, model.PaymentProviderWaffoPancake, fact.Provider)
	require.Equal(t, "delivery-1", fact.ProviderEventId)
	require.Equal(t, "PAY_123", fact.ProviderTransactionId)
	require.Equal(t, "ORD_123", fact.ProviderCheckoutId)
	require.Equal(t, "29.00", fact.Amount)
	require.Equal(t, "31.90", fact.PaidAmount)
	require.Equal(t, "USD", fact.Currency)
	require.Equal(t, model.SubscriptionCurrencySourceProviderCallback, fact.CurrencySource)
	require.Equal(t, "prod_plan", fact.ProductId)
	require.Equal(t, model.PaymentMethodWaffoPancake, fact.PaymentMethod)
	require.Equal(t, model.SubscriptionCheckoutModeOneTime, fact.CheckoutMode)
	require.Equal(t, order.SnapshotHash, fact.SnapshotHash)
	require.Equal(t, payloadHash, fact.PayloadHash)
	require.EqualValues(t, 1_784_455_872, fact.PaidAt)
}

func TestBuildWaffoPancakeSubscriptionPaymentFactFlagsMismatchesForReconciliation(t *testing.T) {
	newFixture := func() (*service.WaffoPancakeWebhookEvent, *model.SubscriptionOrder) {
		order := &model.SubscriptionOrder{
			TradeNo:           "WAFFO_PANCAKE_SUB-17-1-token",
			ExpectedProductId: "prod_plan",
			ExpectedAmount:    "29.00",
			SnapshotHash:      strings.Repeat("a", 64),
		}
		return &service.WaffoPancakeWebhookEvent{
			ID:        "delivery-1",
			Timestamp: "2026-07-19T10:11:12Z",
			Data: service.WaffoPancakeWebhookData{
				OrderID:                 "ORD_123",
				OrderMerchantExternalID: order.TradeNo,
				Currency:                "USD",
				Amount:                  "29.00",
				Subtotal:                "29.00",
				Total:                   "29.00",
				PaymentStatus:           "succeeded",
				OrderMetadata: map[string]string{
					waffoPancakeSubscriptionProductMetadataKey:  order.ExpectedProductId,
					waffoPancakeSubscriptionSnapshotMetadataKey: order.SnapshotHash,
				},
			},
		}, order
	}

	tests := []struct {
		name   string
		mutate func(*service.WaffoPancakeWebhookEvent, *model.SubscriptionOrder)
	}{
		{name: "trade number mismatch", mutate: func(event *service.WaffoPancakeWebhookEvent, _ *model.SubscriptionOrder) {
			event.Data.OrderMerchantExternalID = "WAFFO_PANCAKE_SUB-other"
		}},
		{name: "product mismatch", mutate: func(event *service.WaffoPancakeWebhookEvent, _ *model.SubscriptionOrder) {
			event.Data.OrderMetadata[waffoPancakeSubscriptionProductMetadataKey] = "prod_other"
		}},
		{name: "snapshot mismatch", mutate: func(event *service.WaffoPancakeWebhookEvent, _ *model.SubscriptionOrder) {
			event.Data.OrderMetadata[waffoPancakeSubscriptionSnapshotMetadataKey] = strings.Repeat("c", 64)
		}},
		{name: "currency mismatch", mutate: func(event *service.WaffoPancakeWebhookEvent, _ *model.SubscriptionOrder) {
			event.Data.Currency = "EUR"
		}},
		{name: "amount mismatch", mutate: func(event *service.WaffoPancakeWebhookEvent, _ *model.SubscriptionOrder) {
			event.Data.Subtotal = "28.99"
		}},
		{name: "invalid provider amount", mutate: func(event *service.WaffoPancakeWebhookEvent, _ *model.SubscriptionOrder) {
			event.Data.Amount = "not-an-amount"
		}},
		{name: "total below subtotal", mutate: func(event *service.WaffoPancakeWebhookEvent, _ *model.SubscriptionOrder) {
			event.Data.Total = "28.99"
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, order := newFixture()
			test.mutate(event, order)
			fact, consistent, err := buildWaffoPancakeSubscriptionPaymentFact(event, order, strings.Repeat("b", 64))
			require.NoError(t, err)
			require.False(t, consistent)
			require.NotEmpty(t, fact.ProviderTransactionId, "paid mismatches need a stable fact for reconciliation")
		})
	}
}

func TestBuildWaffoPancakeSubscriptionPaymentFactFallsBackToOrderIdentityAndAmount(t *testing.T) {
	order := &model.SubscriptionOrder{
		TradeNo:           "WAFFO_PANCAKE_SUB-17-1-token",
		ExpectedProductId: "prod_plan",
		ExpectedAmount:    "29.00",
		SnapshotHash:      strings.Repeat("a", 64),
	}
	event := &service.WaffoPancakeWebhookEvent{
		ID:        "delivery-1",
		Timestamp: "2026-07-19",
		Data: service.WaffoPancakeWebhookData{
			OrderID:                 "ORD_123",
			OrderMerchantExternalID: order.TradeNo,
			Currency:                "USD",
			Amount:                  "29.00",
			OrderMetadata: map[string]string{
				waffoPancakeSubscriptionProductMetadataKey:  order.ExpectedProductId,
				waffoPancakeSubscriptionSnapshotMetadataKey: order.SnapshotHash,
			},
		},
	}

	fact, consistent, err := buildWaffoPancakeSubscriptionPaymentFact(event, order, strings.Repeat("b", 64))

	require.NoError(t, err)
	require.True(t, consistent)
	require.Equal(t, "ORD_123", fact.ProviderTransactionId)
	require.Equal(t, "29.00", fact.Amount)
	require.Equal(t, "29.00", fact.PaidAmount)
	require.Equal(t, model.PaymentMethodWaffoPancake, fact.PaymentMethod)
}

func TestWaffoPancakePaidMismatchPersistsReconciliation(t *testing.T) {
	originalDB := model.DB
	originalMainDatabaseType := common.MainDatabaseType()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "waffo-reconciliation.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.SubscriptionPaymentReceipt{},
		&model.SubscriptionPaymentEvidence{},
	))
	model.DB = database
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		common.SetMainDatabaseType(originalMainDatabaseType)
	})

	user := &model.User{Id: 17, Username: "waffo-reconciliation", AffCode: "waffo-reconciliation", Status: common.UserStatusEnabled}
	require.NoError(t, database.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:                    71,
		Title:                 "Waffo snapshot plan",
		PriceAmount:           29,
		Currency:              "USD",
		DurationUnit:          model.SubscriptionDurationMonth,
		DurationValue:         1,
		Enabled:               true,
		WaffoPancakeProductId: "prod_plan",
		QuotaResetPeriod:      model.SubscriptionResetNever,
		AllowWalletOverflow:   common.GetPointer(true),
	}
	require.NoError(t, database.Create(plan).Error)
	order, err := model.CreatePendingSubscriptionOrder(
		user.Id,
		plan.Id,
		"WAFFO_PANCAKE_SUB-17-1-token",
		model.PaymentMethodWaffoPancake,
		model.PaymentProviderWaffoPancake,
		model.SubscriptionCheckoutPolicy{
			Currency:         "USD",
			CurrencySource:   model.SubscriptionCurrencySourceProviderCallback,
			AmountMultiplier: "1",
			CheckoutMode:     model.SubscriptionCheckoutModeOneTime,
		},
	)
	require.NoError(t, err)

	event := &service.WaffoPancakeWebhookEvent{
		ID:        "delivery-reconciliation",
		Timestamp: "2026-07-19T10:11:12Z",
		Data: service.WaffoPancakeWebhookData{
			OrderID:                 "ORD_reconciliation",
			OrderMerchantExternalID: order.TradeNo,
			Currency:                "USD",
			Amount:                  order.ExpectedAmount,
			Subtotal:                order.ExpectedAmount,
			Total:                   order.ExpectedAmount,
			PaymentID:               "PAY_reconciliation",
			PaymentStatus:           "succeeded",
			OrderMetadata: map[string]string{
				waffoPancakeSubscriptionProductMetadataKey:  "prod_wrong",
				waffoPancakeSubscriptionSnapshotMetadataKey: order.SnapshotHash,
			},
		},
	}
	payloadHash := strings.Repeat("b", 64)
	fact, consistent, err := buildWaffoPancakeSubscriptionPaymentFact(event, order, payloadHash)
	require.NoError(t, err)
	require.False(t, consistent)

	err = model.CompleteSubscriptionOrder(fact)
	require.ErrorIs(t, err, model.ErrSubscriptionOrderReconciliationRequired)

	var persisted model.SubscriptionOrder
	require.NoError(t, database.Where("trade_no = ?", order.TradeNo).First(&persisted).Error)
	require.Equal(t, model.SubscriptionOrderStatusReconciliationRequired, persisted.Status)
	require.Contains(t, persisted.PaymentFact, payloadHash)
	require.NotEmpty(t, persisted.ReconciliationReason)
	require.Empty(t, persisted.ProviderPayload)

	var receipt model.SubscriptionPaymentReceipt
	require.NoError(t, database.Where("order_id = ?", persisted.Id).First(&receipt).Error)
	require.Equal(t, "PAY_reconciliation", receipt.ProviderTransactionId)
	require.Equal(t, payloadHash, receipt.PayloadHash)
}
