package mtwire

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/payment/realpay"
	"github.com/QuantumNous/new-api/setting"
)

// ---- 测试夹具：保存/还原 setting.* 支付凭据（包级全局，须隔离） ----

func saveSettings(t *testing.T) {
	t.Helper()
	o := struct {
		wxEn                                          bool
		wxApp, wxMch, wxKey, wxSerial, wxPriv         string
		wxPubID, wxPubKey                             string
		aliEn                                         bool
		aliApp, aliPriv, aliPub, aliSeller, aliReturn string
		aliSandbox                                    bool
	}{
		setting.WechatPayEnabled, setting.WechatPayAppID, setting.WechatPayMchID, setting.WechatPayAPIv3Key, setting.WechatPayCertSerial, setting.WechatPayPrivateKey,
		setting.WechatPayPublicKeyID, setting.WechatPayPublicKey,
		setting.AlipayEnabled, setting.AlipayAppID, setting.AlipayPrivateKey, setting.AlipayPublicKey, setting.AlipaySellerID, setting.AlipayReturnURL, setting.AlipaySandbox,
	}
	t.Cleanup(func() {
		setting.WechatPayEnabled, setting.WechatPayAppID, setting.WechatPayMchID, setting.WechatPayAPIv3Key, setting.WechatPayCertSerial, setting.WechatPayPrivateKey = o.wxEn, o.wxApp, o.wxMch, o.wxKey, o.wxSerial, o.wxPriv
		setting.WechatPayPublicKeyID, setting.WechatPayPublicKey = o.wxPubID, o.wxPubKey
		setting.AlipayEnabled, setting.AlipayAppID, setting.AlipayPrivateKey, setting.AlipayPublicKey, setting.AlipaySellerID, setting.AlipayReturnURL, setting.AlipaySandbox = o.aliEn, o.aliApp, o.aliPriv, o.aliPub, o.aliSeller, o.aliReturn, o.aliSandbox
	})
}

// setWxFull 设一组齐全的微信凭据（启用 + 7 字段全有：含微信支付公钥模式的 PublicKeyID/PublicKey）。
func setWxFull() {
	setting.WechatPayEnabled = true
	setting.WechatPayAppID = "wxapp"
	setting.WechatPayMchID = "1600000"
	setting.WechatPayAPIv3Key = "apiv3key"
	setting.WechatPayCertSerial = "serial"
	setting.WechatPayPrivateKey = "-----BEGIN PRIVATE KEY-----\nXX\n-----END PRIVATE KEY-----"
	setting.WechatPayPublicKeyID = "PUB_KEY_ID_test"
	setting.WechatPayPublicKey = "-----BEGIN PUBLIC KEY-----\nXX\n-----END PUBLIC KEY-----"
}

// TestProviderManagerConfigured 启用 + 凭据齐全才 configured；缺一字段 / enabled=false / 未知渠道 → false。
func TestProviderManagerConfigured(t *testing.T) {
	saveSettings(t)
	m := newProviderManager()

	// 全关 → 都不可用。
	setting.WechatPayEnabled, setting.AlipayEnabled = false, false
	if m.Configured(payment.ProviderWxpay) || m.Configured(payment.ProviderAlipay) {
		t.Fatal("disabled providers must be not configured")
	}

	// 微信启用但缺私钥 → 不可用。
	setWxFull()
	setting.WechatPayPrivateKey = ""
	if m.Configured(payment.ProviderWxpay) {
		t.Fatal("wxpay missing private key must be not configured")
	}
	// 补齐 → 可用。
	setWxFull()
	if !m.Configured(payment.ProviderWxpay) {
		t.Fatal("wxpay enabled + full credentials must be configured")
	}
	// 微信启用但缺微信支付公钥 ID（公钥模式必需字段）→ 不可用。
	setWxFull()
	setting.WechatPayPublicKeyID = ""
	if m.Configured(payment.ProviderWxpay) {
		t.Fatal("wxpay missing public key id must be not configured")
	}
	// 微信启用但缺微信支付公钥内容 → 不可用。
	setWxFull()
	setting.WechatPayPublicKey = ""
	if m.Configured(payment.ProviderWxpay) {
		t.Fatal("wxpay missing public key must be not configured")
	}
	// 凭据齐全但 enabled=false → 不可用。
	setting.WechatPayEnabled = false
	if m.Configured(payment.ProviderWxpay) {
		t.Fatal("disabled wxpay must be not configured even with full credentials")
	}

	// 支付宝：启用但缺公钥 → 不可用；补齐 → 可用。
	setting.AlipayEnabled = true
	setting.AlipayAppID = "2021app"
	setting.AlipayPrivateKey = "priv"
	setting.AlipayPublicKey = ""
	if m.Configured(payment.ProviderAlipay) {
		t.Fatal("alipay missing public key must be not configured")
	}
	setting.AlipayPublicKey = "pub"
	if !m.Configured(payment.ProviderAlipay) {
		t.Fatal("alipay enabled + full credentials must be configured")
	}

	// 未知渠道 → false。
	if m.Configured(payment.Provider("paypal")) {
		t.Fatal("unknown provider must be not configured")
	}
}

