package mtwire

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// errAuthClientUnset 未装配 auth-service 客户端（如单测直构 App）时 querySubPaid 的占位错误。
var errAuthClientUnset = apperr.New("AUTH_CLIENT_UNSET", "支付查单客户端未装配", http.StatusInternalServerError)

// subOrderPaidQuery 查 auth-service 该 SUB 订单是否已支付（对账兜底用）；单测替换为桩。
// provider 在真实模式下决定向微信/支付宝主动查单；mock 模式忽略。
var subOrderPaidQuery = func(a *App, ctx context.Context, orderNo, provider string) (bool, error) {
	if a.authClient == nil {
		return false, errAuthClientUnset
	}
	return a.authClient.QueryOrderStatus(ctx, orderNo, provider)
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

// ReconcileStuckSubscriptions 扫卡在 pending 的套餐订单（SUB），逐笔向 auth-service 查单：
// 已付 → 补激活（ActivatePaidTokenplanOrder 幂等）；真未付 → 不动；查单/激活失败 → 计 Failed 下次再扫。
//
// 成因：用户已付，但 auth-service→主站 /api/internal/order/paid 回调丢失/失败，订单永停 pending
// （SUB 状态机只有 pending/activated、无「已付未激活」中间态，主站无从自知，故须主动查单兜底）。
// before 通常取 now-5min：过滤掉刚下单、用户尚在支付中的在途单。
func (a *App) ReconcileStuckSubscriptions(ctx context.Context, before time.Time) (ReconcileSubResult, error) {
	var rows []subscriptionOrderRow
	if err := a.DB.WithContext(ctx).
		Where("status = ? AND updated_at < ?", subOrderPending, before).
		Find(&rows).Error; err != nil {
		return ReconcileSubResult{}, err
	}
	res := ReconcileSubResult{Scanned: len(rows), Failed: map[string]string{}}
	for _, row := range rows {
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
