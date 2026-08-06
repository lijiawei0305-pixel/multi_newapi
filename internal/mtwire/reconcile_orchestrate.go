package mtwire

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/payment/realpay"
	"github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

func realpayPrepayDriverEnabled() bool { return realpay.PrepayDriverEnabled() }

func withRealpayBgBudget(ctx context.Context) context.Context {
	return realpay.WithBudgetMode(ctx, realpay.BudgetBackground)
}

// 三路径 seam（仿 subOrderPaidQuery/activatePaidSubHook 的包级钩子）：默认接真实网关，单测替换为桩。
var reconcilePaidFn = func(a *App, ctx context.Context, before time.Time) (payment.ReconcileResult, error) {
	if a.RechargeGateway == nil {
		return payment.ReconcileResult{}, nil
	}
	return a.RechargeGateway.ReconcileStuckPaid(ctx, before)
}

var reconcileCreatedFn = func(a *App, ctx context.Context, before time.Time) (payment.ReconcileResult, error) {
	if a.RechargeGateway == nil || a.providerMgr == nil {
		return payment.ReconcileResult{}, nil
	}
	query := func(ctx context.Context, orderNo, provider string) (*payment.QueryResult, error) {
		return a.providerMgr.QueryOrder(ctx, payment.Provider(provider), orderNo)
	}
	return a.RechargeGateway.ReconcileStuckCreated(ctx, before, reconcileCreatedExpireAge, reconcileCreatedLimit, query)
}

// reconcileDueQueryFn 5s/30s/60s 持久化 next_query_at 到期查单（PAY-REC-01）。
var reconcileDueQueryFn = func(a *App, ctx context.Context) (payment.ReconcileResult, error) {
	if a.RechargeGateway == nil || a.providerMgr == nil {
		return payment.ReconcileResult{}, nil
	}
	query := func(ctx context.Context, orderNo, provider string) (*payment.QueryResult, error) {
		return a.providerMgr.QueryOrder(ctx, payment.Provider(provider), orderNo)
	}
	return a.RechargeGateway.ReconcileDueQueries(ctx, reconcileCreatedLimit, query)
}

// reconcilePendingPrepayFn P0-B：local_created && pay_url 空 → 后台补 Prepay（宽预算）。
// PAYMENT_PREPAY_DRIVER_ENABLED=false 可关。
var reconcilePendingPrepayFn = func(a *App, ctx context.Context) (payment.ReconcileResult, error) {
	if a.RechargeGateway == nil {
		return payment.ReconcileResult{}, nil
	}
	if !realpayPrepayDriverEnabled() {
		return payment.ReconcileResult{}, nil
	}
	bg := payment.WithBackgroundPrepay(ctx)
	bg = withRealpayBgBudget(bg)
	return a.RechargeGateway.ReconcilePendingPrepay(bg, 50)
}

var reconcileSubFn = func(a *App, ctx context.Context, before time.Time) (ReconcileSubResult, error) {
	return a.ReconcileStuckSubscriptions(ctx, before)
}

var reconcileAgtFn = func(a *App, ctx context.Context, before time.Time) (ReconcileAgtResult, error) {
	return a.ReconcileStuckAgentPlans(ctx, before)
}

// runReconcileAll 是定时(cron)与手动(manual)统一入口：依次跑 RCG-paid ① / RCG-created ② / SUB ③ /
// AGT ④ 四条路径，聚合结果 → 每轮 upsert 心跳 → 手动总记 or 有实事时落一条历史（均 best-effort，绝不
// 影响对账）。手动经此入口自动补上过去漏跑的 ②（修 drift）。日志保留原 cron 逐路径格式，错误折进
// Failed["_error"]。AGT ④ 为全站最贵 SKU 的对账兜底，此前唯一缺失，2026-07-16 补齐至与 SUB 对等。
//
// 各路径使用独立 min-age（PAY-REC-01）：paid 30s、created 60s、SUB/AGT 仍 5min。
func (a *App) runReconcileAll(ctx context.Context, trigger string) (paid, created payment.ReconcileResult, sub ReconcileSubResult, agt ReconcileAgtResult) {
	a.runBillingReconcileSideJobs(ctx)
	now := time.Now()
	// 先跑 next_query_at 到期快查（5s/30s/60s），再跑 stuck-paid 与全量 created 扫尾。
	a.runReconcileDueQueryPath(ctx)
	paid = a.runReconcilePaidPath(ctx, now.Add(-reconcilePaidMinAge))
	created, sub, agt = a.runReconcileNonPaidPaths(ctx, now)
	a.finishReconcileRun(ctx, trigger, paid, created, sub, agt)
	return paid, created, sub, agt
}

