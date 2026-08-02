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

// Package gormrepo 是 breakage.Repo 的 GORM 实现（P2-BRK-01 底层仓储）：在 new-api 基座上用 raw
// db.Table()/Joins() 跨表做 SUM/GROUP BY，绝不 import 兄弟模块的 model 结构（升级 rebase 安全，
// 对齐 internal/report/reportrepo/reportrepo.go 范式）。本包只借用列名，不借用别包的 struct。
//
// 数据来源六表：
//   - tokenplan_subscriptions  订阅台账（列：tenant_id/user_id/plan_id/month_limit_usd/source_order_id/
//     status/start_at/expire_at；start_at/expire_at 为 DATETIME）—— 活跃剩余 / 到期未用 / 明细 / 快照采集。
//     注意：本表 used_usd 是永为 0 的死列（tokenplan 计费桶未装配，见 nativeUsedUSD 注释）；已用量一律
//     经下两张原生桶表投影，绝不读 used_usd。
//   - mt_subscription_orders   桥接订单（order_no ← source_order_id，native_sub_id → 原生订阅）—— JOIN 关联真源。
//   - user_subscriptions       原生订阅桶（amount_used 为 quota 单位真实用量）—— 真实已用 = amount_used/QuotaPerUnit。
//   - token_plans              套餐定义（id → code）—— JOIN 回填 plan_code（订阅表无 plan_code 列）。
//   - users                    用户额度（quota 为权威余额，/QuotaPerUnit → USD）—— 钱包未消耗。
//   - payment_orders           支付订单（status/updated_at）—— 系统异常卡单计数（对齐对账 updated_at 口径）。
//
// 新表 breakage_snapshots（本包 AutoMigrate 建）：订阅维历史快照，唯一键
// (tenant_id,user_id,sub_id,period_end) 保证快照 job 与回填 Upsert 幂等。
//
// 硬约束（见 .ccg/tasks/breakage-monitor/requirements.md）：
//
//	【门 #4 租户隔离】聚合方法带 tenantID *int64 作用域：nil→跨租户 tenant_id<>0（排除 tenant_id=0）；
//	  非 nil→单租户 tenant_id=*tenantID。tenant_id 绝不从 query/body 读——由 handler 从鉴权中间件注入。
//	【惰性过期口径】判「到期未使用」一律按 expire_at < now，不看持久化 status（status 可能仍 active）。
//	【金额】float64（decimal(20,8) 进出）；不在仓储侧四舍五入——round2 由 handler 边界做；计数用整数。
//
// 时间口径：接口层用 epoch 秒（int64）；DATETIME 列（expire_at/start_at/created_at）比较用
// time.Unix(sec,0).UTC()，扫描回来的 time.Time 再 .Unix() 转 epoch（跨方言一致，对齐 reportrepo unixT）。
package gormrepo

import (
	"context"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/breakage"
)

// 订阅/订单状态字面值（与 tokenplan.SubStatus / payment.OrderStatus 字符串一致；此处以裸串复刻，
// 遵循「不 import 兄弟 model」硬约束——升级 rebase 时这些常量不受上游结构变动影响）。
const (
	subStatusActive = "active" // tokenplan_subscriptions.status 活跃

	orderStatusPaid = "paid" // payment_orders.status 已支付未入账（AnomalyCount 只计此态；created 废单不计）
)

// anomalyMinAgeSec 是「异常卡单」的最小时长（秒）：只把超过此时长仍停在 paid（已支付未入账）的订单
// 计为系统异常，过滤仍在途的正常订单。与 internal/mtwire/reconcile_loop.go reconcileMinAge=5min 同口径。
const anomalyMinAgeSec int64 = 5 * 60

// snapshotBackfillBatch 是回填/采集游标分批大小（按 id 升序游标翻页，避免一次性全表加载）。
const snapshotBackfillBatch = 500

