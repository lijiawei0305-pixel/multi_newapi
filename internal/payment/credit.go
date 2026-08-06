package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"
)

// amountTolerance 是金额比对容差（元）：仅当订单 ActualPaidFen==0（历史行）时回退使用。
const amountTolerance = 0.011

// amountMatches 报告回调实付是否与库内订单一致（优先整数分比对，PAY-FACT-01）。
// paidCNY<=0 且 paidFen<=0 视为「调用方未提供金额」——新路径禁止在 requireStrict 下跳过。
func amountMatches(paidCNY float64, order *PayOrder) bool {
	if order == nil {
		return false
	}
	orderFen := OrderActualPaidFen(order)
	if paidCNY > 0 {
		paidFen := YuanToFen(paidCNY)
		if orderFen > 0 {
			return paidFen == orderFen
		}
		// 历史行无 fen：浮点容差
		return math.Abs(paidCNY-order.ActualPaid) <= amountTolerance
	}
	// 未提供金额：仅历史兼容路径放行
	return true
}

// amountMatchesFen 用分比对。
func amountMatchesFen(paidFen, orderFen int64) bool {
	if paidFen <= 0 || orderFen <= 0 {
		return false
	}
	return paidFen == orderFen
}

// 订单号业务前缀。
const (
	OrderNoPrefixRecharge     = "RCG"
	OrderNoPrefixSubscription = "SUB"
)

// placeholderTxnIDs 禁止作为 provider_transaction_id 持久化的伪交易号。
var placeholderTxnIDs = map[string]struct{}{
	"query": {}, "reconcile": {}, "unknown": {}, "pending": {}, "n/a": {}, "na": {},
}

// IsPlaceholderTxnID 报告是否为禁止持久化的占位交易号。
func IsPlaceholderTxnID(txnID string) bool {
	k := strings.ToLower(strings.TrimSpace(txnID))
	if k == "" {
		return true
	}
	_, ok := placeholderTxnIDs[k]
	return ok
}

// NewOrderNo 生成带业务前缀的全局唯一订单号。
func NewOrderNo(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(b[:])
}

// validatePaymentFacts 校验成功支付事实（PAY-FACT-01 / Phase D P0-1）。
// requireStrict：强制真实交易号 + 正金额匹配；主动查单 SUCCESS 路径也必须 strict。
func validatePaymentFacts(ord *PayOrder, provider Provider, txnID string, paidCNY float64, requireStrict bool) error {
	if ord == nil {
		return ErrOrderNotFound
	}
	if strings.TrimSpace(ord.OrderNo) == "" {
		return ErrPaymentFactInvalid
	}
	if provider != "" && ord.Provider != provider {
		return ErrPaymentFactInvalid
	}
	txn := strings.TrimSpace(txnID)
	if IsPlaceholderTxnID(txn) {
		// 空串或占位符：strict 一律拒绝；非 strict 亦不得用占位符入账
		if requireStrict || txn != "" {
			return ErrPaymentFactInvalid
		}
	}
	if requireStrict {
		if txn == "" || IsPlaceholderTxnID(txn) {
			return ErrPaymentFactInvalid
		}
		// 必须有可校验的正金额
		if paidCNY <= 0 {
			return ErrPaymentFactInvalid
		}
		if !amountMatches(paidCNY, ord) {
			return ErrAmountMismatch
		}
		return nil
	}
	if paidCNY > 0 && !amountMatches(paidCNY, ord) {
		return ErrAmountMismatch
	}
	return nil
}

// CreditPaidOrder 是「可信内网入账」路径（已废弃宽松签名；保留兼容，强制 requireStrict=false 仅历史）。
// 新代码应使用 CreditFromQueryResult / CreditPaidOrderWithProvider(..., requireStrict=true)。
//
// 注意：txnID 为 "query"/"reconcile" 会被拒绝。
func (g *Gateway) CreditPaidOrder(ctx context.Context, orderNo, txnID string, paidAmountCNY float64) error {
	return g.CreditPaidOrderWithProvider(ctx, orderNo, "", txnID, paidAmountCNY, true)
}

