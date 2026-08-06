package mtwire

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// subReconcileExpireAge：等待支付或平台建单未完成的套餐单「过期兜底」阈值。微信 Native 二维码有效期
// 2h——下单超此仍未付
// 或网关查无此单，置 subOrderExpired 停止无谓重试与告警（对齐 RCG created 路径的
// reconcileCreatedExpireAge 语义）。若后续收到可信已付回调，付款事实仍可把 expired 原子恢复并激活；
// 主动查单当场查到已付也始终幂等激活（无论多旧都补，绝不漏真实付款）。
const subReconcileExpireAge = 2 * time.Hour

// subOrderPaidQuery 进程内向微信/支付宝主动查单该 SUB 订单是否已支付（对账兜底用）；单测替换为桩。
// provider 决定向哪个平台查单；providerMgr 未装配（如单测直构 App）时返回 errProviderMgrUnset。
var subOrderPaidQuery = func(a *App, ctx context.Context, orderNo, provider string) (bool, error) {
	if a.providerMgr == nil {
		return false, errProviderMgrUnset
	}
	return a.providerMgr.QueryOrderPaid(ctx, payment.Provider(provider), orderNo)
}

// activatePaidSubHook 激活一笔已确认支付的 SUB 订单（对账兜底用）；单测替换为计数桩。
// 默认指向幂等的 ActivatePaidTokenplanOrder（重复激活只生效一次）。
// 主动查单已确认平台已收款，故跳过金额比对（paidAmountCNY=0）。
var activatePaidSubHook = func(a *App, ctx context.Context, orderNo string) error {
	return a.ActivatePaidTokenplanOrder(ctx, orderNo, 0)
}

// ReconcileSubResult 汇总一次 SUB 卡单对账结果（供 admin 端点 / 日志展示）。
type ReconcileSubResult struct {
	Scanned   int               // 扫到的未完成套餐单数（含平台建单中/失败、pending、activated 未结算）
	Activated []string          // 查到已付 → 补激活成功
	Unpaid    []string          // 查到真未付（用户没付）且未超时 → 留待下次
	Expired   []string          // 下单超 subReconcileExpireAge 仍未付 / 网关查无此单 → 置终态 expired（停止重试与告警）
	Failed    map[string]string // 平台建单未完成或查单/激活错误 → 可见告警并留待下次（不含已过期终态单）
}

// ReconcileStuckSubscriptions 扫三类卡单，逐笔补驱动幂等激活（ActivatePaidTokenplanOrder 幂等）：
//   - pay_creating/pay_failed：平台建单结果未确认或明确失败，不得伪装成普通未付款；有渠道时主动查单，
//     查到已付仍补激活，否则在 Failed/卡单页保持显式可见并允许买家重试；
//   - pending：用户已付但 /api/pay/*/notify 回调丢失/失败，订单永停 pending → 向平台**查单**，已付则补激活；
//   - activated 且 settled=false：步骤②（原生订阅）已成、但步骤③（我们的订阅记录 + 代理分润）持续失败
//     且超过平台重推窗口 → 已确认支付，**无需查单**，直接幂等补驱动步骤③（M1，避免分润/记录永久遗漏）。
//
// before 通常取 now-5min：过滤掉刚下单/刚激活、正常流程仍在途的单。
func (a *App) ReconcileStuckSubscriptions(ctx context.Context, before time.Time) (ReconcileSubResult, error) {
	var rows []subscriptionOrderRow
	if err := a.DB.WithContext(ctx).
		Where("(status IN ? OR (status = ? AND settled = ?)) AND updated_at < ?",
			[]string{subOrderPayCreating, subOrderPayFailed, subOrderPending}, subOrderActivated, false, before).
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
		// 未激活支付态：先向平台主动查单确认是否已付。平台建单未完成态须与普通未付款分开归类。
		paymentCreationIncomplete := row.Status == subOrderPayCreating || row.Status == subOrderPayFailed
		expired := row.CreatedAt.Before(expireCutoff) // 下单已超 2h：二维码失效、永不会被支付
		if paymentCreationIncomplete && row.Provider == "" {
			// Provider 在 CreatePay 前写入；为空证明平台请求尚未发出。近期单保持显式失败供重试/告警，
			// 超过支付窗口后安全过期，可信迟到付款仍可从 expired 恢复。
			if expired {
				a.expireStuckSubOrder(ctx, row.OrderNo)
				res.Expired = append(res.Expired, row.OrderNo)
			} else {
				res.Failed[row.OrderNo] = "create-pay: provider not recorded; retry purchase"
			}
			continue
		}
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
			} else if paymentCreationIncomplete {
				res.Failed[row.OrderNo] = "create-pay query: " + err.Error()
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
			if paymentCreationIncomplete {
				// 支付凭据从未确认返回给买家，不能归为普通「用户未付」；保持显式可重试并触发运维可见性。
				res.Failed[row.OrderNo] = "create-pay: payment credentials not confirmed; retry purchase"
			} else {
				// 未付但仍在有效窗口（未超时）：留待下次。
				res.Unpaid = append(res.Unpaid, row.OrderNo)
			}
		}
	}
	return res, nil
}

// expireStuckSubOrder 把一笔卡死的未激活套餐单置为 subOrderExpired（条件 CAS：仅在仍为平台建单中/失败
// 或 pending 时更新，防与并发真实激活竞态）。可信已付事实可在激活事务中把 expired 恢复为 activated。
// best-effort：写失败仅下轮再来，绝不影响对账其余单。
func (a *App) expireStuckSubOrder(ctx context.Context, orderNo string) {
	_ = a.DB.WithContext(ctx).Model(&subscriptionOrderRow{}).
		Where("order_no = ? AND status IN ?", orderNo,
			[]string{subOrderPayCreating, subOrderPayFailed, subOrderPending}).
		Updates(map[string]any{"status": subOrderExpired, "updated_at": time.Now()}).Error
}

// listStuckSubscriptions 只读列出平台建单中/失败或 pending（早于 before）的套餐订单，
// 供 admin「支付对账」页展示原始状态，避免 provider 失败伪装成普通未付款。
func (a *App) listStuckSubscriptions(ctx context.Context, before time.Time) ([]subscriptionOrderRow, error) {
	var rows []subscriptionOrderRow
	err := a.DB.WithContext(ctx).
		Where("status IN ? AND updated_at < ?",
			[]string{subOrderPayCreating, subOrderPayFailed, subOrderPending}, before).
		Find(&rows).Error
	return rows, err
}