// nativeUsedUSD 是「真实已用 USD」的 SQL 表达式片段（P2-BRK-02 修复）。
//
// 背景：tokenplan_subscriptions.used_usd 是永为 0 的死列——其唯一写入者是 tokenplan 计费桶的
// Meter，而该桶（billing/wallet Service）在 wire.go 从未装配；生产 /v1 计费实际走原生订阅桶，
// 每笔用量记在 user_subscriptions.amount_used（quota 单位）。故读侧一律经 s.source_order_id →
// mt_subscription_orders.native_sub_id → user_subscriptions 关联到真源，amount_used/QuotaPerUnit
// 还原为 USD；无原生订阅（缺链/非桥接行）时 COALESCE 兜底 0。仍遵本包「只借列名、不 import 兄弟
// model」硬约束（跨表 raw JOIN）。
//
// 除数用浮点字面量（500000.0 而非 500000）——sqlite 对两整数相除会截断归零，浮点字面量强制实数
// 除法，跨 MySQL/sqlite 方言一致。QuotaPerUnit 恒 >0。
var nativeUsedUSD = "COALESCE(us.amount_used / " +
	strconv.FormatFloat(common.QuotaPerUnit, 'f', 1, 64) + ", 0)"

// joinNativeUsage 在以 `tokenplan_subscriptions AS s` 为基表的查询上追加两条 LEFT JOIN，关联到
// 原生订阅桶（真实用量所在）。o.order_no 为主键、us.id 为主键 → 至多一行匹配，不产生行放大；
// 缺链行 us.amount_used 为 NULL，由 nativeUsedUSD 的 COALESCE 兜底为 0。
func joinNativeUsage(q *gorm.DB) *gorm.DB {
	return q.
		Joins("LEFT JOIN mt_subscription_orders AS o ON o.order_no = s.source_order_id AND o.native_sub_id > 0").
		Joins("LEFT JOIN user_subscriptions AS us ON us.id = o.native_sub_id")
}

// snapshotRow 是 breakage_snapshots 表的 GORM 模型（本包新表；AutoMigrate 建）。
//
// 唯一键 (tenant_id, user_id, sub_id, period_end) —— 一个订阅在一「期」（period_end）只有一行快照；
// 快照 job 与历史回填都对这张表 Upsert（冲突则刷新 limit/used/unused/status/snapshot_at），故可重跑不重复。
// period_end/snapshot_at 存 epoch 秒（BIGINT），与接口层口径一致，趋势查询无需再做 DATETIME 换算。
type snapshotRow struct {
	ID         int64   `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID   int64   `gorm:"column:tenant_id;not null;uniqueIndex:idx_breakage_snap_uniq,priority:1"`
	UserID     int64   `gorm:"column:user_id;not null;uniqueIndex:idx_breakage_snap_uniq,priority:2"`
	SubID      int64   `gorm:"column:sub_id;not null;uniqueIndex:idx_breakage_snap_uniq,priority:3"`
	PeriodEnd  int64   `gorm:"column:period_end;not null;uniqueIndex:idx_breakage_snap_uniq,priority:4"`
	PlanCode   string  `gorm:"column:plan_code;type:varchar(32);not null;default:''"`
	LimitUSD   float64 `gorm:"column:limit_usd;type:decimal(20,8);not null;default:0"`
	UsedUSD    float64 `gorm:"column:used_usd;type:decimal(20,8);not null;default:0"`
	UnusedUSD  float64 `gorm:"column:unused_usd;type:decimal(20,8);not null;default:0"`
	Status     string  `gorm:"column:status;type:varchar(16);not null;default:''"`
	SnapshotAt int64   `gorm:"column:snapshot_at;not null;default:0"`
}

// TableName 固定表名。
func (snapshotRow) TableName() string { return "breakage_snapshots" }

// Repo 是 breakage.Repo 的 GORM 实现。db 为主库（= model.DB）。
type Repo struct {
	db *gorm.DB
}

// 编译期断言：*Repo 满足 breakage.Repo 契约（签名逐字对齐，防漂移）。
var _ breakage.Repo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB（主库）构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 breakage_snapshots 表结构（含唯一键与趋势组合索引），幂等可重入。
// 由 mtwire.Migrate（master 节点）在启动时链式调用。
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&snapshotRow{}); err != nil {
		return err
	}
	// 趋势查询组合索引 (tenant_id, period_end)：uniqueIndex 已含这两列但顺序为 (tenant,user,sub,period)，
	// 无法服务「按 tenant 作用域 + period_end 区间」的前缀扫描，故补一条 (tenant_id, period_end)。
	// AutoMigrate 对 struct tag 的 index 幂等，这里再显式 ensure 一次组合次序（对齐 reportrepo.ensureIndex 习语）。
	return ensureIndex(db, common.MainDatabaseType(), "breakage_snapshots", "idx_breakage_snap_tenant_period", "tenant_id, period_end")
}

// ensureIndex 幂等补建组合索引（best-effort）。MySQL 不支持 CREATE INDEX IF NOT EXISTS，故先查
// information_schema.statistics 判存在、缺失才 CREATE（无 IF NOT EXISTS）；sqlite/pg 用原生
// IF NOT EXISTS 幂等。组合索引仅为查询加速、唯一键（AutoMigrate 已建）才是正确性所依赖，故一律
// 吞错返回 nil，不阻断启动迁移。对齐 internal/report/reportrepo/migrate.go ensureIndex 习语。
func ensureIndex(db *gorm.DB, dbType common.DatabaseType, table, idx, cols string) error {
	if db == nil {
		return nil
	}
	if dbType == common.DatabaseTypeMySQL {
		var count int64
		if err := db.Raw(
			`SELECT COUNT(*) FROM information_schema.statistics
			 WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
			table, idx,
		).Scan(&count).Error; err != nil {
			return nil // best-effort：探测失败不阻断迁移
		}
		if count == 0 {
			_ = db.Exec("CREATE INDEX " + idx + " ON " + table + " (" + cols + ")").Error
		}
		return nil
	}
	_ = db.Exec("CREATE INDEX IF NOT EXISTS " + idx + " ON " + table + " (" + cols + ")").Error
	return nil
}

