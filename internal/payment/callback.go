package payment

import "context"

// HandleWxpay 处理微信支付异步回调（Nginx ^~ /pay/ 转发）。
func (g *Gateway) HandleWxpay(ctx context.Context, raw []byte) error {
	return g.handle(ctx, ProviderWxpay, raw)
}

// HandleAlipay 处理支付宝异步回调（Nginx ^~ /auth/ 转发）。
func (g *Gateway) HandleAlipay(ctx context.Context, raw []byte) error {
	return g.handle(ctx, ProviderAlipay, raw)
}

// handle 是回调统一编排（detailed-design §2.8 / §3.3）：
//
//	验签 → 定位订单 → [平台失败则置 failed] → CAS(created|failed→paid) 幂等占位
//	  → 已 paid/credited 短路成功（不重复入账）
//	  → 首个推进者：按 type 分发 OnPaid → 成功置 credited / 失败回滚 created 供网关重试
//
// 错误码：错签 PAY_SIGN_INVALID；报文非法 PAY_CALLBACK_INVALID；未知单 ORDER_NOT_FOUND。
func (g *Gateway) handle(ctx context.Context, provider Provider, raw []byte) error {
	// 1) 验签 + 解析（PaySDK）。错签/非法报文 → 不入账。
	info, err := g.sdk.Verify(ctx, provider, raw)
	if err != nil {
		return err // PAY_SIGN_INVALID / PAY_CALLBACK_INVALID
	}

	// 2) 定位订单（order_no）。
	ord, err := g.repo.GetByOrderNo(ctx, info.OrderNo)
	if err != nil {
		return err // ORDER_NOT_FOUND
	}

	// 3) 平台明确支付失败 → 置 failed 终态（幂等：非 created 则 CAS 自然失败），对外 ack。
	if !info.Success {
		_, _ = g.repo.CompareAndSetStatus(ctx, ord.OrderNo, OrderCreated, OrderFailed)
		return nil
	}

	// 4) 幂等占位：可信成功回调可从 created 或本地 failed 状态认领。后者覆盖「本地过期与迟到
	// 成功回调竞态」；不能因 created→paid CAS 失败就把 failed 静默当成已处理。
	first, err := g.claimPaidOrder(ctx, ord.OrderNo, ord.Status)
	if err != nil {
		return err // ORDER_NOT_FOUND（极端竞态：订单被删）
	}
	if !first {
		// 已 paid/credited → 幂等短路，返回成功，不重复入账（ORDER_ALREADY_PAID）。
		return nil
	}

	// 5) 按 type 分发到对应 OrderSink（recharge→Wallet；subscription→TokenPlan）。
	sink, ok := g.sinks[ord.Type]
	if !ok {
		// 防御：下单时已校验 type，正常不达。回滚占位，避免订单卡在 paid。
		_, _ = g.repo.CompareAndSetStatus(ctx, ord.OrderNo, OrderPaid, OrderCreated)
		return ErrOrderTypeUnknown
	}

	paid := ord.toPaidOrder(info, g.now())
	if err := sink.OnPaid(ctx, paid); err != nil {
		// 入账失败 → 回滚 paid→created，使网关重试时可重新分发（避免丢账）。
		_, _ = g.repo.CompareAndSetStatus(ctx, ord.OrderNo, OrderPaid, OrderCreated)
		return err
	}

	// 6) 入账成功 → 置终态 credited。
	_, _ = g.repo.CompareAndSetStatus(ctx, ord.OrderNo, OrderPaid, OrderCredited)
	return nil
}
