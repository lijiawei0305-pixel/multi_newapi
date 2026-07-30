package mtwire

// 代理核心闭环（Phase 2）：设代理 → 下级归属 → 分润落账 → 提现审核 —— 在 new-api 基座内的装配与 HTTP 层。
//
// 决策：代理 = User + Tenant 1:1。一个 new-api 用户独占一个租户（tenants.owner_user_id），
// agent_profiles / agent_wallets / agent_earning_logs / agent_withdrawals 一律以 tenant_id 为键。
// 越权防线：代理自助端点的租户一律取自 AgentOwnerAuth 校验过的 ctx，绝不接受客户端传 tenant_id。
//
// 本文件为共享核心：错误码 + 跨 admin/self 复用的 DTO(withdrawalOut) 与 helper(toWithdrawalOut/walletField)。
// 其余职责已按文件切分（纯平移，同包）：
//   迁移 → agent_migrations.go
//   钩子(注册归因/消耗分润/租户解析/收益适配器) → agent_hooks.go
//   鉴权中间件 + 门控信号 → agent_middleware.go
//   主站管理 handler → agent_admin_http.go
//   代理自助 handler → agent_self_http.go

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// agentFinancialRequestBodyLimit bounds the small JSON payloads used by
// payout-account and withdrawal mutations. The global API limit must remain
// large enough for model requests, so financial control-plane handlers apply
// their own narrow limit before decoding attacker-controlled input.
const agentFinancialRequestBodyLimit int64 = 4 << 10

const (
	defaultWithdrawalPageSize = 20
	maxWithdrawalPageSize     = 100
)

// 代理端点错误码（沿用模块前缀约定）。
var (
	errAgentForbidden     = apperr.New("AGENT_FORBIDDEN", "无权访问该代理资源", http.StatusForbidden)
	errAgentInputInvalid  = apperr.New("AGENT_INPUT_INVALID", "代理入参非法", http.StatusBadRequest)
	errAgentNotFound      = apperr.New("AGENT_NOT_FOUND", "代理不存在", http.StatusNotFound)
	errAgentOwnerTaken    = apperr.New("AGENT_OWNER_TAKEN", "该用户已是其他租户的代理 owner", http.StatusConflict)
	errAgentUserNotFound  = apperr.New("AGENT_USER_NOT_FOUND", "owner 用户不存在", http.StatusBadRequest)
	errAgentTierInvalid   = apperr.New("AGENT_TIER_INVALID", "不允许的用户层级", http.StatusBadRequest)
	errAgentGroupNotModel = apperr.New("AGENT_GROUP_NOT_MODEL", "仅可调整模型分组的倍率", http.StatusBadRequest)
	errAgentLevelLocked   = apperr.New("AGENT_LEVEL_LOCKED", "该能力需升级为独立代理后开启", http.StatusForbidden)
	errAgentUnsettled     = apperr.New("AGENT_HAS_UNSETTLED_BALANCE", "该代理钱包尚有未提现/冻结中收益，请先结清再删除", http.StatusConflict)
)

// withdrawalOut 是提现单响应（代理自助列表 + 管理端列表共用）。Payout* 是申请那一刻从代理收款账户
// 快照下来的打款目标（提现闭环补强 #1，记录不可变）；PayoutRef/PaidAt 由 mark-paid 填充（#2）；
// Remark 是审核备注/驳回理由（#3：标准字段名 remark，见 reviewWithdrawal）。
type withdrawalOut struct {
	ID            int64   `json:"id"`
	TenantID      int64   `json:"tenant_id"`
	AgentName     string  `json:"agent_name"`
	AmountCNY     float64 `json:"amount_cny"`
	Status        string  `json:"status"`
	Remark        string  `json:"remark"`
	PayoutMethod  string  `json:"payout_method"`
	PayoutAccount string  `json:"payout_account"`
	PayoutName    string  `json:"payout_name"`
	PayoutBank    string  `json:"payout_bank"`
	PayoutRef     string  `json:"payout_ref"`
	PaidAt        string  `json:"paid_at"`
	CreatedAt     string  `json:"created_at"`
	ReviewedAt    string  `json:"reviewed_at"`
}

type withdrawalPageOut struct {
	Items    []withdrawalOut `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

func parseWithdrawalPaging(c *gin.Context) (int, int) {
	page, pageSize := 1, defaultWithdrawalPageSize
	if value, err := strconv.Atoi(c.Query("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(c.Query("page_size")); err == nil && value > 0 {
		pageSize = value
		if pageSize > maxWithdrawalPageSize {
			pageSize = maxWithdrawalPageSize
		}
	}
	return page, pageSize
}

func withdrawalPagingRequested(c *gin.Context) bool {
	_, hasPage := c.GetQuery("page")
	_, hasPageSize := c.GetQuery("page_size")
	return hasPage || hasPageSize
}

// toWithdrawalOut 映射提现单（agent_name 取租户名；收款快照/打款凭证/驳回理由一并带出，
// 提现闭环补强 #1/#2/#3）。
func (a *App) toWithdrawalOut(ctx context.Context, w agent.Withdrawal) withdrawalOut {
	name := ""
	if a.TenantService != nil {
		if t, err := a.TenantService.Get(ctx, w.TenantID); err == nil && t != nil {
			name = t.Name
		}
	}
	return withdrawalOutWithAgentName(w, name)
}

func withdrawalOutWithAgentName(w agent.Withdrawal, name string) withdrawalOut {
	return withdrawalOut{
		ID:            w.ID,
		TenantID:      w.TenantID,
		AgentName:     name,
		AmountCNY:     w.Amount,
		Status:        string(w.Status),
		Remark:        w.Remark,
		PayoutMethod:  string(w.PayoutMethod),
		PayoutAccount: w.PayoutAccount,
		PayoutName:    w.PayoutName,
		PayoutBank:    w.PayoutBank,
		PayoutRef:     w.PayoutRef,
		PaidAt:        isoUTC(w.PaidAt),
		CreatedAt:     isoUTC(w.CreatedAt),
		ReviewedAt:    isoUTC(w.ReviewedAt),
	}
}

// withdrawalOuts resolves every agent name with one tenants IN query, then
// maps rows in memory. This keeps both admin and self-service list endpoints
// at a fixed query count instead of one tenant lookup per withdrawal row.
func (a *App) withdrawalOuts(ctx context.Context, rows []agent.Withdrawal) []withdrawalOut {
	tenantIDs := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.TenantID]; exists {
			continue
		}
		seen[row.TenantID] = struct{}{}
		tenantIDs = append(tenantIDs, row.TenantID)
	}
	names := a.tenantNamesByIDs(ctx, tenantIDs)
	out := make([]withdrawalOut, 0, len(rows))
	for _, row := range rows {
		out = append(out, withdrawalOutWithAgentName(row, names[row.TenantID]))
	}
	return out
}

// walletField 安全取钱包字段（w 可能为 nil）。
func walletField(w *agent.AgentWallet, f func(*agent.AgentWallet) float64) float64 {
	if w == nil {
		return 0
	}
	return f(w)
}
