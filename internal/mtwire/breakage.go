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

// breakage 监控 HTTP 层（P2-BRK-01）：把 internal/breakage 的领域结果装配成 snake_case DTO 并经
// 统一信封返回。3 端点：overview（4 卡汇总）/ detail（订阅维明细，筛选+分页+?format=csv 导出）/
// snapshots（历史快照趋势，按期）。
//
// 硬约束（与 internal/mtwire/http.go HandleAdminListSubscriptions 完全一致的租户作用域范式）：
//   - 【门 #4 租户隔离】scope 由 tenantFrom(c) 解析：命中租户（子域名/自定义域名）→ tenantID=&t.ID
//     隔离本租户；未命中（主站 apex/www）→ tenantID=nil 管理员看全平台（repo 侧统一 tenant_id<>0）。
//     绝不从 query/body 读 tenant_id。
//   - 【金额】一律 float64，_usd 后缀；round2 由 breakage.Service 边界完成（本层仅装配 DTO）；计数整数。
//   - 【时间】入参 epoch 秒（start_timestamp/end_timestamp）；出参订阅维明细的 period_end 用 isoUTC
//     （ISO-8601 UTC，与订阅监控页一致）；快照趋势的 period_end 用 epoch 秒（桶锚点，前端自格式化横轴）。
//   - 错误码经 respondErr 统一转 HTTP + JSON（breakage.ErrRangeInvalid / ErrAlertLevelInvalid /
//     ErrFormatInvalid / ErrExportFailed）。

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/breakage"
)

// nowUnix 返回服务端当前时间（epoch 秒），供 breakage handler/job 统一取 now（可在测试整体替换 time.Now）。
func nowUnix() int64 { return time.Now().Unix() }

// isoUnix 把 epoch 秒转 ISO-8601 UTC（复用 http.go 的 isoUTC；<=0 → 空串，与零值同义）。
func isoUnix(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return isoUTC(time.Unix(sec, 0).UTC())
}

// ============================================================================
// DTO（snake_case；对齐 restContract；金额已由 Service 边界 round2，本层直接透传）
// ============================================================================

// breakageOverviewOut 是 GET /api/admin/breakage/overview 的 data（4 卡汇总）。
type breakageOverviewOut struct {
	ActiveRemainingUSD float64 `json:"active_remaining_usd"`
	ExpiredUnusedUSD   float64 `json:"expired_unused_usd"`
	WalletUnusedUSD    float64 `json:"wallet_unused_usd"`
	AnomalyCount       int     `json:"anomaly_count"`
}

// breakageDetailRowOut 是 detail 明细的一行。tenant_id/tenant_name 在代理作用域（scoped）下经 omitempty
// 省略（单租户无需冗余，与 report.go earningDetailOut 同惯例）；alert_level 空串=健康。
type breakageDetailRowOut struct {
	TenantID   int64   `json:"tenant_id,omitempty"`
	TenantName string  `json:"tenant_name,omitempty"`
	UserID     int64   `json:"user_id"`
	Username   string  `json:"username"`
	PlanCode   string  `json:"plan_code"`
	Status     string  `json:"status"`
	LimitUSD   float64 `json:"limit_usd"`
	UsedUSD    float64 `json:"used_usd"`
	UnusedUSD  float64 `json:"unused_usd"`
	UsagePct   float64 `json:"usage_pct"`
	AlertLevel string  `json:"alert_level"`
	PeriodEnd  string  `json:"period_end"` // ISO-8601 UTC
}

