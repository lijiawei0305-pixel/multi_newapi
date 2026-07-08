package mtwire

// 自定义域名绑定（OEM，§6.2/§6.3）的 HTTP 层：
//   - 代理自助（owner 维度，挂 AgentOwnerAuth）：绑定 / 查询 / 触发 TXT 校验 / 解绑；
//   - 内网回写（共享密钥 X-Internal-Secret，Nginx 拒绝公网）：列待发证域名 / 证书就绪回写转 active。
//
// 安全：代理自助一律用 AgentOwnerAuth 校验过的 agentTenantID(c)，绝不接受客户端传 tenant_id；
// 跨租户因此天然隔离（A 代理拿不到/改不动 B 的绑定）。

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/tenant"
)

// internalSecretHeader 内网回写端点共享密钥头（与 auth-service 同名约定）。
const internalSecretHeader = "X-Internal-Secret"

// ============================ 代理自助（owner 维度） ============================

// bindCustomDomainReq 是 POST /api/tenant/custom-domain 入参。
type bindCustomDomainReq struct {
	Domain string `json:"domain"`
}

// customDomainOut 把绑定实体 + 配置指引映射为前端响应（含需添加的 A / TXT 记录）。
func (a *App) customDomainOut(cd *tenant.CustomDomain) gin.H {
	certExpires := ""
	if cd.CertExpiresAt != nil {
		certExpires = isoUTC(*cd.CertExpiresAt)
	}
	return gin.H{
		"bound":           true,
		"domain":          cd.Domain,
		"status":          string(cd.Status),
		"verify_token":    cd.VerifyToken,
		"cert_status":     cd.CertStatus,
		"cert_expires_at": certExpires,
		"last_error":      cd.LastError,
		// 代理需在自己的 DNS 添加的两条记录：A（指向主站）+ TXT（所有权校验）。
		"dns": gin.H{
			"a_record": gin.H{
				"type":  "A",
				"name":  cd.Domain,
				"value": a.siteIP,
			},
			"txt_record": gin.H{
				"type":  "TXT",
				"name":  tenant.VerifyTXTName(cd.Domain),
				"value": cd.VerifyToken,
			},
		},
	}
}

// HandleAgentBindCustomDomain POST /api/tenant/custom-domain —— 绑定自定义域名（返回 A + TXT 指引）。
func (a *App) HandleAgentBindCustomDomain(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	var req bindCustomDomainReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErr(c, tenant.ErrDomainInvalid)
		return
	}
	cd, err := a.CustomDomains.Bind(c.Request.Context(), tenantID, strings.TrimSpace(req.Domain))
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, a.customDomainOut(cd))
}

// HandleAgentGetCustomDomain GET /api/tenant/custom-domain —— 查当前绑定与状态（未绑定返回 {bound:false}）。
func (a *App) HandleAgentGetCustomDomain(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	cd, err := a.CustomDomains.GetByTenant(c.Request.Context(), tenantID)
	if err != nil {
		if isCustomDomainNotFound(err) {
			respondOK(c, gin.H{"bound": false})
			return
		}
		respondErr(c, err)
		return
	}
	respondOK(c, a.customDomainOut(cd))
}

// HandleAgentVerifyCustomDomain POST /api/tenant/custom-domain/verify —— 触发 TXT 所有权校验。
// 通过 → dns_verified（异步发证信号）；失败 → DNS_VERIFY_FAILED（落库已置 failed + last_error，前端再 GET 取详情）。
func (a *App) HandleAgentVerifyCustomDomain(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	cd, err := a.CustomDomains.VerifyOwnership(c.Request.Context(), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, a.customDomainOut(cd))
}

// HandleAgentUnbindCustomDomain DELETE /api/tenant/custom-domain —— 解绑并失效 Host 缓存。
func (a *App) HandleAgentUnbindCustomDomain(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	domain, err := a.CustomDomains.Unbind(c.Request.Context(), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	// 写后失效：解绑的域名若曾 active 被缓存，立即清除，避免旧解析滞留。
	a.tenantCache.Invalidate(c.Request.Context(), domain)
	respondOK(c, gin.H{"unbound": true, "domain": domain})
}

// isCustomDomainNotFound 判断是否"未绑定"错误（GET 时转为 {bound:false} 而非报错）。
func isCustomDomainNotFound(err error) bool {
	return err == tenant.ErrCustomDomainNotFound
}

// ============================ 内网回写（共享密钥） ============================

// InternalSecretAuth 校验内网共享密钥头（恒定时间比较）。未配置密钥时拒绝全部调用（deny-by-default）。
// 与 Nginx `location ^~ /api/internal/ { return 404; }`（拒公网）叠加为纵深防御；签发脚本走 127.0.0.1 直连。
func (a *App) InternalSecretAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader(internalSecretHeader)
		if a.internalSecret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(a.internalSecret)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "unauthorized", "code": "UNAUTHORIZED"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// HandleInternalListPendingCert GET /api/internal/domain/pending-cert —— 列待签发证书（dns_verified）域名。
// 供服务器侧 domain-cert-loop.sh 轮询消费。
func (a *App) HandleInternalListPendingCert(c *gin.Context) {
	pending, err := a.CustomDomains.ListPendingCert(c.Request.Context())
	if err != nil {
		respondErr(c, err)
		return
	}
	domains := make([]string, 0, len(pending))
	for _, p := range pending {
		domains = append(domains, p.Domain)
	}
	respondOK(c, gin.H{"domains": domains})
}

// certIssuedReq 是 POST /api/internal/domain/cert-issued 入参（签发脚本回写）。
type certIssuedReq struct {
	Domain     string `json:"domain"`
	CertStatus string `json:"cert_status"` // issued（默认）
	ExpiresAt  string `json:"expires_at"`  // RFC3339，可空
}

// HandleInternalCertIssued POST /api/internal/domain/cert-issued —— 证书就绪回写：转 active + 失效缓存。
func (a *App) HandleInternalCertIssued(c *gin.Context) {
	var req certIssuedReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErr(c, tenant.ErrDomainInvalid)
		return
	}
	var expires *time.Time
	if s := strings.TrimSpace(req.ExpiresAt); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			expires = &t
		}
	}
	host, err := a.CustomDomains.MarkCertIssued(c.Request.Context(), strings.TrimSpace(req.Domain), req.CertStatus, expires)
	if err != nil {
		respondErr(c, err)
		return
	}
	// 写后失效：转 active 后清掉可能存在的负/旧缓存，确保新域名立即可解析到租户。
	a.tenantCache.Invalidate(c.Request.Context(), host)
	respondOK(c, gin.H{"domain": host, "status": string(tenant.CustomDomainActive)})
}

