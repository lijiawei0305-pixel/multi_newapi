package tenant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"time"
)

// ============================================================================
// 自定义域名（OEM，二期 §6.2/§6.3）：代理自助绑定 → DNS TXT 验所有权 → 异步签发证书 → active。
//
// 安全红线：只有 status=active 的自定义域名才参与 Host 解析（见 gormrepo.GetTenantByDomain）。
// 任何 pending_dns / verifying / dns_verified / failed 状态绝不被 ResolveByHost 命中。
// ============================================================================

// VerifyTXTPrefix 是所有权校验 TXT 记录的子域前缀：代理需添加 `_newapi-verify.<域名> = <token>`。
const VerifyTXTPrefix = "_newapi-verify"

// dnsLookupTimeout 限制单次 TXT 查询时长，避免 handler 长阻塞。
const dnsLookupTimeout = 5 * time.Second

// CustomDomainStatus 是自定义域名绑定的状态机类型。
type CustomDomainStatus string

const (
	// CustomDomainPendingDNS 待代理在 DNS 添加 TXT 记录（初始态，不进解析）。
	CustomDomainPendingDNS CustomDomainStatus = "pending_dns"
	// CustomDomainVerifying TXT 校验进行中。
	CustomDomainVerifying CustomDomainStatus = "verifying"
	// CustomDomainDNSVerified TXT 校验通过、等待异步签发证书（"待发证"信号）。
	CustomDomainDNSVerified CustomDomainStatus = "dns_verified"
	// CustomDomainActive 证书就绪、已进入 Host 解析（唯一可被命中的状态）。
	CustomDomainActive CustomDomainStatus = "active"
	// CustomDomainFailed 任一步失败（带 last_error），可重试回 verifying。
	CustomDomainFailed CustomDomainStatus = "failed"
)

// Valid 判断是否为已知的合法状态值。
func (s CustomDomainStatus) Valid() bool {
	switch s {
	case CustomDomainPendingDNS, CustomDomainVerifying, CustomDomainDNSVerified, CustomDomainActive, CustomDomainFailed:
		return true
	default:
		return false
	}
}

// allowedCustomDomainTransitions 编码绑定状态机（§6.3）：
//
//	pending_dns  -> verifying | failed
//	verifying    -> dns_verified | failed | pending_dns
//	dns_verified -> active | failed | verifying
//	active       -> failed | active（续期幂等）
//	failed       -> verifying | pending_dns（重试）
var allowedCustomDomainTransitions = map[CustomDomainStatus]map[CustomDomainStatus]bool{
	CustomDomainPendingDNS:  {CustomDomainVerifying: true, CustomDomainFailed: true},
	CustomDomainVerifying:   {CustomDomainDNSVerified: true, CustomDomainFailed: true, CustomDomainPendingDNS: true},
	CustomDomainDNSVerified: {CustomDomainActive: true, CustomDomainFailed: true, CustomDomainVerifying: true},
	CustomDomainActive:      {CustomDomainFailed: true, CustomDomainActive: true},
	CustomDomainFailed:      {CustomDomainVerifying: true, CustomDomainPendingDNS: true},
}

// CanTransitionTo 报告从 s 迁移到 next 是否合法（供状态机单测与持久化层守卫）。
func (s CustomDomainStatus) CanTransitionTo(next CustomDomainStatus) bool {
	return allowedCustomDomainTransitions[s][next]
}

