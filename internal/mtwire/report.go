package mtwire

// 财务报表 HTTP 层（S2）：把 reportrepo 的聚合结果装配成 snake_case DTO 并经统一信封返回。
//
// 两套报表、四个透镜（earnings/recharge/consumption/withdrawals）：
//   - 管理端 /api/admin/finance/**（AdminAuth，跨租户）：tenantID=nil。
//   - 代理自助 /api/tenant/finance/**（UserAuth+AgentOwnerAuth，单租户）：tenantID=&agentTenantID(c)。
//
// 硬约束：
//   - 金额一律 float64，币种由字段名后缀（_cny/_usd）表达，在 handler 边界 round2；整数计数精确不舍入。
//   - 请求区间用 epoch 秒（start_timestamp/end_timestamp，int64）；响应时间用 isoUTC（ISO-8601 UTC）；
//     趋势桶 = 日历标签 bucket + 桶起始 epoch bucket_ts。
//   - 代理端点 tenant_id 只取自 AgentOwnerAuth 校验过的 agentTenantID(c)，<=0 → AGENT_FORBIDDEN；
//     绝不从 query/body 读 tenant_id。管理端跨租户（AdminAuth），无租户作用域。
//   - 明细分页嵌套在 data 内：{items,total,page,page_size}。
//   - ?format=csv|pdf 仅作用于两个 detail 端点：委托 report_export.go（S3）的 writeDetailCSV/PDF 落地。

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/report/reportrepo"
	"github.com/QuantumNous/new-api/internal/stats"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 财务报表错误码（模块前缀 REPORT_；STATS_RANGE_INVALID 复用 stats.ErrRangeInvalid，
// AGENT_FORBIDDEN 复用 agent.go 的 errAgentForbidden，见 doc/api-contract.md §1.1）。
var (
	errReportGranularityInvalid = apperr.New("REPORT_GRANULARITY_INVALID", "趋势粒度非法（仅 day/week/month）", http.StatusBadRequest)
	errReportLensInvalid        = apperr.New("REPORT_LENS_INVALID", "报表透镜非法（仅 earnings/recharge/consumption/withdrawals）", http.StatusBadRequest)
	errReportFormatInvalid      = apperr.New("REPORT_FORMAT_INVALID", "导出格式非法（仅 csv/pdf）", http.StatusBadRequest)
	// errReportExportFailed 供 report_export.go（S3）在渲染/写出失败时返回。
	errReportExportFailed = apperr.New("REPORT_EXPORT_FAILED", "报表导出失败", http.StatusInternalServerError)
)

// maxRangeSeconds 是请求区间上限（366 天，含闰年裕量）；超出 → STATS_RANGE_INVALID。
const maxRangeSeconds int64 = 366 * 24 * 60 * 60

// reportExportMaxRows 是导出时单次取回的行数上限（非热路径报表；分页 page=1 取全集）。
const reportExportMaxRows = 100000

// summarySourceOrder 是 summary.earnings.by_source 固定输出的 6 个 source_type（缺则补 0）。
// 顺序对齐 doc/finance-report-contract.md §1.1 示例响应体（该文档写于 ratio_markup 引入之前，
// 待补录）；ratio_markup 紧邻 consume_commission——两者是 L0/L1 互斥的同类「按消耗计的代理收益」
// （agent.SourceRatioMarkup，spec agent-tiering §9.4）。recharge_spread 为幻影来源（恒 0）。
var summarySourceOrder = []string{
	"consume_commission",
	"ratio_markup",
	"tokenplan_spread",
	"tokenplan_commission",
	"recharge_spread",
	"manual_adjustment",
}

// rankSortWhitelist 是 §1.3 允许的排序字段集；非白名单回退 total_earned_cny（与 repo 口径一致）。
var rankSortWhitelist = map[string]struct{}{
	"total_earned_cny":       {},
	"recharge_paid_cny":      {},
	"subscription_paid_cny":  {},
	"consumption_cost_cny":   {},
	"consumption_used_quota": {},
	"withdrawn_cny":          {},
	"pending_withdraw_cny":   {},
	"withdrawable_cny":       {},
}

// ============================================================================
// DTO（snake_case；金额 float64 _cny/_usd 边界 round2；时间 isoUTC）
// ============================================================================

type financeRangeOut struct {
	StartTimestamp int64 `json:"start_timestamp"`
	EndTimestamp   int64 `json:"end_timestamp"`
}

type sourceSumOut struct {
	SourceType string  `json:"source_type"`
	AmountCNY  float64 `json:"amount_cny"`
}

type walletTotalOut struct {
	WithdrawableCNY float64 `json:"withdrawable_cny"`
	FrozenCNY       float64 `json:"frozen_cny"`
	TotalEarnedCNY  float64 `json:"total_earned_cny"`
	APIBalanceUSD   float64 `json:"api_balance_usd"`
}

type earningsBlockOut struct {
	TotalEarnedCNY float64        `json:"total_earned_cny"`
	BySource       []sourceSumOut `json:"by_source"`
	WalletTotal    walletTotalOut `json:"wallet_total"`
}

type rechargeBlockOut struct {
	RechargePaidCNY       float64 `json:"recharge_paid_cny"`
	SubscriptionPaidCNY   float64 `json:"subscription_paid_cny"`
	SubscriptionCostCNY   float64 `json:"subscription_cost_cny"`
	SubscriptionSpreadCNY float64 `json:"subscription_spread_cny"`
	RechargeSpreadCNY     float64 `json:"recharge_spread_cny"`
}

type consumptionBlockOut struct {
	UsedQuota   int64   `json:"used_quota"`
	UsedCostUSD float64 `json:"used_cost_usd"`
	UsedCostCNY float64 `json:"used_cost_cny"`
	Calls       int64   `json:"calls"`
	Tokens      int64   `json:"tokens"`
}

