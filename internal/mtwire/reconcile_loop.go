package mtwire

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	reconcileTickInterval  = 5 * time.Minute // 对账扫描周期
	reconcileMinAge        = 5 * time.Minute // 只对账「落单/占位超过此时长」的卡单，过滤仍在途的订单
	reconcileCreatedMaxAge = 26 * time.Hour  // created 单超此年龄：不再查单、直接置 failed（平台单/二维码早已作废，查也白查）
	// reconcileCreatedExpireAge：created 未付超过此时长 → 对账查证仍未付即自动置 failed（未付超时/过期），
	// 清出「待支付」。贴微信 Native 二维码默认有效期 2h（超时二维码作废、无人能再付）。
	reconcileCreatedExpireAge = 2 * time.Hour
	reconcileCreatedLimit     = 200 // created 卡单单轮主动查单上限
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

// runReconcileOnce 跑一轮全 3 路径对账（cron 触发），atomic 防与上一轮重叠：上一轮还没跑完则跳过本次。
// 编排/日志/心跳/历史统一在 runReconcileAll；手动触发（HandleAdminRunReconcile）共用同一入口。
func (a *App) runReconcileOnce() {
	if !reconcileRunning.CompareAndSwap(false, true) {
		return
	}
	defer reconcileRunning.Store(false)

	ctx := context.Background()
	before := time.Now().Add(-reconcileMinAge)
	a.runReconcileAll(ctx, before, "cron")
}
