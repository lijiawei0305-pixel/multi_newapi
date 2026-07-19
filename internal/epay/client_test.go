package epay

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPurchaseBuildsSignedEpayRequest(t *testing.T) {
	client, err := NewClient(Config{PartnerID: "merchant-7", Key: "secret-key"}, "https://pay.example.com/gateway/")
	require.NoError(t, err)
	notifyURL, err := url.Parse("https://app.example.com/api/epay/notify")
	require.NoError(t, err)
	returnURL, err := url.Parse("/console/topup")
	require.NoError(t, err)

	endpoint, params, err := client.Purchase(&PurchaseArgs{
		Type:           "alipay",
		ServiceTradeNo: "ORDER-123",
		Name:           "credits",
		Money:          "12.34",
		Device:         PC,
		NotifyURL:      notifyURL,
		ReturnURL:      returnURL,
	})

	require.NoError(t, err)
	require.Equal(t, "https://pay.example.com/gateway/submit.php", endpoint)
	require.Equal(t, "merchant-7", params["pid"])
	require.Equal(t, "MD5", params["sign_type"])
	require.Regexp(t, `^[0-9a-f]{32}$`, params["sign"])
}

func TestVerifyAcceptsAuthenticCallbackAndRejectsTampering(t *testing.T) {
	client, err := NewClient(Config{PartnerID: "merchant-7", Key: "secret-key"}, "https://pay.example.com")
	require.NoError(t, err)
	callback := signedParams(map[string]string{
		"pid":          "merchant-7",
		"type":         "alipay",
		"trade_no":     "UPSTREAM-9",
		"out_trade_no": "ORDER-123",
		"name":         "credits",
		"money":        "12.34",
		"trade_status": StatusTradeSuccess,
	}, "secret-key")

	verified, err := client.Verify(callback)
	require.NoError(t, err)
	require.True(t, verified.VerifyStatus)
	require.Equal(t, "ORDER-123", verified.ServiceTradeNo)
	require.Equal(t, StatusTradeSuccess, verified.TradeStatus)

	callback["money"] = "1234.00"
	tampered, err := client.Verify(callback)
	require.NoError(t, err)
	require.False(t, tampered.VerifyStatus)
}

func TestVerifyRejectsWrongMerchantAndSignatureType(t *testing.T) {
	client, err := NewClient(Config{PartnerID: "merchant-7", Key: "secret-key"}, "https://pay.example.com")
	require.NoError(t, err)

	_, err = client.Verify(map[string]string{"pid": "other", "sign": "00000000000000000000000000000000"})
	require.Error(t, err)
	_, err = client.Verify(map[string]string{
		"pid":       "merchant-7",
		"sign_type": "SHA256",
		"sign":      "00000000000000000000000000000000",
	})
	require.Error(t, err)
}

func TestNewClientRejectsUnsafeOrIncompleteBaseURL(t *testing.T) {
	_, err := NewClient(Config{PartnerID: "merchant", Key: "key"}, "javascript:alert(1)")
	require.Error(t, err)
	_, err = NewClient(Config{PartnerID: "merchant", Key: "key"}, "https://user:pass@pay.example.com")
	require.Error(t, err)
	_, err = NewClient(Config{}, "https://pay.example.com")
	require.Error(t, err)
}