type withdrawalsBlockOut struct {
	PendingCNY   float64 `json:"pending_cny"`
	FrozenCNY    float64 `json:"frozen_cny"`
	WithdrawnCNY float64 `json:"withdrawn_cny"`
	RejectedCNY  float64 `json:"rejected_cny"`
}

type exchangeBlockOut struct {
	QuotaPerUnit    float64 `json:"quota_per_unit"`
	USDExchangeRate float64 `json:"usd_exchange_rate"`
}

// ============================================================================
// 财务报表 v3 总览（doc/finance-model-report-v3.md §二）：代理 4 项 / 管理员 6 项，经
// financeSummaryOut.Overview（any）按 scope 二选一装配。两侧字段形状本就不同（代理侧
// apikey_consumption_cny 是单值；管理端把同一类口径拆成 mainsite/agent 两列），不能共用一个 flat
// 结构体——那样要么两侧都装一堆对方用不上的 0，要么靠 omitempty 却让「这个 scope 真实为 0」和
// 「这个 scope 压根没这个字段」无法区分。镜像本文件已有的 trendOut.Series any 先例。
// ============================================================================

// agentFinanceOverviewOut 是代理自助财务总览（4 项，§二「代理报表」）。
type agentFinanceOverviewOut struct {
	// TokenplanRevenueCNY 套餐收益：本代理名下用户为套餐支付的总额（= Σ 零售价，购买时计），
	// 数据源 = SubscriptionPaidCost(...).PaidCNY（即既有 recharge 区块的 subscription_paid_cny）。
	TokenplanRevenueCNY float64 `json:"tokenplan_revenue_cny"`
	// TokenplanWithdrawableCNY 套餐可提现：代理套餐差价，购买时一次性入账、立即可提现
	// （= Σ source_type=tokenplan_spread，见 internal/tokenplan/subscription.go ActivateFromPayment）。
	TokenplanWithdrawableCNY float64 `json:"tokenplan_withdrawable_cny"`
	// ApikeyConsumptionCNY apikey 消费收益：用户消耗的钱包余额。
	//
	// ⚠️口径缺口（doc/finance-model-report-v3.md §二「套餐额度的消耗不计入 apikey 消费收益」，
	// 本字段本应严格排除套餐桶消耗）：现有数据无法把「钱包桶消耗」与「套餐桶消耗」干净分开——
	// 两者共用同一张 logs 表，唯一候选区分信号是 relayInfo.BillingSource，但它只在文本中继一条
	// 路径（service/text_quota.go GenerateTextOtherInfo→appendBillingInfo）写入 Other 的 JSON 字段，
	// image/audio/realtime(wss)/task 计费/违规扣费/Midjourney 代理等其余写 log 路径都不写这个键——
	// 按它过滤会系统性漏记这些路径的真实钱包消耗，比不区分更糟（会把它们排除在两个桶之外）。
	// 故本字段当前 = ConsumptionCost(...).UsedCostCNY，即钱包桶 + 套餐桶消耗合计（全量口径的
	// 「上界」，不是纯钱包值）。若要精确区分，需要在 PostConsumeQuota/BillingSession.Settle 那个
	// 唯一的 wallet-vs-subscription 分支点新增一张专门的钱包消耗流水（比照 agent_earning_logs 的
	// 记账粒度），而不是继续从 logs.other 这个展示用途的 JSON 里挖。
	ApikeyConsumptionCNY float64 `json:"apikey_consumption_cny"`
	// ConsumptionWithdrawableCNY 消耗可提现：代理消耗差价，消耗时逐笔实时入账、立即可提现
	// （= Σ source_type IN (ratio_markup, consume_commission)）。Task 1（本次）起两者只在 WALLET
	// 计费桶产生——套餐桶消耗已不再触发任何一种，故此字段天然只含钱包消耗的分润，无上述口径缺口。
	ConsumptionWithdrawableCNY float64 `json:"consumption_withdrawable_cny"`
}

// adminFinanceOverviewOut 是管理端财务总览（6 项，§二「管理员报表」，主站/代理站分列）。
// 主站 = 平台直销租户（tenants.slug="platform"，见 seed.go platformSlug/isPlatformTenant）；
// 其余归属租户（tenant_id<>0 且 <> 平台租户 ID）= 代理站；未归属（tenant_id=0）不计入任何一侧
// （既有跨租户聚合 applyTenantScope 的 tenant_id<>0 约定）。
type adminFinanceOverviewOut struct {
	MainsiteTokenplanRevenueCNY float64 `json:"mainsite_tokenplan_revenue_cny"` // 主站套餐收益
	AgentTokenplanRevenueCNY    float64 `json:"agent_tokenplan_revenue_cny"`    // 代理站套餐收益（各代理合计）
	TokenplanRebateCNY          float64 `json:"tokenplan_rebate_cny"`           // 给代理的套餐返现：Σ 各代理 tokenplan_spread（不含主站）
	// MainsiteWalletConsumptionCNY / AgentWalletConsumptionCNY：与 agentFinanceOverviewOut.ApikeyConsumptionCNY
	// 同一口径缺口——现有数据无法排除套餐桶消耗，当前为该 scope（主站 / 代理站）下的全量消耗口径
	// （钱包+套餐桶混合，上界而非纯钱包值），详见该字段的长注释。
	MainsiteWalletConsumptionCNY float64 `json:"mainsite_wallet_consumption_cny"` // 主站钱包消耗
	AgentWalletConsumptionCNY    float64 `json:"agent_wallet_consumption_cny"`    // 代理站钱包消耗（各代理合计）
	AgentAPIRebateCNY            float64 `json:"agent_api_rebate_cny"`            // 需返现代理的 api 消耗：Σ 各代理 (ratio_markup+consume_commission)，不含主站
}

