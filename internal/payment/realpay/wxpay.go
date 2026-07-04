package realpay

import (
	"context"
	"crypto/rsa"
	"fmt"
	"math"
	"net/http"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"

	"github.com/QuantumNous/new-api/internal/payment"
)

// wxpayAdapter 封装微信支付 V3：Native 下单 / 回调验签+AES-256-GCM 解密 / 主动查单。
// 采用「微信支付公钥」模式：WithWechatPayPublicKeyAuthCipher 用商户私钥签名出站请求，
// 用微信支付固定公钥（PublicKeyID + PublicKey）验证应答/回调签名——微信自 2024 起对新商户
// 强制此模式，不再签发平台证书（GET /v3/certificates 会返回 404 RESOURCE_NOT_EXISTS）。
type wxpayAdapter struct {
	appID   string
	mchID   string
	svc     native.NativeApiService
	handler *notify.Handler
}

// loadWxPrivateKey 读取微信商户私钥：PrivateKey（PEM 内容）优先，否则回退 PrivateKeyPath（文件路径）。
// 进程内模式由 setting.WechatPayPrivateKey 注入内容；auth-service dormant 仍可用文件路径。
func loadWxPrivateKey(cfg WxpayConfig) (*rsa.PrivateKey, error) {
	if cfg.PrivateKey != "" {
		priv, err := utils.LoadPrivateKey(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("wxpay: load private key (content): %w", err)
		}
		return priv, nil
	}
	priv, err := utils.LoadPrivateKeyWithPath(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("wxpay: load private key %q: %w", cfg.PrivateKeyPath, err)
	}
	return priv, nil
}

func newWxpayAdapter(ctx context.Context, cfg WxpayConfig) (*wxpayAdapter, error) {
	priv, err := loadWxPrivateKey(cfg)
	if err != nil {
		return nil, err
	}
	pubKey, err := utils.LoadPublicKey(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("wxpay: load wechat pay public key: %w", err)
	}
	client, err := core.NewClient(ctx, option.WithWechatPayPublicKeyAuthCipher(
		cfg.MchID, cfg.CertSerialNo, priv, cfg.PublicKeyID, pubKey,
	), option.WithHTTPClient(ipv4OnlyHTTPClient()))
	if err != nil {
		return nil, fmt.Errorf("wxpay: new client: %w", err)
	}
	handler := notify.NewNotifyHandler(cfg.APIv3Key, verifiers.NewSHA256WithRSAPubkeyVerifier(cfg.PublicKeyID, *pubKey))
	return &wxpayAdapter{
		appID:   cfg.AppID,
		mchID:   cfg.MchID,
		svc:     native.NativeApiService{Client: client},
		handler: handler,
	}, nil
}

// yuanToFen 把人民币元换算为分（四舍五入）。微信金额单位为分（整数）。
func yuanToFen(amountCNY float64) int64 {
	return int64(math.Round(amountCNY * 100))
}

// createPay 调 Native 下单，返回 code_url（前端渲染为二维码）。金额：元 → 分（四舍五入）。
func (a *wxpayAdapter) createPay(ctx context.Context, orderNo, subject string, amountCNY float64, notifyURL string) (string, error) {
	totalFen := yuanToFen(amountCNY)
	if totalFen <= 0 {
		return "", fmt.Errorf("wxpay: invalid amount %.2f", amountCNY)
	}
	resp, _, err := a.svc.Prepay(ctx, native.PrepayRequest{
		Appid:       core.String(a.appID),
		Mchid:       core.String(a.mchID),
		Description: core.String(subject),
		OutTradeNo:  core.String(orderNo),
		NotifyUrl:   core.String(notifyURL),
		Amount: &native.Amount{
			Total:    core.Int64(totalFen),
			Currency: core.String("CNY"),
		},
	})
	if err != nil {
		return "", fmt.Errorf("wxpay prepay: %w", err)
	}
	if resp == nil || resp.CodeUrl == nil || *resp.CodeUrl == "" {
		return "", fmt.Errorf("wxpay prepay: empty code_url")
	}
	return *resp.CodeUrl, nil
}

// verifyNotify 验签 + AES-256-GCM 解密回调，解析为 Transaction，再经纯函数映射为 CallbackInfo。
func (a *wxpayAdapter) verifyNotify(ctx context.Context, r *http.Request) (*payment.CallbackInfo, error) {
	tx := new(payments.Transaction)
	// ParseNotifyRequest 内部：读 Wechatpay-* 头验签 → AES-256-GCM 解密 resource → 反序列化到 tx。
	if _, err := a.handler.ParseNotifyRequest(ctx, r, tx); err != nil {
		// 诊断（临时）：surface ParseNotifyRequest 的真实报错 + 回调头 Wechatpay-Serial，
		// 定位验签卡在哪一步（时间戳容差 / serial 不对上 PublicKeyID / RSA 签名 / APIv3 解密）。
		// %w 保留 payment.ErrSignInvalid，下游 handlePayNotify 的 errors.Is 判定与 ack 行为不变。
		return nil, fmt.Errorf("%w: parseNotify: %v (Wechatpay-Serial=%q Timestamp=%q Nonce=%q)",
			payment.ErrSignInvalid, err,
			r.Header.Get("Wechatpay-Serial"), r.Header.Get("Wechatpay-Timestamp"), r.Header.Get("Wechatpay-Nonce"))
	}
	return wxTransactionToInfo(tx, a.appID, a.mchID)
}

// wxTransactionToInfo 把验签解密后的 Transaction 映射为 CallbackInfo，并做商户校验（纯函数，可单测）：
//   - 缺关键字段（out_trade_no/trade_state）→ ErrCallbackInvalid；
//   - appid/mchid 与本商户不符 → ErrCallbackInvalid（拒绝伪造/串站）；
//   - trade_state==SUCCESS → Success；金额分→元。
func wxTransactionToInfo(tx *payments.Transaction, appID, mchID string) (*payment.CallbackInfo, error) {
	if tx == nil || tx.OutTradeNo == nil || tx.TradeState == nil {
		return nil, payment.ErrCallbackInvalid
	}
	if tx.Appid != nil && *tx.Appid != appID {
		return nil, payment.ErrCallbackInvalid
	}
	if tx.Mchid != nil && *tx.Mchid != mchID {
		return nil, payment.ErrCallbackInvalid
	}
	info := &payment.CallbackInfo{
		Provider: payment.ProviderWxpay,
		OrderNo:  *tx.OutTradeNo,
		Success:  *tx.TradeState == "SUCCESS",
	}
	if tx.TransactionId != nil {
		info.TxnID = *tx.TransactionId
	}
	if tx.Amount != nil && tx.Amount.Total != nil {
		info.PaidAmount = float64(*tx.Amount.Total) / 100.0 // 分 → 元
	}
	return info, nil
}

// queryOrder 按商户订单号主动查单：trade_state==SUCCESS 视为已收款。
func (a *wxpayAdapter) queryOrder(ctx context.Context, orderNo string) (bool, error) {
	resp, _, err := a.svc.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(orderNo),
		Mchid:      core.String(a.mchID),
	})
	if err != nil {
		return false, fmt.Errorf("wxpay query: %w", err)
	}
	if resp == nil || resp.TradeState == nil {
		return false, nil
	}
	return *resp.TradeState == "SUCCESS", nil
}