// runReconcileAllExceptPaid 在 stuck-paid 快路径占用时由全量轮调用：跳过 paid，只扫其余路径。
func (a *App) runReconcileAllExceptPaid(ctx context.Context, trigger string) {
	a.runBillingReconcileSideJobs(ctx)
	a.runReconcileDueQueryPath(ctx)
	var paid payment.ReconcileResult
	created, sub, agt := a.runReconcileNonPaidPaths(ctx, time.Now())
	a.finishReconcileRun(ctx, trigger, paid, created, sub, agt)
}

func (a *App) runReconcileDueQueryPath(ctx context.Context) {
	// P0-B：先补 Prepay（缺码），再查单（有码/未知态）
	a.runReconcilePendingPrepayPath(ctx)
	due, err := reconcileDueQueryFn(a, ctx)
	if err != nil {
		logger.LogWarn(ctx, "reconcile RCG(due-query) failed: "+err.Error())
		return
	}
	if len(due.Reconciled) > 0 || len(due.Expired) > 0 || len(due.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(due-query): scanned=%d credited=%d expired=%d failed=%d",
			due.Scanned, len(due.Reconciled), len(due.Expired), len(due.Failed)))
	}
}

func (a *App) runReconcilePendingPrepayPath(ctx context.Context) {
	pre, err := reconcilePendingPrepayFn(a, ctx)
	if err != nil {
		logger.LogWarn(ctx, "reconcile RCG(pending-prepay) failed: "+err.Error())
		// C5：驱动器本身失败进告警
		if a.AlertSink != nil {
			_ = a.AlertSink.Dispatch(ctx, alert.Alert{
				Level:    alert.LevelCritical,
				Subject:  "Prepay 驱动器失败",
				Body:     "reconcile pending-prepay: " + err.Error(),
				DedupKey: "prepay_driver_error",
			})
		}
		return
	}
	if pre.Scanned > 0 || len(pre.Reconciled) > 0 || len(pre.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(pending-prepay): scanned=%d ready=%d failed=%d",
			pre.Scanned, len(pre.Reconciled), len(pre.Failed)))
	}
	// C5：补下单失败（含 unknown 持续）进告警；definitive 与 unknown 都计
	if n := len(pre.Failed); n > 0 && a.AlertSink != nil {
		_ = a.AlertSink.Dispatch(ctx, alert.Alert{
			Level:   alert.LevelCritical,
			Subject: "Prepay 补下单失败",
			Body: fmt.Sprintf("本轮 pending-prepay %d 笔未出码（scanned=%d ready=%d）。unknown 将重试；definitive 见卡单页。",
				n, pre.Scanned, len(pre.Reconciled)),
			DedupKey: "prepay_driver_failed",
		})
	}
}

