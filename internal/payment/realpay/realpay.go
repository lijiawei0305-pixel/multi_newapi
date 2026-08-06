// Package realpay 是「真实支付平台」适配层：把微信支付 V3（Native 扫码）与支付宝（电脑网站支付
// alipay.trade.page.pay）封装成统一的下单 / 验签解析 / 主动查单三件套。
//
// 历史：本包原属 auth-service（mock:false 时替换 internal/payment.StubPaySDK）。Phase 2 支付重构后
// 上移到主站进程内（internal/payment/realpay），由 internal/mtwire 的 providerManager 直接从 DB 凭据
// （setting.* 包级变量）构造、缓存并调用；auth-service 退役但保留可编译（dormant）。
//
// 设计：
//   - 不依赖 authservice / mtwire 包（避免 import 环）；凭据经 Config 注入。
//   - 真实回调验签需要 HTTP 头（微信）/ 表单（支付宝），故 VerifyNotify 接收 *http.Request，
//     不走 payment.PaySDK.Verify(raw []byte)（该签名只够 mock 用）。
//   - 金额口径：对外统一「元」（payment.CallbackInfo.PaidAmount）；微信内部分↔元换算在适配器内完成。
//
// ⚠️ 构建校验（W4）：本包依赖 github.com/wechatpay-apiv3/wechatpay-go 与
// github.com/smartwalle/alipay/v3，本机无 Go 工具链，未经编译。少数随版本演进的方法签名
// （见各文件 NOTE(W4)）须在服务器 `go mod tidy && go build ./...` 时核对、必要时微调。
package realpay

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// 支付 HTTP client 见 httpclient.go（独立 Transport、强制 TLS 校验、IPv4、httptrace）。
// 旧名 ipv4OnlyHTTPClient 保留为 paymentHTTPClientShared 别名。

// WxpayConfig 微信支付商户凭据（微信支付公钥模式：商户私钥 + 证书序列号 + APIv3 密钥 + 微信支付公钥）。
//
// 微信 2024 起对新商户强制启用「微信支付公钥模式」，不再签发平台证书（GET /v3/certificates 返回
// 404 RESOURCE_NOT_EXISTS）；验签改用商户在「商户平台-账户中心-API安全」申请的微信支付公钥
// （固定值，配合 PublicKeyID 使用），不再需要证书自动下载轮换。
//
// 私钥两种注入方式（二选一，PrivateKey 优先）：
//   - PrivateKey：商户私钥 PEM **内容**（主站进程内模式：从 DB option 读入后直接注入）；
//   - PrivateKeyPath：商户私钥 apiclient_key.pem **文件路径**（auth-service dormant 兼容）。
type WxpayConfig struct {
	AppID          string // 公众号/开放平台 appid
	MchID          string // 商户号
	APIv3Key       string // APIv3 密钥（AES-256-GCM 回调解密）
	CertSerialNo   string // 商户 API 证书序列号（用于对外请求签名，须与 PrivateKey 为同一证书）
	PrivateKey     string // 商户私钥（PEM 内容；进程内模式由 setting.NativePaymentConfig 注入，优先于 PrivateKeyPath）
	PrivateKeyPath string // 商户私钥 apiclient_key.pem 路径（PrivateKey 为空时回退；auth-service 兼容）
	PublicKeyID    string // 微信支付公钥 ID（商户平台-API安全 申请，形如 PUB_KEY_ID_...）
	PublicKey      string // 微信支付公钥 PEM 内容（验证微信应答/回调签名）
}

func (c WxpayConfig) complete() bool {
	return c.AppID != "" && c.MchID != "" && c.APIv3Key != "" && c.CertSerialNo != "" &&
		(c.PrivateKey != "" || c.PrivateKeyPath != "") &&
		c.PublicKeyID != "" && c.PublicKey != ""
}

// AlipayConfig 支付宝应用凭据（普通公钥模式：应用私钥 + 支付宝公钥）。
type AlipayConfig struct {
	AppID           string // 应用 appid
	PrivateKey      string // 应用私钥（PEM 内容；由调用方从文件/DB 读入后注入）
	AlipayPublicKey string // 支付宝公钥（PEM 内容）
	SellerID        string // 可选：收款账号 UID（pid，2088 开头）；非空则校验回调 seller_id
	ReturnURL       string // 同步跳转地址（仅展示用，不入账）
	IsProduction    bool   // true=正式网关，false=沙箱
}

func (c AlipayConfig) complete() bool {
	return c.AppID != "" && c.PrivateKey != "" && c.AlipayPublicKey != ""
}

// Config 是 realpay 的总配置（由调用方从 DB 凭据 / authservice.Config 映射而来）。
type Config struct {
	Wxpay  WxpayConfig
	Alipay AlipayConfig
}

