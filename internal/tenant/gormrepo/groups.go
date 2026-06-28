package gormrepo

import (
	"context"
	"time"

	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/tenant"
)

// groupRow 是 tenant_groups 表的 GORM 模型。(tenant_id, group_name) 唯一（租户内组名唯一）。
// 表名 tenant_groups：已 grep 确认不撞 new-api 原生 model/。
type groupRow struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID  int64     `gorm:"column:tenant_id;not null;uniqueIndex:idx_tenant_groups_tg,priority:1"`
	GroupName string    `gorm:"column:group_name;type:varchar(64);not null;uniqueIndex:idx_tenant_groups_tg,priority:2"`
	Ratio     float64   `gorm:"column:ratio;type:decimal(20,8);not null;default:1"`
	Enabled   bool      `gorm:"column:enabled;not null;default:true"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (groupRow) TableName() string { return "tenant_groups" }

// UpsertGroup 按 (tenant_id, group_name) 唯一键 upsert 用户组倍率（设 enabled=true）。
// 倍率合法性（>= floor）由调用方（mtwire handler 经 pricing.Guard）前置校验，本方法只落库。
func (r *Repo) UpsertGroup(ctx context.Context, tenantID int64, groupName string, ratio float64) error {
	now := time.Now()
	row := groupRow{
		TenantID:  tenantID,
		GroupName: groupName,
		Ratio:     ratio,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "group_name"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"ratio":      ratio,
			"enabled":    true,
			"updated_at": now,
		}),
	}).Create(&row).Error
}

// ListGroups 列出某租户的用户组倍率配置（按组名升序）。
// 强制 WHERE tenant_id=? —— 代理自助列表的越权防线（scopeByTenant）。
func (r *Repo) ListGroups(ctx context.Context, tenantID int64) ([]tenant.UserGroup, error) {
	var rows []groupRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).Order("group_name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tenant.UserGroup, 0, len(rows))
	for _, g := range rows {
		out = append(out, tenant.UserGroup{
			TenantID:  g.TenantID,
			GroupName: g.GroupName,
			Ratio:     g.Ratio,
			Enabled:   g.Enabled,
		})
	}
	return out, nil
}