// breakageDetailOut 是 detail JSON 响应体（分页嵌套，镜像 report.go detailOut）。
type breakageDetailOut struct {
	Items    []breakageDetailRowOut `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

// breakageSnapshotPointOut 是 snapshots 趋势的一个桶。period_end 为 epoch 秒（桶锚点）。
type breakageSnapshotPointOut struct {
	PeriodEnd          int64   `json:"period_end"` // epoch 秒
	ExpiredUnusedUSD   float64 `json:"expired_unused_usd"`
	ActiveRemainingUSD float64 `json:"active_remaining_usd"`
	SubscriptionCount  int     `json:"subscription_count"`
}

// breakageSnapshotsOut 是 snapshots 响应体（升序时间序列）。
type breakageSnapshotsOut struct {
	Series []breakageSnapshotPointOut `json:"series"`
}

// ============================================================================
// 作用域解析（与 HandleAdminListSubscriptions 完全一致）
// ============================================================================

// breakageScope 按已解析出的租户判隔离：命中租户 → &t.ID（代理隔离本租户）；未命中 → nil（管理端全平台）。
// 【安全】只读 tenantFrom(c)（鉴权中间件从 Host 解析），绝不从 query/body 取 tenant_id。
func breakageScope(c *gin.Context) *int64 {
	if t := tenantFrom(c); t != nil {
		id := t.ID
		return &id
	}
	return nil
}

// ============================================================================
// Handlers
// ============================================================================

// HandleAdminBreakageOverview GET /api/admin/breakage/overview —— 4 卡汇总（活跃剩余/到期未用/钱包未消耗/
// 系统异常）。需 AdminAuth + TenantMiddleware。now = 服务端时间；金额已由 Service round2。
func (a *App) HandleAdminBreakageOverview(c *gin.Context) {
	ctx := reqCtx(c)
	ov, err := a.Breakage.Overview(ctx, breakageScope(c), nowUnix())
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, breakageOverviewOut{
		ActiveRemainingUSD: ov.ActiveRemainingUSD,
		ExpiredUnusedUSD:   ov.ExpiredUnusedUSD,
		WalletUnusedUSD:    ov.WalletUnusedUSD,
		AnomalyCount:       ov.AnomalyCount,
	})
}

// HandleAdminBreakageDetail GET /api/admin/breakage/detail —— 订阅维明细（筛选 + 分页 + ?format=csv 导出）。
// 需 AdminAuth + TenantMiddleware。筛选：plan_code / [start_timestamp,end_timestamp]（按 period_end）/
// alert_level（warn|critical|exhausted）；format 空=JSON，csv=下载。Service 校验区间/告警级合法性。
func (a *App) HandleAdminBreakageDetail(c *gin.Context) {
	tenantID := breakageScope(c)
	scoped := tenantID != nil

	level, err := parseBreakageAlertLevel(c)
	if err != nil {
		respondErr(c, err)
		return
	}
	format, err := parseBreakageFormat(c)
	if err != nil {
		respondErr(c, err)
		return
	}

	start := parseOptionalEpoch(c, "start_timestamp")
	end := parseOptionalEpoch(c, "end_timestamp")
	page, pageSize := parsePaging(c)

	filter := breakage.Filter{
		TenantID:   tenantID,
		PlanCode:   strings.TrimSpace(c.Query("plan_code")),
		Start:      start,
		End:        end,
		AlertLevel: level,
		Page:       page,
		PageSize:   pageSize,
	}

	// 导出：取全集（page=1, breakageExportMaxRows）委托 writeDetailCSV（复用 report_export.go 惯例）。
	if format == "csv" {
		a.exportBreakageDetail(c, reqCtx(c), filter, scoped, start, end)
		return
	}

	rows, total, err := a.Breakage.Detail(reqCtx(c), filter, nowUnix())
	if err != nil {
		respondErr(c, err)
		return
	}
	items := make([]breakageDetailRowOut, 0, len(rows))
	for i := range rows {
		items = append(items, toBreakageDetailRowOut(&rows[i], scoped))
	}
	respondOK(c, breakageDetailOut{Items: items, Total: total, Page: page, PageSize: pageSize})
}

// HandleAdminBreakageSnapshots GET /api/admin/breakage/snapshots —— 历史快照趋势（按期）。
// 需 AdminAuth + TenantMiddleware。start_timestamp/end_timestamp 必填（缺失/start>end/超跨度 →
// BREAKAGE_RANGE_INVALID，由 Service.Snapshots 校验）。data.series 升序（epoch 秒锚点）。
func (a *App) HandleAdminBreakageSnapshots(c *gin.Context) {
	start, end, ok := parseRequiredEpochRange(c)
	if !ok {
		// 缺失/非法：与 Service 同错误码（区间必填端点在解析层直接判，避免把 0 当"不设"透传）。
		respondErr(c, breakage.ErrRangeInvalid)
		return
	}
	pts, err := a.Breakage.Snapshots(reqCtx(c), breakageScope(c), start, end)
	if err != nil {
		respondErr(c, err)
		return
	}
	series := make([]breakageSnapshotPointOut, 0, len(pts))
	for _, p := range pts {
		series = append(series, breakageSnapshotPointOut{
			PeriodEnd:          p.PeriodEnd,
			ExpiredUnusedUSD:   p.ExpiredUnusedUSD,
			ActiveRemainingUSD: p.ActiveRemainingUSD,
			SubscriptionCount:  p.SubscriptionCount,
		})
	}
	respondOK(c, breakageSnapshotsOut{Series: series})
}

// ============================================================================
// CSV 导出（?format=csv）：取全集 + 渲染 reportExportTable，委托 report_export.go 的 writeDetailCSV
// ============================================================================

// breakageExportMaxRows 是导出时单次取回的行数上限（非热路径；分页 page=1 取全集）。
const breakageExportMaxRows = 100000

// exportBreakageDetail 取该作用域全集明细，渲染为 reportExportTable（列序对齐 JSON 字段名；代理 scope
// 省略 tenant_id/tenant_name 列；金额 formatMoney 2 位；period_end ISO-8601），委托 writeDetailCSV。
func (a *App) exportBreakageDetail(c *gin.Context, ctx context.Context, filter breakage.Filter, scoped bool, start, end int64) {
	full := filter
	full.Page = 1
	full.PageSize = breakageExportMaxRows

	rows, _, err := a.Breakage.Detail(ctx, full, nowUnix())
	if err != nil {
		respondErr(c, breakage.ErrExportFailed)
		return
	}

	table := reportExportTable{
		Filename: "breakage-detail-" + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10),
		Title:    "Breakage detail",
	}
	if scoped {
		table.Headers = []string{"user_id", "username", "plan_code", "status", "limit_usd", "used_usd", "unused_usd", "usage_pct", "alert_level", "period_end"}
	} else {
		table.Headers = []string{"tenant_id", "tenant_name", "user_id", "username", "plan_code", "status", "limit_usd", "used_usd", "unused_usd", "usage_pct", "alert_level", "period_end"}
	}
	for i := range rows {
		r := &rows[i]
		rec := make([]string, 0, len(table.Headers))
		if !scoped {
			rec = append(rec, strconv.FormatInt(r.TenantID, 10), r.TenantName)
		}
		rec = append(rec,
			strconv.FormatInt(r.UserID, 10),
			r.Username,
			r.PlanCode,
			r.Status,
			formatMoney(r.LimitUSD),
			formatMoney(r.UsedUSD),
			formatMoney(r.UnusedUSD),
			formatMoney(r.UsagePct),
			string(r.AlertLevel),
			isoUnix(r.PeriodEnd),
		)
		table.Rows = append(table.Rows, rec)
	}

	if err := writeDetailCSV(c, table); err != nil {
		respondErr(c, breakage.ErrExportFailed)
	}
}

// ============================================================================
// 映射 + 入参解析辅助
// ============================================================================

// toBreakageDetailRowOut 把领域行映射为 DTO 行（scoped 时省略 tenant_id/tenant_name；period_end 转 ISO-8601）。
func toBreakageDetailRowOut(r *breakage.DetailRow, scoped bool) breakageDetailRowOut {
	out := breakageDetailRowOut{
		UserID:     r.UserID,
		Username:   r.Username,
		PlanCode:   r.PlanCode,
		Status:     r.Status,
		LimitUSD:   r.LimitUSD,
		UsedUSD:    r.UsedUSD,
		UnusedUSD:  r.UnusedUSD,
		UsagePct:   r.UsagePct,
		AlertLevel: string(r.AlertLevel),
		PeriodEnd:  isoUnix(r.PeriodEnd),
	}
	if !scoped {
		out.TenantID = r.TenantID
		out.TenantName = r.TenantName
	}
	return out
}

// parseBreakageAlertLevel 解析 alert_level 过滤参数：空=不过滤；warn/critical/exhausted 合法；
// 否则 BREAKAGE_ALERT_LEVEL_INVALID。（Service 亦复校，此处早判给出精确码。）
func parseBreakageAlertLevel(c *gin.Context) (breakage.AlertLevel, error) {
	switch l := strings.TrimSpace(c.Query("alert_level")); l {
	case "":
		return breakage.AlertNone, nil
	case string(breakage.AlertWarn):
		return breakage.AlertWarn, nil
	case string(breakage.AlertCritical):
		return breakage.AlertCritical, nil
	case string(breakage.AlertExhausted):
		return breakage.AlertExhausted, nil
	default:
		return breakage.AlertNone, breakage.ErrAlertLevelInvalid
	}
}

// parseBreakageFormat 解析 format 参数：空=JSON；csv=导出；否则 BREAKAGE_FORMAT_INVALID。
func parseBreakageFormat(c *gin.Context) (string, error) {
	switch f := strings.TrimSpace(c.Query("format")); f {
	case "":
		return "", nil
	case "csv":
		return "csv", nil
	default:
		return "", breakage.ErrFormatInvalid
	}
}

// parseOptionalEpoch 解析可选 epoch 秒参数（缺失/非法 → 0，交由下游按"不设该端"兜底）。
func parseOptionalEpoch(c *gin.Context, key string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(c.Query(key)), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// parseRequiredEpochRange 解析必填 epoch 秒区间（snapshots 端点）：任一端缺失/非法/<=0/start>end/
// 超跨度上限 → (0,0,false)。跨度上限与 Service.maxRangeSeconds 同口径（366 天）。
func parseRequiredEpochRange(c *gin.Context) (int64, int64, bool) {
	start, err1 := strconv.ParseInt(strings.TrimSpace(c.Query("start_timestamp")), 10, 64)
	end, err2 := strconv.ParseInt(strings.TrimSpace(c.Query("end_timestamp")), 10, 64)
	if err1 != nil || err2 != nil || start <= 0 || end <= 0 || start > end || end-start > maxRangeSeconds {
		return 0, 0, false
	}
	return start, end, true
}