func (a *App) runBillingReconcileSideJobs(ctx context.Context) {
	if model.DB != nil {
		processCacheInvalidationOutbox(ctx, model.DB, 100)
		if _, err := model.ReconcilePendingBillingAdjustments(200); err != nil {
			logger.LogWarn(ctx, "reconcile billing adjustment intents failed: "+err.Error())
		}
		if _, err := model.ReconcilePendingBillingSettlements(200); err != nil {
			logger.LogWarn(ctx, "reconcile billing settlement financial intents failed: "+err.Error())
		}
		if _, err := model.ReconcilePendingBillingProjections(200); err != nil {
			logger.LogWarn(ctx, "reconcile billing projections failed: "+err.Error())
		}
		if err := a.reconcileBillingCommissions(ctx, 200); err != nil {
			logger.LogWarn(ctx, "reconcile billing commissions failed: "+err.Error())
		}
	}
	earnings, earningsErr := a.reconcilePayableEarningIntents(ctx, 200)
	if earningsErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("reconcile payable earnings failed: scanned=%d applied=%d failed=%d pending=%d error=%v",
			earnings.Scanned, earnings.Applied, earnings.Failed, earnings.Pending, earningsErr))
	} else if earnings.Scanned > 0 || earnings.Pending > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile payable earnings: scanned=%d applied=%d failed=%d pending=%d",
			earnings.Scanned, earnings.Applied, earnings.Failed, earnings.Pending))
	}
}

func (a *App) runReconcilePaidPath(ctx context.Context, before time.Time) payment.ReconcileResult {
	paid, perr := reconcilePaidFn(a, ctx, before)
	if perr != nil {
		logger.LogWarn(ctx, "reconcile RCG(paid) failed: "+perr.Error())
		paid.Failed = map[string]string{"_error": perr.Error()}
	} else if len(paid.Reconciled) > 0 || len(paid.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(paid): scanned=%d credited=%d failed=%d",
			paid.Scanned, len(paid.Reconciled), len(paid.Failed)))
	}
	return paid
}

func (a *App) runReconcileNonPaidPaths(ctx context.Context, now time.Time) (created payment.ReconcileResult, sub ReconcileSubResult, agt ReconcileAgtResult) {
	createdBefore := now.Add(-reconcileCreatedMinAge)
	legacyBefore := now.Add(-reconcileMinAge)

	var cerr error
	created, cerr = reconcileCreatedFn(a, ctx, createdBefore)
	if cerr != nil {
		logger.LogWarn(ctx, "reconcile RCG(created) failed: "+cerr.Error())
		created.Failed = map[string]string{"_error": cerr.Error()}
	} else if len(created.Reconciled) > 0 || len(created.Expired) > 0 || len(created.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(created): scanned=%d credited=%d expired=%d failed=%d",
			created.Scanned, len(created.Reconciled), len(created.Expired), len(created.Failed)))
	}

	var serr error
	sub, serr = reconcileSubFn(a, ctx, legacyBefore)
	if serr != nil {
		logger.LogWarn(ctx, "reconcile SUB failed: "+serr.Error())
		sub.Failed = map[string]string{"_error": serr.Error()}
	} else if len(sub.Activated) > 0 || len(sub.Expired) > 0 || len(sub.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile SUB: scanned=%d activated=%d unpaid=%d expired=%d failed=%d",
			sub.Scanned, len(sub.Activated), len(sub.Unpaid), len(sub.Expired), len(sub.Failed)))
	}

	var aerr error
	agt, aerr = reconcileAgtFn(a, ctx, legacyBefore)
	if aerr != nil {
		logger.LogWarn(ctx, "reconcile AGT failed: "+aerr.Error())
		agt.Failed = map[string]string{"_error": aerr.Error()}
	} else if len(agt.Activated) > 0 || len(agt.Expired) > 0 || len(agt.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile AGT: scanned=%d activated=%d unpaid=%d expired=%d failed=%d",
			agt.Scanned, len(agt.Activated), len(agt.Unpaid), len(agt.Expired), len(agt.Failed)))
	}
	return created, sub, agt
}

func (a *App) finishReconcileRun(ctx context.Context, trigger string, paid, created payment.ReconcileResult, sub ReconcileSubResult, agt ReconcileAgtResult) {
	stuck := paid.Scanned + created.Scanned + sub.Scanned + agt.Scanned
	failed := len(paid.Failed) + len(created.Failed) + len(sub.Failed) + len(agt.Failed)
	prevRun := a.updateReconcileHeartbeat(ctx, trigger, stuck, failed)
	a.alertReconcileHealth(ctx, trigger, prevRun, failed, paid, created, sub, agt)

	if trigger == "manual" || reconcileHasFacts(paid, created, sub, agt) {
		a.recordReconcileRun(ctx, trigger, paid, created, sub, agt)
	}
}

