package mtwire

import (
	"context"
	"fmt"
	"time"

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
	} else if len(sub.Activated) > 0 || len(sub.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile SUB: scanned=%d activated=%d unpaid=%d failed=%d",
			sub.Scanned, len(sub.Activated), len(sub.Unpaid), len(sub.Failed)))
	}

	stuck := paid.Scanned + created.Scanned + sub.Scanned
	failed := len(paid.Failed) + len(created.Failed) + len(sub.Failed)
	a.updateReconcileHeartbeat(ctx, trigger, stuck, failed)

	if trigger == "manual" || reconcileHasFacts(paid, created, sub) {
		a.recordReconcileRun(ctx, trigger, paid, created, sub)
	}
	return paid, created, sub
}
