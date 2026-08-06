package mtwire

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
	"gorm.io/gorm"
)

// AGT 代理套餐是全站客单价最高的 SKU（¥990–9990），却曾是唯一「无对账兜底」的付费订单类型：RCG/SUB
// 均有主动查单 + 超时置终态 + 卡单可见 + 失败告警，AGT 一层都没有——真实失败场景（如 api_v3_key 轮换
// 配错致回调全部验签失败）下，RCG/SUB 会被对账循环持续查单/救回并在卡单页可见、失败即告警，而 AGT 会
// 静默永停 pending，无任何页面可见、无任何告警，唯一发现途径是手工 SELECT。本文件把 AGT 补齐到与 SUB
// **完全对等**的保护，语义/阈值与 sub_reconcile.go 一一对齐。另扫描陈旧 activating：该状态本身是
// 已确认付款的持久事实，直接恢复开通，不再向网关重复判断未付。

// agtReconcileExpireAge：pending AGT 单「过期兜底」阈值（对齐 subReconcileExpireAge）。微信 Native 二维码
// 有效期 2h——下单超此仍未付或网关查无此单，置本地扫描终态 agtOrderExpired 停止无谓重试与告警。
// 后续可信已付回调仍可恢复；查到已付也无论多旧都幂等激活。
const agtReconcileExpireAge = 2 * time.Hour

// agtOrderPaidQuery 进程内向微信/支付宝主动查单该 AGT 订单是否已支付（对账兜底用）；单测替换为桩。
// provider 决定向哪个平台查单；providerMgr 未装配（如单测直构 App）时返回 errProviderMgrUnset。
var agtOrderPaidQuery = func(a *App, ctx context.Context, orderNo, provider string) (bool, error) {
	if a.providerMgr == nil {
		return false, errProviderMgrUnset
	}
	return a.providerMgr.QueryOrderPaid(ctx, payment.Provider(provider), orderNo)
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

// ReconcileStuckAgentPlans 扫 pending 与陈旧 activating 代理套餐卡单。pending 逐笔向平台主动查单；
// activating 已持久记录可信已付事实，直接恢复开通。before 通常取 now-5min，过滤正常在途认领。
func (a *App) ReconcileStuckAgentPlans(ctx context.Context, before time.Time) (ReconcileAgtResult, error) {
	var rows []agentPlanOrderRow
	if err := a.DB.WithContext(ctx).
		Where("status IN ? AND updated_at < ?", []string{agtOrderPending, agtOrderActivating}, before).
		Find(&rows).Error; err != nil {
		return ReconcileAgtResult{}, err
	}
	res := ReconcileAgtResult{Scanned: len(rows), Failed: map[string]string{}}
	now := before.Add(reconcileMinAge)              // before 恒为 now-reconcileMinAge（cron/manual 一致），反推当前时刻
	expireCutoff := now.Add(-agtReconcileExpireAge) // 下单早于此 = 已超 2h（二维码失效）
	for _, row := range rows {
		if row.Status == agtOrderActivating {
			if err := activatePaidAgtHook(a, ctx, row.OrderNo); err != nil {
				res.Failed[row.OrderNo] = "resume activation: " + err.Error()
			} else {
				res.Activated = append(res.Activated, row.OrderNo)
			}
			continue
		}
		expired := row.CreatedAt.Before(expireCutoff) // 下单已超 2h：二维码失效、永不会被支付
		paid, err := agtOrderPaidQuery(a, ctx, row.OrderNo, row.Provider)
		switch {
		case err != nil:
			// 终态只接受网关**确定性答复**：明确「查无此单」(ORDER_NOT_EXIST/TRADE_NOT_EXIST) 且已超 2h
			// 二维码窗口 → 过期。查单本身失败（超时/限流/凭据不完整 errProviderDisabled）一律留 Failed——
			// 订单保持 pending（卡单页可见）+ 计入 failed（触发 Critical 告警）+ 下轮重扫，直到拿到确定答复。
			// 旧「超 26h ancient 兜底过期」已删（audit 2026-07-17 #7）：其辩护「若曾支付，前面数百轮查单
			// 必已捕获」是循环论证——本分支只在查单失败时进入，凭据轮换配错/渠道停用期间恰恰不存在成功过
			// 的查单轮；它会把已付 ¥9990 静默写成终态且 Failed 为空 → 零告警、卡单页消失、永不重试。代价
			// （刻意）：真废单在网关持续不可查期间一直占卡单页并重复告警——可见的噪音优于无声的钱损；凭据
			// 修复后下一轮即收敛（已付→激活 / 确认未付→过期 / 查无此单→过期）。
			if expired && errors.Is(err, payment.ErrOrderNotExist) {
				won, expireErr := a.expireStuckAgentPlanOrder(ctx, &row)
				if expireErr != nil {
					res.Failed[row.OrderNo] = "expire: " + expireErr.Error()
				} else if won {
					res.Expired = append(res.Expired, row.OrderNo)
				}
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
			won, expireErr := a.expireStuckAgentPlanOrder(ctx, &row)
			if expireErr != nil {
				res.Failed[row.OrderNo] = "expire: " + expireErr.Error()
			} else if won {
				res.Expired = append(res.Expired, row.OrderNo)
			}
		default:
			// 未付但仍在有效窗口（未超时）：留待下次。
			res.Unpaid = append(res.Unpaid, row.OrderNo)
		}
	}
	return res, nil
}

// expireStuckAgentPlanOrder atomically wins pending->expired and releases the
// guarded slug reservation. Keeping both writes in one transaction closes the
// former callback interleave where expiry won the status CAS, a paid callback
// then claimed expired->activating, and the old expiry worker still deleted the
// reservation underneath that durable paid claim.
func (a *App) expireStuckAgentPlanOrder(ctx context.Context, ord *agentPlanOrderRow) (bool, error) {
	won := false
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&agentPlanOrderRow{}).
			Where("order_no = ? AND status = ?", ord.OrderNo, agtOrderPending).
			Updates(map[string]any{"status": agtOrderExpired, "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if err := releaseAgentSlugReservationTx(tx, ord.AgentTenantID); err != nil {
			return err
		}
		won = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return won, nil
}

// listStuckAgentPlans 只读列出卡在 pending/activating（早于 before）的代理套餐订单，供 admin 查看。
func (a *App) listStuckAgentPlans(ctx context.Context, before time.Time) ([]agentPlanOrderRow, error) {
	var rows []agentPlanOrderRow
	err := a.DB.WithContext(ctx).
		Where("status IN ? AND updated_at < ?", []string{agtOrderPending, agtOrderActivating}, before).
		Find(&rows).Error
	return rows, err
}
