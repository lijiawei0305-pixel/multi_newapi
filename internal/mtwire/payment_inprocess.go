package mtwire

// 进程内支付（Phase 2 支付重构）：把微信/支付宝从独立 auth-service 搬进主站进程内。
//
// 三大部件集中于此，保持 recharge.go / payment_providers.go 干净：
//   1. providerManager —— 从 setting.* 读 DB 凭据构造并**缓存** realpay.SDK（按凭据指纹），
//      暴露 CreatePay / VerifyNotify / QueryOrder / Configured；
//   2. inProcessPaySDK —— payment.PaySDK 适配器（下单委托 providerManager；验签不走 Gateway）；
//   3. notify handler —— 微信/支付宝异步回调直达主站（公开路由、handler 内验签），
//      验签成功按 order_no 前缀分发：SUB→ActivatePaidTokenplanOrder、RCG→RechargeGateway.CreditPaidOrder。
//
// 去 mock：删除 StubPaySDK/auth-service/共享密钥链路。configured = DB 凭据齐全（进程内判断）。
// 金额可信：入账一律以**库内订单金额**为准（CreditPaidOrder / ActivatePaidTokenplanOrder 内比对），
// 回调金额仅作反篡改校验。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/payment/realpay"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// 进程内支付相关错误码。
var (
	// errVerifyUnsupported 进程内 PaySDK 适配器不验签（回调直达 /api/pay/*/notify，由 providerManager 验签）。
	errVerifyUnsupported = apperr.New("PAY_VERIFY_UNSUPPORTED", "主站下单适配器不处理验签（回调直达 /api/pay/*/notify）", http.StatusNotImplemented)
	// errProviderMgrUnset 支付适配器未装配（如单测直构 App）。
	errProviderMgrUnset = apperr.New("PROVIDER_MGR_UNSET", "支付适配器未装配", http.StatusInternalServerError)
)

// ---- realpay SDK 构造 seam（单测注入以验证指纹缓存，避免真实证书/网络） ----

// realSDK 抽象 providerManager 用到的 realpay.SDK 方法（*realpay.SDK 实现之）。
type realSDK interface {
	CreatePay(ctx context.Context, provider payment.Provider, orderNo, subject string, amountCNY float64, notifyURL string) (string, error)
	VerifyNotify(ctx context.Context, provider payment.Provider, r *http.Request) (*payment.CallbackInfo, error)
	QueryOrder(ctx context.Context, provider payment.Provider, orderNo string) (bool, error)
}

// 编译期断言：*realpay.SDK 满足 realSDK。
var _ realSDK = (*realpay.SDK)(nil)

// realpayNew 构造真实支付 SDK（含证书下载等副作用，绝不可每请求重建）；单测替换为计数桩。
var realpayNew = func(ctx context.Context, cfg realpay.Config) (realSDK, error) {
	sdk, err := realpay.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return sdk, nil
}

// ---- providerManager：进程内支付适配（凭据指纹缓存 realpay.SDK） ----

// providerManager 按凭据指纹缓存一份 realpay.SDK：指纹不变复用，凭据变更才重建（wechatpay-go client
// 构造含平台证书下载，重建昂贵）。sync.Mutex 保护缓存。
type providerManager struct {
	mu  sync.Mutex
	sdk realSDK // 缓存的真实 SDK
	fp  string  // 上次构造时的凭据指纹
}

func newProviderManager() *providerManager { return &providerManager{} }

// wxpayCredentialsComplete 报告微信凭据是否齐全（与 realpay.WxpayConfig.complete 同口径，私钥用 PEM 内容）。
func wxpayCredentialsComplete() bool {
	return setting.WechatPayAppID != "" && setting.WechatPayMchID != "" &&
		setting.WechatPayAPIv3Key != "" && setting.WechatPayCertSerial != "" &&
		setting.WechatPayPrivateKey != ""
}

// alipayCredentialsComplete 报告支付宝凭据是否齐全（与 realpay.AlipayConfig.complete 同口径）。
func alipayCredentialsComplete() bool {
	return setting.AlipayAppID != "" && setting.AlipayPrivateKey != "" && setting.AlipayPublicKey != ""
}

// Configured 报告某渠道是否可对外提供支付：启用开关（setting.*Enabled）为真且凭据齐全。
func (m *providerManager) Configured(provider payment.Provider) bool {
	switch provider {
	case payment.ProviderWxpay:
		return setting.WechatPayEnabled && wxpayCredentialsComplete()
	case payment.ProviderAlipay:
		return setting.AlipayEnabled && alipayCredentialsComplete()
	default:
		return false
	}
}

