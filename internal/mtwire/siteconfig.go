package mtwire

// 租户站点装修（OEM 最小版，§5/§9）的 HTTP 层 + 存储装配：
//   - 代理自助（owner 维度）：读/改装修配置（站名/Logo/品牌隐藏/主题色）+ 上传 Logo；
//   - Logo 以 data: URL 内联存储（免对象存储/静态目录；后续切对象存储仅换 Blob 实现）。
//
// /api/tenant/current 的品牌合并见 http.go：HandleTenantCurrent 用 siteConfigRepo found-aware 直读，
// 未配置租户回退租户名（而非 siteconfig 主站默认 "New API"）。

import (
	"context"
	"encoding/base64"
	"io"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/siteconfig"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// ---- data: URL Blob（免对象存储的最小实现）----

// dataURLBlob 把素材内联为 `data:<mime>;base64,<...>` URL，随站点配置一并存库、随响应回传。
// 适合 OEM 小体积 Logo；切对象存储/CDN 仅需替换为另一个 siteconfig.Blob 实现。
type dataURLBlob struct{}

func newDataURLBlob() *dataURLBlob { return &dataURLBlob{} }

func (dataURLBlob) Put(_ context.Context, _ string, data []byte, contentType string) (string, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (dataURLBlob) Delete(_ context.Context, _ string) error { return nil }

// ---- DTO ----

// siteConfigPatchIn 是 PUT /api/tenant/site-config 入参（snake_case，指针=局部更新）。
type siteConfigPatchIn struct {
	SiteName        *string `json:"site_name"`
	LogoURL         *string `json:"logo_url"`
	FaviconURL      *string `json:"favicon_url"`
	HeroTitle       *string `json:"hero_title"`
	HeroSubtitle    *string `json:"hero_subtitle"`
	Announcement    *string `json:"announcement"`
	CustomerService *string `json:"customer_service"`
	Footer          *string `json:"footer"`
	BrandHidden     *bool   `json:"brand_hidden"`
	ThemePreset     *string `json:"theme_preset"`
	ThemeColor      *string `json:"theme_color"`
}

func (in siteConfigPatchIn) toPatch() siteconfig.SiteConfigPatch {
	return siteconfig.SiteConfigPatch{
		SiteName:        in.SiteName,
		LogoURL:         in.LogoURL,
		FaviconURL:      in.FaviconURL,
		HeroTitle:       in.HeroTitle,
		HeroSubtitle:    in.HeroSubtitle,
		Announcement:    in.Announcement,
		CustomerService: in.CustomerService,
		Footer:          in.Footer,
		BrandHidden:     in.BrandHidden,
		ThemePreset:     in.ThemePreset,
		ThemeColor:      in.ThemeColor,
	}
}

// siteConfigOut 把装修配置映射为前端响应（snake_case）。
func siteConfigOut(cfg *siteconfig.SiteConfig) gin.H {
	return gin.H{
		"site_name":        cfg.SiteName,
		"logo_url":         cfg.LogoURL,
		"favicon_url":      cfg.FaviconURL,
		"hero_title":       cfg.HeroTitle,
		"hero_subtitle":    cfg.HeroSubtitle,
		"announcement":     cfg.Announcement,
		"customer_service": cfg.CustomerService,
		"footer":           cfg.Footer,
		"brand_hidden":     cfg.BrandHidden,
		"theme_preset":     cfg.ThemePreset,
		"theme_color":      cfg.ThemeColor,
	}
}

// effectiveSiteConfig 返回租户"有效装修配置"：已配置则用之；未配置时回退（站名=租户名、主题色=默认色）。
// found-aware 直读（不走 service 的主站 "New API" 默认），避免代理站显示成主站名。
func (a *App) effectiveSiteConfig(ctx context.Context, t *tenant.Tenant) *siteconfig.SiteConfig {
	base := siteconfig.MainSiteDefault()
	base.TenantID = t.ID
	base.SiteName = t.Name // 默认品牌名=租户名
	if a.siteConfigRepo != nil {
		if cfg, found, err := a.siteConfigRepo.GetConfig(ctx, t.ID); err == nil && found {
			if cfg.SiteName == "" {
				cfg.SiteName = t.Name
			}
			if cfg.ThemeColor == "" {
				cfg.ThemeColor = base.ThemeColor
			}
			return cfg
		}
	}
	return base
}

// ---- 代理自助 handlers ----

// HandleAgentGetSiteConfig GET /api/tenant/site-config —— 读当前租户装修配置（含回退默认）。
// 租户一律取自 agentTenantID(c)（AgentOwnerAuthByUser 校验过的 owner 租户），绝不用 tenantFrom(c)
// （Host 租户）——否则调用者的 Host 若解析到别的租户，会读到别人的装修配置（越权读，Fix 2）。
func (a *App) HandleAgentGetSiteConfig(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	t, err := a.TenantService.Get(c.Request.Context(), agentTenantID(c))
	if err != nil {
		respondErr(c, err) // TENANT_NOT_FOUND
		return
	}
	respondOK(c, siteConfigOut(a.effectiveSiteConfig(c.Request.Context(), t)))
}

// HandleAgentUpdateSiteConfig PUT /api/tenant/site-config —— 局部更新装修配置（受控字段校验在包内）。
// 读/改/鉴权三处租户一律取自 agentTenantID(c)，与 HandleAgentGetSiteConfig 同一口径（Fix 2）；
// 绝不用 tenantFrom(c)（Host 租户）—— 其在无 Host→租户映射时（如主站）为 nil，用于响应会 nil-deref。
func (a *App) HandleAgentUpdateSiteConfig(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	var in siteConfigPatchIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, siteconfig.ErrThemeNotInPalette) // 入参非法：复用 400 语义码（最常见的受控字段）
		return
	}
	if err := a.SiteConfig.Patch(c.Request.Context(), tenantID, in.toPatch()); err != nil {
		respondErr(c, err)
		return
	}
	t, err := a.TenantService.Get(c.Request.Context(), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, siteConfigOut(a.effectiveSiteConfig(c.Request.Context(), t)))
}

// HandleAgentUploadLogo POST /api/tenant/site-config/logo —— 上传 Logo（multipart "file"），
// 经 AssetService 校验类型/大小后内联为 data: URL 并写入 logo_url。
func (a *App) HandleAgentUploadLogo(c *gin.Context) {
	if !a.ensureAgentLevel(c, 1) {
		return
	}
	tenantID := agentTenantID(c)
	fh, err := c.FormFile("file")
	if err != nil {
		respondErr(c, siteconfig.ErrAssetTypeForbidden)
		return
	}
	f, err := fh.Open()
	if err != nil {
		respondErr(c, siteconfig.ErrAssetTypeForbidden)
		return
	}
	defer f.Close()
	// 限制读取上限（policy 再次校验 ≤2MB）；+1 便于检出超限。
	data, err := io.ReadAll(io.LimitReader(f, siteconfig.MaxAssetBytes+1))
	if err != nil {
		respondErr(c, err)
		return
	}
	url, err := a.Assets.Upload(c.Request.Context(), tenantID, siteconfig.File{Name: fh.Filename, Data: data})
	if err != nil {
		respondErr(c, err)
		return
	}
	if err := a.SiteConfig.Patch(c.Request.Context(), tenantID, siteconfig.SiteConfigPatch{LogoURL: &url}); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"logo_url": url})
}