// ============================================================================
// Overview —— 4 指标聚合（活跃剩余 / 到期未用 / 钱包未消耗 / 系统异常）
// ============================================================================

// Overview 聚合 4 指标，按 tenantID 作用域 + now（epoch 秒）判惰性过期。见 breakage.Overview 口径注释。
func (r *Repo) Overview(ctx context.Context, tenantID *int64, now int64) (breakage.Overview, error) {
	var out breakage.Overview
	nowT := unixT(now)

	// (1) 活跃套餐剩余 = Σ(month_limit_usd - used_usd) WHERE status='active' AND expire_at>now AND scope。
	//     双条件（status='active' 且 expire_at>now）：惰性过期下 status 可能滞后，用 expire_at 兜住。
	{
		var row struct {
			Remaining float64
		}
		q := joinNativeUsage(r.db.WithContext(ctx).Table("tokenplan_subscriptions AS s")).
			Select("COALESCE(SUM(s.month_limit_usd - "+nativeUsedUSD+"),0) AS remaining").
			Where("s.status = ?", subStatusActive).
			Where("s.expire_at > ?", nowT)
		q = applyTenantScope(q, "s.tenant_id", tenantID)
		if err := q.Scan(&row).Error; err != nil {
			return breakage.Overview{}, err
		}
		out.ActiveRemainingUSD = row.Remaining
	}

	// (2) 到期未使用 = Σ(month_limit_usd - used_usd) WHERE expire_at<now AND (limit-used)>0 AND scope。
	//     只看 expire_at<now，不看 status（惰性过期口径核心）；(limit-used)>0 过滤已用满/负值行。
	{
		var row struct {
			Unused float64
		}
		q := joinNativeUsage(r.db.WithContext(ctx).Table("tokenplan_subscriptions AS s")).
			Select("COALESCE(SUM(s.month_limit_usd - "+nativeUsedUSD+"),0) AS unused").
			Where("s.expire_at < ?", nowT).
			Where("s.month_limit_usd - " + nativeUsedUSD + " > 0")
		q = applyTenantScope(q, "s.tenant_id", tenantID)
		if err := q.Scan(&row).Error; err != nil {
			return breakage.Overview{}, err
		}
		out.ExpiredUnusedUSD = row.Unused
	}

	// (3) 钱包未消耗 = Σ(users.quota)/QuotaPerUnit。
	// 生产入账/消耗权威在 users.quota（兑换码、充值、relay 扣费）；user_balances 表无非测试
	// 写入路径（C7 死台账），读它会导致「钱包未消耗」恒 ~0 的假绿指标。
	{
		var row struct {
			Balance float64
		}
		quotaUSD := "COALESCE(SUM(quota),0) / " + strconv.FormatFloat(common.QuotaPerUnit, 'f', 1, 64)
		q := r.db.WithContext(ctx).Table("users").
			Select(quotaUSD + " AS balance").
			Where("deleted_at IS NULL")
		q = applyTenantScope(q, "tenant_id", tenantID)
		if err := q.Scan(&row).Error; err != nil {
			return breakage.Overview{}, err
		}
		out.WalletUnusedUSD = row.Balance
	}

	// (4) 系统异常卡单数 = COUNT payment_orders WHERE status='paid' AND updated_at<now-5min AND scope。
	//     只计 paid（已支付未入账＝钱到了没入账，真异常）；created（已下单未支付）绝大多数是废弃购物车
	//     （用户下单没付），由对账 loop 主动查单/超时过期处理，不计为异常——否则废单会把本数刷高、告警狼来了。
	//     用 updated_at（而非 created_at）对齐对账（reconcile）的「超 minAge 未变动即卡单」口径：paid 后
	//     正在入账（updated_at 新）的在途订单不误计。
	{
		var count int64
		q := r.db.WithContext(ctx).Table("payment_orders").
			Where("status = ?", orderStatusPaid).
			Where("updated_at < ?", unixT(now-anomalyMinAgeSec))
		q = applyTenantScope(q, "tenant_id", tenantID)
		if err := q.Count(&count).Error; err != nil {
			return breakage.Overview{}, err
		}
		out.AnomalyCount = int(count)
	}

	return out, nil
}

