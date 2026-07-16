package mtwire

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// AGT 代理套餐是全站客单价最高的 SKU（¥990–9990），却曾是唯一「无对账兜底」的付费订单类型：RCG/SUB
// 均有主动查单 + 超时置终态 + 卡单可见 + 失败告警，AGT 一层都没有——真实失败场景（如 api_v3_key 轮换
// 配错致回调全部验签失败）下，RCG/SUB 会被对账循环持续查单/救回并在卡单页可见、失败即告警，而 AGT 会
// 静默永停 pending，无任何页面可见、无任何告警，唯一发现途径是手工 SELECT。本文件把 AGT 补齐到与 SUB
// **完全对等**的保护，语义/阈值与 sub_reconcile.go 一一对齐（AGT 无 SUB 的「已激活未结算」二次驱动路径
// ——其激活为 provision 先行、CAS 后置的原子链，故只需 pending 单查单一条路径）。

// agtReconcileExpireAge：pending AGT 单「过期兜底」阈值（对齐 subReconcileExpireAge）。微信 Native 二维码
// 有效期 2h——下单超此仍未付或网关查无此单，即永不会被支付，置终态 agtOrderExpired 停止无谓重试与告警。
// 注意：查到已付仍幂等激活（无论多旧都补，绝不漏真实付款）。
const agtReconcileExpireAge = 2 * time.Hour

// agtReconcileMaxAge：pending AGT 单「兜底强制过期」阈值（对齐 subReconcileMaxAge=26h）。下单超此即极旧：
// 二维码作废多日，若曾被支付，前面数百轮查单/回调必已捕获激活。故即便本轮查单持续超时 / 被网关限流拿不到
// 确定答复，也终态过期——止住 ancient 卡单在网关不可达时永久重试与告警。查到已付仍先激活。
const agtReconcileMaxAge = 26 * time.Hour

// agtOrderPaidQuery 进程内向微信/支付宝主动查单该 AGT 订单是否已支付（对账兜底用）；单测替换为桩。
// provider 决定向哪个平台查单；providerMgr 未装配（如单测直构 App）时返回 errProviderMgrUnset。
var agtOrderPaidQuery = func(a *App, ctx context.Context, orderNo, provider string) (bool, error) {
	if a.providerMgr == nil {
		return false, errProviderMgrUnset
	}
	return a.providerMgr.QueryOrder(ctx, payment.Provider(provider), orderNo)
}

// activatePaidAgtHook 激活一笔已确认支付的 AGT 订单（对账兜底用）；单测替换为计数桩。
// 默认指向幂等的 ActivatePaidAgentPlanOrder（重复激活只生效一次）。主动查单已确认平台已收款，
// 故跳过金额比对（paidAmountCNY=0，amountMatchesCNY 对 <=0 返回 true 即跳过）。
var activatePaidAgtHook = func(a *App, ctx context.Context, orderNo string) error {
	return a.ActivatePaidAgentPlanOrder(ctx, orderNo, 0)
}

// ReconcileAgtResult 汇总一次 AGT 卡单对账结果（供 admin 端点 / 日志 / 历史展示）。字段语义对齐 ReconcileSubResult。
type ReconcileAgtResult struct {
	Scanned   int               // 扫到的 pending 代理套餐单数
	Activated []string          // 查到已付 → 补激活成功
	Unpaid    []string          // 查到真未付（用户没付）且未超时 → 留待下次
	Expired   []string          // 下单超 agtReconcileExpireAge 仍未付 / 网关查无此单 → 置终态 expired（停止重试与告警）
	Failed    map[string]string // 查单/激活的**瞬时**错误 → 留待下次再扫（不含已过期的终态单）
}

