package mtwire

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

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
	query := func(ctx context.Context, orderNo, provider string) (bool, error) {
		return a.providerMgr.QueryOrder(ctx, payment.Provider(provider), orderNo)
	}
	return a.RechargeGateway.ReconcileStuckCreated(ctx, before, reconcileCreatedExpireAge, reconcileCreatedLimit, query)
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
func (a *App) runReconcileAll(ctx context.Context, before time.Time, trigger string) (paid, created payment.ReconcileResult, sub ReconcileSubResult, agt ReconcileAgtResult) {
	if model.DB != nil {
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
	paid, perr := reconcilePaidFn(a, ctx, before)
	if perr != nil {
		logger.LogWarn(ctx, "reconcile RCG(paid) failed: "+perr.Error())
		paid.Failed = map[string]string{"_error": perr.Error()}
	} else if len(paid.Reconciled) > 0 || len(paid.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(paid): scanned=%d credited=%d failed=%d",
			paid.Scanned, len(paid.Reconciled), len(paid.Failed)))
	}

	created, cerr := reconcileCreatedFn(a, ctx, before)
	if cerr != nil {
		logger.LogWarn(ctx, "reconcile RCG(created) failed: "+cerr.Error())
		created.Failed = map[string]string{"_error": cerr.Error()}
	} else if len(created.Reconciled) > 0 || len(created.Expired) > 0 || len(created.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(created): scanned=%d credited=%d expired=%d failed=%d",
			created.Scanned, len(created.Reconciled), len(created.Expired), len(created.Failed)))
	}

	sub, serr := reconcileSubFn(a, ctx, before)
	if serr != nil {
		logger.LogWarn(ctx, "reconcile SUB failed: "+serr.Error())
		sub.Failed = map[string]string{"_error": serr.Error()}
	} else if len(sub.Activated) > 0 || len(sub.Expired) > 0 || len(sub.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile SUB: scanned=%d activated=%d unpaid=%d expired=%d failed=%d",
			sub.Scanned, len(sub.Activated), len(sub.Unpaid), len(sub.Expired), len(sub.Failed)))
	}

	agt, aerr := reconcileAgtFn(a, ctx, before)
	if aerr != nil {
		logger.LogWarn(ctx, "reconcile AGT failed: "+aerr.Error())
		agt.Failed = map[string]string{"_error": aerr.Error()}
	} else if len(agt.Activated) > 0 || len(agt.Expired) > 0 || len(agt.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile AGT: scanned=%d activated=%d unpaid=%d expired=%d failed=%d",
			agt.Scanned, len(agt.Activated), len(agt.Unpaid), len(agt.Expired), len(agt.Failed)))
	}

	stuck := paid.Scanned + created.Scanned + sub.Scanned + agt.Scanned
	failed := len(paid.Failed) + len(created.Failed) + len(sub.Failed) + len(agt.Failed)
	prevRun := a.updateReconcileHeartbeat(ctx, trigger, stuck, failed)
	a.alertReconcileHealth(ctx, trigger, prevRun, failed, paid, created, sub, agt)

	if trigger == "manual" || reconcileHasFacts(paid, created, sub, agt) {
		a.recordReconcileRun(ctx, trigger, paid, created, sub, agt)
	}
	return paid, created, sub, agt
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
}
