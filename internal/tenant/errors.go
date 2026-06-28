package tenant

import (
	"net/http"

	"newapi-mt/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.1 / §6.4）。
// TENANT_NOT_FOUND / SLUG_RESERVED / SLUG_DUPLICATE / TENANT_SUSPENDED 见设计文档；
// SLUG_INVALID（格式错误）与 TENANT_STATUS_INVALID（非法状态迁移）为本轮补充的同命名空间错误码。
var (
	// ErrTenantNotFound 按 id / host / slug 均未找到租户。
	ErrTenantNotFound = apperr.New("TENANT_NOT_FOUND", "租户不存在", http.StatusNotFound)
	// ErrTenantSuspended 租户被禁用（运行期守卫使用；本轮仅定义命名空间）。
	ErrTenantSuspended = apperr.New("TENANT_SUSPENDED", "租户已被禁用", http.StatusForbidden)
	// ErrSlugReserved slug 命中系统保留词。
	ErrSlugReserved = apperr.New("SLUG_RESERVED", "slug 为系统保留词", http.StatusBadRequest)
	// ErrSlugInvalid slug 格式非法（长度/字符/连字符位置）。
	ErrSlugInvalid = apperr.New("SLUG_INVALID", "slug 格式非法", http.StatusBadRequest)
	// ErrSlugDuplicate slug（或其派生域名）已被占用。
	ErrSlugDuplicate = apperr.New("SLUG_DUPLICATE", "slug 已被占用", http.StatusConflict)
	// ErrStatusTransition 非法的租户状态迁移。
	ErrStatusTransition = apperr.New("TENANT_STATUS_INVALID", "非法的租户状态迁移", http.StatusConflict)
)
