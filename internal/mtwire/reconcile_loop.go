package mtwire

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	reconcileTickInterval = 5 * time.Minute  // 对账扫描周期
	reconcileMinAge       = 5 * time.Minute   // 只对账「落单/占位超过此时长」的卡单，过滤仍在途的订单
	reconcileCreatedMaxAge = 26 * time.Hour   // created 卡单主动查单的最大年龄（超此视为过期废弃单，停止查单）
	reconcileCreatedLimit  = 200              // created 卡单单轮主动查单上限
)

var (
	reconcileOnce    sync.Once
	reconcileRunning atomic.Bool
)

// StartReconcileLoop 启动支付卡单对账兜底定时任务（仅 master 节点，sync.Once 保证只起一次）：
// 每 reconcileTickInterval 扫一次 RCG 充值卡单（重跑入账）+ SUB 套餐卡单（查单→补激活）。
// 全程 best-effort：幂等 + atomic 防重入，失败仅记日志，绝不影响主流程。
// 由 App 装配完成后（InstallHooks 同处）调用。
func (a *App) StartReconcileLoop() {
	reconcileOnce.Do(func() {
		if !common.IsMasterNode {
			return // 非主节点不跑，避免多副本重复对账
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "payment reconcile loop started: tick="+reconcileTickInterval.String())
			ticker := time.NewTicker(reconcileTickInterval)
			defer ticker.Stop()
			a.runReconcileOnce() // 启动即先跑一轮，不必干等一个周期
			for range ticker.C {
				a.runReconcileOnce()
			}
		})
	})
}

// runReconcileOnce 跑一轮 RCG + SUB 对账（atomic 防与上一轮重叠：上一轮还没跑完则跳过本次）。
func (a *App) runReconcileOnce() {
	if !reconcileRunning.CompareAndSwap(false, true) {
		return
	}
	defer reconcileRunning.Store(false)

	ctx := context.Background()
	before := time.Now().Add(-reconcileMinAge)

	// RCG 充值卡单：扫 paid 未 credited → 重跑入账（幂等）。
	if a.RechargeGateway != nil {
		if res, err := a.RechargeGateway.ReconcileStuckPaid(ctx, before); err != nil {
			logger.LogWarn(ctx, "reconcile RCG(paid) failed: "+err.Error())
		} else if len(res.Reconciled) > 0 || len(res.Failed) > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(paid): scanned=%d credited=%d failed=%d",
				res.Scanned, len(res.Reconciled), len(res.Failed)))
		}
	}

	// RCG 充值卡单：扫 created（回调始终未送达）→ 向平台主动查单 → 已付补入账。
	if a.RechargeGateway != nil && a.authClient != nil {
		query := func(ctx context.Context, orderNo, provider string) (bool, error) {
			return a.authClient.QueryOrderStatus(ctx, orderNo, provider)
		}
		if res, err := a.RechargeGateway.ReconcileStuckCreated(ctx, before, reconcileCreatedMaxAge, reconcileCreatedLimit, query); err != nil {
			logger.LogWarn(ctx, "reconcile RCG(created) failed: "+err.Error())
		} else if len(res.Reconciled) > 0 || len(res.Failed) > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(created): scanned=%d credited=%d failed=%d",
				res.Scanned, len(res.Reconciled), len(res.Failed)))
		}
	}

	// SUB 套餐卡单：扫 pending → 查单 → 已付则补激活。
	if res, err := a.ReconcileStuckSubscriptions(ctx, before); err != nil {
		logger.LogWarn(ctx, "reconcile SUB failed: "+err.Error())
	} else if len(res.Activated) > 0 || len(res.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile SUB: scanned=%d activated=%d unpaid=%d failed=%d",
			res.Scanned, len(res.Activated), len(res.Unpaid), len(res.Failed)))
	}
}
