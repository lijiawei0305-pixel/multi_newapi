// Package gormstore 用真实 GORM(MySQL) 实现 identity.TokenStore（tenant_users + tenant_tokens 两表）。
//
// 设计取舍（Slice 2 集成阶段）：
//   - 本包同时拥有「租户用户」与「API Token」两张表——FindByHash 需返回 Role/UserDisabled，
//     用 JOIN 而非把用户字段反范式化进 token 行，避免与 cmd/main 的表 schema 隐式耦合。
//   - 表名用 tenant_users（而非 users），与基线 new-api 的 users 表区分，杜绝同 DB 误迁移。
//   - SeedToken 为「仅测试栈」字段：dev-login 据此直接下发 Cookie（明文存储，生产环境删除）。
//     正式鉴权仍只比对 hex(sha256(raw))（token_hash），绝不接受明文回查。
package gormstore

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

// userRow 是 tenant_users 表的 GORM 模型。(tenant_id, username) 唯一，保证租户内用户名唯一。
type userRow struct {
	ID       int64  `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID int64  `gorm:"column:tenant_id;not null;uniqueIndex:idx_tenant_users_tenant_username,priority:1"`
	Username string `gorm:"column:username;type:varchar(128);not null;uniqueIndex:idx_tenant_users_tenant_username,priority:2"`
	Role     string `gorm:"column:role;type:varchar(16);not null;default:user"`
	Disabled bool   `gorm:"column:disabled;not null;default:false"`
	// SeedToken 仅测试栈：dev-login 直接下发该明文为 Cookie。生产环境应删除此列与 dev-login。
	SeedToken string    `gorm:"column:seed_token;type:varchar(128)"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (userRow) TableName() string { return "tenant_users" }

// tokenRow 是 tenant_tokens 表的 GORM 模型。token_hash 唯一索引，供 FindByHash 反查。
type tokenRow struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID  int64     `gorm:"column:tenant_id;not null;index:idx_tenant_tokens_tenant"`
	UserID    int64     `gorm:"column:user_id;not null;index:idx_tenant_tokens_user"`
	Name      string    `gorm:"column:name;type:varchar(64)"`
	TokenHash string    `gorm:"column:token_hash;type:char(64);not null;uniqueIndex:idx_tenant_tokens_hash"`
	Disabled  bool      `gorm:"column:disabled;not null;default:false"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (tokenRow) TableName() string { return "tenant_tokens" }

// User 是供 cmd/main（seed + dev-login）使用的导出用户视图。
type User struct {
	ID        int64
	TenantID  int64
	Username  string
	Role      appctx.Role
	Disabled  bool
	SeedToken string // 仅测试栈
}

// Store 是 identity.TokenStore 的 GORM 实现，并附带 seed/dev-login 辅助方法。
type Store struct {
	db *gorm.DB
}

// 编译期断言：*Store 满足 identity.TokenStore 契约。
var _ identity.TokenStore = (*Store)(nil)

// New 用已建立连接的 *gorm.DB 构造 Store。
func New(db *gorm.DB) *Store { return &Store{db: db} }

// AutoMigrate 建/补 tenant_users 与 tenant_tokens 表结构（含唯一/普通索引）。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&userRow{}, &tokenRow{})
}

// FindByHash 实现 identity.TokenStore：按 token_hash 反查，JOIN tenant_users 取角色与冻结态。
// 无此令牌（或其用户缺失）-> found=false；err 仅用于基础设施故障。
func (s *Store) FindByHash(ctx context.Context, tokenHash string) (*identity.TokenRecord, bool, error) {
	var row struct {
		UserID        int64
		TenantID      int64
		TokenDisabled bool
		Role          string
		UserDisabled  bool
	}
	err := s.db.WithContext(ctx).
		Table("tenant_tokens AS t").
		Select("t.user_id AS user_id, t.tenant_id AS tenant_id, t.disabled AS token_disabled, "+
			"u.role AS role, u.disabled AS user_disabled").
		Joins("JOIN tenant_users AS u ON u.id = t.user_id").
		Where("t.token_hash = ?", tokenHash).
		Limit(1).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &identity.TokenRecord{
		UserID:       row.UserID,
		TenantID:     row.TenantID,
		Role:         appctx.Role(row.Role),
		Disabled:     row.TokenDisabled,
		UserDisabled: row.UserDisabled,
	}, true, nil
}

// EnsureUser 幂等地建用户（按 tenant_id+username 去重），回读规范行返回（含 ID）。供 seed。
func (s *Store) EnsureUser(ctx context.Context, u *User) (*User, error) {
	row := userRow{
		TenantID:  u.TenantID,
		Username:  u.Username,
		Role:      string(u.Role),
		Disabled:  u.Disabled,
		SeedToken: u.SeedToken,
	}
	if row.Role == "" {
		row.Role = string(appctx.RoleUser)
	}
	// 冲突 DoNothing：不覆盖既有用户（含人工修改）。
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row).Error; err != nil {
		return nil, err
	}
	// 回读：插入命中冲突时 row.ID 为 0，需按唯一键取规范行。
	return s.FindUserByUsernameStrict(ctx, u.TenantID, u.Username)
}

// EnsureToken 幂等地为用户建一条 Token（按 token_hash 去重，存 hash 不存明文）。供 seed。
func (s *Store) EnsureToken(ctx context.Context, tenantID, userID int64, name, rawToken string) error {
	row := tokenRow{
		TenantID:  tenantID,
		UserID:    userID,
		Name:      name,
		TokenHash: identity.HashToken(rawToken),
	}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row).Error
}

// FindUserByUsername 按 (tenant_id, username) 查用户（含 SeedToken）；found=false 表示无此用户。
// 仅测试栈 dev-login 使用。
func (s *Store) FindUserByUsername(ctx context.Context, tenantID int64, username string) (*User, bool, error) {
	var row userRow
	err := s.db.WithContext(ctx).Take(&row, "tenant_id = ? AND username = ?", tenantID, username).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return toUser(&row), true, nil
}

// FindUserByUsernameStrict 同 FindUserByUsername，但「不存在」视为基础设施异常返回 err（供 seed 回读用）。
func (s *Store) FindUserByUsernameStrict(ctx context.Context, tenantID int64, username string) (*User, error) {
	var row userRow
	if err := s.db.WithContext(ctx).Take(&row, "tenant_id = ? AND username = ?", tenantID, username).Error; err != nil {
		return nil, err
	}
	return toUser(&row), nil
}

// toUser 把 DB 行映射为导出视图。
func toUser(r *userRow) *User {
	return &User{
		ID:        r.ID,
		TenantID:  r.TenantID,
		Username:  r.Username,
		Role:      appctx.Role(r.Role),
		Disabled:  r.Disabled,
		SeedToken: r.SeedToken,
	}
}
