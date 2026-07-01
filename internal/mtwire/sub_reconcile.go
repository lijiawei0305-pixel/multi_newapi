package mtwire

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// subOrderPaidQuery 进程内向微信/支付宝主动查单该 SUB 订单是否已支付（对账兜底用）；单测替换为桩。
// provider 决定向哪个平台查单；providerMgr 未装配（如单测直构 App）时返回 errProviderMgrUnset。
var subOrderPaidQuery = func(a *App, ctx context.Context, orderNo, provider string) (bool, error) {
	if a.providerMgr == nil {
		return false, errProviderMgrUnset
	}
	return a.providerMgr.QueryOrder(ctx, payment.Provider(provider), orderNo)
}

// activatePaidSubHook 激活一笔已确认支付的 SUB 订单（对账兜底用）；单测替换为计数桩。
// 默认指向幂等的 ActivatePaidTokenplanOrder（重复激活只生效一次）。
// 主动查单已确认平台已收款，故跳过金额比对（paidAmountCNY=0）。
var activatePaidSubHook = func(a *App, ctx context.Context, orderNo string) error {
	return a.ActivatePaidTokenplanOrder(ctx, orderNo, 0)
}

// ReconcileSubResult 汇总一次 SUB 卡单对账结果（供 admin 端点 / 日志展示）。
type ReconcileSubResult struct {
	Scanned   int               // 扫到的 pending 套餐单数
	Activated []string          // 查到已付 → 补激活成功
	Unpaid    []string          // 查到真未付（用户没付）→ 不动
	Failed    map[string]string // 查单/激活失败 → 留待下次再扫
}

// ReconcileStuckSubscriptions 扫两类卡单，逐笔补驱动幂等激活（ActivatePaidTokenplanOrder 幂等）：
//   - pending：用户已付但 /api/pay/*/notify 回调丢失/失败，订单永停 pending → 向平台**查单**，已付则补激活；
//   - activated 且 settled=false：步骤②（原生订阅）已成、但步骤③（我们的订阅记录 + 代理分润）持续失败
//     且超过平台重推窗口 → 已确认支付，**无需查单**，直接幂等补驱动步骤③（M1，避免分润/记录永久遗漏）。
//
// before 通常取 now-5min：过滤掉刚下单/刚激活、正常流程仍在途的单。
func (a *App) ReconcileStuckSubscriptions(ctx context.Context, before time.Time) (ReconcileSubResult, error) {
	var rows []subscriptionOrderRow
	if err := a.DB.WithContext(ctx).
		Where("(status = ? OR (status = ? AND settled = ?)) AND updated_at < ?",
			subOrderPending, subOrderActivated, false, before).
		Find(&rows).Error; err != nil {
		return ReconcileSubResult{}, err
	}
	res := ReconcileSubResult{Scanned: len(rows), Failed: map[string]string{}}
	for _, row := range rows {
		// 已激活未结算：已确认支付，跳过查单，直接补驱动步骤③（幂等：步骤②命中 activated 短路）。
		if row.Status == subOrderActivated {
			if err := activatePaidSubHook(a, ctx, row.OrderNo); err != nil {
				res.Failed[row.OrderNo] = "settle: " + err.Error()
				continue
			}
			res.Activated = append(res.Activated, row.OrderNo)
			continue
		}
		// pending：向平台主动查单确认是否已付。
		paid, err := subOrderPaidQuery(a, ctx, row.OrderNo, row.Provider)
		if err != nil {
			res.Failed[row.OrderNo] = "query: " + err.Error()
			continue
		}
		if !paid {
			res.Unpaid = append(res.Unpaid, row.OrderNo) // 真未付：用户没付，不激活
			continue
		}
		if err := activatePaidSubHook(a, ctx, row.OrderNo); err != nil {
			res.Failed[row.OrderNo] = "activate: " + err.Error()
			continue
		}
		res.Activated = append(res.Activated, row.OrderNo)
	}
	return res, nil
}

// listStuckSubscriptions 只读列出卡在 pending（早于 before）的套餐订单，供 admin「支付对账」页展示。
func (a *App) listStuckSubscriptions(ctx context.Context, before time.Time) ([]subscriptionOrderRow, error) {
	var rows []subscriptionOrderRow
	err := a.DB.WithContext(ctx).
		Where("status = ? AND updated_at < ?", subOrderPending, before).
		Find(&rows).Error
	return rows, err
}