// financeSummaryOut 是 §1.1 / §1.5 汇总响应体（管理端=跨租户，代理=单租户）。
type financeSummaryOut struct {
	Range       financeRangeOut     `json:"range"`
	Earnings    earningsBlockOut    `json:"earnings"`
	Recharge    rechargeBlockOut    `json:"recharge"`
	Consumption consumptionBlockOut `json:"consumption"`
	Withdrawals withdrawalsBlockOut `json:"withdrawals"`
	Exchange    exchangeBlockOut    `json:"exchange"`
	// Overview 是财务报表 v3 总览（doc/finance-model-report-v3.md §二）：tenantID!=nil（代理自助）时
	// 装 agentFinanceOverviewOut（4 项）；tenantID==nil（管理端）时装 adminFinanceOverviewOut（6 项）。
	Overview any `json:"overview"`
}

// trendOut 是 §1.2 / §1.6 趋势响应体；series 为按透镜的具体点切片（见下方各 *TrendOut）。
type trendOut struct {
	Lens        string `json:"lens"`
	Granularity string `json:"granularity"`
	Series      any    `json:"series"`
}

type earningsTrendOut struct {
	Bucket    string  `json:"bucket"`
	BucketTS  int64   `json:"bucket_ts"`
	AmountCNY float64 `json:"amount_cny"`
}

type rechargeTrendOut struct {
	Bucket                string  `json:"bucket"`
	BucketTS              int64   `json:"bucket_ts"`
	RechargePaidCNY       float64 `json:"recharge_paid_cny"`
	SubscriptionPaidCNY   float64 `json:"subscription_paid_cny"`
	SubscriptionCostCNY   float64 `json:"subscription_cost_cny"`
	SubscriptionSpreadCNY float64 `json:"subscription_spread_cny"`
}

type consumptionTrendOut struct {
	Bucket      string  `json:"bucket"`
	BucketTS    int64   `json:"bucket_ts"`
	UsedQuota   int64   `json:"used_quota"`
	UsedCostCNY float64 `json:"used_cost_cny"`
	Calls       int64   `json:"calls"`
	Tokens      int64   `json:"tokens"`
}

type withdrawalsTrendOut struct {
	Bucket       string  `json:"bucket"`
	BucketTS     int64   `json:"bucket_ts"`
	PendingCNY   float64 `json:"pending_cny"`
	WithdrawnCNY float64 `json:"withdrawn_cny"`
	RejectedCNY  float64 `json:"rejected_cny"`
}

// agentRankOut 是 §1.3 按代理/租户排行的一行（管理端专用，tenant_id/agent_name 恒在）。
type agentRankOut struct {
	TenantID             int64   `json:"tenant_id"`
	AgentName            string  `json:"agent_name"`
	OwnerUserID          int64   `json:"owner_user_id"`
	OwnerUsername        string  `json:"owner_username"`
	TotalEarnedCNY       float64 `json:"total_earned_cny"`
	ConsumeCommissionCNY float64 `json:"consume_commission_cny"`
	RatioMarkupCNY       float64 `json:"ratio_markup_cny"`
	TokenplanSpreadCNY   float64 `json:"tokenplan_spread_cny"`
	ManualAdjustmentCNY  float64 `json:"manual_adjustment_cny"`
	RechargePaidCNY      float64 `json:"recharge_paid_cny"`
	SubscriptionPaidCNY  float64 `json:"subscription_paid_cny"`
	SubscriptionCostCNY  float64 `json:"subscription_cost_cny"`
	ConsumptionUsedQuota int64   `json:"consumption_used_quota"`
	ConsumptionCostCNY   float64 `json:"consumption_cost_cny"`
	WithdrawnCNY         float64 `json:"withdrawn_cny"`
	PendingWithdrawCNY   float64 `json:"pending_withdraw_cny"`
	WithdrawableCNY      float64 `json:"withdrawable_cny"`
}

// agentRankingOut 是 §1.3 排行响应体（分页嵌套 + 回显 sort_by/order）。
type agentRankingOut struct {
	Items    []agentRankOut `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	SortBy   string         `json:"sort_by"`
	Order    string         `json:"order"`
}

// detailOut 是 §1.4 / §1.7 明细 JSON 响应体（items 为按透镜的具体行切片）。
type detailOut struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// 明细行：管理端含 tenant_id/agent_name；代理自助置零经 omitempty 省略（单租户无需冗余）。
type earningDetailOut struct {
	TenantID   int64   `json:"tenant_id,omitempty"`
	AgentName  string  `json:"agent_name,omitempty"`
	SourceType string  `json:"source_type"`
	AmountCNY  float64 `json:"amount_cny"`
	Reference  string  `json:"reference"`
	CreatedAt  string  `json:"created_at"`
}

type withdrawalDetailOut struct {
	ID         int64   `json:"id"`
	TenantID   int64   `json:"tenant_id,omitempty"`
	AgentName  string  `json:"agent_name,omitempty"`
	AmountCNY  float64 `json:"amount_cny"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	ReviewedAt string  `json:"reviewed_at"`
}

type rechargeDetailOut struct {
	OrderNo           string  `json:"order_no"`
	TenantID          int64   `json:"tenant_id,omitempty"`
	Kind              string  `json:"kind"`
	Provider          string  `json:"provider"`
	AmountUSD         float64 `json:"amount_usd"`
	ActualPaidCNY     float64 `json:"actual_paid_cny"`
	AgentCostPriceCNY float64 `json:"agent_cost_price_cny"`
	Status            string  `json:"status"`
	CreatedAt         string  `json:"created_at"`
}

type consumptionDetailOut struct {
	TenantID    int64   `json:"tenant_id,omitempty"`
	AgentName   string  `json:"agent_name,omitempty"`
	ModelName   string  `json:"model_name"`
	Calls       int64   `json:"calls"`
	Tokens      int64   `json:"tokens"`
	UsedQuota   int64   `json:"used_quota"`
	UsedCostCNY float64 `json:"used_cost_cny"`
}