// ============================================================================
// Detail —— 订阅维明细（筛选 + 分页 + total；派生 Unused/UsagePct/AlertLevel）
// ============================================================================

// detailScanRow 是 Detail 查询的落地行（JOIN token_plans 回填 plan_code；LEFT JOIN 租户/用户名回填展示名）。
// expire_at 用 time.Time 扫描（DATETIME 列），再转 epoch。
type detailScanRow struct {
	TenantID      int64
	TenantName    string
	UserID        int64
	Username      string
	PlanCode      string
	Status        string
	MonthLimitUSD float64
	UsedUSD       float64
	ExpireAt      time.Time
}

// Detail 分页返回订阅维明细。筛选：plan_code / [start,end]（按 expire_at）/ alert_level（Go 内按派生级过滤）。
// 惰性过期：不改库，只在结果里按 expire_at 与 now 对齐 status 展示口径由上层决定；派生字段用 used/limit 计算。
//
// alert_level 过滤放在 Go 内（派生自 used/limit + status，非持久列，SQL 无法直接过滤）：先取满足
// plan_code/时间 的全集，算派生级，按级过滤后再内存分页——与 reportrepo DetailConsumption「取行+Go分页」同构。
func (r *Repo) Detail(ctx context.Context, f breakage.Filter, now int64) ([]breakage.DetailRow, int64, error) {
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("tokenplan_subscriptions AS s").
			Joins("LEFT JOIN token_plans AS tp ON tp.id = s.plan_id").
			Joins("LEFT JOIN tenants AS t ON t.id = s.tenant_id").
			Joins("LEFT JOIN users AS u ON u.id = s.user_id")
		q = joinNativeUsage(q)
		q = applyTenantScope(q, "s.tenant_id", f.TenantID)
		if f.PlanCode != "" {
			q = q.Where("tp.code = ?", f.PlanCode)
		}
		if f.Start > 0 {
			q = q.Where("s.expire_at >= ?", unixT(f.Start))
		}
		if f.End > 0 {
			q = q.Where("s.expire_at <= ?", unixT(f.End))
		}
		return q
	}

	// alert_level 过滤下推 SQL（等价 breakage.AlertLevelForPct）：alert_level 虽是派生级(非持久列)，
	// 但其判据仅依赖持久列 status/used_usd/month_limit_usd,故可完全表达为 SQL WHERE。下推后 count 与
	// 分页均走 SQL,不再「全表物化后 Go 过滤+分页」(管理端跨租户全表进内存的 OOM 风险，性能审计 M8)。
	if where, args := alertLevelWhere(f.AlertLevel); where != "" {
		orig := base
		base = func() *gorm.DB { return orig().Where(where, args...) }
	}

	// total 由 SQL COUNT 得出（含 alert 谓词），不再是「全表物化后 len」。
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 只取当前页（SQL LIMIT/OFFSET，与既有 lim/off 归一化口径一致），杜绝全表读入内存。
	var scan []detailScanRow
	if err := base().
		Select("s.tenant_id AS tenant_id, COALESCE(t.name,'') AS tenant_name, " +
			"s.user_id AS user_id, COALESCE(u.username,'') AS username, " +
			"COALESCE(tp.code,'') AS plan_code, s.status AS status, " +
			"s.month_limit_usd AS month_limit_usd, " + nativeUsedUSD + " AS used_usd, s.expire_at AS expire_at").
		Order("s.expire_at DESC, s.id DESC").
		Limit(lim(f.PageSize)).Offset(off(f.Page, f.PageSize)).
		Scan(&scan).Error; err != nil {
		return nil, 0, err
	}

	// 组装派生字段（仅当前页）。惰性过期口径（read-only，不回写库）：expire_at<now 且库里仍
	// 'active' 时，展示 status 定格为 expired——与 CollectSnapshots 一致，使明细 status 列真实反映到期，
	// 而非依赖持久化翻牌（订阅台账的惰性翻牌发生在计量/激活路径，只读投影不触发）。
	// alert_level 过滤已在 SQL 完成,此处只按已过滤的页算展示字段。
	rows := make([]breakage.DetailRow, 0, len(scan))
	for i := range scan {
		x := &scan[i]
		limit := x.MonthLimitUSD
		used := x.UsedUSD
		unused := limit - used
		if unused < 0 {
			unused = 0
		}
		pct := 0.0
		if limit > 0 {
			pct = used / limit * 100
		}
		expireEpoch := x.ExpireAt.Unix()
		status := x.Status
		if status == subStatusActive && expireEpoch < now {
			status = "expired"
		}
		statusExhausted := status == "exhausted"
		rows = append(rows, breakage.DetailRow{
			TenantID:   x.TenantID,
			TenantName: x.TenantName,
			UserID:     x.UserID,
			Username:   x.Username,
			PlanCode:   x.PlanCode,
			Status:     status,
			LimitUSD:   limit,
			UsedUSD:    used,
			UnusedUSD:  unused,
			UsagePct:   pct,
			AlertLevel: breakage.AlertLevelForPct(pct, statusExhausted),
			PeriodEnd:  expireEpoch,
		})
	}
	return rows, total, nil
}

