package realpay

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

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
type wxpayAdapter struct {
	appID   string
	mchID   string
	svc     native.NativeApiService
	handler *notify.Handler
}

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
	// 独立安全 HTTP client；不 Clone DefaultTransport（避免 TLS_INSECURE_SKIP_VERIFY 污染）。
	client, err := core.NewClient(ctx, option.WithWechatPayPublicKeyAuthCipher(
		cfg.MchID, cfg.CertSerialNo, priv, cfg.PublicKeyID, pubKey,
	), option.WithHTTPClient(paymentHTTPClientShared()))
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

func yuanToFen(amountCNY float64) int64 {
	return int64(math.Round(amountCNY * 100))
}

// createPay 调 Native 下单。
//
// 重要（PAY-LAT-02）：微信官方对重复 out_trade_no 返回 OUT_TRADE_NO_USED，
// **不得**假设「相同 out_trade_no 重试会返回同一 code_url」。
// 仅 pre-write 瞬时失败可短重试；写出后 unknown 须走查单恢复。
func (a *wxpayAdapter) createPay(ctx context.Context, orderNo, subject string, amountCNY float64, notifyURL string, expiresAt time.Time) (string, error) {
	totalFen := yuanToFen(amountCNY)
	if totalFen <= 0 {
		return "", payment.NewOutcomeError(payment.CreateOutcomeDefinitiveReject, "invalid_amount", "validate",
			fmt.Errorf("wxpay: invalid amount %.2f", amountCNY))
	}
	req := native.PrepayRequest{
		Appid:       core.String(a.appID),
		Mchid:       core.String(a.mchID),
		Description: core.String(subject),
		OutTradeNo:  core.String(orderNo),
		NotifyUrl:   core.String(notifyURL),
		Amount: &native.Amount{
			Total:    core.Int64(totalFen),
			Currency: core.String("CNY"),
		},
	}
	// Phase E：必须传 TimeExpire，与 DB expires_at 对齐；禁止依赖微信默认最长 7 天
	if !expiresAt.IsZero() {
		req.TimeExpire = core.Time(expiresAt)
	}

	var codeURL string
	err := retryCreatePay(ctx, realClock(),
		func(tryCtx context.Context, attempt, maxAttempts int) error {
			// P1-B：主备对冲（同 out_trade_no）；单 attempt 内主域 700ms 无果则并发备域
			url, e := runHedged(tryCtx, func(hctx context.Context, preferBackup bool) (string, error) {
				hctx = context.WithValue(hctx, ctxKeyPayAttempt{}, attempt)
				hctx = context.WithValue(hctx, ctxKeyPayMaxAttempts{}, maxAttempts)
				hctx = context.WithValue(hctx, ctxKeyPayOperation{}, "prepay")
				hctx = context.WithValue(hctx, ctxKeyPayProvider{}, "wxpay")
				hctx = context.WithValue(hctx, ctxKeyPayPreferBackup{}, preferBackup)
				r, result, pe := a.svc.Prepay(hctx, req)
				if pe != nil {
					return "", sanitizeAPIError(classifyWxAPIError(pe, result))
				}
				if r == nil || r.CodeUrl == nil || *r.CodeUrl == "" {
					return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "empty_code_url", "response",
						fmt.Errorf("wxpay prepay: empty code_url"))
				}
				return *r.CodeUrl, nil
			})
			if e != nil {
				return e
			}
			codeURL = url
			return nil
		},
		func(attempt, maxAttempts int, err error) {
			_ = attempt
			_ = maxAttempts
			_ = err
		},
	)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(codeURL) == "" {
		return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "empty_code_url", "response",
			fmt.Errorf("wxpay prepay: empty code_url"))
	}
	return codeURL, nil
}

// classifyWxAPIError 将 wechatpay-go 错误转为 OutcomeError（P1-3：不保留 Body/Detail）。
func classifyWxAPIError(err error, result *core.APIResult) error {
	if err == nil {
		return nil
	}
	var apiErr *core.APIError
	if errorsAsAPIError(err, &apiErr) {
		code := apiErr.Code
		status := apiErr.StatusCode
		if result != nil && result.Response != nil && status == 0 {
			status = result.Response.StatusCode
		}
		oe := payment.ClassifyHTTPStatus(status, code, true)
		// 仅保留稳定 code/status，不包装完整 APIError（含 Body/Detail）
		oe.Err = fmt.Errorf("wxpay: status=%d code=%s", status, code)
		oe.HTTPStatus = status
		return oe
	}
	msg := err.Error()
	for _, c := range []string{"OUT_TRADE_NO_USED", "PARAM_ERROR", "NO_AUTH", "SIGN_ERROR"} {
		if strings.Contains(msg, c) {
			oe := payment.ClassifyHTTPStatus(0, c, true)
			oe.Err = fmt.Errorf("wxpay: code=%s", c)
			return oe
		}
	}
	return payment.AsOutcome(fmt.Errorf("wxpay: request failed"))
}

func errorsAsAPIError(err error, target **core.APIError) bool {
	if err == nil || target == nil {
		return false
	}
	return errors.As(err, target)
}

// sanitizeAPIError 确保不向上游泄漏 SDK Detail/Body。
func sanitizeAPIError(err error) error {
	if err == nil {
		return nil
	}
	// 已是 OutcomeError 且消息安全则直接返回
	if oe := payment.AsOutcome(err); oe != nil {
		return oe
	}
	return payment.AsOutcome(fmt.Errorf("wxpay: request failed"))
}