// ReconcileStuckAgentPlans 扫 pending 代理套餐卡单，逐笔向平台主动查单确认是否已付（ActivatePaidAgentPlanOrder
// 幂等）：已付 → 补激活；确认未付且超时 / 网关查无此单 → 终态过期；瞬时错误（网络/超时/限流）仍在窗口内 →
// 留 Failed 下轮重试，**绝不因瞬时错误误杀已付单**。before 通常取 now-5min，过滤刚下单、正常流程仍在途的单。
func (a *App) ReconcileStuckAgentPlans(ctx context.Context, before time.Time) (ReconcileAgtResult, error) {
	var rows []agentPlanOrderRow
	if err := a.DB.WithContext(ctx).
		Where("status = ? AND updated_at < ?", agtOrderPending, before).
		Find(&rows).Error; err != nil {
		return ReconcileAgtResult{}, err
	}
	res := ReconcileAgtResult{Scanned: len(rows), Failed: map[string]string{}}
	now := before.Add(reconcileMinAge)              // before 恒为 now-reconcileMinAge（cron/manual 一致），反推当前时刻
	expireCutoff := now.Add(-agtReconcileExpireAge) // 下单早于此 = 已超 2h（二维码失效）
	maxAgeCutoff := now.Add(-agtReconcileMaxAge)    // 下单早于此 = 已超 26h（极旧，查也白查）
	for _, row := range rows {
		expired := row.CreatedAt.Before(expireCutoff) // 下单已超 2h：二维码失效、永不会被支付
		ancient := row.CreatedAt.Before(maxAgeCutoff) // 下单已超 26h：极旧，网关拿不到答复也兜底过期
		paid, err := agtOrderPaidQuery(a, ctx, row.OrderNo, row.Provider)
		switch {
		case err != nil:
			// 网关明确「查无此单」(ORDER_NOT_EXIST/TRADE_NOT_EXIST) 且已超 2h，或下单已超 maxAge（极旧、
			// 网关持续超时/限流拿不到答复也无妨——若曾支付早被前面数百轮捕获激活）→ 终态过期；其余瞬时错误
			// 仍在窗口内 → 留 Failed 下轮重试，绝不误杀。
			if ancient || (expired && errors.Is(err, payment.ErrOrderNotExist)) {
				a.expireStuckAgentPlanOrder(ctx, row.OrderNo)
				res.Expired = append(res.Expired, row.OrderNo)
			} else {
				res.Failed[row.OrderNo] = "query: " + err.Error()
			}
		case paid:
			// 已付 → 幂等激活（无论下单多旧都补，保证绝不漏真实付款）。
			if err := activatePaidAgtHook(a, ctx, row.OrderNo); err != nil {
				res.Failed[row.OrderNo] = "activate: " + err.Error()
				continue
			}
			res.Activated = append(res.Activated, row.OrderNo)
		case expired:
			// 确认未付且已超时 → 终态过期（二维码失效、永不会付），停止扫描与告警。
			a.expireStuckAgentPlanOrder(ctx, row.OrderNo)
			res.Expired = append(res.Expired, row.OrderNo)
		default:
			// 未付但仍在有效窗口（未超时）：留待下次。
			res.Unpaid = append(res.Unpaid, row.OrderNo)
		}
	}
	return res, nil
}

// expireStuckAgentPlanOrder 把一笔卡死的 pending 代理套餐单置终态 agtOrderExpired（条件 CAS：仅在仍 pending
// 时更新，防与并发真实激活竞态——即便本函数与一笔迟到的真实回调激活同时发生，CAS 也只有一方成功）。
// best-effort：写失败仅下轮再来，绝不影响对账其余单。
func (a *App) expireStuckAgentPlanOrder(ctx context.Context, orderNo string) {
	_ = a.DB.WithContext(ctx).Model(&agentPlanOrderRow{}).
		Where("order_no = ? AND status = ?", orderNo, agtOrderPending).
		Updates(map[string]any{"status": agtOrderExpired, "updated_at": time.Now()}).Error
}

// listStuckAgentPlans 只读列出卡在 pending（早于 before）的代理套餐订单，供 admin「支付对账」页展示。
func (a *App) listStuckAgentPlans(ctx context.Context, before time.Time) ([]agentPlanOrderRow, error) {
	var rows []agentPlanOrderRow
	err := a.DB.WithContext(ctx).
		Where("status = ? AND updated_at < ?", agtOrderPending, before).
		Find(&rows).Error
	return rows, err
}