// reportExportTable 是「透镜无关、已渲染完成」的导出载荷：detail handler 负责全部格式化
// （金额 2 位小数、时间 ISO-8601、列顺序对齐 JSON 字段名），report_export.go（S3）只负责
// 编码 + 流式写出。Filename 不含扩展名——writer 自行追加 ".csv"/".pdf" 并设置
// Content-Type + Content-Disposition(attachment)。
type reportExportTable struct {
	Filename string     // 例：finance-earnings-1750000000-1752000000
	Title    string     // PDF 文档标题（例：Finance report — earnings）
	Headers  []string   // 列头 == JSON 字段名，按列序
	Rows     [][]string // 每行已预渲染为字符串，与 Headers 对齐
}

// ============================================================================
// 管理端 handlers（AdminAuth，跨租户，tenantID=nil）
// ============================================================================

// HandleAdminFinanceSummary GET /api/admin/finance/summary —— 平台财务汇总（跨租户）。需 AdminAuth。
func (a *App) HandleAdminFinanceSummary(c *gin.Context) {
	a.handleFinanceSummary(c, nil)
}

// HandleAdminFinanceTrend GET /api/admin/finance/trend —— 平台趋势（按 lens/granularity）。需 AdminAuth。
func (a *App) HandleAdminFinanceTrend(c *gin.Context) {
	a.handleFinanceTrend(c, nil)
}