func (a *wxpayAdapter) verifyNotify(ctx context.Context, r *http.Request) (*payment.CallbackInfo, error) {
	tx := new(payments.Transaction)
	if _, err := a.handler.ParseNotifyRequest(ctx, r, tx); err != nil {
		return nil, fmt.Errorf("%w: parseNotify: %v (Wechatpay-Serial=%q Timestamp=%q Nonce=%q)",
			payment.ErrSignInvalid, err,
			r.Header.Get("Wechatpay-Serial"), r.Header.Get("Wechatpay-Timestamp"), r.Header.Get("Wechatpay-Nonce"))
	}
	return wxTransactionToInfo(tx, a.appID, a.mchID)
}

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
		info.PaidAmount = float64(*tx.Amount.Total) / 100.0
	}
	return info, nil
}

func (a *wxpayAdapter) queryOrder(ctx context.Context, orderNo string) (*payment.QueryResult, error) {
	qCtx, cancel := context.WithTimeout(ctx, payQueryPerAttempt)
	defer cancel()
	qCtx = context.WithValue(qCtx, ctxKeyPayOperation{}, "query")
	qCtx = context.WithValue(qCtx, ctxKeyPayProvider{}, "wxpay")
	qCtx = context.WithValue(qCtx, ctxKeyPayAttempt{}, 1)
	qCtx = context.WithValue(qCtx, ctxKeyPayMaxAttempts{}, payQueryMaxAttempts)
	// 查单始终从主域发起，不继承 Prepay 的备域粘滞
	qCtx = context.WithValue(qCtx, ctxKeyPayPreferBackup{}, false)

	resp, result, err := a.svc.QueryOrderByOutTradeNo(qCtx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(orderNo),
		Mchid:      core.String(a.mchID),
	})
	if err != nil {
		if core.IsAPIError(err, "ORDER_NOT_EXIST") {
			return &payment.QueryResult{
				Provider: payment.ProviderWxpay,
				OrderNo:  orderNo,
				NotExist: true,
			}, fmt.Errorf("wxpay query order not exist: %w", payment.ErrOrderNotExist)
		}
		return nil, classifyWxAPIError(err, result)
	}
	qr := &payment.QueryResult{
		Provider:      payment.ProviderWxpay,
		ExpectedMchID: a.mchID,
		ExpectedAppID: a.appID,
	}
	if resp == nil || resp.TradeState == nil {
		qr.NormalizedState = payment.TradeStateUnknown
		return qr, nil
	}
	// 响应字段：不得用请求 orderNo 冒充
	if resp.OutTradeNo != nil {
		qr.OrderNo = *resp.OutTradeNo
	}
	qr.TradeState = *resp.TradeState
	qr.NormalizedState = normalizeWxTradeState(*resp.TradeState)
	if resp.Mchid != nil {
		qr.MchID = *resp.Mchid
	}
	if resp.Appid != nil {
		qr.AppID = *resp.Appid
	}
	switch qr.NormalizedState {
	case payment.TradeStateSuccess:
		qr.Paid = true
		if resp.TransactionId != nil {
			qr.TransactionID = *resp.TransactionId
		}
		if resp.Amount != nil {
			if resp.Amount.Total != nil {
				qr.PaidAmountFen = *resp.Amount.Total
			}
			// 禁止伪造 currency：仅响应有值时填充
			if resp.Amount.Currency != nil {
				qr.Currency = *resp.Amount.Currency
			}
		}
		if resp.SuccessTime != nil {
			if t, e := time.Parse(time.RFC3339, *resp.SuccessTime); e == nil {
				qr.ProviderPaidAt = t
			}
		}
	case payment.TradeStateClosed:
		qr.Closed = true
	case payment.TradeStateOrderNotExist:
		qr.NotExist = true
	}
	return qr, nil
}

func normalizeWxTradeState(s string) payment.ProviderTradeState {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SUCCESS":
		return payment.TradeStateSuccess
	case "NOTPAY":
		return payment.TradeStateNotPay
	case "CLOSED":
		return payment.TradeStateClosed
	case "REFUND":
		return payment.TradeStateRefund
	case "REVOKED":
		return payment.TradeStateRevoked
	case "USERPAYING":
		return payment.TradeStateUserPaying
	case "PAYERROR":
		return payment.TradeStatePayError
	default:
		return payment.TradeStateUnknown
	}
}

// CloseOrder 关闭未支付订单（官方 close）。204 成功不表示可立即创建新单——须再 Query 确认 CLOSED。
func (a *wxpayAdapter) CloseOrder(ctx context.Context, orderNo string) error {
	cCtx, cancel := context.WithTimeout(ctx, payQueryPerAttempt)
	defer cancel()
	cCtx = context.WithValue(cCtx, ctxKeyPayOperation{}, "close")
	cCtx = context.WithValue(cCtx, ctxKeyPayProvider{}, "wxpay")
	result, err := a.svc.CloseOrder(cCtx, native.CloseOrderRequest{
		OutTradeNo: core.String(orderNo),
		Mchid:      core.String(a.mchID),
	})
	if err != nil {
		return sanitizeAPIError(classifyWxAPIError(err, result))
	}
	return nil
}

// context keys for attempt observability（Transport 从 typed context 读取，禁止 X-Pay-* Header）
type (
	ctxKeyPayAttempt      struct{}
	ctxKeyPayMaxAttempts  struct{}
	ctxKeyPayOperation    struct{}
	ctxKeyPayProvider     struct{}
	ctxKeyPayPreferBackup struct{} // true 时本请求走备域（request-local，不粘滞）
)