// CreditFromQueryResult 用主动查单的结构化结果入账（P0-1）。
// 必须通过 QueryPaidOK + BindingsOK；不写 callback_received_at；ProviderPaidAt 用平台时间。
func (g *Gateway) CreditFromQueryResult(ctx context.Context, orderNo string, qr *QueryResult) error {
	if qr == nil {
		return ErrPaymentFactInvalid
	}
	if qr.NotExist {
		return ErrOrderNotExist
	}
	ord, err := g.repo.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	if !qr.QueryPaidOK() {
		return ErrPaymentFactInvalid
	}
	if !qr.BindingsOK(orderNo, ord.Provider) {
		return ErrPaymentFactInvalid
	}
	if IsPlaceholderTxnID(qr.TransactionID) {
		return ErrPaymentFactInvalid
	}
	orderFen := OrderActualPaidFen(ord)
	if orderFen <= 0 || qr.PaidAmountFen != orderFen {
		return ErrAmountMismatch
	}
	paidCNY := FenToYuan(qr.PaidAmountFen)
	return g.creditPaidWithPaidAt(ctx, orderNo, qr.Provider, qr.TransactionID, paidCNY, false, qr.ProviderPaidAt)
}

// validateMerchantBinding 校验查单返回的商户/应用与订单渠道一致（字段非空时）。
func validateMerchantBinding(ord *PayOrder, qr *QueryResult) error {
	if ord == nil || qr == nil {
		return ErrPaymentFactInvalid
	}
	// Provider 已在上层比对；此处预留 MchID/AppID 扩展字段匹配。
	if qr.MchID != "" && qr.ExpectedMchID != "" && qr.MchID != qr.ExpectedMchID {
		return ErrPaymentFactInvalid
	}
	if qr.AppID != "" && qr.ExpectedAppID != "" && qr.AppID != qr.ExpectedAppID {
		return ErrPaymentFactInvalid
	}
	return nil
}

// CreditPaidOrderWithProvider 带渠道绑定的入账（回调路径传真实 provider 与 requireStrict）。
// fromCallback 语义由 requireStrict+真实回调调用方保证：仅回调路径写 callback_received_at。
func (g *Gateway) CreditPaidOrderWithProvider(ctx context.Context, orderNo string, provider Provider, txnID string, paidAmountCNY float64, requireStrict bool) error {
	// 回调路径：fromCallback=true
	return g.creditPaid(ctx, orderNo, provider, txnID, paidAmountCNY, requireStrict)
}

// creditPaid 核心入账。
func (g *Gateway) creditPaid(ctx context.Context, orderNo string, provider Provider, txnID string, paidAmountCNY float64, fromCallback bool) error {
	return g.creditPaidWithPaidAt(ctx, orderNo, provider, txnID, paidAmountCNY, fromCallback, time.Time{})
}

