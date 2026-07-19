package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyNativePaymentOptionsPublishesCompleteSnapshot(t *testing.T) {
	original := GetNativePaymentConfig()
	t.Cleanup(func() { SetNativePaymentConfig(original) })

	SetNativePaymentConfig(NativePaymentConfig{
		WechatPayAppID: "old-wx-app",
		WechatPayMchID: "old-wx-merchant",
		AlipaySellerID: "old-alipay-seller",
	})

	handled, err := ApplyNativePaymentOptions(map[string]string{
		"WechatPayEnabled":     "true",
		"WechatPayAppID":       "new-wx-app",
		"WechatPayMchID":       "new-wx-merchant",
		"AlipayEnabled":        "true",
		"AlipayAppID":          "new-alipay-app",
		"AlipayPrivateKey":     "new-alipay-private-key",
		"AlipayPublicKey":      "new-alipay-public-key",
		"AlipaySellerID":       "new-alipay-seller",
		"AlipayReturnURL":      "https://example.test/return",
		"AlipaySandbox":        "true",
		"unrelated.option.key": "ignored",
	})
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, NativePaymentConfig{
		WechatPayEnabled: true,
		WechatPayAppID:   "new-wx-app",
		WechatPayMchID:   "new-wx-merchant",
		AlipayEnabled:    true,
		AlipayAppID:      "new-alipay-app",
		AlipayPrivateKey: "new-alipay-private-key",
		AlipayPublicKey:  "new-alipay-public-key",
		AlipaySellerID:   "new-alipay-seller",
		AlipayReturnURL:  "https://example.test/return",
		AlipaySandbox:    true,
	}, GetNativePaymentConfig())
}

func TestApplyNativePaymentOptionsRejectsInvalidBooleanWithoutPublication(t *testing.T) {
	original := GetNativePaymentConfig()
	t.Cleanup(func() { SetNativePaymentConfig(original) })

	want := NativePaymentConfig{WechatPayEnabled: true, WechatPayAppID: "stable"}
	SetNativePaymentConfig(want)
	handled, err := ApplyNativePaymentOptions(map[string]string{
		"WechatPayEnabled": "not-a-boolean",
		"WechatPayAppID":   "must-not-publish",
	})
	require.Error(t, err)
	assert.False(t, handled)
	assert.Equal(t, want, GetNativePaymentConfig())
}
