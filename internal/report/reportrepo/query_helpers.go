package reportrepo

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ============================================================================
// 内部：分桶 SQL/Go 通用 + 名称解析 + LOG_DB 闸门
// ============================================================================

// bucketedSumDatetime 在主库 DATETIME 列上按日历桶 SUM(sumCol)，返回 label→sum。
// MySQL 用 DATE_FORMAT 在 DB 内 GROUP BY；其余方言取行在 Go 内分桶（UTC 一致）。
func (r *Repo) bucketedSumDatetime(ctx context.Context, table, sumCol, timeCol string, tenantID *int64, start, end int64, granularity string, extra func(*gorm.DB) *gorm.DB) (map[string]float64, error) {
	res := map[string]float64{}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		expr := mysqlBucketDatetime(timeCol, granularity)
		var rows []struct {
			Bucket string
			Val    float64
		}
		q := r.db.WithContext(ctx).Table(table).
			Select(expr+" AS bucket, COALESCE(SUM("+sumCol+"),0) AS val").
			Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "tenant_id", tenantID)
		if extra != nil {
			q = extra(q)
		}
		if err := q.Group(expr).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			res[x.Bucket] = x.Val
		}
		return res, nil
	}
	var rows []struct {
		CreatedAt time.Time
		Val       float64
	}
	q := r.db.WithContext(ctx).Table(table).
		Select(timeCol+" AS created_at, "+sumCol+" AS val").
		Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if extra != nil {
		q = extra(q)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		label, _ := bucketize(granularity, x.CreatedAt.Unix())
		res[label] += x.Val
	}
	return res, nil
}

// bucketedStatusSum 在主库 DATETIME 列上按 (日历桶, status) SUM(amount)，返回 label→status→sum。
func (r *Repo) bucketedStatusSum(ctx context.Context, table, timeCol string, tenantID *int64, start, end int64, granularity string) (map[string]map[string]float64, error) {
	res := map[string]map[string]float64{}
	add := func(label, status string, v float64) {
		if res[label] == nil {
			res[label] = map[string]float64{}
		}
		res[label][status] += v
	}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		expr := mysqlBucketDatetime(timeCol, granularity)
		var rows []struct {
			Bucket string
			Status string
			Val    float64
		}
		q := r.db.WithContext(ctx).Table(table).
			Select(expr+" AS bucket, status, COALESCE(SUM(amount),0) AS val").
			Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "tenant_id", tenantID)
		if err := q.Group(expr + ", status").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			add(x.Bucket, x.Status, x.Val)
		}
		return res, nil
	}
	var rows []struct {
		CreatedAt time.Time
		Status    string
		Amount    float64
	}
	q := r.db.WithContext(ctx).Table(table).
		Select(timeCol+" AS created_at, status, amount").
		Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		label, _ := bucketize(granularity, x.CreatedAt.Unix())
		add(label, x.Status, x.Amount)
	}
	return res, nil
}

// bucketedSubPaidCost 在套餐 JOIN 上按日历桶 SUM(零售, 成本)（按 o.created_at），返回 label→[paid,cost]。
func (r *Repo) bucketedSubPaidCost(ctx context.Context, tenantID *int64, start, end int64, granularity string) (map[string][2]float64, error) {
	res := map[string][2]float64{}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		expr := mysqlBucketDatetime("o.created_at", granularity)
		var rows []struct {
			Bucket string
			Paid   float64
			Cost   float64
		}
		q := r.db.WithContext(ctx).
			Table("pending_subscription_orders AS p").
			Select(expr+" AS bucket, COALESCE(SUM(p.retail_price),0) AS paid, COALESCE(SUM(p.agent_cost_price),0) AS cost").
			Joins("JOIN mt_subscription_orders AS o ON o.order_no = p.order_id").
			Where("o.status = ?", "activated").
			Where("o.created_at >= ? AND o.created_at <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "p.tenant_id", tenantID)
		if err := q.Group(expr).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			res[x.Bucket] = [2]float64{x.Paid, x.Cost}
		}
		return res, nil
	}
	var rows []struct {
		CreatedAt time.Time
		Paid      float64
		Cost      float64
	}
	q := r.db.WithContext(ctx).
		Table("pending_subscription_orders AS p").
		Select("o.created_at AS created_at, p.retail_price AS paid, p.agent_cost_price AS cost").
		Joins("JOIN mt_subscription_orders AS o ON o.order_no = p.order_id").
		Where("o.status = ?", "activated").
		Where("o.created_at >= ? AND o.created_at <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "p.tenant_id", tenantID)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		label, _ := bucketize(granularity, x.CreatedAt.Unix())
		cur := res[label]
		cur[0] += x.Paid
		cur[1] += x.Cost
		res[label] = cur
	}
	return res, nil
}

// tenantMeta 是 tenant_id → 展示名/owner 解析结果。
type tenantMeta struct {
	Name          string
	OwnerUserID   int64
	OwnerUsername string
}

