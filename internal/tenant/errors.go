package tenant

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.1 / §6.4）。
// TENANT_NOT_FOUND / SLUG_RESERVED / SLUG_DUPLICATE / TENANT_SUSPENDED 见设计文档；
// SLUG_INVALID（格式错误）与 TENANT_STATUS_INVALID（非法状态迁移）为本轮补充的同命名空间错误码。
var (
	// ErrTenantNotFound 按 id / host / slug 均未找到租户。
	ErrTenantNotFound = apperr.New("TENANT_NOT_FOUND", "租户不存在", http.StatusNotFound)
	// ErrSiteNotActivated 命中 *.wedreamhub.com 下的未注册子域（既非 www/apex 主站，也无对应租户）。
	// 供 /api/tenant/current 区分「主站」与「站点未开通」两种"无租户命中"场景（见 IsMainSiteHost）。
	ErrSiteNotActivated = apperr.New("SITE_NOT_ACTIVATED", "站点未开通", http.StatusNotFound)
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

// 自定义域名（OEM，§6.2/§6.3）错误码（沿用模块前缀约定；规格里的 ERR_* 前缀按本仓库惯例落为无前缀 DOMAIN_*）。
var (
	// ErrDomainInvalid 域名格式非法（非 FQDN / 字符或长度越界）。
	ErrDomainInvalid = apperr.New("DOMAIN_INVALID", "域名格式非法", http.StatusBadRequest)
	// ErrDomainReserved 命中主站保留域名（wedreamhub.com 及其任意子域、主站功能子域）。
	ErrDomainReserved = apperr.New("DOMAIN_RESERVED", "该域名为系统保留域名，不可绑定", http.StatusBadRequest)
	// ErrDomainTaken 域名已被（本租户或其他租户）占用。
	ErrDomainTaken = apperr.New("DOMAIN_TAKEN", "该域名已被占用", http.StatusConflict)
	// ErrDomainLimit 超出每租户自定义域名上限（仅 1 个）。
	ErrDomainLimit = apperr.New("DOMAIN_LIMIT", "每个站点仅可绑定一个自定义域名", http.StatusConflict)
	// ErrDNSVerifyFailed DNS TXT 所有权校验失败（记录未生效或不匹配）。
	ErrDNSVerifyFailed = apperr.New("DNS_VERIFY_FAILED", "DNS TXT 校验失败，请确认记录已生效后重试", http.StatusBadRequest)
	// ErrCustomDomainNotFound 当前租户未绑定自定义域名。
	ErrCustomDomainNotFound = apperr.New("CUSTOM_DOMAIN_NOT_FOUND", "未绑定自定义域名", http.StatusNotFound)
	// ErrDomainStatusInvalid 当前状态不允许该操作（如对非 dns_verified 的域名回写证书）。
	ErrDomainStatusInvalid = apperr.New("DOMAIN_STATUS_INVALID", "自定义域名状态不允许该操作", http.StatusConflict)
)
