package tenant

import "time"

// BaseDomain 是一期 wildcard 二级域名的根域。每个租户自动获得
// `<slug>.wedreamhub.com` 的二级域名记录（见 proposal §9）。
// 自定义域名（OEM）顺延二期，本轮不处理。
const BaseDomain = "wedreamhub.com"

// TenantStatus 是租户状态机的类型常量。
type TenantStatus string

const (
	// StatusActive 正常营业（管理员开通后的初始态）。
	StatusActive TenantStatus = "active"
	// StatusSuspended 被禁用，可恢复为 active。
	StatusSuspended TenantStatus = "suspended"
	// StatusDeleted 软删除，终态，不可再迁移。
	StatusDeleted TenantStatus = "deleted"
)

// Valid 判断是否为已知的合法状态值。
func (s TenantStatus) Valid() bool {
	switch s {
	case StatusActive, StatusSuspended, StatusDeleted:
		return true
	default:
		return false
	}
}

// allowedTransitions 编码 detailed-design §2.1 的状态机：
//
//	active    -> suspended | deleted
//	suspended -> active     | deleted
//	deleted   -> (终态)
//
// 不在表内的迁移（含 same->same、任何 from deleted）均为非法。
var allowedTransitions = map[TenantStatus]map[TenantStatus]bool{
	StatusActive:    {StatusSuspended: true, StatusDeleted: true},
	StatusSuspended: {StatusActive: true, StatusDeleted: true},
	StatusDeleted:   {},
}

// CanTransitionTo 报告从 s 迁移到 next 是否合法。
func (s TenantStatus) CanTransitionTo(next TenantStatus) bool {
	return allowedTransitions[s][next]
}

// Tenant 是租户实体（对应 proposal §6 `tenants` 表，含 tokenplan_enabled）。
//
// OwnerUserID 是「代理=User+Tenant 1:1」决策落地的归属列：指向 new-api users.id 中
// 独占本租户的代理 owner（0 = 尚未设代理 / 主站根域）。设代理时由管理端写入，
// principalFrom 据此把「Host 解析出的租户.owner == 当前 session 用户」识别为 RoleAgentOwner。
type Tenant struct {
	ID               int64
	Slug             string
	Name             string
	Status           TenantStatus
	TokenplanEnabled bool
	OwnerUserID      int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TenantDomain 是租户的域名映射（对应 proposal §6 `tenant_domains` 表）。
// 一期仅 wildcard 二级域名；SSL/自定义域名字段顺延二期。
type TenantDomain struct {
	ID        int64
	TenantID  int64
	Domain    string
	IsPrimary bool
	CreatedAt time.Time
}

// DomainForSlug 返回 slug 对应的一期二级域名。
func DomainForSlug(slug string) string {
	return slug + "." + BaseDomain
}