// fakeRealSDK 是 realSDK 的桩（不触真实证书/网络），供指纹缓存测试注入。
type fakeRealSDK struct{}

func (fakeRealSDK) CreatePay(context.Context, payment.Provider, string, string, float64, string) (string, error) {
	return "stub://pay", nil
}
func (fakeRealSDK) VerifyNotify(context.Context, payment.Provider, *http.Request) (*payment.CallbackInfo, error) {
	return nil, nil
}
func (fakeRealSDK) QueryOrder(context.Context, payment.Provider, string) (bool, error) {
	return false, nil
}

// TestProviderManagerSDKCaching 同凭据复用同一 SDK（只构造一次）；凭据变更触发重建（指纹缓存）。
func TestProviderManagerSDKCaching(t *testing.T) {
	saveSettings(t)
	setWxFull() // 稳定凭据

	orig := realpayNew
	t.Cleanup(func() { realpayNew = orig })
	var builds int
	realpayNew = func(context.Context, realpay.Config) (realSDK, error) {
		builds++
		return fakeRealSDK{}, nil
	}

	m := newProviderManager()
	ctx := context.Background()
	if _, err := m.getSDK(ctx); err != nil {
		t.Fatalf("getSDK: %v", err)
	}
	if _, err := m.getSDK(ctx); err != nil {
		t.Fatalf("getSDK 2nd: %v", err)
	}
	if builds != 1 {
		t.Fatalf("same credentials must build SDK exactly once, got %d builds", builds)
	}

	// 改一处凭据 → 指纹变化 → 重建。
	setting.WechatPayAppID = "wxapp-changed"
	if _, err := m.getSDK(ctx); err != nil {
		t.Fatalf("getSDK after change: %v", err)
	}
	if builds != 2 {
		t.Fatalf("changed credentials must rebuild SDK, got %d builds", builds)
	}
}

// ---- notify handler 桩注入 ----

func stubVerifyNotify(t *testing.T, info *payment.CallbackInfo, err error) {
	t.Helper()
	orig := providerVerifyNotify
	t.Cleanup(func() { providerVerifyNotify = orig })
	providerVerifyNotify = func(*App, context.Context, payment.Provider, *http.Request) (*payment.CallbackInfo, error) {
		return info, err
	}
}

func stubNotifyCredit(t *testing.T, fn func(*App, context.Context, string, string, float64) error) {
	t.Helper()
	orig := notifyCreditRecharge
	t.Cleanup(func() { notifyCreditRecharge = orig })
	notifyCreditRecharge = fn
}

func stubNotifyActivate(t *testing.T, fn func(*App, context.Context, string, float64) error) {
	t.Helper()
	orig := notifyActivateSub
	t.Cleanup(func() { notifyActivateSub = orig })
	notifyActivateSub = fn
}