// ============================ 主站 admin（跨租户，全局） ============================

// adminCustomDomainOut 是主站 admin「自定义域名」列表条目（跨租户，带租户标识）。
type adminCustomDomainOut struct {
	ID            int64  `json:"id"`
	TenantID      int64  `json:"tenant_id"`
	TenantSlug    string `json:"tenant_slug"`
	TenantName    string `json:"tenant_name"`
	Domain        string `json:"domain"`
	Status        string `json:"status"`
	CertStatus    string `json:"cert_status"`
	CertExpiresAt string `json:"cert_expires_at"`
	LastError     string `json:"last_error"`
	CreatedAt     string `json:"created_at"`
}

// HandleAdminListCustomDomains GET /api/admin/custom-domains —— 列出全部租户的自定义域名（AdminAuth）。
func (a *App) HandleAdminListCustomDomains(c *gin.Context) {
	list, err := a.CustomDomains.ListAll(c.Request.Context())
	if err != nil {
		respondErr(c, err)
		return
	}
	ids := make([]int64, 0, len(list))
	for _, d := range list {
		ids = append(ids, d.TenantID)
	}
	tenants := a.tenantsByIDs(c.Request.Context(), ids)
	out := make([]adminCustomDomainOut, 0, len(list))
	for _, d := range list {
		ti := tenants[d.TenantID]
		exp := ""
		if d.CertExpiresAt != nil {
			exp = isoUTC(*d.CertExpiresAt)
		}
		out = append(out, adminCustomDomainOut{
			ID:            d.ID,
			TenantID:      d.TenantID,
			TenantSlug:    ti.slug,
			TenantName:    ti.name,
			Domain:        d.Domain,
			Status:        string(d.Status),
			CertStatus:    d.CertStatus,
			CertExpiresAt: exp,
			LastError:     d.LastError,
			CreatedAt:     isoUTC(d.CreatedAt),
		})
	}
	respondOK(c, out)
}

// HandleAdminUnbindCustomDomain DELETE /api/admin/custom-domains/:id —— 主站 admin 强制解绑（AdminAuth）。
func (a *App) HandleAdminUnbindCustomDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, tenant.ErrCustomDomainNotFound)
		return
	}
	domain, err := a.CustomDomains.UnbindByID(c.Request.Context(), id)
	if err != nil {
		respondErr(c, err)
		return
	}
	a.tenantCache.Invalidate(c.Request.Context(), domain)
	respondOK(c, gin.H{"unbound": true, "domain": domain})
}

// tenantBrief 是 admin 列表用的最小租户标识。
type tenantBrief struct {
	slug string
	name string
}

// tenantsByIDs 批量回查 tenants 表的 id→(slug,name)（单次 IN 查询，避免 N+1；失败/缺失给空）。
func (a *App) tenantsByIDs(ctx context.Context, ids []int64) map[int64]tenantBrief {
	out := make(map[int64]tenantBrief, len(ids))
	if len(ids) == 0 {
		return out
	}
	var rows []struct {
		ID   int64
		Slug string
		Name string
	}
	if err := a.DB.WithContext(ctx).
		Table("tenants").Select("id, slug, name").
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = tenantBrief{slug: r.Slug, name: r.Name}
	}
	return out
}

// HandleInternalListActiveCert GET /api/internal/domain/active-cert —— 列全部 active 自定义域名。
// 供 cert-loop 周期回刷磁盘证书到期时间进 DB(SSL 到期提醒 P3 #9 的数据真实性):acme.sh --cron
// 自动续期只更新磁盘证书、不写 DB,不回刷则 cert_expires_at 停在首签时刻,续期后提醒变假警报。
func (a *App) HandleInternalListActiveCert(c *gin.Context) {
	list, err := a.CustomDomains.ListAll(c.Request.Context())
	if err != nil {
		respondErr(c, err)
		return
	}
	domains := make([]string, 0, len(list))
	for _, d := range list {
		if d.Status == tenant.CustomDomainActive {
			domains = append(domains, d.Domain)
		}
	}
	respondOK(c, gin.H{"domains": domains})
}
