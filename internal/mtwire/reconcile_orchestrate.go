package mtwire

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/logger"
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
	return a.RechargeGateway.ReconcileStuckCreated(ctx, before, reconcileCreatedExpireAge, reconcileCreatedMaxAge, reconcileCreatedLimit, query)
}

var reconcileSubFn = func(a *App, ctx context.Context, before time.Time) (ReconcileSubResult, error) {
	return a.ReconcileStuckSubscriptions(ctx, before)
}

// runReconcileAll 是定时(cron)与手动(manual)统一入口：依次跑 RCG-paid ① / RCG-created ② / SUB ③
// 三条路径，聚合结果 → 每轮 upsert 心跳 → 手动总记 or 有实事时落一条历史（均 best-effort，绝不影响对账）。
// 手动经此入口自动补上过去漏跑的 ②（修 drift）。日志保留原 cron 逐路径格式，错误折进 Failed["_error"]。
func (a *App) runReconcileAll(ctx context.Context, before time.Time, trigger string) (paid, created payment.ReconcileResult, sub ReconcileSubResult) {
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

	stuck := paid.Scanned + created.Scanned + sub.Scanned
	failed := len(paid.Failed) + len(created.Failed) + len(sub.Failed)
	prevRun := a.updateReconcileHeartbeat(ctx, trigger, stuck, failed)
	a.alertReconcileHealth(ctx, trigger, prevRun, failed, paid, created, sub)

	if trigger == "manual" || reconcileHasFacts(paid, created, sub) {
		a.recordReconcileRun(ctx, trigger, paid, created, sub)
	}
	return paid, created, sub
}

// alertReconcileHealth 对账健康告警（best-effort，经 AlertSink；未装配 sink 则跳过，不影响对账）：
//   - 陈旧（仅 cron）：本轮开跑说明循环仍活着，但若距上一轮心跳 > reconcileStaleAfter，说明期间漏跑了
//     ≥2 轮（上游查单挂起被整轮超时切断 / gopool 延迟 / 进程卡顿），现已恢复——补上「循环停摆无人知」缺口。
//     手动触发不判陈旧（ad-hoc，间隔不代表停滞）。
//   - 失败：本轮任一路径有失败（线上实证每 5min 3 笔 SUB 对账失败此前零通知）。
//
// 两类均用 Critical + DedupKey：AlertSink 未启用/未配收件人时由 Sink 的 Critical 兜底日志留痕（见
// internal/alert/sink.go，绝不静默），启用后走邮件/webhook；DedupKey 令去重窗口内只发一次，防每 5min 轰炸。
func (a *App) alertReconcileHealth(ctx context.Context, trigger string, prevRun time.Time, failed int, paid, created payment.ReconcileResult, sub ReconcileSubResult) {
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
			Body: fmt.Sprintf("本轮对账 %d 笔失败：RCG-paid %d / RCG-created %d / SUB %d。已付未入账订单将于下轮重试，持续失败需人工核查对账记录。",
				failed, len(paid.Failed), len(created.Failed), len(sub.Failed)),
			DedupKey: "reconcile_failed",
		})
	}
}
