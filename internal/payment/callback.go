package payment

import "context"

// HandleWxpay 处理微信支付异步回调。
func (g *Gateway) HandleWxpay(ctx context.Context, raw []byte) error {
	return g.handle(ctx, ProviderWxpay, raw)
}

// HandleAlipay 处理支付宝异步回调。
func (g *Gateway) HandleAlipay(ctx context.Context, raw []byte) error {
	return g.handle(ctx, ProviderAlipay, raw)
}

// handle 是回调统一编排（PAY-FACT-01 / PAY-OBS-01）：
//
//	验签 → 支付事实校验 → CAS → OnPaid → credited
func (g *Gateway) handle(ctx context.Context, provider Provider, raw []byte) error {
	t0 := g.now()
	g.logStage(StageNotifyReceived, "", provider, 0, "")

	info, err := g.sdk.Verify(ctx, provider, raw)
	if err != nil {
		g.logStage(StageSignatureVerified, "", provider, elapsedMs(t0), "ok=0")
		return err
	}
	g.logStage(StageSignatureVerified, info.OrderNo, provider, elapsedMs(t0), "ok=1")

	ord, err := g.repo.GetByOrderNo(ctx, info.OrderNo)
	if err != nil {
		return err
	}

	if !info.Success {
		_, _ = g.repo.CompareAndSetStatus(ctx, ord.OrderNo, OrderCreated, OrderFailed)
		g.logStage(StageAckSent, ord.OrderNo, provider, elapsedMs(t0), "result=platform_failed")
		return nil
	}

	// 严格支付事实：渠道一致、交易号非空、金额正且匹配
	err = g.CreditPaidOrderWithProvider(ctx, ord.OrderNo, provider, info.TxnID, info.PaidAmount, true)
	if err != nil {
		g.logStage(StageAckSent, ord.OrderNo, provider, elapsedMs(t0), "result=credit_err")
		return err
	}
	g.logStage(StageAckSent, ord.OrderNo, provider, elapsedMs(t0), "result=ok")
	return nil
}
