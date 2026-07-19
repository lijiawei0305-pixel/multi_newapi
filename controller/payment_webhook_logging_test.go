package controller

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	stripewebhook "github.com/stripe/stripe-go/v81/webhook"
)

func TestStripeWebhookLogsMetadataWithoutSignatureOrBody(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	setting.StripeApiSecret = "sk_test_logging"
	setting.StripeWebhookSecret = "whsec_logging"
	setting.StripePriceId = "price_logging"
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	var output bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultWriter
	originalErrorWriter := gin.DefaultErrorWriter
	gin.DefaultWriter = &output
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = originalWriter
		gin.DefaultErrorWriter = originalErrorWriter
		common.LogWriterMu.Unlock()
	})

	payload := []byte(`{"type":"checkout.session.completed","buyer_email":"private-buyer@example.test"}`)
	signature := "private-stripe-signature"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", strings.NewReader(string(payload)))
	c.Request.Header.Set("Stripe-Signature", signature)

	StripeWebhook(c)

	digest := sha256.Sum256(payload)
	logged := output.String()
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, logged, fmt.Sprintf("size=%d", len(payload)))
	assert.Contains(t, logged, fmt.Sprintf("sha256=%x", digest))
	assert.Contains(t, logged, "verification=false")
	assert.NotContains(t, logged, signature)
	assert.NotContains(t, logged, string(payload))
	assert.NotContains(t, logged, "private-buyer@example.test")
}

func TestStripeWebhookSuccessfulVerificationLogsMetadataOnly(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	setting.StripeApiSecret = "sk_test_logging"
	setting.StripeWebhookSecret = "whsec_logging_success"
	setting.StripePriceId = "price_logging"
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	var output bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultWriter
	originalErrorWriter := gin.DefaultErrorWriter
	gin.DefaultWriter = &output
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = originalWriter
		gin.DefaultErrorWriter = originalErrorWriter
		common.LogWriterMu.Unlock()
	})

	payload := []byte(`{"id":"evt_logging","object":"event","type":"customer.created","data":{"object":{"id":"cus_private","email":"private-success@example.test"}}}`)
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: payload,
		Secret:  setting.StripeWebhookSecret,
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", bytes.NewReader(payload))
	c.Request.Header.Set("Stripe-Signature", signed.Header)

	StripeWebhook(c)

	logged := output.String()
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, logged, "verification=true")
	assert.Contains(t, logged, "event_type=customer.created")
	assert.Contains(t, logged, common.PayloadMetadata(payload))
	assert.NotContains(t, logged, signed.Header)
	assert.NotContains(t, logged, "private-success@example.test")
	assert.NotContains(t, logged, "cus_private")
	assert.NotContains(t, logged, string(payload))
}

func TestEpayWebhookLogsMetadataWithoutSignatureOrBuyerData(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	operation_setting.PayAddress = "https://pay.example.test"
	operation_setting.EpayId = "epay-merchant"
	operation_setting.EpayKey = "private-epay-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	var output bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultWriter
	originalErrorWriter := gin.DefaultErrorWriter
	gin.DefaultWriter = &output
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = originalWriter
		gin.DefaultErrorWriter = originalErrorWriter
		common.LogWriterMu.Unlock()
	})

	params := map[string]string{
		"money":        "1.00",
		"name":         "private-buyer@example.test",
		"out_trade_no": "internal-order-123",
		"pid":          "epay-merchant",
		"sign":         "private-payment-signature",
		"sign_type":    "MD5",
		"trade_status": "TRADE_SUCCESS",
		"type":         "alipay",
	}
	request := httptest.NewRequest(http.MethodGet, "/api/epay/notify", nil)
	query := request.URL.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	request.URL.RawQuery = query.Encode()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request

	EpayNotify(c)

	logged := output.String()
	assert.Contains(t, logged, common.PayloadMetadata([]byte(common.GetJsonString(params))))
	assert.Contains(t, logged, "verification=false")
	assert.NotContains(t, logged, "private-payment-signature")
	assert.NotContains(t, logged, "private-buyer@example.test")
	assert.NotContains(t, logged, "private-epay-key")
}