// TestHandlePayNotify_DispatchRecharge 微信成功回调、RCG 前缀 → 走充值入账（不走激活）；200 + SUCCESS ack。
func TestHandlePayNotify_DispatchRecharge(t *testing.T) {
	app := &App{}
	stubVerifyNotify(t, &payment.CallbackInfo{Provider: payment.ProviderWxpay, OrderNo: "RCG-1", Success: true, TxnID: "T1", PaidAmount: 7.3}, nil)
	var credited []string
	stubNotifyCredit(t, func(_ *App, _ context.Context, orderNo, txnID string, paid float64) error {
		credited = append(credited, orderNo)
		if txnID != "T1" || paid != 7.3 {
			t.Fatalf("credit args txn=%q paid=%v want T1/7.3", txnID, paid)
		}
		return nil
	})
	stubNotifyActivate(t, func(_ *App, _ context.Context, orderNo string, _ float64) error {
		t.Fatalf("RCG order must not dispatch to subscription activate: %s", orderNo)
		return nil
	})

	c, rec := newTenantReqCtx(http.MethodPost, "", nil, 0, nil)
	app.HandleWechatNotify(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("wxpay success ack must be 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SUCCESS") {
		t.Fatalf("wxpay ack body=%q want contain SUCCESS", rec.Body.String())
	}
	if len(credited) != 1 || credited[0] != "RCG-1" {
		t.Fatalf("credited=%v want [RCG-1] exactly once", credited)
	}
}

// TestHandlePayNotify_DispatchSubscription 支付宝成功回调、SUB 前缀 → 走套餐激活（不走充值）；200 + "success" ack。
func TestHandlePayNotify_DispatchSubscription(t *testing.T) {
	app := &App{}
	stubVerifyNotify(t, &payment.CallbackInfo{Provider: payment.ProviderAlipay, OrderNo: "SUB-1", Success: true, PaidAmount: 79}, nil)
	var activated []string
	stubNotifyActivate(t, func(_ *App, _ context.Context, orderNo string, paid float64) error {
		activated = append(activated, orderNo)
		if paid != 79 {
			t.Fatalf("activate paid=%v want 79", paid)
		}
		return nil
	})
	stubNotifyCredit(t, func(_ *App, _ context.Context, orderNo, _ string, _ float64) error {
		t.Fatalf("SUB order must not dispatch to recharge credit: %s", orderNo)
		return nil
	})

	c, rec := newTenantReqCtx(http.MethodPost, "", nil, 0, nil)
	app.HandleAlipayNotify(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("alipay success ack must be 200, got %d", rec.Code)
	}
	if rec.Body.String() != "success" {
		t.Fatalf("alipay ack body=%q want \"success\"", rec.Body.String())
	}
	if len(activated) != 1 || activated[0] != "SUB-1" {
		t.Fatalf("activated=%v want [SUB-1] exactly once", activated)
	}
}

// TestHandlePayNotify_NotSuccessNoCredit 交易未成功（Success=false）→ 不入账、不激活，仍成功 ack（停止重推）。
func TestHandlePayNotify_NotSuccessNoCredit(t *testing.T) {
	app := &App{}
	stubVerifyNotify(t, &payment.CallbackInfo{Provider: payment.ProviderWxpay, OrderNo: "RCG-2", Success: false}, nil)
	stubNotifyCredit(t, func(*App, context.Context, string, string, float64) error {
		t.Fatal("Success=false must not credit")
		return nil
	})
	stubNotifyActivate(t, func(*App, context.Context, string, float64) error {
		t.Fatal("Success=false must not activate")
		return nil
	})

	c, rec := newTenantReqCtx(http.MethodPost, "", nil, 0, nil)
	app.HandleWechatNotify(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("Success=false must still 200-ack, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SUCCESS") {
		t.Fatalf("ack body=%q want contain SUCCESS", rec.Body.String())
	}
}

// TestHandlePayNotify_CreditFailAckFail 入账失败 → 非成功 ack（触发平台重推）。
func TestHandlePayNotify_CreditFailAckFail(t *testing.T) {
	app := &App{}
	stubVerifyNotify(t, &payment.CallbackInfo{Provider: payment.ProviderWxpay, OrderNo: "RCG-3", Success: true, PaidAmount: 7.3}, nil)
	stubNotifyCredit(t, func(*App, context.Context, string, string, float64) error {
		return payment.ErrAmountMismatch // 入账被拒（反篡改）
	})

	c, rec := newTenantReqCtx(http.MethodPost, "", nil, 0, nil)
	app.HandleWechatNotify(c)

	if rec.Code == http.StatusOK {
		t.Fatalf("credit failure must NOT 200-ack (must trigger retry), got %d", rec.Code)
	}
}

// TestHandlePayNotify_VerifyFailAckFail 验签失败 → 非成功 ack（支付宝 body 非 "success"）。
func TestHandlePayNotify_VerifyFailAckFail(t *testing.T) {
	app := &App{}
	stubVerifyNotify(t, nil, payment.ErrSignInvalid)

	c, rec := newTenantReqCtx(http.MethodPost, "", nil, 0, nil)
	app.HandleAlipayNotify(c)

	if rec.Code == http.StatusOK {
		t.Fatalf("verify failure must NOT 200-ack, got %d", rec.Code)
	}
	if rec.Body.String() == "success" {
		t.Fatalf("alipay verify-fail body must not be \"success\", got %q", rec.Body.String())
	}
}
