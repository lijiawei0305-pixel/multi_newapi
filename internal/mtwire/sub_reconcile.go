package mtwire

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// subReconcileExpireAge：pending 套餐单「过期兜底」阈值。微信 Native 二维码有效期 2h——下单超此仍未付
// 或网关查无此单，即永不会被支付，置终态 subOrderExpired 停止无谓重试与告警（对齐 RCG created 路径的
// reconcileCreatedExpireAge 语义）。注意：查到已付仍幂等激活（无论多旧都补，绝不漏真实付款）。
const subReconcileExpireAge = 2 * time.Hour

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
	Unpaid    []string          // 查到真未付（用户没付）且未超时 → 留待下次
	Expired   []string          // 下单超 subReconcileExpireAge 仍未付 / 网关查无此单 → 置终态 expired（停止重试与告警）
	Failed    map[string]string // 查单/激活的**瞬时**错误 → 留待下次再扫（不含已过期的终态单）
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
	now := before.Add(reconcileMinAge)              // before 恒为 now-reconcileMinAge（cron/manual 一致），反推当前时刻
	expireCutoff := now.Add(-subReconcileExpireAge) // 下单早于此 = 已超 subReconcileExpireAge（二维码失效）
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
		// pending：先向平台主动查单确认是否已付。
		expired := row.CreatedAt.Before(expireCutoff) // 下单已超 2h：二维码失效、永不会被支付
		paid, err := subOrderPaidQuery(a, ctx, row.OrderNo, row.Provider)
		switch {
		case err != nil:
			// 终态只接受网关**确定性答复**：明确「查无此单」(ORDER_NOT_EXIST/TRADE_NOT_EXIST) 且已超 2h
			// → 过期；查单本身失败（超时/限流/凭据不完整）一律留 Failed（可见+告警+下轮重扫），无论多旧。
			// 旧「超 26h ancient 兜底过期」已删——循环论证会把已付单静默写成终态且零告警，与 AGT 同源同修，
			// 完整论证见 agent_plan_reconcile.go 同位置注释（audit 2026-07-17 #7）。
			if expired && errors.Is(err, payment.ErrOrderNotExist) {
				a.expireStuckSubOrder(ctx, row.OrderNo)
				res.Expired = append(res.Expired, row.OrderNo)
			} else {
				res.Failed[row.OrderNo] = "query: " + err.Error()
			}
		case paid:
			// 已付 → 幂等激活（无论下单多旧都补，保证绝不漏真实付款）。
			if err := activatePaidSubHook(a, ctx, row.OrderNo); err != nil {
				res.Failed[row.OrderNo] = "activate: " + err.Error()
				continue
			}
			res.Activated = append(res.Activated, row.OrderNo)
		case expired:
			// 确认未付且已超时 → 终态过期（二维码失效、永不会付），停止扫描与告警。
			a.expireStuckSubOrder(ctx, row.OrderNo)
			res.Expired = append(res.Expired, row.OrderNo)
		default:
			// 未付但仍在有效窗口（未超时）：留待下次。
			res.Unpaid = append(res.Unpaid, row.OrderNo)
		}
	}
	return res, nil
}

// expireStuckSubOrder 把一笔卡死的 pending 套餐单置终态 subOrderExpired（条件 CAS：仅在仍 pending 时更新，
// 防与并发真实激活竞态）。best-effort：写失败仅下轮再来，绝不影响对账其余单。
func (a *App) expireStuckSubOrder(ctx context.Context, orderNo string) {
	_ = a.DB.WithContext(ctx).Model(&subscriptionOrderRow{}).
		Where("order_no = ? AND status = ?", orderNo, subOrderPending).
		Updates(map[string]any{"status": subOrderExpired, "updated_at": time.Now()}).Error
}

// listStuckSubscriptions 只读列出卡在 pending（早于 before）的套餐订单，供 admin「支付对账」页展示。
func (a *App) listStuckSubscriptions(ctx context.Context, before time.Time) ([]subscriptionOrderRow, error) {
	var rows []subscriptionOrderRow
	err := a.DB.WithContext(ctx).
		Where("status = ? AND updated_at < ?", subOrderPending, before).
		Find(&rows).Error
	return rows, err
}
