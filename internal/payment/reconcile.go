package payment

import (
	"context"
	"time"
)

// ReconcileResult 汇总一次对账兜底的结果（供 admin 端点 / 日志展示）。
type ReconcileResult struct {
	Scanned    int               // 扫到的卡单数
	Reconciled []string          // 成功补入账的 order_no
	Failed     map[string]string // order_no → 错误（本次仍失败、留待下次再扫）
}

// ReconcileStuckPaid 扫描卡在 paid（CAS 占位成功但入账未完成、既没 credited 也没回滚 created）
// 的订单，对每笔重跑 OnPaid（钱包/套餐入账强幂等、不双扣）后置 credited。
//
// 卡单成因：入账进程在 OnPaid 完成与 CAS(paid→credited) 之间崩溃，订单永久停在 paid——
// 此时重跑 CreditPaidOrder 会被 CAS(created→paid) 短路（误判已处理），钱永不入账。本方法是其兜底。
//
// before 通常取 now-5min：过滤掉刚占位、可能正在入账的在途订单，避免与正常回调竞态导致重复入账
// （即便竞态，OnPaid 的强幂等仍兜底不双扣；before 只是进一步降低无谓重跑）。
func (g *Gateway) ReconcileStuckPaid(ctx context.Context, before time.Time) (ReconcileResult, error) {
	stuck, err := g.repo.ListByStatus(ctx, OrderPaid, before)
	if err != nil {
		return ReconcileResult{}, err
	}
	res := ReconcileResult{Scanned: len(stuck), Failed: map[string]string{}}
	for _, ord := range stuck {
		sink, ok := g.sinks[ord.Type]
		if !ok {
			// 类型无 sink（理论不达：下单已校验）→ 记失败、不动状态，避免误判。
			res.Failed[ord.OrderNo] = "no sink for type " + string(ord.Type)
			continue
		}
		info := &CallbackInfo{Provider: ord.Provider, OrderNo: ord.OrderNo, Success: true, TxnID: "reconcile"}
		paid := ord.toPaidOrder(info, g.now())
		if err := sink.OnPaid(ctx, paid); err != nil {
			// 入账仍失败 → 留在 paid，下次再扫（不回滚 created：回 created 会被正常回调流误当新单丢弃）。
			res.Failed[ord.OrderNo] = err.Error()
			continue
		}
		_, _ = g.repo.CompareAndSetStatus(ctx, ord.OrderNo, OrderPaid, OrderCredited)
		res.Reconciled = append(res.Reconciled, ord.OrderNo)
	}
	return res, nil
}

// ListStuckPaid 只读列出卡在 paid（早于 before）的订单，供 admin「支付对账」页展示；不触发入账。
func (g *Gateway) ListStuckPaid(ctx context.Context, before time.Time) ([]*PayOrder, error) {
	return g.repo.ListByStatus(ctx, OrderPaid, before)
}

// ReconcileStuckCreated 扫卡在 created（早于 before）的订单，逐笔经 query 向支付平台主动查单：
// 已付 → 走 CreditPaidOrder 补入账（paidAmount=0 跳过金额比对，信己方查单结果）；
// 真未付 / 查单失败 → 不动状态、计入结果，下次再扫。
//
// 卡单成因：用户已付，但平台异步回调始终未成功送达主站（极端：平台多次重推全失败），
// 订单永停 created。本方法是其兜底（与 ReconcileStuckPaid 互补：后者管「已 paid 未 credited」崩溃缺口）。
//
//   - query：由调用方注入（主站经 auth-service 向微信/支付宝查单），返回该单平台是否已收款。
//   - maxAge：早于 now-maxAge 的 created 单视为已过期废弃单，跳过查单（微信≈8h、支付宝≈25h 后必失效）。
//   - limit：单轮最多处理笔数（>0 生效），防一轮查单过多。
func (g *Gateway) ReconcileStuckCreated(
	ctx context.Context,
	before time.Time,
	maxAge time.Duration,
	limit int,
	query func(ctx context.Context, orderNo, provider string) (bool, error),
) (ReconcileResult, error) {
	if query == nil {
		return ReconcileResult{}, nil
	}
	created, err := g.repo.ListByStatus(ctx, OrderCreated, before)
	if err != nil {
		return ReconcileResult{}, err
	}
	res := ReconcileResult{Failed: map[string]string{}}
	cutoff := g.now().Add(-maxAge)
	for _, ord := range created {
		if maxAge > 0 && ord.CreatedAt.Before(cutoff) {
			continue // 早已过期的废弃单，不再查单
		}
		if limit > 0 && res.Scanned >= limit {
			break
		}
		res.Scanned++
		paid, qErr := query(ctx, ord.OrderNo, string(ord.Provider))
		if qErr != nil {
			res.Failed[ord.OrderNo] = "query: " + qErr.Error()
			continue
		}
		if !paid {
			continue // 真未付：用户没付，不动
		}
		// 信己方查单结果，金额已由下单时落库；paidAmount=0 跳过比对，复用强幂等入账。
		if cErr := g.CreditPaidOrder(ctx, ord.OrderNo, "reconcile", 0); cErr != nil {
			res.Failed[ord.OrderNo] = "credit: " + cErr.Error()
			continue
		}
		res.Reconciled = append(res.Reconciled, ord.OrderNo)
	}
	return res, nil
}
