// Package gormrepo 用真实 GORM(MySQL) 实现 agentplan.PlanRepo。
//
// 一张表 agent_plans：主站代理套餐定义（code 唯一，seed 幂等基准）。
// 金额列用 decimal(20,2)（¥）；折扣系数用 decimal(10,4)。
package gormrepo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/agentplan"
)

// planRow 是 agent_plans 表的 GORM 模型。code 唯一索引（自然键，seed 幂等基准）。
type planRow struct {
	ID                 int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Code               string    `gorm:"column:code;type:varchar(32);not null;uniqueIndex:idx_agent_plans_code"`
	Name               string    `gorm:"column:name;type:varchar(64);not null"`
	Desc               string    `gorm:"column:description;type:varchar(255)"`
	Price              float64   `gorm:"column:price;type:decimal(20,2);not null;default:0"`
	AnchorPrice        float64   `gorm:"column:anchor_price;type:decimal(20,2);not null;default:0"`
	DiscountLabel      string    `gorm:"column:discount_label;type:varchar(32)"`
	GrantLevel         int       `gorm:"column:grant_level;not null;default:0"`
	GrantCanAPI        bool      `gorm:"column:grant_can_api;not null;default:false"`
	GrantDiscountRatio float64   `gorm:"column:grant_discount_ratio;type:decimal(10,4);not null;default:0"`
	ValidDays          int       `gorm:"column:valid_days;not null;default:365"`
	IsRecommended      bool      `gorm:"column:is_recommended;not null;default:false"`
	Badge              string    `gorm:"column:badge;type:varchar(64)"`
	Sort               int       `gorm:"column:sort;not null;default:0"`
	Status             string    `gorm:"column:status;type:varchar(16);not null;default:enabled"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (planRow) TableName() string { return "agent_plans" }

// Repo 是 agentplan.PlanRepo 的 GORM 实现，并附带 seed 辅助方法。
type Repo struct {
	db *gorm.DB
}

// 编译期断言。
var _ agentplan.PlanRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 agent_plans 表结构。由启动时的 App.Migrate 调用。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&planRow{})
}

// CreatePlan 入库并回填 ID/时间戳；code 冲突翻译为 ErrPlanInputInvalid。
func (r *Repo) CreatePlan(ctx context.Context, p *agentplan.Plan) error {
	row := toRow(p)
	now := time.Now()
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return agentplan.ErrPlanInputInvalid
		}
		return err
	}
	p.ID = row.ID
	p.CreatedAt = row.CreatedAt
	p.UpdatedAt = row.UpdatedAt
	return nil
}

// UpdatePlan 全量更新；不存在返回 ErrPlanNotFound，code 撞他行返回 ErrPlanInputInvalid。
func (r *Repo) UpdatePlan(ctx context.Context, id int64, in agentplan.PlanInput) error {
	var cur planRow
	if err := r.db.WithContext(ctx).Take(&cur, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return agentplan.ErrPlanNotFound
		}
		return err
	}
	res := r.db.WithContext(ctx).Model(&planRow{}).Where("id = ?", id).Updates(map[string]any{
		"code":                 in.Code,
		"name":                 in.Name,
		"description":          in.Desc,
		"price":                in.Price,
		"anchor_price":         in.AnchorPrice,
		"discount_label":       in.DiscountLabel,
		"grant_level":          in.GrantLevel,
		"grant_can_api":        in.GrantCanAPI,
		"grant_discount_ratio": in.GrantDiscountRatio,
		"valid_days":           in.ValidDays,
		"is_recommended":       in.IsRecommended,
		"badge":                in.Badge,
		"sort":                 in.Sort,
		"status":               string(in.Status),
		"updated_at":           time.Now(),
	})
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
			return agentplan.ErrPlanInputInvalid
		}
		return res.Error
	}
	return nil
}

// GetPlan 按 id 读取；不存在返回 ErrPlanNotFound。
func (r *Repo) GetPlan(ctx context.Context, id int64) (*agentplan.Plan, error) {
	var row planRow
	if err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, agentplan.ErrPlanNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

// ListPlans 返回全部套餐（按 sort、id 升序）。
func (r *Repo) ListPlans(ctx context.Context) ([]agentplan.Plan, error) {
	var rows []planRow
	if err := r.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]agentplan.Plan, 0, len(rows))
	for i := range rows {
		out = append(out, *toDomain(&rows[i]))
	}
	return out, nil
}

// EnsurePlan 幂等地按 code 落一个套餐（已存在则不覆盖人工改动），回读返回其 ID。供 seed。
func (r *Repo) EnsurePlan(ctx context.Context, p *agentplan.Plan) (int64, error) {
	row := toRow(p)
	now := time.Now()
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return 0, err
	}
	var saved planRow
	if err := r.db.WithContext(ctx).Take(&saved, "code = ?", p.Code).Error; err != nil {
		return 0, err
	}
	return saved.ID, nil
}

// ---- domain <-> row 映射 ----

func toRow(p *agentplan.Plan) planRow {
	return planRow{
		ID:                 p.ID,
		Code:               p.Code,
		Name:               p.Name,
		Desc:               p.Desc,
		Price:              p.Price,
		AnchorPrice:        p.AnchorPrice,
		DiscountLabel:      p.DiscountLabel,
		GrantLevel:         p.GrantLevel,
		GrantCanAPI:        p.GrantCanAPI,
		GrantDiscountRatio: p.GrantDiscountRatio,
		ValidDays:          p.ValidDays,
		IsRecommended:      p.IsRecommended,
		Badge:              p.Badge,
		Sort:               p.Sort,
		Status:             string(p.Status),
	}
}

func toDomain(row *planRow) *agentplan.Plan {
	return &agentplan.Plan{
		ID:                 row.ID,
		Code:               row.Code,
		Name:               row.Name,
		Desc:               row.Desc,
		Price:              row.Price,
		AnchorPrice:        row.AnchorPrice,
		DiscountLabel:      row.DiscountLabel,
		GrantLevel:         row.GrantLevel,
		GrantCanAPI:        row.GrantCanAPI,
		GrantDiscountRatio: row.GrantDiscountRatio,
		ValidDays:          row.ValidDays,
		IsRecommended:      row.IsRecommended,
		Badge:              row.Badge,
		Sort:               row.Sort,
		Status:             agentplan.PlanStatus(row.Status),
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}