// HandleAdminFinanceAgents GET /api/admin/finance/agents —— 按代理/租户排行（可排序分页）。需 AdminAuth。
func (a *App) HandleAdminFinanceAgents(c *gin.Context) {
	start, end, err := parseTimeRange(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	sortBy := normRankSort(c.Query("sort_by"))
	order := normOrder(c.Query("order"))
	page, pageSize := parsePaging(c)

	rows, total, err := a.ReportRepo.AgentRanking(reqCtx(c), start, end, sortBy, order, page, pageSize)
	if err != nil {
		respondErr(c, err)
		return
	}
	items := make([]agentRankOut, 0, len(rows))
	for _, r := range rows {
		items = append(items, agentRankOut{
			TenantID:             r.TenantID,
			AgentName:            r.AgentName,
			OwnerUserID:          r.OwnerUserID,
			OwnerUsername:        r.OwnerUsername,
			TotalEarnedCNY:       round2(r.TotalEarnedCNY),
			ConsumeCommissionCNY: round2(r.ConsumeCommissionCNY),
			RatioMarkupCNY:       round2(r.RatioMarkupCNY),
			TokenplanSpreadCNY:   round2(r.TokenplanSpreadCNY),
			ManualAdjustmentCNY:  round2(r.ManualAdjustmentCNY),
			RechargePaidCNY:      round2(r.RechargePaidCNY),
			SubscriptionPaidCNY:  round2(r.SubscriptionPaidCNY),
			SubscriptionCostCNY:  round2(r.SubscriptionCostCNY),
			ConsumptionUsedQuota: r.ConsumptionUsedQuota,
			ConsumptionCostCNY:   round2(r.ConsumptionCostCNY),
			WithdrawnCNY:         round2(r.WithdrawnCNY),
			PendingWithdrawCNY:   round2(r.PendingWithdrawCNY),
			WithdrawableCNY:      round2(r.WithdrawableCNY),
		})
	}
	respondOK(c, agentRankingOut{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		SortBy:   sortBy,
		Order:    order,
	})
}

// HandleAdminFinanceDetail GET /api/admin/finance/detail —— 明细（lens 必填，可 ?format=csv|pdf）。需 AdminAuth。
func (a *App) HandleAdminFinanceDetail(c *gin.Context) {
	a.handleFinanceDetail(c, nil)
}

// ============================================================================
// 代理自助 handlers（UserAuth+AgentOwnerAuth，单租户，tenantID=&agentTenantID(c)）
// ============================================================================

// HandleTenantFinanceSummary GET /api/tenant/finance/summary —— 本代理财务汇总（单租户）。
func (a *App) HandleTenantFinanceSummary(c *gin.Context) {
	tenantID, ok := agentScope(c)
	if !ok {
		return
	}
	a.handleFinanceSummary(c, tenantID)
}

// HandleTenantFinanceTrend GET /api/tenant/finance/trend —— 本代理趋势（单租户）。
func (a *App) HandleTenantFinanceTrend(c *gin.Context) {
	tenantID, ok := agentScope(c)
	if !ok {
		return
	}
	a.handleFinanceTrend(c, tenantID)
}

// HandleTenantFinanceDetail GET /api/tenant/finance/detail —— 本代理明细（单租户，可 ?format=csv|pdf）。
func (a *App) HandleTenantFinanceDetail(c *gin.Context) {
	tenantID, ok := agentScope(c)
	if !ok {
		return
	}
	a.handleFinanceDetail(c, tenantID)
}

// agentScope 取 AgentOwnerAuth 校验过的租户 ID 指针；<=0 即 AGENT_FORBIDDEN（已写响应，返回 false）。
func agentScope(c *gin.Context) (*int64, bool) {
	tid := agentTenantID(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return nil, false
	}
	return &tid, true
}

// ============================================================================
// 共享 handler 实现（tenantID=nil → 管理端跨租户；非 nil → 单租户）
// ============================================================================

func (a *App) handleFinanceSummary(c *gin.Context, tenantID *int64) {
	start, end, err := parseTimeRange(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	ctx := reqCtx(c)
	repo := a.ReportRepo

	srcSums, err := repo.SummaryEarnings(ctx, tenantID, start, end)
	if err != nil {
		respondErr(c, err)
		return
	}
	wallet, err := repo.WalletTotals(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	rechargeMap, err := repo.RechargePaid(ctx, tenantID, start, end)
	if err != nil {
		respondErr(c, err)
		return
	}
	subMap, err := repo.SubscriptionPaidCost(ctx, tenantID, start, end)
	if err != nil {
		respondErr(c, err)
		return
	}
	cons, err := repo.ConsumptionCost(ctx, tenantID, start, end)
	if err != nil {
		respondErr(c, err)
		return
	}
	wd, err := repo.Withdrawals(ctx, tenantID, start, end)
	if err != nil {
		respondErr(c, err)
		return
	}

	// 收益 by source：仓储只返回有行的来源，handler 补齐 5 个固定枚举（缺则 0）。
	bySrcMap := make(map[string]float64, len(srcSums))
	var totalEarned float64
	for _, s := range srcSums {
		bySrcMap[s.SourceType] += s.AmountCNY
		totalEarned += s.AmountCNY
	}
	bySource := make([]sourceSumOut, 0, len(summarySourceOrder))
	for _, st := range summarySourceOrder {
		bySource = append(bySource, sourceSumOut{SourceType: st, AmountCNY: round2(bySrcMap[st])})
	}

	// 充值/订阅：跨租户 map 汇总为单值（单租户时 map 至多一条）。
	var rechargePaid float64
	for _, v := range rechargeMap {
		rechargePaid += v
	}
	var subPaid, subCost float64
	for _, v := range subMap {
		subPaid += v.PaidCNY
		subCost += v.CostCNY
	}

	// 财务报表 v3 总览（doc/finance-model-report-v3.md §二）：代理 4 项全部复用上面已查出的数据
	// （不加查询）；管理端 6 项需要按租户拆分主站/代理站，另经 adminFinanceOverview 装配。
	var overview any
	if tenantID != nil {
		overview = agentFinanceOverviewOut{
			TokenplanRevenueCNY:        round2(subPaid),
			TokenplanWithdrawableCNY:   round2(bySrcMap["tokenplan_spread"]),
			ApikeyConsumptionCNY:       round2(cons.UsedCostCNY),
			ConsumptionWithdrawableCNY: round2(bySrcMap["ratio_markup"] + bySrcMap["consume_commission"]),
		}
	} else {
		adminOverview, oerr := a.adminFinanceOverview(ctx, start, end)
		if oerr != nil {
			respondErr(c, oerr)
			return
		}
		overview = adminOverview
	}

	respondOK(c, financeSummaryOut{
		Range: financeRangeOut{StartTimestamp: start, EndTimestamp: end},
		Earnings: earningsBlockOut{
			TotalEarnedCNY: round2(totalEarned),
			BySource:       bySource,
			WalletTotal: walletTotalOut{
				WithdrawableCNY: round2(wallet.WithdrawableCNY),
				FrozenCNY:       round2(wallet.FrozenCNY),
				TotalEarnedCNY:  round2(wallet.TotalEarnedCNY),
				APIBalanceUSD:   round2(wallet.APIBalanceUSD),
			},
		},
		Recharge: rechargeBlockOut{
			RechargePaidCNY:       round2(rechargePaid),
			SubscriptionPaidCNY:   round2(subPaid),
			SubscriptionCostCNY:   round2(subCost),
			SubscriptionSpreadCNY: round2(bySrcMap["tokenplan_spread"]), // 差价回退取自收益台账
			RechargeSpreadCNY:     0,                                    // 幻影来源：恒 0（无 writer）
		},
		Consumption: consumptionBlockOut{
			UsedQuota:   cons.UsedQuota,
			UsedCostUSD: round2(cons.UsedCostUSD),
			UsedCostCNY: round2(cons.UsedCostCNY),
			Calls:       cons.Calls,
			Tokens:      cons.Tokens,
		},
		Withdrawals: withdrawalsBlockOut{
			PendingCNY:   round2(wd["pending"]),
			FrozenCNY:    round2(wallet.FrozenCNY),
			WithdrawnCNY: round2(wd["approved"]),
			RejectedCNY:  round2(wd["rejected"]),
		},
		Exchange: exchangeBlockOut{
			QuotaPerUnit:    common.QuotaPerUnit,
			USDExchangeRate: operation_setting.USDExchangeRate,
		},
		Overview: overview,
	})
}

// adminFinanceOverview 装配管理端财务总览（6 项，主站/代理站分列；doc/finance-model-report-v3.md
// §二）。三个透镜均已有跨租户 per-tenant 聚合可复用（SubscriptionPaidCost/EarningsByTenant/
// ConsumptionByTenant，均已被 AgentRanking 用过），此处只按「租户是否为平台租户」二分求和，不新增查询。
func (a *App) adminFinanceOverview(ctx context.Context, start, end int64) (adminFinanceOverviewOut, error) {
	repo := a.ReportRepo
	platformID := a.resolvePlatformTenantID(ctx)

	subPC, err := repo.SubscriptionPaidCost(ctx, nil, start, end)
	if err != nil {
		return adminFinanceOverviewOut{}, err
	}
	earn, err := repo.EarningsByTenant(ctx, start, end)
	if err != nil {
		return adminFinanceOverviewOut{}, err
	}
	cons, err := repo.ConsumptionByTenant(ctx, start, end)
	if err != nil {
		return adminFinanceOverviewOut{}, err
	}

	subPaidByTenant := make(map[int64]float64, len(subPC))
	for tid, pc := range subPC {
		subPaidByTenant[tid] = pc.PaidCNY
	}
	mainsiteTokenplanRevenue, agentTokenplanRevenue := splitByPlatform(subPaidByTenant, platformID)

	tokenplanSpreadByTenant := make(map[int64]float64, len(earn))
	apiRebateByTenant := make(map[int64]float64, len(earn))
	for tid, e := range earn {
		tokenplanSpreadByTenant[tid] = e.TokenplanSpreadCNY
		apiRebateByTenant[tid] = e.RatioMarkupCNY + e.ConsumeCommissionCNY
	}
	// tokenplan_rebate_cny / agent_api_rebate_cny 口径是「给*代理*的返现/需返现*代理*的消耗」（spec
	// 原文"各代理"），只取代理站半（丢弃 mainsite 半）——平台不是代理，不给自己发返现；正常配置下
	// 平台租户本就不会产生 tokenplan_spread/ratio_markup/consume_commission（platform 自身列表的
	// retail_price 与 plan.AgentCostPrice 相同、且从不为其 SetAgentType/EnsureWallet，见 seed.go），
	// 这里的排除是双保险，不依赖那个假设也成立。
	_, tokenplanRebate := splitByPlatform(tokenplanSpreadByTenant, platformID)
	_, agentAPIRebate := splitByPlatform(apiRebateByTenant, platformID)

	consCNYByTenant := make(map[int64]float64, len(cons))
	for tid, c := range cons {
		consCNYByTenant[tid] = reportrepo.QuotaToCNY(c.UsedQuota)
	}
	mainsiteWalletConsumption, agentWalletConsumption := splitByPlatform(consCNYByTenant, platformID)

	return adminFinanceOverviewOut{
		MainsiteTokenplanRevenueCNY:  round2(mainsiteTokenplanRevenue),
		AgentTokenplanRevenueCNY:     round2(agentTokenplanRevenue),
		TokenplanRebateCNY:           round2(tokenplanRebate),
		MainsiteWalletConsumptionCNY: round2(mainsiteWalletConsumption),
		AgentWalletConsumptionCNY:    round2(agentWalletConsumption),
		AgentAPIRebateCNY:            round2(agentAPIRebate),
	}, nil
}

// resolvePlatformTenantID 解析平台（主站）直销租户 ID（seed.go platformTenant/platformSlug）；
// 未 seed / 查询失败 / TenantRepo 未装配（如仅挂了 ReportRepo 的最小化测试 App）一律返回 0
// （best-effort，报表不因此报错——0 是安全哨兵：真实租户 ID 恒 >0，且跨租户聚合已用 tenant_id<>0
// 排除未归属数据，不会误撞该哨兵值）。
func (a *App) resolvePlatformTenantID(ctx context.Context) int64 {
	if a.TenantRepo == nil {
		return 0
	}
	t, err := a.platformTenant(ctx)
	if err != nil || t == nil {
		return 0
	}
	return t.ID
}

// splitByPlatform 按 tenant_id==platformID 把 per-tenant 金额 map 二分求和为 (主站合计, 代理站合计)。
// platformID<=0（平台租户未 seed / 解析失败）时全部计入代理站合计，保守地不误判任何真实租户为主站。
func splitByPlatform(m map[int64]float64, platformID int64) (mainsite, agentSite float64) {
	for tid, v := range m {
		if platformID > 0 && tid == platformID {
			mainsite += v
			continue
		}
		agentSite += v
	}
	return
}

func (a *App) handleFinanceTrend(c *gin.Context, tenantID *int64) {
	start, end, err := parseTimeRange(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	gran, err := parseGranularity(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	lens, err := parseLens(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	ctx := reqCtx(c)
	repo := a.ReportRepo

	var series any
	switch lens {
	case "earnings":
		pts, e := repo.TrendEarnings(ctx, tenantID, start, end, gran)
		if e != nil {
			respondErr(c, e)
			return
		}
		out := make([]earningsTrendOut, 0, len(pts))
		for _, p := range pts {
			out = append(out, earningsTrendOut{Bucket: p.Bucket, BucketTS: p.BucketTS, AmountCNY: round2(p.AmountCNY)})
		}
		series = out
	case "recharge":
		pts, e := repo.TrendRecharge(ctx, tenantID, start, end, gran)
		if e != nil {
			respondErr(c, e)
			return
		}
		out := make([]rechargeTrendOut, 0, len(pts))
		for _, p := range pts {
			out = append(out, rechargeTrendOut{
				Bucket:                p.Bucket,
				BucketTS:              p.BucketTS,
				RechargePaidCNY:       round2(p.RechargePaidCNY),
				SubscriptionPaidCNY:   round2(p.SubscriptionPaidCNY),
				SubscriptionCostCNY:   round2(p.SubscriptionCostCNY),
				SubscriptionSpreadCNY: round2(p.SubscriptionSpreadCNY),
			})
		}
		series = out
	case "consumption":
		pts, e := repo.ConsumptionTrend(ctx, tenantID, start, end, gran)
		if e != nil {
			respondErr(c, e)
			return
		}
		out := make([]consumptionTrendOut, 0, len(pts))
		for _, p := range pts {
			out = append(out, consumptionTrendOut{
				Bucket:      p.Bucket,
				BucketTS:    p.BucketTS,
				UsedQuota:   p.UsedQuota,
				UsedCostCNY: round2(p.UsedCostCNY),
				Calls:       p.Calls,
				Tokens:      p.Tokens,
			})
		}
		series = out
	case "withdrawals":
		pts, e := repo.TrendWithdrawals(ctx, tenantID, start, end, gran)
		if e != nil {
			respondErr(c, e)
			return
		}
		out := make([]withdrawalsTrendOut, 0, len(pts))
		for _, p := range pts {
			out = append(out, withdrawalsTrendOut{
				Bucket:       p.Bucket,
				BucketTS:     p.BucketTS,
				PendingCNY:   round2(p.PendingCNY),
				WithdrawnCNY: round2(p.WithdrawnCNY),
				RejectedCNY:  round2(p.RejectedCNY),
			})
		}
		series = out
	}
	respondOK(c, trendOut{Lens: lens, Granularity: gran, Series: series})
}

func (a *App) handleFinanceDetail(c *gin.Context, tenantID *int64) {
	start, end, err := parseTimeRange(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	lens, err := parseLens(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	format, err := parseFormat(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	ctx := reqCtx(c)
	scoped := tenantID != nil

	if format != "" {
		a.exportFinanceDetail(c, ctx, tenantID, start, end, lens, format, scoped)
		return
	}

	page, pageSize := parsePaging(c)
	repo := a.ReportRepo

	switch lens {
	case "earnings":
		rows, total, e := repo.DetailEarnings(ctx, tenantID, start, end, page, pageSize)
		if e != nil {
			respondErr(c, e)
			return
		}
		items := make([]earningDetailOut, 0, len(rows))
		for _, r := range rows {
			o := earningDetailOut{
				SourceType: r.SourceType,
				AmountCNY:  round2(r.AmountCNY),
				Reference:  r.Reference,
				CreatedAt:  isoUTC(r.CreatedAt),
			}
			if !scoped {
				o.TenantID = r.TenantID
				o.AgentName = r.AgentName
			}
			items = append(items, o)
		}
		respondOK(c, detailOut{Items: items, Total: total, Page: page, PageSize: pageSize})
	case "withdrawals":
		rows, total, e := repo.DetailWithdrawals(ctx, tenantID, start, end, page, pageSize)
		if e != nil {
			respondErr(c, e)
			return
		}
		items := make([]withdrawalDetailOut, 0, len(rows))
		for _, r := range rows {
			o := withdrawalDetailOut{
				ID:         r.ID,
				AmountCNY:  round2(r.AmountCNY),
				Status:     r.Status,
				CreatedAt:  isoUTC(r.CreatedAt),
				ReviewedAt: isoPtr(r.ReviewedAt),
			}
			if !scoped {
				o.TenantID = r.TenantID
				o.AgentName = r.AgentName
			}
			items = append(items, o)
		}
		respondOK(c, detailOut{Items: items, Total: total, Page: page, PageSize: pageSize})
	case "recharge":
		rows, total, e := repo.DetailRecharge(ctx, tenantID, start, end, page, pageSize)
		if e != nil {
			respondErr(c, e)
			return
		}
		items := make([]rechargeDetailOut, 0, len(rows))
		for _, r := range rows {
			o := rechargeDetailOut{
				OrderNo:           r.OrderNo,
				Kind:              r.Kind,
				Provider:          r.Provider,
				AmountUSD:         round2(r.AmountUSD),
				ActualPaidCNY:     round2(r.ActualPaidCNY),
				AgentCostPriceCNY: round2(r.AgentCostPriceCNY),
				Status:            r.Status,
				CreatedAt:         isoUTC(r.CreatedAt),
			}
			if !scoped {
				o.TenantID = r.TenantID
			}
			items = append(items, o)
		}
		respondOK(c, detailOut{Items: items, Total: total, Page: page, PageSize: pageSize})
	case "consumption":
		rows, total, e := repo.DetailConsumption(ctx, tenantID, start, end, page, pageSize)
		if e != nil {
			respondErr(c, e)
			return
		}
		items := make([]consumptionDetailOut, 0, len(rows))
		for _, r := range rows {
			o := consumptionDetailOut{
				ModelName:   r.ModelName,
				Calls:       r.Calls,
				Tokens:      r.Tokens,
				UsedQuota:   r.UsedQuota,
				UsedCostCNY: round2(r.UsedCostCNY),
			}
			if !scoped {
				o.TenantID = r.TenantID
				o.AgentName = r.AgentName
			}
			items = append(items, o)
		}
		respondOK(c, detailOut{Items: items, Total: total, Page: page, PageSize: pageSize})
	}
}

// ============================================================================
// 导出（?format=csv|pdf）：handler 取全集 + 渲染表格，委托 report_export.go（S3）写出
// ============================================================================

// exportFinanceDetail 取该透镜全集（page=1, reportExportMaxRows），渲染为 reportExportTable
// （列序对齐 JSON 字段名；代理 scope 省略 tenant_id/agent_name 列），再委托 S3 的 writeDetail{CSV,PDF}。
func (a *App) exportFinanceDetail(c *gin.Context, ctx context.Context, tenantID *int64, start, end int64, lens, format string, scoped bool) {
	repo := a.ReportRepo
	table := reportExportTable{
		Filename: fmt.Sprintf("finance-%s-%d-%d", lens, start, end),
		Title:    "Finance report — " + lens,
	}

	switch lens {
	case "earnings":
		rows, _, e := repo.DetailEarnings(ctx, tenantID, start, end, 1, reportExportMaxRows)
		if e != nil {
			respondErr(c, errReportExportFailed)
			return
		}
		if scoped {
			table.Headers = []string{"source_type", "amount_cny", "reference", "created_at"}
		} else {
			table.Headers = []string{"tenant_id", "agent_name", "source_type", "amount_cny", "reference", "created_at"}
		}
		for _, r := range rows {
			rec := make([]string, 0, len(table.Headers))
			if !scoped {
				rec = append(rec, strconv.FormatInt(r.TenantID, 10), r.AgentName)
			}
			rec = append(rec, r.SourceType, formatMoney(r.AmountCNY), r.Reference, isoUTC(r.CreatedAt))
			table.Rows = append(table.Rows, rec)
		}
	case "withdrawals":
		rows, _, e := repo.DetailWithdrawals(ctx, tenantID, start, end, 1, reportExportMaxRows)
		if e != nil {
			respondErr(c, errReportExportFailed)
			return
		}
		if scoped {
			table.Headers = []string{"id", "amount_cny", "status", "created_at", "reviewed_at"}
		} else {
			table.Headers = []string{"id", "tenant_id", "agent_name", "amount_cny", "status", "created_at", "reviewed_at"}
		}
		for _, r := range rows {
			rec := make([]string, 0, len(table.Headers))
			rec = append(rec, strconv.FormatInt(r.ID, 10))
			if !scoped {
				rec = append(rec, strconv.FormatInt(r.TenantID, 10), r.AgentName)
			}
			rec = append(rec, formatMoney(r.AmountCNY), r.Status, isoUTC(r.CreatedAt), isoPtr(r.ReviewedAt))
			table.Rows = append(table.Rows, rec)
		}
	case "recharge":
		rows, _, e := repo.DetailRecharge(ctx, tenantID, start, end, 1, reportExportMaxRows)
		if e != nil {
			respondErr(c, errReportExportFailed)
			return
		}
		if scoped {
			table.Headers = []string{"order_no", "kind", "provider", "amount_usd", "actual_paid_cny", "agent_cost_price_cny", "status", "created_at"}
		} else {
			table.Headers = []string{"order_no", "tenant_id", "kind", "provider", "amount_usd", "actual_paid_cny", "agent_cost_price_cny", "status", "created_at"}
		}
		for _, r := range rows {
			rec := make([]string, 0, len(table.Headers))
			rec = append(rec, r.OrderNo)
			if !scoped {
				rec = append(rec, strconv.FormatInt(r.TenantID, 10))
			}
			rec = append(rec, r.Kind, r.Provider, formatMoney(r.AmountUSD), formatMoney(r.ActualPaidCNY), formatMoney(r.AgentCostPriceCNY), r.Status, isoUTC(r.CreatedAt))
			table.Rows = append(table.Rows, rec)
		}
	case "consumption":
		rows, _, e := repo.DetailConsumption(ctx, tenantID, start, end, 1, reportExportMaxRows)
		if e != nil {
			respondErr(c, errReportExportFailed)
			return
		}
		if scoped {
			table.Headers = []string{"model_name", "calls", "tokens", "used_quota", "used_cost_cny"}
		} else {
			table.Headers = []string{"tenant_id", "agent_name", "model_name", "calls", "tokens", "used_quota", "used_cost_cny"}
		}
		for _, r := range rows {
			rec := make([]string, 0, len(table.Headers))
			if !scoped {
				rec = append(rec, strconv.FormatInt(r.TenantID, 10), r.AgentName)
			}
			rec = append(rec, r.ModelName, strconv.FormatInt(r.Calls, 10), strconv.FormatInt(r.Tokens, 10), strconv.FormatInt(r.UsedQuota, 10), formatMoney(r.UsedCostCNY))
			table.Rows = append(table.Rows, rec)
		}
	}

	var werr error
	switch format {
	case "csv":
		werr = writeDetailCSV(c, table)
	case "pdf":
		werr = writeDetailPDF(c, table)
	}
	// 约定：writeDetail{CSV,PDF} 先在内存缓冲完整渲染、成功才向 c.Writer 写首字节；
	// 故渲染失败时仍可返回干净的 REPORT_EXPORT_FAILED JSON。
	if werr != nil {
		respondErr(c, errReportExportFailed)
	}
}

// ============================================================================
// 入参解析 / 校验（区间 / 粒度 / 透镜 / 格式 / 分页 / 排序）
// ============================================================================

// parseTimeRange 解析并校验 epoch 秒区间：缺失/非法/start>end/跨度>366天 → STATS_RANGE_INVALID。
func parseTimeRange(c *gin.Context) (int64, int64, error) {
	start, err1 := strconv.ParseInt(strings.TrimSpace(c.Query("start_timestamp")), 10, 64)
	end, err2 := strconv.ParseInt(strings.TrimSpace(c.Query("end_timestamp")), 10, 64)
	if err1 != nil || err2 != nil || start <= 0 || end <= 0 || start > end || end-start > maxRangeSeconds {
		return 0, 0, stats.ErrRangeInvalid
	}
	return start, end, nil
}

// parseGranularity 校验趋势粒度（day/week/month）；否则 REPORT_GRANULARITY_INVALID。
func parseGranularity(c *gin.Context) (string, error) {
	switch g := c.Query("granularity"); g {
	case "day", "week", "month":
		return g, nil
	default:
		return "", errReportGranularityInvalid
	}
}

// parseLens 校验透镜（earnings/recharge/consumption/withdrawals）；否则 REPORT_LENS_INVALID。
func parseLens(c *gin.Context) (string, error) {
	switch l := c.Query("lens"); l {
	case "earnings", "recharge", "consumption", "withdrawals":
		return l, nil
	default:
		return "", errReportLensInvalid
	}
}

// parseFormat 校验导出格式：空 → JSON（""）；csv/pdf → 导出；否则 REPORT_FORMAT_INVALID。
func parseFormat(c *gin.Context) (string, error) {
	switch f := c.Query("format"); f {
	case "":
		return "", nil
	case "csv", "pdf":
		return f, nil
	default:
		return "", errReportFormatInvalid
	}
}

// parsePaging 解析分页并归一化（page<=0→1，page_size<=0→20）；用于响应回显（仓储另有同口径兜底）。
func parsePaging(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(c.Query("page_size")))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return page, pageSize
}

// normRankSort 归一化排序字段（非白名单回退 total_earned_cny），保证回显与实际排序一致。
func normRankSort(s string) string {
	if _, ok := rankSortWhitelist[s]; ok {
		return s
	}
	return "total_earned_cny"
}

// normOrder 归一化排序方向：asc 即升序，其余一律 desc（与仓储口径一致）。
func normOrder(s string) string {
	if s == "asc" {
		return "asc"
	}
	return "desc"
}

// ============================================================================
// 小工具
// ============================================================================

// isoPtr 把 *time.Time 格式化为 ISO-8601 UTC；nil/零值 → 空串（复用 http.go 的 isoUTC）。
func isoPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return isoUTC(*t)
}

// formatMoney 把金额渲染为定 2 位小数字符串（导出 CSV/PDF 列用，与 JSON round2 同口径）。
func formatMoney(v float64) string {
	return strconv.FormatFloat(round2(v), 'f', 2, 64)
}
