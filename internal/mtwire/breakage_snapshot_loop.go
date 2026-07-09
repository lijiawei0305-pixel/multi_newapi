/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package mtwire

// breakage 快照定时任务（P2-BRK-01，master-only）：复刻 StartReconcileLoop / StartAgentPlanExpiryLoop
// 的定时范式（common.IsMasterNode + sync.Once + atomic 防重入）。每 breakageSnapshotTickInterval：
//  1. a.Breakage.RunSnapshot(ctx, nil, now) —— 采集全平台快照幂等落 breakage_snapshots；RunSnapshot
//     内部已按阈值 best-effort 触发「到期未使用沉淀」告警（见 internal/breakage/service.go maybeAlert）。
//  2. 扫「满额/接近满额订阅」+「系统异常卡单」，经 a.AlertSink 分发告警（收编 STATUS §三 7c-2「满额
//     服务端主动推送」可选待办）。去重键带当日锚点，防同日重复 job 轰炸。
//
// best-effort：全程幂等 + 失败仅记日志，绝不影响主流程；由 App 装配完成后（master 块）调用。

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

// breakageSnapshotTickInterval 是快照采集周期（每日）：breakage 是慢变量（按订阅「期」沉淀），
// 日级快照足够刻画趋势且开销小。首轮在启动即跑一次（不必干等一天）。
const breakageSnapshotTickInterval = 24 * time.Hour

// breakageExhaustedThresholdPct 是「满额/接近满额订阅」告警的用量占比阈值（>= 此值即计入满额告警）。
// 与 alert.DefaultThresholdPct（95，订阅监控 critical 档）同口径。
const breakageExhaustedThresholdPct = alert.DefaultThresholdPct

var (
	breakageSnapshotOnce    sync.Once
	breakageSnapshotRunning atomic.Bool
)

// StartBreakageSnapshotLoop 启动 breakage 快照定时任务（仅 master 节点，sync.Once 只起一次）。
func (a *App) StartBreakageSnapshotLoop() {
	breakageSnapshotOnce.Do(func() {
		if !common.IsMasterNode {
			return // 非主节点不跑，避免多副本重复落快照/重复告警
		}
		if a.Breakage == nil {
			return // 未装配（防御性；正常 New 恒装配）
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "breakage snapshot loop started: tick="+breakageSnapshotTickInterval.String())
			ticker := time.NewTicker(breakageSnapshotTickInterval)
			defer ticker.Stop()
			// 每轮经 safeLoopRun 隔离 panic：单轮 panic 不再终结整个循环任务。见 loop_safe.go。
			safeLoopRun("breakage-snapshot", a.runBreakageSnapshotOnce) // 启动即先跑一轮
			for range ticker.C {
				safeLoopRun("breakage-snapshot", a.runBreakageSnapshotOnce)
			}
		})
	})
}

// runBreakageSnapshotOnce 跑一轮快照 + 告警扫描，atomic 防与上一轮重叠（上一轮未跑完则跳过本次）。
func (a *App) runBreakageSnapshotOnce() {
	if !breakageSnapshotRunning.CompareAndSwap(false, true) {
		return
	}
	defer breakageSnapshotRunning.Store(false)

	ctx := context.Background()
	now := time.Now().Unix()

	// (1) 采集 + 落快照（跨租户全量 tenantID=nil）。RunSnapshot 内部含「到期未使用沉淀」告警旁路。
	n, err := a.Breakage.RunSnapshot(ctx, nil, now)
	if err != nil {
		common.SysLog("breakage snapshot: RunSnapshot failed: " + err.Error())
		// 落库失败不 return——满额/异常告警仍可基于实时聚合发出（互不依赖）。
	} else {
		logger.LogInfo(ctx, fmt.Sprintf("breakage snapshot: upserted %d rows", n))
	}

	// (2) 满额/异常告警（经 AlertSink，收编 7c-2）。best-effort，失败仅记日志。
	a.dispatchBreakageAlerts(ctx, now)
}

// dispatchBreakageAlerts 扫「满额/接近满额订阅」+「系统异常卡单」并经 AlertSink 分发告警。
// AlertSink 内部按 Config.Enabled 总开关判是否真发（未配置即不发，安全默认）+ 按 DedupKey 去重。
func (a *App) dispatchBreakageAlerts(ctx context.Context, now int64) {
	if a.AlertSink == nil {
		return
	}
	day := now - now%86400 // 当日锚点，供去重键（同日同类告警只发一次）

	// 满额/接近满额订阅数（用量占比 >= 阈值，仅计仍在有效期内的活跃订阅——过期沉淀由 RunSnapshot 侧告警覆盖）。
	if exhausted := a.countExhaustedActiveSubs(ctx, now); exhausted > 0 {
		_ = a.AlertSink.Dispatch(ctx, alert.Alert{
			Level:    alert.LevelWarning,
			Subject:  "满额订阅告警",
			Body:     fmt.Sprintf("检测到 %d 笔活跃订阅用量已达或接近满额（>= %.0f%%），可能触发续费或额度沉淀。", exhausted, breakageExhaustedThresholdPct),
			DedupKey: fmt.Sprintf("breakage_exhausted_subs:%d", day),
		})
	}

	// 系统异常卡单数（复用 Overview 的 AnomalyCount 口径：payment_orders created/paid 超 5min）。
	if ov, err := a.Breakage.Overview(ctx, nil, now); err == nil && ov.AnomalyCount > 0 {
		_ = a.AlertSink.Dispatch(ctx, alert.Alert{
			Level:    alert.LevelCritical,
			Subject:  "支付异常卡单告警",
			Body:     fmt.Sprintf("检测到 %d 笔已下单/已支付但未入账的异常卡单（超过 5 分钟），请核对对账记录。", ov.AnomalyCount),
			DedupKey: fmt.Sprintf("breakage_payment_anomaly:%d", day),
		})
	}
}

// countExhaustedActiveSubs 统计仍在有效期内、用量占比 >= 阈值的活跃订阅数（跨租户，排除 tenant_id=0）。
// 走 raw db.Table() 的 COUNT，绝不 import 兄弟 model 结构（升级安全，门 #3）：
// used_usd >= month_limit_usd * 阈值/100 且 month_limit_usd > 0 且 status='active' 且 expire_at > now。
func (a *App) countExhaustedActiveSubs(ctx context.Context, now int64) int64 {
	if a.DB == nil {
		return 0
	}
	nowT := time.Unix(now, 0).UTC()
	ratio := breakageExhaustedThresholdPct / 100.0
	var count int64
	err := a.DB.WithContext(ctx).
		Table("tokenplan_subscriptions").
		Where("status = ?", "active").
		Where("expire_at > ?", nowT).
		Where("month_limit_usd > 0").
		Where("used_usd >= month_limit_usd * ?", ratio).
		Where("tenant_id <> 0").
		Count(&count).Error
	if err != nil {
		common.SysLog("breakage snapshot: count exhausted subs failed: " + err.Error())
		return 0
	}
	return count
}