// buildRealpayConfig 从 setting.* 组装 realpay.Config（仅纳入已启用渠道；微信私钥用 PEM 内容注入）。
func buildRealpayConfig() realpay.Config {
	cfg := realpay.Config{}
	if setting.WechatPayEnabled {
		cfg.Wxpay = realpay.WxpayConfig{
			AppID:        setting.WechatPayAppID,
			MchID:        setting.WechatPayMchID,
			APIv3Key:     setting.WechatPayAPIv3Key,
			CertSerialNo: setting.WechatPayCertSerial,
			PrivateKey:   setting.WechatPayPrivateKey, // PEM 内容，realpay 经 utils.LoadPrivateKey 解析
		}
	}
	if setting.AlipayEnabled {
		cfg.Alipay = realpay.AlipayConfig{
			AppID:           setting.AlipayAppID,
			PrivateKey:      setting.AlipayPrivateKey,
			AlipayPublicKey: setting.AlipayPublicKey,
			SellerID:        setting.AlipaySellerID,
			ReturnURL:       setting.AlipayReturnURL,
			IsProduction:    !setting.AlipaySandbox,
		}
	}
	return cfg
}

// credentialFingerprint 拼接全部相关 setting 值的 SHA-256，作缓存键：任一凭据/开关变更即指纹变化 → 重建 SDK。
func credentialFingerprint() string {
	h := sha256.New()
	for _, v := range []string{
		strconv.FormatBool(setting.WechatPayEnabled),
		setting.WechatPayAppID, setting.WechatPayMchID, setting.WechatPayAPIv3Key,
		setting.WechatPayCertSerial, setting.WechatPayPrivateKey,
		strconv.FormatBool(setting.AlipayEnabled),
		setting.AlipayAppID, setting.AlipayPrivateKey, setting.AlipayPublicKey,
		setting.AlipaySellerID, setting.AlipayReturnURL,
		strconv.FormatBool(setting.AlipaySandbox),
	} {
		_, _ = h.Write([]byte(v))
		_, _ = h.Write([]byte{0}) // 分隔符，避免拼接歧义
	}
	return hex.EncodeToString(h.Sum(nil))
}

// getSDK 取（或按需重建）缓存的 realpay.SDK：指纹一致则复用，变化则 realpayNew 重建。
func (m *providerManager) getSDK(ctx context.Context) (realSDK, error) {
	fp := credentialFingerprint()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sdk != nil && m.fp == fp {
		return m.sdk, nil
	}
	sdk, err := realpayNew(ctx, buildRealpayConfig())
	if err != nil {
		return nil, err // 不缓存失败结果：下次调用重试
	}
	m.sdk = sdk
	m.fp = fp
	return sdk, nil
}

// CreatePay 向真实平台下单（先校验渠道 Configured），返回支付凭据内容（微信 code_url / 支付宝跳转 URL）。
func (m *providerManager) CreatePay(ctx context.Context, provider payment.Provider, orderNo, subject string, amountCNY float64, notifyURL string) (string, error) {
	if !m.Configured(provider) {
		return "", errProviderDisabled
	}
	sdk, err := m.getSDK(ctx)
	if err != nil {
		return "", err
	}
	return sdk.CreatePay(ctx, provider, orderNo, subject, amountCNY, notifyURL)
}

// VerifyNotify 验签并解析异步回调（不预检 Configured：SDK 内部按渠道是否装配返回 ErrCallbackInvalid）。
func (m *providerManager) VerifyNotify(ctx context.Context, provider payment.Provider, r *http.Request) (*payment.CallbackInfo, error) {
	sdk, err := m.getSDK(ctx)
	if err != nil {
		return nil, err
	}
	return sdk.VerifyNotify(ctx, provider, r)
}

// QueryOrder 主动查单（对账兜底）：先校验渠道 Configured。
func (m *providerManager) QueryOrder(ctx context.Context, provider payment.Provider, orderNo string) (bool, error) {
	if !m.Configured(provider) {
		return false, errProviderDisabled
	}
	sdk, err := m.getSDK(ctx)
	if err != nil {
		return false, err
	}
	return sdk.QueryOrder(ctx, provider, orderNo)
}

// ---- 回调路径 / 公网基址 ----

// notifyPathFor 返回某渠道异步回调在主站的固定路径（契约；与 auth-service 旧 /pay,/auth 路径无关）。
func notifyPathFor(provider payment.Provider) string {
	switch provider {
	case payment.ProviderWxpay:
		return "/api/pay/wechat/notify"
	case payment.ProviderAlipay:
		return "/api/pay/alipay/notify"
	default:
		return ""
	}
}

