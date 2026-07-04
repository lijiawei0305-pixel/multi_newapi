package mtwire

// 支付概览（doc/payment-overview.md）：增强对账页——4 态汇总(笔数+金额) + 可筛分页订单列表。
// 数据全查 payment_orders（充值+套餐同表，共用 created/paid/credited/failed 四态）。跨租户 AdminAuth。

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// paymentSummaryOut 是某状态的汇总（笔数 + 实付金额¥）。summary 恒输出 4 态（缺补 0）。
type paymentSummaryOut struct {
	Status    string  `json:"status"` // created | paid | credited | failed
	Count     int64   `json:"count"`
	AmountCNY float64 `json:"amount_cny"`
}

// paymentOrderOut 是列表的一行支付订单。
type paymentOrderOut struct {
	OrderNo     string  `json:"order_no"`
	Type        string  `json:"type"`     // recharge | subscription
	Provider    string  `json:"provider"` // wxpay | alipay
	AmountCNY   float64 `json:"amount_cny"`
	Status      string  `json:"status"`
	UserID      int64   `json:"user_id"`
	TenantID    int64   `json:"tenant_id"`
	CreatedAtTS int64   `json:"created_at_ts"` // epoch 秒（前端按东八区格式化）
}

type paymentOrdersPage struct {
	Items    []paymentOrderOut `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

type paymentOverviewOut struct {
	Summary []paymentSummaryOut `json:"summary"`
	Orders  paymentOrdersPage   `json:"orders"`
}

// paymentOverviewStatuses 是 summary 恒定输出的 4 态顺序。
var paymentOverviewStatuses = []string{"created", "paid", "credited", "failed"}

// HandleAdminPaymentOverview GET /api/admin/reconcile/overview —— 支付概览（4 态汇总 + 分页订单）。需 AdminAuth。
// 入参：start_timestamp/end_timestamp（epoch 秒，parseTimeRange）+ provider/type（可选过滤）+ status（仅作用于列表）
// + page/page_size。summary 按 时间+方式+类型 聚合（不含 status），列表再叠加 status。金额取 actual_paid（¥）。
func (a *App) HandleAdminPaymentOverview(c *gin.Context) {
	start, end, err := parseTimeRange(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	ctx := reqCtx(c)
	provider := c.Query("provider")
	typ := c.Query("type")
	status := c.Query("status")
	page, pageSize := parseOverviewPage(c)

	// scoped 每次返回一条独立的、已叠加「时间+方式+类型」过滤的查询链（供 summary/list 各自复用，互不污染）。
	scoped := func() *gorm.DB {
		q := a.DB.WithContext(ctx).Table("payment_orders").
			Where("created_at >= ? AND created_at <= ?", time.Unix(start, 0), time.Unix(end, 0))
		if provider != "" {
			q = q.Where("provider = ?", provider)
		}
		if typ != "" {
			q = q.Where("type = ?", typ)
		}
		return q
	}

	// summary：按 status 聚合（不含 status 过滤——4 卡恒显全部），缺的态补 0。
	var aggRows []struct {
		Status string
		Cnt    int64
		Amt    float64
	}
	if err := scoped().
		Select("status, COUNT(*) AS cnt, COALESCE(SUM(actual_paid),0) AS amt").
		Group("status").Scan(&aggRows).Error; err != nil {
		respondErr(c, err)
		return
	}
	type agg struct {
		Cnt int64
		Amt float64
	}
	byStatus := make(map[string]agg, len(aggRows))
	for _, r := range aggRows {
		byStatus[r.Status] = agg{r.Cnt, r.Amt}
	}
	summary := make([]paymentSummaryOut, 0, len(paymentOverviewStatuses))
	for _, s := range paymentOverviewStatuses {
		v := byStatus[s]
		summary = append(summary, paymentSummaryOut{Status: s, Count: v.Cnt, AmountCNY: round2(v.Amt)})
	}

	// orders：同过滤 + 可选 status，按时间倒序分页。
	listQ := scoped()
	if status != "" {
		listQ = listQ.Where("status = ?", status)
	}
	var total int64
	if err := listQ.Count(&total).Error; err != nil {
		respondErr(c, err)
		return
	}
	var rows []struct {
		OrderNo    string
		Type       string
		Provider   string
		ActualPaid float64
		Status     string
		UserID     int64
		TenantID   int64
		CreatedAt  time.Time
	}
	if err := listQ.
		Select("order_no, type, provider, actual_paid, status, user_id, tenant_id, created_at").
		Order("created_at DESC").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Scan(&rows).Error; err != nil {
		respondErr(c, err)
		return
	}
	items := make([]paymentOrderOut, 0, len(rows))
	for _, r := range rows {
		items = append(items, paymentOrderOut{
			OrderNo:     r.OrderNo,
			Type:        r.Type,
			Provider:    r.Provider,
			AmountCNY:   round2(r.ActualPaid),
			Status:      r.Status,
			UserID:      r.UserID,
			TenantID:    r.TenantID,
			CreatedAtTS: r.CreatedAt.Unix(),
		})
	}

	respondOK(c, paymentOverviewOut{
		Summary: summary,
		Orders:  paymentOrdersPage{Items: items, Total: total, Page: page, PageSize: pageSize},
	})
}

// parseOverviewPage 解析分页（page≥1，page_size∈[1,100]，默认 1/20）。
func parseOverviewPage(c *gin.Context) (page, pageSize int) {
	page, pageSize = 1, 20
	if p, err := strconv.Atoi(c.Query("page")); err == nil && p >= 1 {
		page = p
	}
	if s, err := strconv.Atoi(c.Query("page_size")); err == nil && s >= 1 && s <= 100 {
		pageSize = s
	}
	return page, pageSize
}
