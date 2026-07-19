package setting

import (
	"fmt"
	"strconv"
	"sync/atomic"
)

// NativePaymentConfig is the immutable runtime snapshot for the coupled
// WeChat Pay and Alipay settings. Readers must load one snapshot per operation
// so a bulk credential rotation cannot expose a mixture of old and new fields.
type NativePaymentConfig struct {
	WechatPayEnabled     bool
	WechatPayAppID       string
	WechatPayMchID       string
	WechatPayAPIv3Key    string
	WechatPayCertSerial  string
	WechatPayPrivateKey  string
	WechatPayPublicKeyID string
	WechatPayPublicKey   string

	AlipayEnabled    bool
	AlipayAppID      string
	AlipayPrivateKey string
	AlipayPublicKey  string
	AlipaySellerID   string
	AlipayReturnURL  string
	AlipaySandbox    bool
}

var nativePaymentConfig atomic.Pointer[NativePaymentConfig]

// GetNativePaymentConfig returns a value copy of the current runtime snapshot.
func GetNativePaymentConfig() NativePaymentConfig {
	config := nativePaymentConfig.Load()
	if config == nil {
		return NativePaymentConfig{}
	}
	return *config
}

// SetNativePaymentConfig publishes all coupled payment fields in one atomic
// operation. The stored copy is never mutated after publication.
func SetNativePaymentConfig(config NativePaymentConfig) {
	snapshot := config
	nativePaymentConfig.Store(&snapshot)
}

// IsNativePaymentOption reports whether key belongs to the coupled native
// payment snapshot.
func IsNativePaymentOption(key string) bool {
	switch key {
	case "WechatPayEnabled", "WechatPayAppID", "WechatPayMchID", "WechatPayAPIv3Key",
		"WechatPayCertSerial", "WechatPayPrivateKey", "WechatPayPublicKeyID", "WechatPayPublicKey",
		"AlipayEnabled", "AlipayAppID", "AlipayPrivateKey", "AlipayPublicKey", "AlipaySellerID",
		"AlipayReturnURL", "AlipaySandbox":
		return true
	default:
		return false
	}
}

// ApplyNativePaymentOptions applies every recognized option to a copy of the
// current snapshot, then publishes the complete result with one compare-and-
// swap. Concurrent updates therefore cannot lose unrelated fields.
func ApplyNativePaymentOptions(values map[string]string) (bool, error) {
	handled := false
	for {
		current := nativePaymentConfig.Load()
		var next NativePaymentConfig
		if current != nil {
			next = *current
		}

		for key, value := range values {
			switch key {
			case "WechatPayEnabled":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return false, fmt.Errorf("invalid WechatPayEnabled: %w", err)
				}
				next.WechatPayEnabled = parsed
				handled = true
			case "WechatPayAppID":
				next.WechatPayAppID = value
				handled = true
			case "WechatPayMchID":
				next.WechatPayMchID = value
				handled = true
			case "WechatPayAPIv3Key":
				next.WechatPayAPIv3Key = value
				handled = true
			case "WechatPayCertSerial":
				next.WechatPayCertSerial = value
				handled = true
			case "WechatPayPrivateKey":
				next.WechatPayPrivateKey = value
				handled = true
			case "WechatPayPublicKeyID":
				next.WechatPayPublicKeyID = value
				handled = true
			case "WechatPayPublicKey":
				next.WechatPayPublicKey = value
				handled = true
			case "AlipayEnabled":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return false, fmt.Errorf("invalid AlipayEnabled: %w", err)
				}
				next.AlipayEnabled = parsed
				handled = true
			case "AlipayAppID":
				next.AlipayAppID = value
				handled = true
			case "AlipayPrivateKey":
				next.AlipayPrivateKey = value
				handled = true
			case "AlipayPublicKey":
				next.AlipayPublicKey = value
				handled = true
			case "AlipaySellerID":
				next.AlipaySellerID = value
				handled = true
			case "AlipayReturnURL":
				next.AlipayReturnURL = value
				handled = true
			case "AlipaySandbox":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return false, fmt.Errorf("invalid AlipaySandbox: %w", err)
				}
				next.AlipaySandbox = parsed
				handled = true
			}
		}

		if !handled {
			return false, nil
		}
		snapshot := next
		if nativePaymentConfig.CompareAndSwap(current, &snapshot) {
			return true, nil
		}
	}
}