// resolveNotifyBase 解析异步回调公网基址：优先 MT_PAY_NOTIFY_BASE，缺省回退 system_setting.ServerAddress。
// 每次解析以吸收运行时 ServerAddress 变更（管理员改公网地址无需重启）。
func resolveNotifyBase() string {
	if v := os.Getenv("MT_PAY_NOTIFY_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(system_setting.ServerAddress, "/")
}

// ---- 进程内 PaySDK 适配器（供 RechargeGateway 下单） ----

// inProcessPaySDK 实现 payment.PaySDK：CreatePay 委托 providerManager 向真实平台下单；
// Verify 不走本适配器（回调直达主站 notify handler 验签），返回 errVerifyUnsupported。
type inProcessPaySDK struct {
	mgr *providerManager
}

// 编译期断言：*inProcessPaySDK 实现 payment.PaySDK。
var _ payment.PaySDK = (*inProcessPaySDK)(nil)

// CreatePay 组装 notify_url（base + 契约回调路径，不用 Gateway 注入的旧 NotifyPath）后向平台下单。
func (s *inProcessPaySDK) CreatePay(ctx context.Context, req payment.PayRequest) (*payment.PayCredential, error) {
	notifyURL := resolveNotifyBase() + notifyPathFor(req.Provider)
	payURL, err := s.mgr.CreatePay(ctx, req.Provider, req.OrderNo, req.Subject, req.ActualPaid, notifyURL)
	if err != nil {
		return nil, err
	}
	return &payment.PayCredential{PayURL: payURL}, nil
}

// Verify 进程内适配器不验签（回调走 /api/pay/*/notify）。
func (s *inProcessPaySDK) Verify(_ context.Context, _ payment.Provider, _ []byte) (*payment.CallbackInfo, error) {
	return nil, errVerifyUnsupported
}

// ---- 异步回调 handler（公开路由，handler 内验签，无 UserAuth/TenantMiddleware） ----

// providerVerifyNotify 验签并解析回调；单测替换为桩（仿既有 seam）。
var providerVerifyNotify = func(a *App, ctx context.Context, provider payment.Provider, r *http.Request) (*payment.CallbackInfo, error) {
	if a.providerMgr == nil {
		return nil, errProviderMgrUnset
	}
	return a.providerMgr.VerifyNotify(ctx, provider, r)
}

// notifyActivateSub 激活一笔已支付的 SUB 套餐订单（幂等）；单测替换为计数桩。
var notifyActivateSub = func(a *App, ctx context.Context, orderNo string, paidCNY float64) error {
	return a.ActivatePaidTokenplanOrder(ctx, orderNo, paidCNY)
}

// notifyCreditRecharge 入账一笔已支付的 RCG 充值订单（强幂等状态机）；单测替换为计数桩。
var notifyCreditRecharge = func(a *App, ctx context.Context, orderNo, txnID string, paidCNY float64) error {
	if a.RechargeGateway == nil {
		return apperr.New("RECHARGE_UNAVAILABLE", "充值服务未装配", http.StatusServiceUnavailable)
	}
	return a.RechargeGateway.CreditPaidOrder(ctx, orderNo, txnID, paidCNY)
}

// HandleWechatNotify POST /api/pay/wechat/notify —— 微信支付异步回调（公开，handler 内验签，无 UserAuth）。
func (a *App) HandleWechatNotify(c *gin.Context) { a.handlePayNotify(c, payment.ProviderWxpay) }

// HandleAlipayNotify POST /api/pay/alipay/notify —— 支付宝异步回调（公开，handler 内验签，无 UserAuth）。
func (a *App) HandleAlipayNotify(c *gin.Context) { a.handlePayNotify(c, payment.ProviderAlipay) }

// handlePayNotify 回调统一处理：验签 → 成功交易按前缀分发入账 → 平台 ack。
//   - 验签失败：ack 失败（微信 4xx、支付宝非 success），触发平台重推；
//   - 交易未成功(info.Success=false)：成功 ack（不入账，停止无谓重推）；
//   - 入账失败（含金额不符被拒）：ack 失败，触发平台重推（主站强幂等吸收重复）；
//   - 入账成功：成功 ack。
func (a *App) handlePayNotify(c *gin.Context, provider payment.Provider) {
	ctx := c.Request.Context()
	info, err := providerVerifyNotify(a, ctx, provider, c.Request)
	if err != nil || info == nil {
		if err != nil {
			common.SysLog("pay notify verify failed (" + string(provider) + "): " + err.Error())
		}
		ackNotifyFail(c, provider) // 验签失败/报文非法 → ack 失败触发重推
		return
	}
	if !info.Success {
		ackNotifyOK(c, provider) // 交易未成功：成功 ack，不入账
		return
	}
	var creditErr error
	if IsSubscriptionOrderNo(info.OrderNo) {
		creditErr = notifyActivateSub(a, ctx, info.OrderNo, info.PaidAmount)
	} else {
		creditErr = notifyCreditRecharge(a, ctx, info.OrderNo, info.TxnID, info.PaidAmount)
	}
	if creditErr != nil {
		common.SysLog("pay notify credit failed (" + info.OrderNo + "): " + creditErr.Error())
		ackNotifyFail(c, provider) // 入账失败 → ack 失败触发平台重推
		return
	}
	ackNotifyOK(c, provider)
}

// ackNotifyOK 返回平台期望的成功应答（停止重推）。
func ackNotifyOK(c *gin.Context, provider payment.Provider) {
	switch provider {
	case payment.ProviderWxpay:
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "OK"})
	default: // 支付宝要求返回纯文本 "success"
		c.String(http.StatusOK, "success")
	}
}

// ackNotifyFail 返回平台「失败」应答（微信 4xx + FAIL；支付宝非 success），触发平台按策略重推。
func ackNotifyFail(c *gin.Context, provider payment.Provider) {
	switch provider {
	case payment.ProviderWxpay:
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "FAILED"})
	default:
		c.String(http.StatusBadRequest, "failure")
	}
}
