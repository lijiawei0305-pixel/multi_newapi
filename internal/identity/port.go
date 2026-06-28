package identity

import (
	"context"

	"newapi-mt/internal/platform/appctx"
)

// ---- 对外接口（detailed-design §2.2）----

// Authenticator 负责 API Token 鉴权：把原始令牌反查为请求级身份 Principal。
type Authenticator interface {
	// AuthenticateToken 用 sha256 对 raw 取 hash 后反查 TokenStore，
	// 命中且启用则返回 *appctx.Principal{UserID,TenantID,Role}。
	// 空令牌返回 UNAUTHORIZED；未命中/被冻结返回 TOKEN_INVALID。
	AuthenticateToken(ctx context.Context, raw string) (*appctx.Principal, error)
}

// AccessGuard 提供角色与租户三类守卫。
type AccessGuard interface {
	// RequireAdmin 非管理员返回 FORBIDDEN_ADMIN。
	RequireAdmin(p *appctx.Principal) error
	// RequireTenantOwner 非该租户 owner（管理员除外）返回 FORBIDDEN_TENANT。
	RequireTenantOwner(p *appctx.Principal, tenantID int64) error
	// RequireTenantActive 租户非 active 返回 TENANT_INACTIVE。
	RequireTenantActive(ctx context.Context, tenantID int64) error
}

// ---- 消费者定义接口（本模块声明其依赖，运行时由 cmd/main 注入实现）----
// 依据 detailed-design §1.4：模块只 import 自己声明的接口，不 import 兄弟模块。

// TokenRecord 是 TokenStore 按 hash 反查返回的一条令牌记录。
// 零值（Disabled/UserDisabled 均为 false）表示令牌正常可用，便于 mock 构造。
type TokenRecord struct {
	UserID   int64
	TenantID int64
	Role     appctx.Role
	// Disabled 表示令牌自身被冻结/删除；false=正常。
	Disabled bool
	// UserDisabled 表示所属用户被冻结；false=正常。
	UserDisabled bool
}

// TokenStore 按令牌 hash 反查记录。真实实现为 GORM 版（见 TODO），
// 本轮提供 MemTokenStore 内存假实现。复用 New API 用户/Token 基座（增量：绑 tenant_id）。
type TokenStore interface {
	// FindByHash 以 hex(sha256(raw)) 反查；found=false 表示无此令牌，err 仅用于基础设施故障。
	FindByHash(ctx context.Context, tokenHash string) (rec *TokenRecord, found bool, err error)
}

// TenantStatus 租户状态。与 Tenant 模块状态机对齐，但本模块不 import 兄弟模块，
// 仅在此声明所需的最小契约（detailed-design §2.1 状态机）。
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDeleted   TenantStatus = "deleted"
)

// TenantStatusChecker 查询租户状态，供 RequireTenantActive 使用（内存假实现见 MemTenantStatusChecker）。
type TenantStatusChecker interface {
	// StatusOf 返回租户状态；found=false 表示租户不存在（视为非 active）。
	StatusOf(ctx context.Context, tenantID int64) (status TenantStatus, found bool, err error)
}

// ---- 错误码（detailed-design §2.2，apperr 命名空间）----
const (
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeTokenInvalid    = "TOKEN_INVALID"
	CodeForbiddenAdmin  = "FORBIDDEN_ADMIN"
	CodeForbiddenTenant = "FORBIDDEN_TENANT"
	CodeTenantInactive  = "TENANT_INACTIVE"
)
