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