// tenantNames 批量解析 tenant_id → (tenants.name, owner_user_id, users.username)。best-effort：
// 解析失败仅返回已得部分，缺失项为空串（报表不因名称缺失而失败）。
func (r *Repo) tenantNames(ctx context.Context, ids []int64) map[int64]tenantMeta {
	out := map[int64]tenantMeta{}
	if len(ids) == 0 {
		return out
	}
	var trows []struct {
		ID          int64
		Name        string
		OwnerUserID int64
	}
	if err := r.db.WithContext(ctx).Table("tenants").
		Select("id, name, owner_user_id").
		Where("id IN ?", ids).
		Scan(&trows).Error; err != nil {
		return out
	}
	ownerIDs := make([]int64, 0, len(trows))
	for _, t := range trows {
		out[t.ID] = tenantMeta{Name: t.Name, OwnerUserID: t.OwnerUserID}
		if t.OwnerUserID > 0 {
			ownerIDs = append(ownerIDs, t.OwnerUserID)
		}
	}
	if len(ownerIDs) > 0 {
		var urows []struct {
			ID       int64
			Username string
		}
		if err := r.db.WithContext(ctx).Table("users").
			Select("id, username").
			Where("id IN ?", ownerIDs).
			Scan(&urows).Error; err == nil {
			uname := make(map[int64]string, len(urows))
			for _, u := range urows {
				uname[u.ID] = u.Username
			}
			for tid, m := range out {
				m.OwnerUsername = uname[m.OwnerUserID]
				out[tid] = m
			}
		}
	}
	return out
}

// fillEarningNames 为收益明细批量回填 agent_name。
func (r *Repo) fillEarningNames(ctx context.Context, rows []EarningDetailRow) {
	ids := collectTenantIDs(rows, func(e EarningDetailRow) int64 { return e.TenantID })
	metas := r.tenantNames(ctx, ids)
	for i := range rows {
		rows[i].AgentName = metas[rows[i].TenantID].Name
	}
}