// alertLevelWhere 把 alert_level 过滤下推为 SQL WHERE 片段,口径与 breakage.AlertLevelForPct 严格等价:
//
//	statusExhausted || pct>=100 → exhausted;  pct>=95 → critical;  pct>=80 → warn;  否则 none
//
// 仅 warn/critical/exhausted 有片段;AlertNone("") 语义是「不过滤」→ 返回空片段(不加 WHERE)。
// 已用量用 nativeUsedUSD（原生桶 us.amount_used 投影，与 Detail 展示 SELECT 同一表达式）——**绝不读死列
// s.used_usd**（恒 0，见 nativeUsedUSD 注释）：合并 500L「used_usd→原生桶真源」后，过滤源必须与展示源一致，
// 否则 s.used_usd=0 使谓词永假、alert 过滤恒空（merge 回归，2026-07-09 修）。us 别名由 Detail base 的
// joinNativeUsage 供给，本片段总在其后 Where 追加，故列可解析。阈值仍用「used*100 与 limit*阈值」乘法比较
// （规避除零）；used 与展示同一 COALESCE(amount_used/QuotaPerUnit) 表达式，SQL 过滤与 Go AlertLevelForPct(pct)
// 边界完全同源、无分歧。exhausted 判据里 status='exhausted' 对齐 Detail 中 `status == "exhausted"`
// (惰性过期只把 active→expired,不改 exhausted)。
func alertLevelWhere(level breakage.AlertLevel) (string, []any) {
	u := nativeUsedUSD // 真实已用 USD（原生桶投影），与 Detail 展示口径同源；不读死列 s.used_usd
	switch level {
	case breakage.AlertExhausted:
		return "(s.status = ? OR (s.month_limit_usd > 0 AND " + u + " >= s.month_limit_usd))",
			[]any{"exhausted"}
	case breakage.AlertCritical:
		return "(s.status <> ? AND s.month_limit_usd > 0 AND " + u + " < s.month_limit_usd AND " + u + " * 100 >= s.month_limit_usd * 95)",
			[]any{"exhausted"}
	case breakage.AlertWarn:
		return "(s.status <> ? AND s.month_limit_usd > 0 AND " + u + " * 100 < s.month_limit_usd * 95 AND " + u + " * 100 >= s.month_limit_usd * 80)",
			[]any{"exhausted"}
	default:
		return "", nil
	}
}