// SDK 是真实支付适配器，按 provider 分发到微信/支付宝子适配器。
type SDK struct {
	wx  *wxpayAdapter
	ali *alipayAdapter
}

// New 装配真实适配器。两个渠道独立装配：某渠道凭据不全则该渠道不可用（调用时报错），
// 另一渠道仍可用（允许只接其中之一）。两者都不可用则返回错误（避免静默空跑）。
// 凭据轮换重建时关闭旧 idle 连接。
func New(ctx context.Context, cfg Config) (*SDK, error) {
	ClosePaymentIdleConnections()
	s := &SDK{}
	var err error
	if cfg.Wxpay.complete() {
		if s.wx, err = newWxpayAdapter(ctx, cfg.Wxpay); err != nil {
			return nil, err
		}
	}
	if cfg.Alipay.complete() {
		if s.ali, err = newAlipayAdapter(cfg.Alipay); err != nil {
			return nil, err
		}
	}
	if s.wx == nil && s.ali == nil {
		return nil, fmt.Errorf("realpay: no provider configured (need wxpay and/or alipay credentials)")
	}
	return s, nil
}

// Available 报告某渠道是否已装配可用（供调用方在下单前校验、给出清晰错误）。
func (s *SDK) Available(provider payment.Provider) bool {
	switch provider {
	case payment.ProviderWxpay:
		return s.wx != nil
	case payment.ProviderAlipay:
		return s.ali != nil
	default:
		return false
	}
}

// CreatePay 向支付平台下单，返回支付凭据（微信：code_url 二维码内容；支付宝：跳转 URL）。
// amountCNY 为用户应付人民币（元）。expiresAt 写入平台 TimeExpire/TimeoutExpress。
func (s *SDK) CreatePay(ctx context.Context, provider payment.Provider, orderNo, subject string, amountCNY float64, notifyURL string, expiresAt ...time.Time) (string, error) {
	var exp time.Time
	if len(expiresAt) > 0 {
		exp = expiresAt[0]
	}
	switch provider {
	case payment.ProviderWxpay:
		if s.wx == nil {
			return "", fmt.Errorf("realpay: wxpay not configured")
		}
		return s.wx.createPay(ctx, orderNo, subject, amountCNY, notifyURL, exp)
	case payment.ProviderAlipay:
		if s.ali == nil {
			return "", fmt.Errorf("realpay: alipay not configured")
		}
		return s.ali.createPay(ctx, orderNo, subject, amountCNY, notifyURL, exp)
	default:
		return "", fmt.Errorf("realpay: unknown provider %q", provider)
	}
}

// CloseOrder 关闭未支付订单（微信 Native close；支付宝 trade.close）。
func (s *SDK) CloseOrder(ctx context.Context, provider payment.Provider, orderNo string) error {
	switch provider {
	case payment.ProviderWxpay:
		if s.wx == nil {
			return fmt.Errorf("realpay: wxpay not configured")
		}
		return s.wx.CloseOrder(ctx, orderNo)
	case payment.ProviderAlipay:
		if s.ali == nil {
			return fmt.Errorf("realpay: alipay not configured")
		}
		return s.ali.CloseOrder(ctx, orderNo)
	default:
		return fmt.Errorf("realpay: unknown provider %q", provider)
	}
}

// VerifyNotify 验签并解析支付平台异步回调（微信需 HTTP 头，支付宝需表单），返回结构化结果。
// 验签失败 → payment.ErrSignInvalid；报文非法/商户不匹配 → payment.ErrCallbackInvalid。
func (s *SDK) VerifyNotify(ctx context.Context, provider payment.Provider, r *http.Request) (*payment.CallbackInfo, error) {
	switch provider {
	case payment.ProviderWxpay:
		if s.wx == nil {
			return nil, payment.ErrCallbackInvalid
		}
		return s.wx.verifyNotify(ctx, r)
	case payment.ProviderAlipay:
		if s.ali == nil {
			return nil, payment.ErrCallbackInvalid
		}
		return s.ali.verifyNotify(ctx, r)
	default:
		return nil, payment.ErrCallbackInvalid
	}
}

// QueryOrder 主动向支付平台查单（对账兜底）：返回结构化 QueryResult（含真实交易号与金额分）。
func (s *SDK) QueryOrder(ctx context.Context, provider payment.Provider, orderNo string) (*payment.QueryResult, error) {
	switch provider {
	case payment.ProviderWxpay:
		if s.wx == nil {
			return nil, fmt.Errorf("realpay: wxpay not configured")
		}
		return s.wx.queryOrder(ctx, orderNo)
	case payment.ProviderAlipay:
		if s.ali == nil {
			return nil, fmt.Errorf("realpay: alipay not configured")
		}
		return s.ali.queryOrder(ctx, orderNo)
	default:
		return nil, fmt.Errorf("realpay: unknown provider %q", provider)
	}
}