// CustomDomain 是自定义域名绑定实体（对应表 tenant_custom_domains）。
type CustomDomain struct {
	ID            int64
	TenantID      int64
	Domain        string
	Status        CustomDomainStatus
	VerifyToken   string
	CertStatus    string     // ""=未签发；issued=已签发
	CertExpiresAt *time.Time // 可空：证书侧回填
	LastError     string     // 失败原因，供前端展示
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// VerifyTXTName 返回所有权校验需添加的 TXT 记录名（`_newapi-verify.<域名>`）。
func VerifyTXTName(domain string) string {
	return VerifyTXTPrefix + "." + domain
}

// ----------------------------------------------------------------------------
// 纯函数：域名格式校验 + 保留域名判定（表驱动单测覆盖）
// ----------------------------------------------------------------------------

// reservedBaseDomains 是主站根域：其本身及任意子域（含 *.wedreamhub.com 通配代理子域、
// api./www./admin. 等主站功能子域）一律不可被代理自助绑定为自定义域名。
var reservedBaseDomains = []string{BaseDomain}

// isReservedDomain 判断 domain 是否为不可绑定的保留域名（domain 须为已归一化的小写主机名）。
// 命中条件：等于任一保留根域，或为其子域。主站三类功能子域（api/www/admin.wedreamhub.com）
// 因均在 wedreamhub.com 之下被此规则覆盖。
func isReservedDomain(domain string) bool {
	d := normalizeHost(domain)
	if d == "" {
		return true
	}
	for _, base := range reservedBaseDomains {
		if d == base || strings.HasSuffix(d, "."+base) {
			return true
		}
	}
	return false
}

// validDomainFormat 判断是否为合法可绑定的 FQDN：长度 ≤253、至少一个点（拒单标签如 localhost）、
// 每个 DNS label 合法、顶级 label 非纯数字（拒 IP 字面量）。输入须已小写。
func validDomainFormat(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	if strings.Count(domain, ".") < 1 {
		return false
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if !validDNSLabel(label) {
			return false
		}
	}
	if isAllDigits(labels[len(labels)-1]) {
		return false
	}
	return true
}

// validDNSLabel 校验单个 DNS label：长度 [1,63]、仅 a-z0-9-、连字符不在首尾。
func validDNSLabel(s string) bool {
	n := len(s)
	if n == 0 || n > 63 {
		return false
	}
	for i := 0; i < n; i++ {
		ch := s[i]
		ok := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-'
		if !ok {
			return false
		}
		if ch == '-' && (i == 0 || i == n-1) {
			return false
		}
	}
	return true
}

// genVerifyToken 生成 32 字节十六进制随机 TXT 校验 token（crypto/rand）。
func genVerifyToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ----------------------------------------------------------------------------
// 领域服务
// ----------------------------------------------------------------------------

type customDomainService struct {
	repo CustomDomainRepo
	dns  DNSVerifier
}

// 编译期断言。
var _ CustomDomainService = (*customDomainService)(nil)

// NewCustomDomainService 组装自定义域名服务。dns 传 nil 时回退标准库 net.LookupTXT 实现。
func NewCustomDomainService(repo CustomDomainRepo, dns DNSVerifier) CustomDomainService {
	if dns == nil {
		dns = NewNetDNSVerifier()
	}
	return &customDomainService{repo: repo, dns: dns}
}

// Bind 校验 → 查每租户上限 → 落 pending_dns（不进解析）。
func (s *customDomainService) Bind(ctx context.Context, tenantID int64, domain string) (*CustomDomain, error) {
	d := normalizeHost(domain)
	if !validDomainFormat(d) {
		return nil, ErrDomainInvalid
	}
	if isReservedDomain(d) {
		return nil, ErrDomainReserved
	}
	// 每租户仅 1 个：已存在任何绑定（含 failed）即拒，需先 Unbind 再绑。
	if _, err := s.repo.GetCustomDomainByTenant(ctx, tenantID); err == nil {
		return nil, ErrDomainLimit
	} else if !errors.Is(err, ErrCustomDomainNotFound) {
		return nil, err
	}
	token, err := genVerifyToken()
	if err != nil {
		return nil, err
	}
	cd := &CustomDomain{
		TenantID:    tenantID,
		Domain:      d,
		Status:      CustomDomainPendingDNS,
		VerifyToken: token,
	}
	if err := s.repo.CreateCustomDomain(ctx, cd); err != nil {
		return nil, err // 域名全局冲突 -> ErrDomainTaken
	}
	return cd, nil
}

// VerifyOwnership 查 TXT 记录校验所有权。匹配 token → dns_verified（待发证）；否则 → failed。
// active 绑定直接幂等返回，不重复校验。
func (s *customDomainService) VerifyOwnership(ctx context.Context, tenantID int64) (*CustomDomain, error) {
	cd, err := s.repo.GetCustomDomainByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if cd.Status == CustomDomainActive {
		return cd, nil
	}
	// 置 verifying（best-effort，仅状态展示用）。
	_ = s.repo.UpdateCustomDomainStatus(ctx, cd.ID, CustomDomainVerifying, "")

	values, lerr := s.dns.LookupTXT(ctx, VerifyTXTName(cd.Domain))
	if lerr != nil {
		_ = s.repo.UpdateCustomDomainStatus(ctx, cd.ID, CustomDomainFailed, "DNS 查询失败："+lerr.Error())
		return nil, ErrDNSVerifyFailed
	}
	matched := false
	for _, v := range values {
		if strings.TrimSpace(v) == cd.VerifyToken {
			matched = true
			break
		}
	}
	if !matched {
		_ = s.repo.UpdateCustomDomainStatus(ctx, cd.ID, CustomDomainFailed, "未找到匹配的 TXT 记录（可能尚未生效）")
		return nil, ErrDNSVerifyFailed
	}
	if err := s.repo.UpdateCustomDomainStatus(ctx, cd.ID, CustomDomainDNSVerified, ""); err != nil {
		return nil, err
	}
	cd.Status = CustomDomainDNSVerified
	cd.LastError = ""
	return cd, nil
}

// Unbind 删除绑定，返回被删域名供装配层失效 Host 缓存。
func (s *customDomainService) Unbind(ctx context.Context, tenantID int64) (string, error) {
	return s.repo.DeleteCustomDomainByTenant(ctx, tenantID)
}

// GetByTenant 返回当前绑定（未绑定返回 ErrCustomDomainNotFound）。
func (s *customDomainService) GetByTenant(ctx context.Context, tenantID int64) (*CustomDomain, error) {
	return s.repo.GetCustomDomainByTenant(ctx, tenantID)
}

// ListPendingCert 返回待签发证书（dns_verified）的绑定。
func (s *customDomainService) ListPendingCert(ctx context.Context) ([]CustomDomain, error) {
	return s.repo.ListPendingCert(ctx)
}

// MarkCertIssued 证书就绪回写：仅允许 dns_verified（首签）或 active（续期）→ active。
func (s *customDomainService) MarkCertIssued(ctx context.Context, domain, certStatus string, expiresAt *time.Time) (string, error) {
	d := normalizeHost(domain)
	cd, err := s.repo.GetCustomDomainByName(ctx, d)
	if err != nil {
		return "", err
	}
	if !cd.Status.CanTransitionTo(CustomDomainActive) {
		return "", ErrDomainStatusInvalid
	}
	if certStatus == "" {
		certStatus = "issued"
	}
	if err := s.repo.UpdateCustomDomainCert(ctx, d, certStatus, expiresAt, CustomDomainActive); err != nil {
		return "", err
	}
	return d, nil
}

// ----------------------------------------------------------------------------
// DNSVerifier 默认实现（标准库 net.Resolver，带超时）
// ----------------------------------------------------------------------------

type netDNSVerifier struct {
	resolver *net.Resolver
}

// NewNetDNSVerifier 返回基于标准库 net.Resolver 的 TXT 查询实现（每次查询内置超时）。
func NewNetDNSVerifier() DNSVerifier {
	return &netDNSVerifier{resolver: net.DefaultResolver}
}

func (v *netDNSVerifier) LookupTXT(ctx context.Context, name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, dnsLookupTimeout)
	defer cancel()
	return v.resolver.LookupTXT(ctx, name)
}
