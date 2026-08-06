package realpay

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/smartwalle/alipay/v3"

	"github.com/QuantumNous/new-api/internal/payment"
)

// alipayAdapter 封装支付宝电脑网站支付（alipay.trade.page.pay）：下单（生成跳转 URL）/
// 异步通知验签解析 / 主动查单。采用普通公钥模式（应用私钥签名 + 支付宝公钥验签）。
type alipayAdapter struct {
	appID     string
	sellerID  string
	returnURL string
	prod      bool
	client    *alipay.Client
}

func newAlipayAdapter(cfg AlipayConfig) (*alipayAdapter, error) {
	// 与微信共用独立安全 paymentHTTPClient；不 Clone DefaultTransport。
	client, err := alipay.New(cfg.AppID, cfg.PrivateKey, cfg.IsProduction, alipay.WithHTTPClient(paymentHTTPClientShared()))
	if err != nil {
		return nil, fmt.Errorf("alipay: new client: %w", err)
	}
	if err := client.LoadAliPayPublicKey(cfg.AlipayPublicKey); err != nil {
		return nil, fmt.Errorf("alipay: load alipay public key: %w", err)
	}
	return &alipayAdapter{
		appID:     cfg.AppID,
		sellerID:  cfg.SellerID,
		returnURL: cfg.ReturnURL,
		prod:      cfg.IsProduction,
		client:    client,
	}, nil
}

// createPay 构造电脑网站支付跳转 URL（本地签名，不等同微信网络 Prepay）。
// expiresAt 映射 TimeoutExpress（相对时长），与 DB expires_at 对齐。
func (a *alipayAdapter) createPay(ctx context.Context, orderNo, subject string, amountCNY float64, notifyURL string, expiresAt time.Time) (string, error) {
	if amountCNY <= 0 {
		return "", fmt.Errorf("alipay: invalid amount %.2f", amountCNY)
	}
	var p = alipay.TradePagePay{}
	p.OutTradeNo = orderNo
	p.Subject = subject
	p.TotalAmount = strconv.FormatFloat(amountCNY, 'f', 2, 64)
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"
	p.NotifyURL = notifyURL
	p.ReturnURL = a.returnURL
	if !expiresAt.IsZero() {
		// 相对超时：向上取整到分钟，最低 1m
		d := time.Until(expiresAt)
		if d < time.Minute {
			d = time.Minute
		}
		mins := int(d.Minutes())
		if mins < 1 {
			mins = 1
		}
		p.TimeoutExpress = fmt.Sprintf("%dm", mins)
	}
	u, err := a.client.TradePagePay(p)
	if err != nil {
		return "", fmt.Errorf("alipay page pay: %w", err)
	}
	return u.String(), nil
}

// CloseOrder 支付宝交易关闭。
func (a *alipayAdapter) CloseOrder(ctx context.Context, orderNo string) error {
	_, err := a.client.TradeClose(ctx, alipay.TradeClose{OutTradeNo: orderNo})
	if err != nil {
		return payment.AsOutcome(fmt.Errorf("alipay close: request failed"))
	}
	return nil
}

// verifyNotify 解析并验签异步通知（DecodeNotification 内部已验签），校验 app_id / seller_id。
func (a *alipayAdapter) verifyNotify(ctx context.Context, r *http.Request) (*payment.CallbackInfo, error) {
	if err := r.ParseForm(); err != nil {
		return nil, payment.ErrCallbackInvalid
	}
	// v3.2.29 verified: DecodeNotification(ctx, values) —— 内部调用 VerifySign（支付宝公钥）；失败即验签不通过。
	noti, err := a.client.DecodeNotification(ctx, r.Form)
	if err != nil {
		return nil, payment.ErrSignInvalid
	}
	return aliNotificationToInfo(noti, a.appID, a.sellerID)
}

// aliNotificationToInfo 把验签后的通知映射为 CallbackInfo，并做商户校验（纯函数，可单测）：
//   - app_id 与本应用不符 → ErrCallbackInvalid；
//   - 配置了 sellerID 且通知 seller_id 与之不符 → ErrCallbackInvalid；
//   - trade_status∈{TRADE_SUCCESS,TRADE_FINISHED} → Success；total_amount（元）解析为 PaidAmount。
func aliNotificationToInfo(noti *alipay.Notification, appID, sellerID string) (*payment.CallbackInfo, error) {
	if noti == nil || noti.OutTradeNo == "" {
		return nil, payment.ErrCallbackInvalid
	}
	if noti.AppId != appID {
		return nil, payment.ErrCallbackInvalid
	}
	if sellerID != "" && noti.SellerId != "" && noti.SellerId != sellerID {
		return nil, payment.ErrCallbackInvalid
	}
	info := &payment.CallbackInfo{
		Provider: payment.ProviderAlipay,
		OrderNo:  noti.OutTradeNo,
		Success:  noti.TradeStatus == alipay.TradeStatusSuccess || noti.TradeStatus == alipay.TradeStatusFinished,
		TxnID:    noti.TradeNo,
	}
	if amt, e := strconv.ParseFloat(noti.TotalAmount, 64); e == nil {
		info.PaidAmount = amt // 元
	}
	return info, nil
}

// queryOrder 按商户订单号主动查单：返回结构化 QueryResult（P0-1）。
func (a *alipayAdapter) queryOrder(ctx context.Context, orderNo string) (*payment.QueryResult, error) {
	rsp, err := a.client.TradeQuery(ctx, alipay.TradeQuery{OutTradeNo: orderNo})
	if err != nil {
		if rsp != nil && strings.Contains(rsp.SubCode, "TRADE_NOT_EXIST") {
			return &payment.QueryResult{
				Provider: payment.ProviderAlipay,
				OrderNo:  orderNo,
				NotExist: true,
			}, fmt.Errorf("alipay query trade not exist: %w", payment.ErrOrderNotExist)
		}
		// 脱敏：不返回完整 SDK 错误正文
		return nil, payment.AsOutcome(fmt.Errorf("alipay query: request failed"))
	}
	qr := &payment.QueryResult{
		Provider:      payment.ProviderAlipay,
		ExpectedAppID: a.appID,
		ExpectedMchID: a.sellerID,
		// TradeQuery 经 SDK 验签成功：无 seller 字段时以 AuthorityVerified 表达绑定可信
		AuthorityVerified: true,
		OrderNo:           rsp.OutTradeNo,
		TradeState:        string(rsp.TradeStatus),
		Currency:          "CNY",
	}
	switch {
	case rsp.TradeStatus == alipay.TradeStatusSuccess || rsp.TradeStatus == alipay.TradeStatusFinished:
		qr.Paid = true
		qr.NormalizedState = payment.TradeStateSuccess
		qr.TransactionID = rsp.TradeNo
		if amt, e := strconv.ParseFloat(rsp.TotalAmount, 64); e == nil {
			qr.PaidAmountFen = payment.YuanToFen(amt)
		}
		if rsp.SendPayDate != "" {
			if t, e := time.ParseInLocation("2006-01-02 15:04:05", rsp.SendPayDate, time.Local); e == nil {
				qr.ProviderPaidAt = t
			}
		}
	case strings.EqualFold(string(rsp.TradeStatus), "TRADE_CLOSED"):
		qr.Closed = true
		qr.NormalizedState = payment.TradeStateClosed
	default:
		qr.NormalizedState = payment.TradeStateNotPay
	}
	return qr, nil
}