// ============================================================================
// 快照：Upsert（幂等）/ 趋势读 / 采集（供 job + 回填）
// ============================================================================

// UpsertSnapshots 幂等写入一批快照行（唯一键 (tenant_id,user_id,sub_id,period_end)，
// 冲突则更新 plan_code/limit/used/unused/status/snapshot_at）。空切片直接返回。分批 Create 避免超长 SQL。
func (r *Repo) UpsertSnapshots(ctx context.Context, rows []breakage.Snapshot) error {
	if len(rows) == 0 {
		return nil
	}
	conflict := clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "user_id"}, {Name: "sub_id"}, {Name: "period_end"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"plan_code", "limit_usd", "used_usd", "unused_usd", "status", "snapshot_at",
		}),
	}
	// 逐批（每批 snapshotBackfillBatch 行）写入，规避单条超长 INSERT。
	for start := 0; start < len(rows); start += snapshotBackfillBatch {
		end := start + snapshotBackfillBatch
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]
		dst := make([]snapshotRow, 0, len(batch))
		for i := range batch {
			dst = append(dst, toSnapshotRow(&batch[i]))
		}
		if err := r.db.WithContext(ctx).Clauses(conflict).Create(&dst).Error; err != nil {
			return err
		}
	}
	return nil
}

// SnapshotTrend 读快照趋势：按 period_end 分桶聚合，tenantID 作用域 + [start,end] 期末区间（epoch 秒）。
// 一桶（同 period_end）= 该期作用域内所有订阅快照的汇总：到期未用/活跃剩余合计 + 订阅条数；按 period_end 升序。
func (r *Repo) SnapshotTrend(ctx context.Context, tenantID *int64, start, end int64) ([]breakage.SnapshotPoint, error) {
	var scan []struct {
		PeriodEnd     int64
		ExpiredUnused float64
		ActiveRemain  float64
		SubCount      int64
	}
	// 两列互补、与 Overview 同口径：快照定格时按 expire_at<now 把到期订阅的 status 落为非 active，故
	// ExpiredUnused = Σ unused_usd WHERE status<>'active'（到期沉淀，严格对齐 Overview.ExpiredUnusedUSD）；
	// ActiveRemain = Σ unused_usd WHERE status='active'（仍活跃的剩余额度）。以 SQL CASE 分列避免二次查询。
	q := r.db.WithContext(ctx).Table("breakage_snapshots").
		Select("period_end, " +
			"COALESCE(SUM(CASE WHEN status <> 'active' THEN unused_usd ELSE 0 END),0) AS expired_unused, " +
			"COALESCE(SUM(CASE WHEN status = 'active' THEN unused_usd ELSE 0 END),0) AS active_remain, " +
			"COUNT(*) AS sub_count")
	q = applyTenantScope(q, "tenant_id", tenantID)
	if start > 0 {
		q = q.Where("period_end >= ?", start)
	}
	if end > 0 {
		q = q.Where("period_end <= ?", end)
	}
	if err := q.Group("period_end").Order("period_end ASC").Scan(&scan).Error; err != nil {
		return nil, err
	}
	out := make([]breakage.SnapshotPoint, 0, len(scan))
	for _, x := range scan {
		out = append(out, breakage.SnapshotPoint{
			PeriodEnd:          x.PeriodEnd,
			ExpiredUnusedUSD:   x.ExpiredUnused,
			ActiveRemainingUSD: x.ActiveRemain,
			SubscriptionCount:  int(x.SubCount),
		})
	}
	return out, nil
}

