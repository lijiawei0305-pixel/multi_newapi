package mtwire

// 代理自助 HTTP handler（AgentOwnerAuth 校验 owner==当前用户）：提现申请/列表/收益 + 收款账户。

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/agent"
)

// payoutAccountIn 是 PUT /api/tenant/payout-account 入参。
type payoutAccountIn struct {
	PayoutMethod  string `json:"payout_method"`
	PayoutAccount string `json:"payout_account"`
	PayoutName    string `json:"payout_name"`
	PayoutBank    string `json:"payout_bank"`
}

// payoutAccountOut 是收款账户响应（GET/PUT 共用）。Configured=false 表示尚未设置
// （此时其余字段均为空串，前端据此提示「请先设置收款账户」）。
type payoutAccountOut struct {
	Configured    bool   `json:"configured"`
	PayoutMethod  string `json:"payout_method"`
	PayoutAccount string `json:"payout_account"`
	PayoutName    string `json:"payout_name"`
	PayoutBank    string `json:"payout_bank"`
}

func toPayoutAccountOut(p agent.PayoutAccount, found bool) payoutAccountOut {
	return payoutAccountOut{
		Configured:    found,
		PayoutMethod:  string(p.Method),
		PayoutAccount: p.Account,
		PayoutName:    p.Name,
		PayoutBank:    p.Bank,
	}
}

type earningOut struct {
	SourceType string  `json:"source_type"`
	AmountCNY  float64 `json:"amount_cny"`
	Reference  string  `json:"reference"`
	CreatedAt  string  `json:"created_at"`
}

// ============================================================================
// 代理自助：提现申请 / 列表 / 收益（AgentOwnerAuth 校验 owner == 当前用户）
// ============================================================================

// HandleAgentRequestWithdrawal POST /api/tenant/withdrawals —— 申请提现（冻结可提现余额）。
// 租户取自 AgentOwnerAuth 校验过的 ctx，绝不接受客户端 tenant_id。尚未设置收款账户时拒绝
// （PAYOUT_ACCOUNT_REQUIRED，提现闭环补强 #1）；通过则把当前收款账户整份快照进提现单。
func (a *App) HandleAgentRequestWithdrawal(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	var body struct {
		AmountCNY float64 `json:"amount_cny"`
	}
	if err := decodeOptionalJSONObject(c, &body, agentFinancialRequestBodyLimit); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	wd, err := a.Withdrawals.Request(reqCtx(c), agent.WithdrawInput{
		TenantID:   tenantID,
		UserID:     int64(c.GetInt("id")),
		Amount:     body.AmountCNY,
		RequestKey: c.GetHeader("Idempotency-Key"),
	})
	if err != nil {
		respondErr(c, err) // WITHDRAW_INSUFFICIENT / PAYOUT_ACCOUNT_REQUIRED
		return
	}
	respondOK(c, a.toWithdrawalOut(reqCtx(c), *wd))
}

// HandleAgentListWithdrawals GET /api/tenant/withdrawals —— 本代理的提现单（按时间倒序）。
func (a *App) HandleAgentListWithdrawals(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ctx := reqCtx(c)
	page, pageSize := parseWithdrawalPaging(c)
	paged := withdrawalPagingRequested(c)
	if !paged {
		rows, err := a.AgentRepo.ListWithdrawalsByTenant(ctx, tenantID)
		if err != nil {
			respondErr(c, err)
			return
		}
		respondOK(c, a.withdrawalOuts(ctx, rows))
		return
	}
	rows, total, err := a.AgentRepo.ListWithdrawalsByTenantPage(ctx, tenantID, page, pageSize)
	if err != nil {
		respondErr(c, err)
		return
	}
	items := a.withdrawalOuts(ctx, rows)
	respondOK(c, withdrawalPageOut{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// HandleAgentListEarnings GET /api/tenant/earnings —— 本代理的收益台账（按时间倒序）。
func (a *App) HandleAgentListEarnings(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ctx := reqCtx(c)
	rows, err := a.AgentRepo.ListEarningsByTenant(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	items := make([]earningOut, 0, len(rows))
	for _, e := range rows {
		items = append(items, earningOut{
			SourceType: string(e.SourceType),
			AmountCNY:  e.Amount,
			Reference:  e.Remark,
			CreatedAt:  isoUTC(e.CreatedAt),
		})
	}
	// 对象形态：顶部汇总卡（可提现/冻结/累计）+ 明细 items（前端 agent-earnings 页消费）。
	w, _ := a.AgentService.GetWallet(ctx, tenantID)
	respondOK(c, gin.H{
		"withdrawable_cny": walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
		"frozen_cny":       walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
		"total_earned_cny": walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
		"items":            items,
	})
}

// ============================================================================
// 代理自助：收款账户（提现闭环补强 #1；AgentOwnerAuth 校验 owner == 当前用户）
// ============================================================================

// HandleAgentGetPayoutAccount GET /api/tenant/payout-account —— 返回当前收款账户；
// configured=false 表示尚未设置（申请提现前必须先设置，见 HandleAgentRequestWithdrawal /
// agent.WithdrawalService.Request 的 PAYOUT_ACCOUNT_REQUIRED 拦截）。
func (a *App) HandleAgentGetPayoutAccount(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	p, found, err := a.AgentService.GetPayoutAccount(reqCtx(c), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, toPayoutAccountOut(p, found))
}

// HandleAgentSetPayoutAccount PUT /api/tenant/payout-account —— 设置/修改收款账户
// （方式∈{alipay,bank}、账号/姓名非空、bank 方式下开户行必填，见 agent.PayoutAccount.Validate）。
func (a *App) HandleAgentSetPayoutAccount(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	var in payoutAccountIn
	if err := decodeOptionalJSONObject(c, &in, agentFinancialRequestBodyLimit); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	p := agent.PayoutAccount{
		Method:  agent.PayoutMethod(in.PayoutMethod),
		Account: in.PayoutAccount,
		Name:    in.PayoutName,
		Bank:    in.PayoutBank,
	}.Normalized()
	if err := a.AgentService.SetPayoutAccount(reqCtx(c), tenantID, p); err != nil {
		respondErr(c, err) // PAYOUT_ACCOUNT_INVALID
		return
	}
	respondOK(c, toPayoutAccountOut(p, true))
}
