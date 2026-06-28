package gormrepo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
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

// LookupEnabledGroupRatio 读某租户某用户组「已启用」的倍率覆盖（计费路径用）。
// 命中 (tenant_id, group_name) 且 enabled=true → 返回 (ratio, true, nil)；
// 无行 / 已禁用 → (0, false, nil)；查询出错 → (0, false, err)。调用方据 (false 或 err) 回退全局倍率。
// 走 (tenant_id, group_name) 唯一索引，单行点查，无 N+1。
func (r *Repo) LookupEnabledGroupRatio(ctx context.Context, tenantID int64, groupName string) (float64, bool, error) {
	var row groupRow
	err := r.db.WithContext(ctx).
		Select("ratio").
		Where("tenant_id = ? AND group_name = ? AND enabled = ?", tenantID, groupName, true).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return row.Ratio, true, nil
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