// scopeUserIDs 取 scope 内 user_id 集（分库降级用）：代理=tenant_id=tid；管理端=tenant_id<>0。
func (r *Repo) scopeUserIDs(ctx context.Context, tenantID *int64) ([]int64, error) {
	q := r.db.WithContext(ctx).Table("users").Where("deleted_at IS NULL")
	q = applyTenantScope(q, "tenant_id", tenantID)
	var ids []int64
	if err := q.Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// CountTenantUsers 返回归属某租户的下级用户数（软删除排除），与 scopeUserIDs 同过滤口径
// （tenant_id=? AND deleted_at IS NULL）。供 admin 代理升档决策指标（无现成 per-agent 用户计数聚合，
// 故补此一条薄查询）。
func (r *Repo) CountTenantUsers(ctx context.Context, tenantID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Table("users").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// scopeUserTenant 取 scope 内 user_id→tenant_id 映射（分库降级用）。
func (r *Repo) scopeUserTenant(ctx context.Context, tenantID *int64) (map[int64]int64, error) {
	var rows []struct {
		ID       int64
		TenantID int64
	}
	q := r.db.WithContext(ctx).Table("users").
		Select("id, tenant_id").
		Where("deleted_at IS NULL")
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, x := range rows {
		m[x.ID] = x.TenantID
	}
	return m, nil
}

// logDB 返回日志库句柄：分库（LOG_DB!=DB）走 model.LOG_DB，否则走主库（含单测 globals 未初始化）。
func (r *Repo) logDB() *gorm.DB {
	if model.LOG_DB != nil && model.DB != nil && model.LOG_DB != model.DB {
		return model.LOG_DB
	}
	return r.db
}

// sameLogDB 报告日志库与主库是否同库（可 logs JOIN users）。globals 未初始化（单测）视为同库。
func (r *Repo) sameLogDB() bool {
	return !(model.LOG_DB != nil && model.DB != nil && model.LOG_DB != model.DB)
}

// ============================================================================
// 内部：纯函数辅助（分桶 / 方言表达式 / 作用域 / 排序 / 分页）
// ============================================================================

// unixT 把 epoch 秒转为 UTC time.Time（用于 DATETIME 台账列的区间比较）。
func unixT(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// applyTenantScope 加租户作用域：tenantID!=nil → col=*tid；否则管理端 col<>0（排除主站/未归属）。
func applyTenantScope(q *gorm.DB, col string, tenantID *int64) *gorm.DB {
	if tenantID != nil {
		return q.Where(col+" = ?", *tenantID)
	}
	return q.Where(col + " <> 0")
}

// normGranularity 归一化粒度；非法回退 day（handler 另行校验并回 REPORT_GRANULARITY_INVALID）。
func normGranularity(g string) string {
	switch g {
	case "day", "week", "month":
		return g
	default:
		return "day"
	}
}

// bucketize 返回 UTC 日历桶标签 + 桶起始 epoch。week 以周一为界（UTC）。
func bucketize(granularity string, ts int64) (string, int64) {
	t := time.Unix(ts, 0).UTC()
	switch granularity {
	case "month":
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start.Format("2006-01"), start.Unix()
	case "week":
		offset := (int(t.Weekday()) + 6) % 7 // 周一=0 … 周日=6
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		start := day.AddDate(0, 0, -offset)
		return start.Format("2006-01-02"), start.Unix()
	default: // day
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return start.Format("2006-01-02"), start.Unix()
	}
}

// bucketStartTS 把 SQL 产出的桶标签解析回 UTC 起始 epoch（与 bucketize 同口径，跨方言一致）。
func bucketStartTS(granularity, label string) int64 {
	if granularity == "month" {
		if t, err := time.ParseInLocation("2006-01", label, time.UTC); err == nil {
			return t.Unix()
		}
		return 0
	}
	// day 与 week（week 标签即周一日期）
	if t, err := time.ParseInLocation("2006-01-02", label, time.UTC); err == nil {
		return t.Unix()
	}
	return 0
}

// mysqlBucketDatetime 构造 MySQL DATETIME 列的桶标签表达式。
func mysqlBucketDatetime(col, granularity string) string {
	switch granularity {
	case "month":
		return "DATE_FORMAT(" + col + ", '%Y-%m')"
	case "week":
		return "DATE_FORMAT(DATE_SUB(" + col + ", INTERVAL WEEKDAY(" + col + ") DAY), '%Y-%m-%d')"
	default:
		return "DATE_FORMAT(" + col + ", '%Y-%m-%d')"
	}
}

// mysqlBucketEpoch 构造 MySQL epoch 列的桶标签表达式（先 FROM_UNIXTIME 再走 DATETIME 口径）。
func mysqlBucketEpoch(col, granularity string) string {
	return mysqlBucketDatetime("FROM_UNIXTIME("+col+")", granularity)
}

// logRow 是分库降级路径下从 LOG_DB 取回的最小日志行。
type logRow struct {
	CreatedAt        int64
	Quota            int64
	PromptTokens     int64
	CompletionTokens int64
}

// lim 归一化页大小（<=0 → 20）。
func lim(pageSize int) int {
	if pageSize <= 0 {
		return 20
	}
	return pageSize
}

// off 计算 OFFSET（page<=0 视为 1）。
func off(page, pageSize int) int {
	if page <= 0 {
		page = 1
	}
	return (page - 1) * lim(pageSize)
}

// paginateSlice 在已排序切片上做内存分页。
func paginateSlice[T any](rows []T, page, pageSize int) []T {
	size := lim(pageSize)
	offset := off(page, pageSize)
	if offset >= len(rows) {
		return []T{}
	}
	end := offset + size
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end]
}

// collectTenantIDs 收集切片中去重、非零的 tenant_id（保序）。
func collectTenantIDs[T any](rows []T, get func(T) int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		id := get(row)
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// sortedStringKeys 返回 map 的升序键（day/week/month 标签按字典序即时间序）。
func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedInt64Set 返回 set 的升序键。
func sortedInt64Set(set map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(set))
	for k := range set {
		ids = append(ids, k)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// sortRanking 按白名单字段排序（稳定，租户号升序为同值兜底）；order!='asc' 即降序。
func sortRanking(rows []AgentRankRow, sortBy, order string) {
	less := rankLess(sortBy)
	asc := order == "asc"
	sort.SliceStable(rows, func(i, j int) bool {
		if asc {
			return less(rows[i], rows[j])
		}
		return less(rows[j], rows[i])
	})
}

// rankLess 返回升序比较器；sortBy 非白名单回退 total_earned_cny。
func rankLess(sortBy string) func(a, b AgentRankRow) bool {
	switch sortBy {
	case "recharge_paid_cny":
		return func(a, b AgentRankRow) bool { return a.RechargePaidCNY < b.RechargePaidCNY }
	case "subscription_paid_cny":
		return func(a, b AgentRankRow) bool { return a.SubscriptionPaidCNY < b.SubscriptionPaidCNY }
	case "consumption_cost_cny":
		return func(a, b AgentRankRow) bool { return a.ConsumptionCostCNY < b.ConsumptionCostCNY }
	case "consumption_used_quota":
		return func(a, b AgentRankRow) bool { return a.ConsumptionUsedQuota < b.ConsumptionUsedQuota }
	case "withdrawn_cny":
		return func(a, b AgentRankRow) bool { return a.WithdrawnCNY < b.WithdrawnCNY }
	case "pending_withdraw_cny":
		return func(a, b AgentRankRow) bool { return a.PendingWithdrawCNY < b.PendingWithdrawCNY }
	case "withdrawable_cny":
		return func(a, b AgentRankRow) bool { return a.WithdrawableCNY < b.WithdrawableCNY }
	default: // total_earned_cny
		return func(a, b AgentRankRow) bool { return a.TotalEarnedCNY < b.TotalEarnedCNY }
	}
}