func (a *App) reconcileBillingCommissions(ctx context.Context, limit int) error {
	resolutions, err := model.ListPendingBillingCommissionResolutions(limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, event := range resolutions {
		sourceID := model.BillingCommissionSourceId(event.RequestId, event.Operation)
		var snapshot agenthook.CommissionSnapshot
		var prepareErr error
		if event.CommissionPolicy != "" {
			var policy agenthook.CommissionPolicy
			if prepareErr = common.UnmarshalJsonStr(event.CommissionPolicy, &policy); prepareErr == nil {
				snapshot, prepareErr = materializeConsumeCommissionPolicy(policy, int64(event.FinalQuota), sourceID)
			}
		} else {
			snapshot, prepareErr = a.prepareConsumeCommission(int64(event.UserId), int64(event.FinalQuota), sourceID, event.FundingSource, event.UsingGroup, event.ChargedGroupRatio)
		}
		if prepareErr != nil {
			model.MarkBillingSettlementError(event.RequestId, event.Operation, prepareErr)
			if firstErr == nil {
				firstErr = prepareErr
			}
			continue
		}
		attachErr := model.AttachBillingCommissionSnapshot(event.RequestId, event.Operation, model.BillingCommissionSnapshot{
			SourceId: snapshot.SourceID, OccurredAt: snapshot.OccurredAt,
			WalletTenantId: snapshot.WalletTenantID, WalletUserId: snapshot.WalletUserID, WalletQuota: snapshot.WalletQuota,
			EarningApplicable: snapshot.EarningApplicable, EarningTenantId: snapshot.EarningTenantID, EarningUserId: snapshot.EarningUserID,
			EarningSourceType: snapshot.EarningSourceType, EarningAmount: snapshot.EarningAmount, EarningRemark: snapshot.EarningRemark,
		})
		if attachErr != nil {
			model.MarkBillingSettlementError(event.RequestId, event.Operation, attachErr)
			if firstErr == nil {
				firstErr = attachErr
			}
		}
	}
	pending, err := model.ListPendingBillingCommissionEvents(limit)
	if err != nil {
		if firstErr != nil {
			return firstErr
		}
		return err
	}
	for _, event := range pending {
		s := event.Snapshot
		persistErr := a.persistConsumeCommission(agenthook.CommissionSnapshot{
			SourceID: s.SourceId, OccurredAt: s.OccurredAt,
			WalletTenantID: s.WalletTenantId, WalletUserID: s.WalletUserId, WalletQuota: s.WalletQuota,
			EarningApplicable: s.EarningApplicable, EarningTenantID: s.EarningTenantId, EarningUserID: s.EarningUserId,
			EarningSourceType: s.EarningSourceType, EarningAmount: s.EarningAmount, EarningRemark: s.EarningRemark,
		})
		if persistErr == nil {
			persistErr = model.MarkBillingCommissionDispatched(event.RequestId, event.Operation)
		}
		if persistErr != nil {
			model.MarkBillingSettlementError(event.RequestId, event.Operation, persistErr)
			if firstErr == nil {
				firstErr = persistErr
			}
		}
	}
	return firstErr
}

// alertReconcileHealth 对账健康告警（best-effort，经 AlertSink；未装配 sink 则跳过，不影响对账）：
//   - 陈旧（仅 cron）：本轮开跑说明循环仍活着，但若距上一轮心跳 > reconcileStaleAfter，说明期间漏跑了
//     ≥2 轮（上游查单挂起被整轮超时切断 / gopool 延迟 / 进程卡顿），现已恢复——补上「循环停摆无人知」缺口。
//     手动触发不判陈旧（ad-hoc，间隔不代表停滞）。
//   - 失败：本轮任一路径有失败（线上实证每 5min 3 笔 SUB 对账失败此前零通知）。
//
// 两类均用 Critical + DedupKey：AlertSink 未启用/未配收件人时由 Sink 的 Critical 兜底日志留痕（见
// internal/alert/sink.go，绝不静默），启用后走邮件/webhook；DedupKey 令去重窗口内只发一次，防每 5min 轰炸。
func (a *App) alertReconcileHealth(ctx context.Context, trigger string, prevRun time.Time, failed int, paid, created payment.ReconcileResult, sub ReconcileSubResult, agt ReconcileAgtResult) {
	if a.AlertSink == nil {
		return
	}
	if trigger == "cron" && !prevRun.IsZero() {
		if gap := time.Since(prevRun); gap > reconcileStaleAfter {
			_ = a.AlertSink.Dispatch(ctx, alert.Alert{
				Level:   alert.LevelCritical,
				Subject: "对账循环曾停滞",
				Body: fmt.Sprintf("对账循环距上一轮已 %s（正常每 %s 一轮，约漏跑 %d 轮），现已恢复。请排查该时段上游查单是否挂起或进程卡顿。",
					gap.Round(time.Second), reconcileTickInterval, int(gap/reconcileTickInterval)),
				DedupKey: "reconcile_stalled",
			})
		}
	}
	if failed > 0 {
		_ = a.AlertSink.Dispatch(ctx, alert.Alert{
			Level:   alert.LevelCritical,
			Subject: "对账失败告警",
			Body: fmt.Sprintf("本轮对账 %d 笔失败：RCG-paid %d / RCG-created %d / SUB %d / AGT %d。已付未入账订单将于下轮重试，持续失败需人工核查对账记录。",
				failed, len(paid.Failed), len(created.Failed), len(sub.Failed), len(agt.Failed)),
			DedupKey: "reconcile_failed",
		})
	}
	// P2 SLI：熔断开路 / TLS 成功率过低 → 健康告警（供 egress Go/No-Go 对照）
	a.alertPaymentSLI(ctx)
}

// alertPaymentSLI 支付出口 SLI 告警（breaker / TLS / 复用率）。
func (a *App) alertPaymentSLI(ctx context.Context) {
	if a.AlertSink == nil {
		return
	}
	if realpay.BreakerIsOpen() {
		snap := realpay.SnapshotMetrics()
		_ = a.AlertSink.Dispatch(ctx, alert.Alert{
			Level:   alert.LevelCritical,
			Subject: "支付 Prepay 熔断开路",
			Body: fmt.Sprintf("连续 unknown 触发熔断，同步下单将直接 queued。breaker_open_seconds=%.1f unknown_streak=%d prepay_sync_p95_ms=%d conn_reuse_rate=%.2f tls_success_rate=%.2f",
				snap.BreakerOpenSeconds, snap.UnknownStreak, snap.PrepaySyncP95Ms, snap.ConnReuseRate, snap.TLSSuccessRate),
			DedupKey: "payment_breaker_open",
		})
	}
	snap := realpay.SnapshotMetrics()
	// 样本足够且 TLS 成功率 < 99% 时告警（对齐 payment-egress-ops-plan §7）
	if snap.TLSSuccessRate >= 0 && snap.TLSSuccessRate < 0.99 {
		_ = a.AlertSink.Dispatch(ctx, alert.Alert{
			Level:   alert.LevelCritical,
			Subject: "支付 TLS 成功率低于门禁",
			Body: fmt.Sprintf("tls_success_rate=%.3f（门禁≥0.99）；cold+reused 样本不足时不报。conn_reuse_rate=%.3f prepay_sync_p95_ms=%d prepay_bg_p95_ms=%d",
				snap.TLSSuccessRate, snap.ConnReuseRate, snap.PrepaySyncP95Ms, snap.PrepayBgP95Ms),
			DedupKey: "payment_tls_sli",
		})
	}
}
