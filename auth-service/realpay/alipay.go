package realpay

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/smartwalle/alipay/v3"

	"github.com/QuantumNous/new-api/internal/payment"
)

// alipayAdapter 封装支付宝电脑网站支付（alipay.trade.page.pay）：下单（生成跳转 URL）/
// 异步通知验签解析 / 主动查单。采用普通公钥模式（应用私钥签名 + 支付宝公钥验签）。
type alipayAdapter struct {
	appID     string
	sellerID  string
	returnURL string
	client    *alipay.Client
}

func newAlipayAdapter(cfg AlipayConfig) (*alipayAdapter, error) {
	client, err := alipay.New(cfg.AppID, cfg.PrivateKey, cfg.IsProduction)
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
		client:    client,
	}, nil
}

// createPay 构造电脑网站支付跳转 URL（前端 GET 跳转到此 URL 进入支付宝收银台）。
// 金额单位：元（字符串，两位小数）。notifyURL 为异步通知地址。
func (a *alipayAdapter) createPay(ctx context.Context, orderNo, subject string, amountCNY float64, notifyURL string) (string, error) {
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
	// NOTE(W4): smartwalle/alipay/v3 的 TradePagePay 返回 (*url.URL, error)（页面跳转类、无网络调用）。
	// 若 pinned 版本签名带 ctx，请改为 a.client.TradePagePay(ctx, p)。
	u, err := a.client.TradePagePay(p)
	if err != nil {
		return "", fmt.Errorf("alipay page pay: %w", err)
	}
	return u.String(), nil
}

// verifyNotify 解析并验签异步通知（DecodeNotification 内部已验签），校验 app_id / seller_id。
func (a *alipayAdapter) verifyNotify(_ context.Context, r *http.Request) (*payment.CallbackInfo, error) {
	if err := r.ParseForm(); err != nil {
		return nil, payment.ErrCallbackInvalid
	}
	// DecodeNotification 内部调用 VerifySign（用支付宝公钥）；失败即验签不通过。
	noti, err := a.client.DecodeNotification(r.Form)
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

// queryOrder 按商户订单号主动查单：TRADE_SUCCESS/TRADE_FINISHED 视为已收款。
func (a *alipayAdapter) queryOrder(ctx context.Context, orderNo string) (bool, error) {
	// NOTE(W4): smartwalle/alipay/v3 v3.2+ 的 TradeQuery 形如 (ctx, param) (*TradeQueryRsp, error)，
	// 且交易状态字段为 rsp.TradeStatus（扁平）。若 pinned 版本无 ctx 或字段嵌套于 rsp.Content，
	// 请相应改为 a.client.TradeQuery(param) / rsp.Content.TradeStatus。
	rsp, err := a.client.TradeQuery(ctx, alipay.TradeQuery{OutTradeNo: orderNo})
	if err != nil {
		return false, fmt.Errorf("alipay query: %w", err)
	}
	return rsp.TradeStatus == alipay.TradeStatusSuccess || rsp.TradeStatus == alipay.TradeStatusFinished, nil
}
