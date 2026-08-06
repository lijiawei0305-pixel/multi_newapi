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
	reconcileTickInterval = 5 * time.Minute // 全量对账扫描周期（created / SUB / AGT 扫尾）
	reconcileMinAge       = 5 * time.Minute // created/SUB/AGT：只对账「落单超过此时长」的卡单
	// reconcilePaidMinAge：RCG stuck-paid 可更短——入账中间态 paid 应秒级恢复，不必等 5 分钟
	// （PAY-REC-01 / PAY-TXN-01）。OnPaid 强幂等，与回调竞态不会双扣。
	reconcilePaidMinAge = 30 * time.Second
	// reconcilePaidTickInterval：stuck-paid + next_query_at 到期查单的快路径周期。
	reconcilePaidTickInterval = 5 * time.Second
	// reconcileCreatedMinAge：created 主动查单最短年龄。低于全量 5min，使回调失达后约 1–1.5min
	// 内有机会被查单补账（完整 5s/30s/60s 持久化调度见后续 next_query_at 方案）。
	reconcileCreatedMinAge = 60 * time.Second
	// reconcileCreatedExpireAge：created 未付超过此时长 → 对账查证仍未付即自动置 failed（未付超时/过期），
	// 清出「待支付」。贴微信 Native 二维码默认有效期 2h（超时二维码作废、无人能再付）。
	reconcileCreatedExpireAge = 2 * time.Hour
	reconcileCreatedLimit     = 200 // created 卡单单轮主动查单上限
	// reconcileRunTimeout：单轮对账整轮超时上限。上游 QueryOrder 挂起时，防 reconcileRunning 永久为真、
	// 心跳冻结、循环"看着活着却什么都不对账"。须 < reconcileTickInterval（下一轮能重新起跑）且远长于
	// 健康轮（正常几秒~几十秒）；超时即取消本轮，幂等，未完成项下一轮重试。下游 QueryOrder(ctx) 认取消。
	reconcileRunTimeout = 3 * time.Minute
	// reconcileStaleAfter：对账循环「陈旧」阈值。cron 轮开跑时若距上一轮心跳 > 此值，说明期间漏跑了
	// ≥2 轮（正常每 reconcileTickInterval 一轮），判定曾停滞并告警（现已恢复）。取 3× tick，容一轮抖动。
	reconcileStaleAfter = 3 * reconcileTickInterval
)

var (
	reconcileOnce    sync.Once
	reconcileRunning atomic.Bool
	// reconcilePaidRunning 保护 stuck-paid 快路径与全量轮互斥（共享网关/DB，防双跑）。
	reconcilePaidRunning atomic.Bool
)

// StartReconcileLoop 启动支付卡单对账兜底定时任务（仅 master 节点，sync.Once 保证只起一次）：
//   - 全量：每 reconcileTickInterval 扫 RCG paid/created + SUB + AGT
//   - 快路径：每 reconcilePaidTickInterval 只扫 RCG stuck-paid（缩短 paid→credited 崩溃窗口）
//
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
			// 每轮经 safeLoopRun 隔离 panic：单轮 runReconcileOnce panic 不再展开、终结整个
			// 循环任务（否则支付卡单对账永久静默停摆）。见 loop_safe.go。
			safeLoopRun("reconcile", a.runReconcileOnce) // 启动即先跑一轮，不必干等一个周期
			for range ticker.C {
				safeLoopRun("reconcile", a.runReconcileOnce)
			}
		})
		// stuck-paid 快路径：与全量循环独立 ticker，缩短回调后崩溃/卡 paid 的恢复时间。
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "payment stuck-paid fast loop started: tick="+reconcilePaidTickInterval.String())
			ticker := time.NewTicker(reconcilePaidTickInterval)
			defer ticker.Stop()
			safeLoopRun("reconcile-paid", a.runReconcilePaidOnce)
			for range ticker.C {
				safeLoopRun("reconcile-paid", a.runReconcilePaidOnce)
			}
		})
	})
}

// runReconcileOnce 跑一轮全 4 路径对账（cron 触发），atomic 防与上一轮重叠：上一轮还没跑完则跳过本次。
// 编排/日志/心跳/历史统一在 runReconcileAll；手动触发（HandleAdminRunReconcile）共用同一入口。
func (a *App) runReconcileOnce() {
	if !reconcileRunning.CompareAndSwap(false, true) {
		return
	}
	defer reconcileRunning.Store(false)
	// 与 paid 快路径互斥：全量轮占用时快路径跳过；快路径占用时全量仍跑但 paid 子路径由 atomic 防重。
	// 这里额外拿 paid 锁，避免与快路径并发扫同一批 stuck-paid。
	if !reconcilePaidRunning.CompareAndSwap(false, true) {
		// paid 快路径在跑：本轮仍扫 created/SUB/AGT，paid 交给快路径（或下轮）。
		ctx, cancel := context.WithTimeout(context.Background(), reconcileRunTimeout)
		defer cancel()
		a.runReconcileAllExceptPaid(ctx, "cron")
		return
	}
	defer reconcilePaidRunning.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), reconcileRunTimeout)
	defer cancel()
	a.runReconcileAll(ctx, "cron")
}

// runReconcilePaidOnce 快路径：next_query_at 到期查单 + stuck-paid 恢复。
func (a *App) runReconcilePaidOnce() {
	if !reconcilePaidRunning.CompareAndSwap(false, true) {
		return
	}
	defer reconcilePaidRunning.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), reconcileRunTimeout)
	defer cancel()
	a.runReconcileDueQueryPath(ctx)
	before := time.Now().Add(-reconcilePaidMinAge)
	paid, err := reconcilePaidFn(a, ctx, before)
	if err != nil {
		logger.LogWarn(ctx, "reconcile RCG(paid-fast) failed: "+err.Error())
		return
	}
	if len(paid.Reconciled) > 0 || len(paid.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(paid-fast): scanned=%d credited=%d failed=%d",
			paid.Scanned, len(paid.Reconciled), len(paid.Failed)))
	}
}
