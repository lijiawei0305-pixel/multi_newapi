package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

// 订单号业务前缀（与 defaultOrderNo 的 "PAY" 同构，便于人工与日志辨识订单用途）。
const (
	// OrderNoPrefixRecharge 钱包充值订单前缀。
	OrderNoPrefixRecharge = "RCG"
	// OrderNoPrefixSubscription tokenplan 套餐订单前缀。
	OrderNoPrefixSubscription = "SUB"
)

// NewOrderNo 生成带业务前缀的全局唯一订单号：<PREFIX> + 纳秒时间(base36) + 6 字节随机(hex)。
// 供主站下单时按订单类型选择前缀（recharge→RCG、subscription→SUB），注入 WithOrderNoFunc。
func NewOrderNo(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:]) // crypto/rand 失败概率可忽略；退化为纯时间序仍唯一性极高
	return prefix + strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(b[:])
}

// CreditPaidOrder 是「可信内网入账」路径（detailed-design §2.8 / §3.3 的入账段）。
//
// 与 handle（公网回调）的区别：调用方（auth-service）**已**在支付侧完成平台验签，并经
// 共享密钥认证后才调用本方法，故此处**不再验签**；金额一律以库内订单为准，不信外部报文
// （只透传 txnID 作审计/收益引用）。强幂等仍由订单状态机保证：
//
//	定位订单 → CAS(created→paid) 原子占位（并发/重复只有一个胜者）
//	  → 已 paid/credited → 幂等短路成功（不重复入账）
//	  → 首个推进者：按 type 分发 OnPaid → 成功置 credited / 失败回滚 created 供上游重试
//
// 错误码：未知单 ORDER_NOT_FOUND；类型无 Sink PAY_ORDER_TYPE_UNKNOWN；OnPaid 错误原样上浮。
func (g *Gateway) CreditPaidOrder(ctx context.Context, orderNo, txnID string) error {
	ord, err := g.repo.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return err // ORDER_NOT_FOUND
	}

	// 幂等占位：created→paid 原子 CAS。并发/重复回调只有一个胜者继续入账。
	first, err := g.repo.CompareAndSetStatus(ctx, orderNo, OrderCreated, OrderPaid)
	if err != nil {
		return err // ORDER_NOT_FOUND（极端竞态：订单被删）
	}
	if !first {
		// 已 paid/credited（或 failed）→ 幂等短路返回成功，绝不重复入账。
		return nil
	}

	sink, ok := g.sinks[ord.Type]
	if !ok {
		// 防御：下单时已校验 type，正常不达。回滚占位避免卡在 paid。
		_, _ = g.repo.CompareAndSetStatus(ctx, orderNo, OrderPaid, OrderCreated)
		return ErrOrderTypeUnknown
	}

	info := &CallbackInfo{Provider: ord.Provider, OrderNo: orderNo, Success: true, TxnID: txnID}
	paid := ord.toPaidOrder(info, g.now())
	if err := sink.OnPaid(ctx, paid); err != nil {
		// 入账失败 → 回滚 paid→created，使上游重试时可重新分发（避免丢账）。
		_, _ = g.repo.CompareAndSetStatus(ctx, orderNo, OrderPaid, OrderCreated)
		return err
	}

	// 入账成功 → 置终态 credited。
	_, _ = g.repo.CompareAndSetStatus(ctx, orderNo, OrderPaid, OrderCredited)
	return nil
}
