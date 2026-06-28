// Package gormrepo 用真实 GORM(MySQL) 实现 billing.CallLogWriter（tenant_billing_logs 表），
// 并附带「我的计费流水」查询辅助（ListByUser）。
//
// 多租户隔离：每条日志绑 (tenant_id, user_id)，查询恒按 tenant+user scope（本人本租户）。
// 金额列用 decimal(20,8) 精确存储，与 wallet / tokenplan 计量口径一致。
//
// status 列区分「已扣费(charged)」与「后付费未扣成(unpaid)」：后付费下扣费失败仍回送上游响应，
// 据此落一条 unpaid 流水留痕（charged_usd=0），供对账/风控（预扣防滥用见 cmd handler TODO）。
package gormrepo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/billing"
)

// 计费日志状态字面值。
const (
	statusCharged = "charged" // 正常后付费扣减成功
	statusUnpaid  = "unpaid"  // 转发已发生但扣费失败（余额/套餐不足/过期），仍回送响应
)

// logRow 是 tenant_billing_logs 表的 GORM 模型。按 (tenant_id, user_id) 建联合索引支撑流水查询，
// request_id 建普通索引便于按调用链回查。
type logRow struct {
	ID               int64     `gorm:"column:id;primaryKey;autoIncrement"`
	RequestID        string    `gorm:"column:request_id;type:varchar(64);index:idx_billing_logs_request"`
	TenantID         int64     `gorm:"column:tenant_id;not null;index:idx_billing_logs_tenant_user,priority:1"`
	UserID           int64     `gorm:"column:user_id;not null;index:idx_billing_logs_tenant_user,priority:2"`
	Model            string    `gorm:"column:model;type:varchar(128)"`
	PromptTokens     int64     `gorm:"column:prompt_tokens;not null;default:0"`
	CompletionTokens int64     `gorm:"column:completion_tokens;not null;default:0"`
	BucketKind       string    `gorm:"column:bucket_kind;type:varchar(16)"`
	UpstreamCostUSD  float64   `gorm:"column:upstream_cost_usd;type:decimal(20,8);not null;default:0"`
	ChargedUSD       float64   `gorm:"column:charged_usd;type:decimal(20,8);not null;default:0"`
	GrossProfitUSD   float64   `gorm:"column:gross_profit_usd;type:decimal(20,8);not null;default:0"`
	GroupKey         string    `gorm:"column:group_key;type:varchar(64)"`
	Status           string    `gorm:"column:status;type:varchar(16);not null;default:charged"`
	CreatedAt        time.Time `gorm:"column:created_at;index:idx_billing_logs_created"`
}

// TableName 固定表名。
func (logRow) TableName() string { return "tenant_billing_logs" }

// LogView 是「我的计费流水」单项（含 ID/状态/时间戳，供 cmd handler 渲染 JSON）。
type LogView struct {
	ID               int64
	RequestID        string
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	BucketKind       string
	UpstreamCostUSD  float64
	ChargedUSD       float64
	GrossProfitUSD   float64
	Status           string
	CreatedAt        time.Time
}

// Repo 是 billing.CallLogWriter 的 GORM 实现，并附带流水查询辅助。
type Repo struct {
	db *gorm.DB
}

// 编译期断言：*Repo 满足 billing.CallLogWriter 契约。
var _ billing.CallLogWriter = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 tenant_billing_logs 表结构（含联合/普通索引）。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&logRow{})
}

// Write 落一条「已扣费」计费日志（实现 billing.CallLogWriter，扣费成功路径由 BillingService 调用）。
func (r *Repo) Write(ctx context.Context, entry billing.CallLogEntry) error {
	return r.insert(ctx, entry, statusCharged)
}

// WriteUnpaid 落一条「后付费未扣成」计费日志（charged_usd 取 0；由 cmd handler 在扣费失败仍回送时调用）。
// 非接口方法：BillingService 不感知后付费豁免，留痕逻辑收敛在装配层。
func (r *Repo) WriteUnpaid(ctx context.Context, entry billing.CallLogEntry) error {
	entry.ChargedUSD = 0
	return r.insert(ctx, entry, statusUnpaid)
}

// insert 写入一行（status 由调用方指定）。
func (r *Repo) insert(ctx context.Context, e billing.CallLogEntry, status string) error {
	row := logRow{
		RequestID:        e.RequestID,
		TenantID:         e.TenantID,
		UserID:           e.UserID,
		Model:            e.Model,
		PromptTokens:     e.PromptTokens,
		CompletionTokens: e.CompletionTokens,
		BucketKind:       string(e.BucketKind),
		UpstreamCostUSD:  e.UpstreamCostUSD,
		ChargedUSD:       e.ChargedUSD,
		GrossProfitUSD:   e.GrossProfitUSD,
		GroupKey:         e.GroupKey,
		Status:           status,
		CreatedAt:        time.Now(),
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

// ListByUser 返回某用户在本租户的近 limit 条计费流水（按 id 降序，最近优先）。
// 恒按 (tenant_id, user_id) scope，保证「本人本租户」隔离。
func (r *Repo) ListByUser(ctx context.Context, tenantID, userID int64, limit int) ([]LogView, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []logRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]LogView, 0, len(rows))
	for i := range rows {
		out = append(out, LogView{
			ID:               rows[i].ID,
			RequestID:        rows[i].RequestID,
			Model:            rows[i].Model,
			PromptTokens:     rows[i].PromptTokens,
			CompletionTokens: rows[i].CompletionTokens,
			BucketKind:       rows[i].BucketKind,
			UpstreamCostUSD:  rows[i].UpstreamCostUSD,
			ChargedUSD:       rows[i].ChargedUSD,
			GrossProfitUSD:   rows[i].GrossProfitUSD,
			Status:           rows[i].Status,
			CreatedAt:        rows[i].CreatedAt,
		})
	}
	return out, nil
}