// collectScanRow 是 CollectSnapshots 的落地行（JOIN token_plans 回填 plan_code；expire_at 转 epoch）。
type collectScanRow struct {
	ID            int64
	TenantID      int64
	UserID        int64
	PlanCode      string
	MonthLimitUSD float64
	UsedUSD       float64
	Status        string
	ExpireAt      time.Time
}

// CollectSnapshots 从订阅台账装配「当下应落库」的快照行（供 job 与回填共用；只读，不改额度/状态）。
// tenantID=nil 跨租户全量（tenant_id<>0）；now 用于按 expire_at 修正展示状态（惰性过期口径）。
// 按 s.id 升序游标分批取全量（避免一次性全表加载），在 Go 内装配 Snapshot。
func (r *Repo) CollectSnapshots(ctx context.Context, tenantID *int64, now int64) ([]breakage.Snapshot, error) {
	var out []breakage.Snapshot
	var lastID int64 = 0
	for {
		var batch []collectScanRow
		q := joinNativeUsage(r.db.WithContext(ctx).
			Table("tokenplan_subscriptions AS s").
			Joins("LEFT JOIN token_plans AS tp ON tp.id = s.plan_id")).
			Select("s.id AS id, s.tenant_id AS tenant_id, s.user_id AS user_id, "+
				"COALESCE(tp.code,'') AS plan_code, s.month_limit_usd AS month_limit_usd, "+
				nativeUsedUSD+" AS used_usd, s.status AS status, s.expire_at AS expire_at").
			Where("s.id > ?", lastID)
		q = applyTenantScope(q, "s.tenant_id", tenantID)
		if err := q.Order("s.id ASC").Limit(snapshotBackfillBatch).Scan(&batch).Error; err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			x := &batch[i]
			unused := x.MonthLimitUSD - x.UsedUSD
			if unused < 0 {
				unused = 0
			}
			// 惰性过期口径：expire_at<now 且库里仍 active → 快照里定格为 expired（不回写库，只作快照状态）。
			status := x.Status
			periodEnd := x.ExpireAt.Unix()
			if status == subStatusActive && periodEnd < now {
				status = "expired"
			}
			out = append(out, breakage.Snapshot{
				TenantID:   x.TenantID,
				UserID:     x.UserID,
				SubID:      x.ID,
				PlanCode:   x.PlanCode,
				LimitUSD:   x.MonthLimitUSD,
				UsedUSD:    x.UsedUSD,
				UnusedUSD:  unused,
				Status:     status,
				PeriodEnd:  periodEnd,
				SnapshotAt: now,
			})
			lastID = x.ID
		}
		if len(batch) < snapshotBackfillBatch {
			break
		}
	}
	return out, nil
}

// ============================================================================
// 内部：映射 + 纯函数辅助（作用域 / epoch / 分页）
// ============================================================================

// toSnapshotRow 把域快照映射为表行。
func toSnapshotRow(s *breakage.Snapshot) snapshotRow {
	return snapshotRow{
		TenantID:   s.TenantID,
		UserID:     s.UserID,
		SubID:      s.SubID,
		PeriodEnd:  s.PeriodEnd,
		PlanCode:   s.PlanCode,
		LimitUSD:   s.LimitUSD,
		UsedUSD:    s.UsedUSD,
		UnusedUSD:  s.UnusedUSD,
		Status:     s.Status,
		SnapshotAt: s.SnapshotAt,
	}
}

// unixT 把 epoch 秒转为 UTC time.Time（用于 DATETIME 台账列的区间比较；对齐 reportrepo.unixT）。
func unixT(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// applyTenantScope 加租户作用域：tenantID!=nil → col=*tid（代理单租户）；否则管理端 col<>0
// （排除主站/未归属 tenant_id=0；门 #4 硬约束）。对齐 reportrepo.applyTenantScope。
func applyTenantScope(q *gorm.DB, col string, tenantID *int64) *gorm.DB {
	if tenantID != nil {
		return q.Where(col+" = ?", *tenantID)
	}
	return q.Where(col + " <> 0")
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

// paginateSlice 在已排序切片上做内存分页（对齐 reportrepo.paginateSlice）。
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
