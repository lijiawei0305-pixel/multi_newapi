package mtwire

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

// agentPlanExpiryTickInterval 是代理套餐到期扫描周期。
const agentPlanExpiryTickInterval = 1 * time.Hour

var agentPlanExpiryOnce sync.Once

// StartAgentPlanExpiryLoop 启动代理套餐到期降级定时任务（P4，仅 master 节点，sync.Once 只起一次）：
// 每 agentPlanExpiryTickInterval 扫一次 mt_agent_memberships 里 active 且已过期的会员 → 撤销该档授予的
// 付费能力（Level→0 / CanAPI→false / DiscountRatio→0，保留其余管理员配置与账户），并置会员 expired。
// best-effort：幂等 + 失败仅记日志，绝不影响主流程。由 App 装配完成后（master 块）调用。
func (a *App) StartAgentPlanExpiryLoop() {
	agentPlanExpiryOnce.Do(func() {
		if !common.IsMasterNode {
			return // 非主节点不跑，避免多副本重复降级
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "agent-plan expiry loop started: tick="+agentPlanExpiryTickInterval.String())
			ticker := time.NewTicker(agentPlanExpiryTickInterval)
			defer ticker.Stop()
			// 每轮经 safeLoopRun 隔离 panic：单轮 panic 不再终结整个循环任务。见 loop_safe.go。
			safeLoopRun("agent-plan-expiry", a.runAgentPlanExpiryOnce) // 启动即先跑一轮
			for range ticker.C {
				safeLoopRun("agent-plan-expiry", a.runAgentPlanExpiryOnce)
			}
		})
	})
}

// runAgentPlanExpiryOnce 扫一批到期会员并降级（单轮上限防长事务；未处理完下一周期继续）。
func (a *App) runAgentPlanExpiryOnce() {
	ctx := context.Background()
	now := time.Now()
	var rows []agentMembershipRow
	if err := a.DB.WithContext(ctx).
		Where("status = ? AND expire_at <= ?", agtMembershipActive, now).
		Order("expire_at ASC").Limit(500).Find(&rows).Error; err != nil {
		common.SysLog("agent-plan expiry: scan failed: " + err.Error())
		return
	}
	for i := range rows {
		if err := a.downgradeExpiredAgent(ctx, &rows[i], now); err != nil {
			// 继续处理其余，失败者下轮重试（幂等）。
			common.SysLog("agent-plan expiry: downgrade tenant " +
				strconv.FormatInt(rows[i].TenantID, 10) + " failed: " + err.Error())
		}
	}
}

// downgradeExpiredAgent 撤销到期代理的付费授予并置会员 expired（幂等）。
//
// 保守策略：只撤销本套餐授予的能力（Level→0 / CanAPI→false / DiscountRatio→0），不删除租户、不迁移下级
// 用户、不回收子域名——「完全归档为普通用户」是破坏性操作，留给管理员经「删除代理」显式执行。
// 撤销后用户仍是最基础的 L0 代理但无任何付费权益；重新购买即再次开通/升级。
func (a *App) downgradeExpiredAgent(ctx context.Context, m *agentMembershipRow, now time.Time) error {
	params, found, err := a.AgentRepo.GetAgentType(ctx, m.TenantID)
	if err != nil {
		return err
	}
	if found {
		params.Level = 0
		params.CanAPI = false
		params.DiscountRatio = 0
		if err := a.AgentService.SetAgentType(ctx, m.TenantID, params); err != nil {
			return err
		}
	}
	return a.DB.WithContext(ctx).Model(&agentMembershipRow{}).
		Where("tenant_id = ? AND status = ?", m.TenantID, agtMembershipActive).
		Updates(map[string]any{"status": agtMembershipExpired, "updated_at": now}).Error
}
