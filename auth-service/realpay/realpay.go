// Package realpay 是 auth-service 的「真实支付平台」适配层：把微信支付 V3（Native 扫码）与
// 支付宝（电脑网站支付 alipay.trade.page.pay）封装成统一的下单 / 验签解析 / 主动查单三件套，
// 供 auth-service 在 mock:false 时替换 internal/payment.StubPaySDK。
//
// 设计：
//   - 不依赖 authservice 包（避免 import 环：authservice → realpay）；凭据经 Config 注入。
//   - 真实回调验签需要 HTTP 头（微信）/ 表单（支付宝），故 VerifyNotify 接收 *http.Request，
//     不走 payment.PaySDK.Verify(raw []byte)（该签名只够 mock 用）。
//   - 金额口径：对外统一「元」（payment.CallbackInfo.PaidAmount）；微信内部分↔元换算在适配器内完成。
//
// ⚠️ 构建校验（W4）：本包依赖 github.com/wechatpay-apiv3/wechatpay-go 与
// github.com/smartwalle/alipay/v3，本机无 Go 工具链，未经编译。少数随版本演进的方法签名
// （见各文件 NOTE(W4)）须在服务器 `go mod tidy && go build ./auth-service/...` 时核对、必要时微调。
package realpay

import (
	"context"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/internal/payment"
)

// WxpayConfig 微信支付商户凭据（平台证书自动下载模式：仅需商户私钥 + 证书序列号 + APIv3 密钥）。
type WxpayConfig struct {
	AppID          string // 公众号/开放平台 appid
	MchID          string // 商户号
	APIv3Key       string // APIv3 密钥（AES-256-GCM 回调解密 + 平台证书解密）
	CertSerialNo   string // 商户证书序列号
	PrivateKeyPath string // 商户私钥 apiclient_key.pem 路径
}

func (c WxpayConfig) complete() bool {
	return c.AppID != "" && c.MchID != "" && c.APIv3Key != "" && c.CertSerialNo != "" && c.PrivateKeyPath != ""
}

// AlipayConfig 支付宝应用凭据（普通公钥模式：应用私钥 + 支付宝公钥）。
type AlipayConfig struct {
	AppID           string // 应用 appid
	PrivateKey      string // 应用私钥（PEM 内容；由 server 从文件读入后注入）
	AlipayPublicKey string // 支付宝公钥（PEM 内容）
	SellerID        string // 可选：收款账号 UID（pid，2088 开头）；非空则校验回调 seller_id
	ReturnURL       string // 同步跳转地址（仅展示用，不入账）
	IsProduction    bool   // true=正式网关，false=沙箱
}

func (c AlipayConfig) complete() bool {
	return c.AppID != "" && c.PrivateKey != "" && c.AlipayPublicKey != ""
}

// Config 是 realpay 的总配置（由 server 从 authservice.Config 映射而来）。
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
func New(ctx context.Context, cfg Config) (*SDK, error) {
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

// Available 报告某渠道是否已装配可用（供 server 在下单前校验、给出清晰错误）。
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
// amountCNY 为用户应付人民币（元）。
func (s *SDK) CreatePay(ctx context.Context, provider payment.Provider, orderNo, subject string, amountCNY float64, notifyURL string) (string, error) {
	switch provider {
	case payment.ProviderWxpay:
		if s.wx == nil {
			return "", fmt.Errorf("realpay: wxpay not configured")
		}
		return s.wx.createPay(ctx, orderNo, subject, amountCNY, notifyURL)
	case payment.ProviderAlipay:
		if s.ali == nil {
			return "", fmt.Errorf("realpay: alipay not configured")
		}
		return s.ali.createPay(ctx, orderNo, subject, amountCNY, notifyURL)
	default:
		return "", fmt.Errorf("realpay: unknown provider %q", provider)
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

// QueryOrder 主动向支付平台查单（对账兜底）：返回该订单是否已收款（成功/已结）。
func (s *SDK) QueryOrder(ctx context.Context, provider payment.Provider, orderNo string) (bool, error) {
	switch provider {
	case payment.ProviderWxpay:
		if s.wx == nil {
			return false, fmt.Errorf("realpay: wxpay not configured")
		}
		return s.wx.queryOrder(ctx, orderNo)
	case payment.ProviderAlipay:
		if s.ali == nil {
			return false, fmt.Errorf("realpay: alipay not configured")
		}
		return s.ali.queryOrder(ctx, orderNo)
	default:
		return false, fmt.Errorf("realpay: unknown provider %q", provider)
	}
}
