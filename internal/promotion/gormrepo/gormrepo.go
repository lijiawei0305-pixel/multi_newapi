// Package gormrepo 用真实 GORM(MySQL) 实现 promotion.PromotionRepo
// （agent_promotion_channels + agent_promotion_attributions 两表）。
//
// 键统一为 tenant_id（代理=租户）。关键不变量：
//   - 渠道码 code 全局唯一索引；CreateChannel 冲突翻译为 promotion.ErrChannelPrefixDup
//     （随机码下实际不可达，仅守约）。
//   - IncrRegisteredCount 用原子 UPDATE ... SET registered_count = registered_count + 1
//     在 DB 层累加，杜绝读改写竞态。
//   - ListChannelsByTenant 强制 WHERE tenant_id=? （scopeByTenant，越权防线）。
//
// 表名 agent_promotion_*：已 grep 确认不撞 new-api 原生 model/。
package gormrepo

import (
	"context"
	"errors"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/promotion"
)

// mysqlDupErrNo 是 MySQL "Duplicate entry" 的错误号（唯一键冲突）。
const mysqlDupErrNo = 1062

// channelRow 是 agent_promotion_channels 表的 GORM 模型。
// code 全局唯一（<prefix>_<rand>，随机段保证唯一）；tenant_id 带索引支持 scopeByTenant 列表。
type channelRow struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID        int64     `gorm:"column:tenant_id;not null;index:idx_apc_tenant"`
	Code            string    `gorm:"column:code;type:varchar(64);not null;uniqueIndex:idx_apc_code"`
	Name            string    `gorm:"column:name;type:varchar(128);not null;default:''"`
	RegisteredCount int64     `gorm:"column:registered_count;not null;default:0"`
	Voided          bool      `gorm:"column:voided;not null;default:false"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (channelRow) TableName() string { return "agent_promotion_channels" }

// attributionRow 是 agent_promotion_attributions 表的 GORM 模型（用户→渠道归属）。
// user_id 唯一：一个用户只归属一个渠道（重复注册不重复计数）。
type attributionRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      int64     `gorm:"column:user_id;not null;uniqueIndex:idx_apa_user"`
	TenantID    int64     `gorm:"column:tenant_id;not null;index:idx_apa_tenant"`
	ChannelID   int64     `gorm:"column:channel_id;not null;index:idx_apa_channel"`
	ChannelCode string    `gorm:"column:channel_code;type:varchar(64);not null"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (attributionRow) TableName() string { return "agent_promotion_attributions" }

// Repo 是 promotion.PromotionRepo 的 GORM 实现。
type Repo struct {
	db  *gorm.DB
	now func() time.Time
}

// 编译期断言：*Repo 满足 promotion.PromotionRepo 契约。
var _ promotion.PromotionRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db, now: time.Now} }

// AutoMigrate 建/补 推广渠道与归属两表结构。由 mtwire.Migrate 在 master 节点调用。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&channelRow{}, &attributionRow{})
}

// CreateChannel 入库渠道并回填 c.ID/时间戳；code 唯一冲突 -> promotion.ErrChannelPrefixDup。
func (r *Repo) CreateChannel(ctx context.Context, c *promotion.Channel) error {
	now := r.now()
	row := channelRow{
		TenantID:        c.TenantID,
		Code:            c.ChannelCode,
		Name:            c.Name,
		RegisteredCount: c.RegisteredCount,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isDuplicate(err) {
			return promotion.ErrChannelPrefixDup
		}
		return err
	}
	c.ID = row.ID
	c.CreatedAt = row.CreatedAt
	c.UpdatedAt = row.UpdatedAt
	return nil
}

// GetChannelByCode 按完整 channel_code 查渠道；未找到 -> promotion.ErrChannelNotFound。
func (r *Repo) GetChannelByCode(ctx context.Context, code string) (*promotion.Channel, error) {
	var row channelRow
	err := r.db.WithContext(ctx).Take(&row, "code = ?", code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, promotion.ErrChannelNotFound
		}
		return nil, err
	}
	return toChannel(&row), nil
}

// IncrRegisteredCount 原子地令渠道 registered_count+1；渠道不存在 -> promotion.ErrChannelNotFound。
func (r *Repo) IncrRegisteredCount(ctx context.Context, channelID int64) error {
	res := r.db.WithContext(ctx).Model(&channelRow{}).
		Where("id = ?", channelID).
		UpdateColumn("registered_count", gorm.Expr("registered_count + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return promotion.ErrChannelNotFound
	}
	return nil
}

// CreateAttribution 落库一条用户归属记录（user -> tenant+channel），按 user_id 幂等。
func (r *Repo) CreateAttribution(ctx context.Context, a *promotion.Attribution) error {
	row := attributionRow{
		UserID:      a.UserID,
		TenantID:    a.TenantID,
		ChannelID:   a.ChannelID,
		ChannelCode: a.ChannelCode,
		CreatedAt:   r.now(),
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isDuplicate(err) {
			return nil // 同一用户重复归属：幂等忽略（不重复计数由调用方据此跳过）
		}
		return err
	}
	return nil
}

// ListChannelsByTenant 列出某租户的全部推广渠道（按创建时间倒序）。
// 强制 WHERE tenant_id=? —— 代理自助列表的越权防线（scopeByTenant）。非接口方法。
func (r *Repo) ListChannelsByTenant(ctx context.Context, tenantID int64) ([]promotion.Channel, error) {
	var rows []channelRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at desc, id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]promotion.Channel, 0, len(rows))
	for i := range rows {
		out = append(out, *toChannel(&rows[i]))
	}
	return out, nil
}

// VoidChannelsByTenant 原子地将某租户的全部渠道标记为已作废（voided=true）；幂等——无渠道 /
// 已全部作废的租户调用不报错（RowsAffected 可为 0，不视为失败）。由 mtwire.HandleAdminUpdateAgent
// 在代理升级为独立档（level>=1）时调用。
func (r *Repo) VoidChannelsByTenant(ctx context.Context, tenantID int64) error {
	return r.db.WithContext(ctx).Model(&channelRow{}).
		Where("tenant_id = ?", tenantID).
		Update("voided", true).Error
}

// toChannel 把 DB 行映射为 domain 模型（Prefix/SignupURL 不持久化，列表/创建响应无需）。
func toChannel(row *channelRow) *promotion.Channel {
	return &promotion.Channel{
		ID:              row.ID,
		TenantID:        row.TenantID,
		Name:            row.Name,
		ChannelCode:     row.Code,
		RegisteredCount: row.RegisteredCount,
		Voided:          row.Voided,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

// isDuplicate 判断是否唯一键冲突：优先用 GORM TranslateError 归一化的 ErrDuplicatedKey，
// 兜底再看 MySQL 原生错误号 1062（生产未开 TranslateError），与 tenant 仓储口径一致。
func isDuplicate(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var myErr *driver.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == mysqlDupErrNo
	}
	return false
}