// creditPaidWithPaidAt 带平台支付时间的入账。
func (g *Gateway) creditPaidWithPaidAt(ctx context.Context, orderNo string, provider Provider, txnID string, paidAmountCNY float64, fromCallback bool, providerPaidAt time.Time) error {
	t0 := g.now()
	ord, err := g.repo.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	if provider == "" {
		provider = ord.Provider
	}
	if err := validatePaymentFacts(ord, provider, txnID, paidAmountCNY, true); err != nil {
		return err
	}

	first, err := g.claimPaidOrder(ctx, orderNo, ord.Status)
	if err != nil {
		return err
	}
	if !first {
		current, gErr := g.repo.GetByOrderNo(ctx, orderNo)
		if gErr != nil {
			return gErr
		}
		_ = g.persistFactsAt(ctx, current, txnID, fromCallback, false, providerPaidAt)
		if current.Status == OrderCredited {
			return nil
		}
		if current.Status == OrderPaid {
			g.logf("payment: credit %s: not CAS winner, status=paid; defer credit to stuck-paid", orderNo)
			return nil
		}
		return nil
	}
	g.logStage(StageOrderClaimed, orderNo, provider, elapsedMs(t0), "")

	if err := g.persistFactsAt(ctx, ord, txnID, fromCallback, false, providerPaidAt); err != nil {
		_, _ = g.repo.CompareAndSetStatus(ctx, orderNo, OrderPaid, OrderCreated)
		return err
	}

	sink, ok := g.sinks[ord.Type]
	if !ok {
		_, _ = g.repo.CompareAndSetStatus(ctx, orderNo, OrderPaid, OrderCreated)
		_ = g.repo.ScheduleNextQuery(ctx, orderNo, g.now().Add(5*time.Second), ord.QueryAttempts)
		return ErrOrderTypeUnknown
	}

	info := &CallbackInfo{Provider: provider, OrderNo: orderNo, Success: true, TxnID: txnID, PaidAmount: paidAmountCNY}
	if refreshed, rErr := g.repo.GetByOrderNo(ctx, orderNo); rErr == nil {
		ord = refreshed
	}
	paid := ord.toPaidOrder(info, g.now())
	if err := sink.OnPaid(ctx, paid); err != nil {
		_, _ = g.repo.CompareAndSetStatus(ctx, orderNo, OrderPaid, OrderCreated)
		_ = g.repo.ScheduleNextQuery(ctx, orderNo, g.now().Add(5*time.Second), ord.QueryAttempts)
		return err
	}
	g.logStage(StageLedgerCommitted, orderNo, provider, elapsedMs(t0), "")

	_ = g.persistFactsAt(ctx, ord, txnID, fromCallback, true, providerPaidAt)

	if ok, csErr := g.repo.MarkCredited(ctx, orderNo, g.now()); csErr != nil || !ok {
		if ok2, csErr2 := g.repo.CompareAndSetStatus(ctx, orderNo, OrderPaid, OrderCredited); csErr2 != nil || !ok2 {
			g.logf("payment: credit %s: advance paid→credited failed (ok=%v err=%v); left paid for reconcile", orderNo, ok2, csErr2)
		}
	} else {
		g.logStage(StageOrderCredited, orderNo, provider, elapsedMs(t0), "")
	}
	return nil
}

func (g *Gateway) persistFacts(ctx context.Context, ord *PayOrder, txnID string, fromCallback, clearNextQuery bool) error {
	return g.persistFactsAt(ctx, ord, txnID, fromCallback, clearNextQuery, time.Time{})
}

func (g *Gateway) persistFactsAt(ctx context.Context, ord *PayOrder, txnID string, fromCallback, clearNextQuery bool, providerPaidAt time.Time) error {
	if ord == nil {
		return nil
	}
	txn := strings.TrimSpace(txnID)
	paidAt := providerPaidAt
	if paidAt.IsZero() {
		// 仅回调路径在无平台时间时用本站 now；查单路径应尽量带 SuccessTime
		if fromCallback {
			paidAt = g.now()
		}
	}
	if IsPlaceholderTxnID(txn) {
		if fromCallback {
			return ErrPaymentFactInvalid
		}
		return g.repo.SavePaymentFacts(ctx, ord.OrderNo, PaymentFacts{
			ClearNextQuery: clearNextQuery,
			ProviderPaidAt: paidAt,
		})
	}
	facts := PaymentFacts{
		ProviderTransactionID: txn,
		ProviderPaidAt:        paidAt,
		ClearNextQuery:        clearNextQuery,
		Provider:              ord.Provider,
	}
	if fromCallback {
		facts.CallbackReceivedAt = g.now()
		if facts.ProviderPaidAt.IsZero() {
			facts.ProviderPaidAt = g.now()
		}
	}
	return g.repo.SavePaymentFacts(ctx, ord.OrderNo, facts)
}

// claimPaidOrder 用可信已付事实原子认领订单。
// 允许从 created / failed / cancelled（legacy）→ paid。
func (g *Gateway) claimPaidOrder(ctx context.Context, orderNo string, status OrderStatus) (bool, error) {
	for {
		switch status {
		case OrderPaid, OrderCredited:
			return false, nil
		case OrderCreated, OrderFailed, OrderCancelled:
			claimed, err := g.repo.CompareAndSetStatus(ctx, orderNo, status, OrderPaid)
			if err != nil {
				return false, err
			}
			if claimed {
				return true, nil
			}
		default:
			return false, ErrOrderInvalid
		}

		current, err := g.repo.GetByOrderNo(ctx, orderNo)
		if err != nil {
			return false, err
		}
		status = current.Status
	}
}
