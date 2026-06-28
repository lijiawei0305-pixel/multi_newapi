// Command server 是集成 Demo 的薄 gin 服务，逐 Slice 打通「DB -> 端点 -> 前端页」最小链路：
//
//	Slice 1：请求 Host -> TenantResolver 解析租户 -> 读站点装修 -> 返回品牌信息。
//	Slice 2：API Token / Cookie 鉴权 -> 注入 Principal -> 钱包余额 / 兑换码 / 充值下单(stub)。
//
// 计费/渠道/支付等其余模块顺延后续 Slice，本服务以最小适配桩注入其依赖。
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"newapi-mt/internal/identity"
	idstore "newapi-mt/internal/identity/gormstore"
	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
	"newapi-mt/internal/tenant"
	"newapi-mt/internal/tenant/gormrepo"
	"newapi-mt/internal/wallet"
	walletrepo "newapi-mt/internal/wallet/gormrepo"
)

const (
	// ctxTenantKey 是中间件解析出的租户在 gin.Context 中的存放键。
	ctxTenantKey = "tenant"
	// ctxPrincipalKey 是鉴权中间件解析出的 Principal 在 gin.Context 中的存放键。
	ctxPrincipalKey = "principal"
	// sessionCookie 是 dev-login 下发的会话 Cookie 名（也是鉴权中间件回退读取的 Cookie）。
	sessionCookie = "td_session"
)

// siteConfigRow 是演示用站点装修配置（site_configs 表，按 tenant_id 一对一）。
// 一期仅承载 /api/tenant/current 渲染前台所需的最小品牌字段；完整 SiteConfig
// （internal/siteconfig）顺延后续 Slice 接入，故此模型留在 cmd 装配层而非 gormrepo。
type siteConfigRow struct {
	TenantID   int64  `gorm:"column:tenant_id;primaryKey"`
	SiteName   string `gorm:"column:site_name;type:varchar(128)"`
	LogoURL    string `gorm:"column:logo_url;type:varchar(255)"`
	ThemeColor string `gorm:"column:theme_color;type:varchar(16)"`
	FooterText string `gorm:"column:footer_text;type:varchar(255)"`
}

// TableName 固定表名。
func (siteConfigRow) TableName() string { return "site_configs" }

// tenantSeed 描述一个演示租户连同其演示用户/钱包/兑换码的幂等 seed 规格。
type tenantSeed struct {
	slug          string
	siteName      string
	themeColor    string
	footer        string
	withLocalhost bool // 仅 demo 站补 localhost 域名，便于本地调试

	username   string // 演示用户名（租户内唯一）
	rawToken   string // 演示 API Token 明文（hash 入库；同值存 SeedToken 供 dev-login）
	balanceUSD float64
	redeemCode string
	redeemUSD  float64
}

// tenantSeeds 返回内置演示租户清单。新增/调整演示数据集中在此一处。
func tenantSeeds() []tenantSeed {
	return []tenantSeed{
		{
			slug: "demo", siteName: "Demo 代理站", themeColor: "#2F54EB",
			footer:        "© 2026 Demo 代理站 · Powered by newapi628",
			withLocalhost: true,
			username:      "demo@demo", rawToken: "sk-td-demo-demo",
			balanceUSD: 100, redeemCode: "WELCOME10", redeemUSD: 10,
		},
		{
			slug: "tokendream", siteName: "TokenDream", themeColor: "#7C3AED",
			footer:        "© 2026 TokenDream · Powered by newapi628",
			withLocalhost: false,
			username:      "demo@td", rawToken: "sk-td-demo-tokendream",
			balanceUSD: 100, redeemCode: "WELCOME10", redeemUSD: 10,
		},
	}
}

// --- Slice 2 最小适配桩（wallet.Credit 的依赖；Slice 3 接真实模块）---

// fixedRatioPricing 是 wallet.PricingService 的最小实现：固定分组倍率 1.0（差价为 0）。
// Slice 3 接真实 pricing 模块（按租户/分组有效倍率算充值差价）。
type fixedRatioPricing struct{}

func (fixedRatioPricing) GroupRatio(ctx context.Context, tenantID, groupID int64) (float64, error) {
	return 1.0, nil
}

// noopEarningSink 是 wallet.EarningSink 的最小实现：丢弃收益记录。
// Slice 3 接真实 agent 模块（写 agent_earning_logs）。
type noopEarningSink struct{}

func (noopEarningSink) AddEarning(ctx context.Context, e wallet.EarningEntry) error { return nil }

func main() {
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	dsn := os.Getenv("SQL_DSN")
	if dsn == "" {
		log.Fatal("[server] SQL_DSN is required (mysql DSN)")
	}
	port := getenv("PORT", "3000")
	webDist := getenv("WEB_DIST", "./web/dist")

	db, err := openDB(dsn)
	if err != nil {
		log.Fatalf("[server] connect mysql: %v", err)
	}

	// 迁移：租户两表 + 站点装修 + 身份(用户/Token) + 钱包(余额/兑换码)。
	if err := gormrepo.AutoMigrate(db); err != nil {
		log.Fatalf("[server] migrate tenant tables: %v", err)
	}
	if err := db.AutoMigrate(&siteConfigRow{}); err != nil {
		log.Fatalf("[server] migrate site_configs: %v", err)
	}
	if err := idstore.AutoMigrate(db); err != nil {
		log.Fatalf("[server] migrate identity tables: %v", err)
	}
	if err := walletrepo.AutoMigrate(db); err != nil {
		log.Fatalf("[server] migrate wallet tables: %v", err)
	}

	repo := gormrepo.New(db)
	svc := tenant.NewService(repo, tenant.NewSlugValidator())
	resolver := tenant.NewResolver(repo, tenant.NewMemCache())

	store := idstore.New(db)
	authn := identity.NewAuthenticator(store)

	wRepo := walletrepo.New(db)
	wSvc := wallet.NewService(wRepo, fixedRatioPricing{}, noopEarningSink{})

	if err := seedAll(context.Background(), db, svc, repo, store, wRepo); err != nil {
		log.Fatalf("[server] seed: %v", err)
	}

	r := buildRouter(routerDeps{
		resolver: resolver,
		db:       db,
		webDist:  webDist,
		authn:    authn,
		store:    store,
		wRepo:    wRepo,
		wSvc:     wSvc,
	})
	log.Printf("[server] listening on :%s (web_dist=%s)", port, webDist)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("[server] run: %v", err)
	}
}

// openDB 连接 MySQL，并对「容器编排下 mysql 尚未就绪」做有限重试后再放弃。
func openDB(dsn string) (*gorm.DB, error) {
	const (
		maxAttempts = 30
		interval    = 2 * time.Second
	)
	cfg := &gorm.Config{
		TranslateError: true, // 把驱动错误归一化为 gorm.ErrDuplicatedKey 等，供 gormrepo 判重
		Logger:         logger.Default.LogMode(logger.Warn),
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		db, err := gorm.Open(mysql.Open(dsn), cfg)
		if err == nil {
			var sqlDB *sql.DB
			if sqlDB, err = db.DB(); err == nil {
				if err = sqlDB.Ping(); err == nil {
					return db, nil
				}
			}
		}
		lastErr = err
		log.Printf("[server] mysql not ready (attempt %d/%d): %v", attempt, maxAttempts, err)
		time.Sleep(interval)
	}
	return nil, fmt.Errorf("mysql unreachable after %d attempts: %w", maxAttempts, lastErr)
}

// seedAll 幂等地建立全部演示租户及其演示用户/钱包/兑换码。已存在则仅补缺，不覆盖人工改动。
func seedAll(ctx context.Context, db *gorm.DB, svc tenant.TenantService, repo tenant.TenantRepo, store *idstore.Store, wRepo *walletrepo.Repo) error {
	for _, spec := range tenantSeeds() {
		tid, err := ensureTenant(ctx, svc, repo, db, spec)
		if err != nil {
			return err
		}
		if err := seedIdentityWallet(ctx, store, wRepo, tid, spec); err != nil {
			return err
		}
	}
	return nil
}

// ensureTenant 幂等地建租户（slug 自动写 `<slug>.wedreamhub.com` 域名），可选补 localhost，
// 并 upsert 站点装修。返回租户 ID。
func ensureTenant(ctx context.Context, svc tenant.TenantService, repo tenant.TenantRepo, db *gorm.DB, spec tenantSeed) (int64, error) {
	var tid int64
	existing, err := repo.GetTenantBySlug(ctx, spec.slug)
	switch {
	case err == nil:
		tid = existing.ID
	case apperr.Is(err, "TENANT_NOT_FOUND"):
		t, cerr := svc.Create(ctx, tenant.CreateTenantInput{Slug: spec.slug, Name: spec.siteName})
		if cerr != nil {
			return 0, fmt.Errorf("create tenant %s: %w", spec.slug, cerr)
		}
		tid = t.ID
	default:
		return 0, fmt.Errorf("lookup tenant %s: %w", spec.slug, err)
	}

	if spec.withLocalhost {
		d := &tenant.TenantDomain{TenantID: tid, Domain: "localhost"}
		if e := repo.CreateDomain(ctx, d); e != nil && !apperr.Is(e, "SLUG_DUPLICATE") {
			return 0, fmt.Errorf("seed localhost domain: %w", e)
		}
	}

	cfg := siteConfigRow{
		TenantID:   tid,
		SiteName:   spec.siteName,
		ThemeColor: spec.themeColor,
		FooterText: spec.footer,
	}
	// OnConflict DoNothing：幂等写入，不覆盖后续人工修改。
	if e := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&cfg).Error; e != nil {
		return 0, fmt.Errorf("seed site config %s: %w", spec.slug, e)
	}
	return tid, nil
}

// seedIdentityWallet 幂等地为租户建演示用户 + API Token + 钱包余额 + 兑换码，并把 Token 明文写入日志（供测试）。
func seedIdentityWallet(ctx context.Context, store *idstore.Store, wRepo *walletrepo.Repo, tid int64, spec tenantSeed) error {
	u, err := store.EnsureUser(ctx, &idstore.User{
		TenantID:  tid,
		Username:  spec.username,
		Role:      appctx.RoleUser,
		SeedToken: spec.rawToken,
	})
	if err != nil {
		return fmt.Errorf("seed user %s: %w", spec.username, err)
	}
	if err := store.EnsureToken(ctx, tid, u.ID, "default", spec.rawToken); err != nil {
		return fmt.Errorf("seed token for %s: %w", spec.username, err)
	}
	if err := wRepo.EnsureBalance(ctx, tid, u.ID, spec.balanceUSD); err != nil {
		return fmt.Errorf("seed balance for %s: %w", spec.username, err)
	}
	if err := wRepo.EnsureRedemption(ctx, &wallet.RedemptionCode{
		TenantID:  tid,
		Code:      spec.redeemCode,
		AmountUSD: spec.redeemUSD,
		Status:    wallet.RedemptionEnabled,
	}); err != nil {
		return fmt.Errorf("seed redemption for %s: %w", spec.username, err)
	}
	log.Printf("[seed] tenant=%s(id=%d) user=%s(id=%d) token=%q balance=%.2fUSD redeem=%s(%.0fUSD)",
		spec.slug, tid, spec.username, u.ID, spec.rawToken, spec.balanceUSD, spec.redeemCode, spec.redeemUSD)
	return nil
}

// routerDeps 聚合 buildRouter 所需依赖，避免过长形参。
type routerDeps struct {
	resolver tenant.TenantResolver
	db       *gorm.DB
	webDist  string
	authn    identity.Authenticator
	store    *idstore.Store
	wRepo    wallet.WalletRepo
	wSvc     wallet.WalletService
}

// buildRouter 装配路由：健康检查、租户中间件 + /api（公开/鉴权两组）、静态资源（SPA fallback）。
func buildRouter(d routerDeps) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	api.Use(tenantMiddleware(d.resolver))

	// 公开（无需 Token）：站点品牌 + 测试栈 dev 登录。
	api.GET("/tenant/current", currentTenantHandler(d.db))
	api.POST("/dev/login", devLoginHandler(d.store)) // 仅测试栈，生产删除

	// 鉴权组：Bearer / Cookie -> Principal。
	authed := api.Group("")
	authed.Use(authMiddleware(d.authn))
	authed.GET("/tenant/wallet", walletBalanceHandler(d.wRepo))
	authed.POST("/tenant/wallet/redeem", walletRedeemHandler(d.wSvc, d.wRepo))
	authed.POST("/tenant/wallet/recharge", walletRechargeHandler())

	registerStatic(r, d.webDist)
	return r
}

// tenantMiddleware 按 Host 解析租户并存入上下文。
// 测试/调试可用 ?host= 或 X-Debug-Host 覆盖真实 Host。
// 解析失败不在此中断（静态/健康检查无需租户）；需要租户的 handler 自行返回 404。
func tenantMiddleware(resolver tenant.TenantResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		host := c.Query("host")
		if host == "" {
			host = c.GetHeader("X-Debug-Host")
		}
		if host == "" {
			host = c.Request.Host
		}
		if t, err := resolver.ResolveByHost(c.Request.Context(), host); err == nil {
			c.Set(ctxTenantKey, t)
		}
		c.Next()
	}
}

// authMiddleware 解析 Bearer Authorization 或 Cookie td_session -> Principal，注入请求上下文。
// 未认证/令牌无效 -> 401（UNAUTHORIZED / TOKEN_INVALID）并中断。
func authMiddleware(authn identity.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := authn.AuthenticateToken(c.Request.Context(), tokenFromRequest(c))
		if err != nil {
			writeErr(c, err)
			c.Abort()
			return
		}
		// 注入 Principal（user+tenant+role）：下游 wallet 等模块据此 scopeByTenant。
		c.Request = c.Request.WithContext(appctx.WithPrincipal(c.Request.Context(), *p))
		c.Set(ctxPrincipalKey, p)
		c.Next()
	}
}

// tokenFromRequest 取令牌：优先 Authorization 头（authn 自剥 "Bearer "），回退 Cookie td_session。
func tokenFromRequest(c *gin.Context) string {
	if h := strings.TrimSpace(c.GetHeader("Authorization")); h != "" {
		return h
	}
	if ck, err := c.Cookie(sessionCookie); err == nil {
		return ck
	}
	return ""
}

// principalFrom 读取鉴权中间件注入的 Principal。
func principalFrom(c *gin.Context) (*appctx.Principal, bool) {
	v, ok := c.Get(ctxPrincipalKey)
	if !ok {
		return nil, false
	}
	p, ok := v.(*appctx.Principal)
	return p, ok
}

// currentTenantHandler 返回当前站点品牌：{slug, site_name, logo_url, theme_color, footer_text}。
// 未解析到租户 -> 404 + {"code":"TENANT_NOT_FOUND","message":...}。
func currentTenantHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, ok := tenantFromCtx(c)
		if !ok {
			writeErr(c, tenant.ErrTenantNotFound)
			return
		}
		var cfg siteConfigRow
		// 站点装修缺失不致命：回退用租户名占位。
		_ = db.WithContext(c.Request.Context()).Take(&cfg, "tenant_id = ?", t.ID).Error

		c.JSON(http.StatusOK, gin.H{
			"slug":        t.Slug,
			"site_name":   firstNonEmpty(cfg.SiteName, t.Name),
			"logo_url":    cfg.LogoURL,
			"theme_color": cfg.ThemeColor,
			"footer_text": cfg.FooterText,
		})
	}
}

// devLoginHandler 仅测试栈使用（生产删除）：按 Host 解析的租户 + body.username 查 seed 用户，
// 下发其 seed Token 为 httpOnly Cookie td_session，便于 E2E 自动登录。
func devLoginHandler(store *idstore.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, ok := tenantFromCtx(c)
		if !ok {
			writeErr(c, tenant.ErrTenantNotFound)
			return
		}
		var body struct {
			Username string `json:"username"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Username) == "" {
			writeErr(c, errUnauthorized())
			return
		}
		u, found, err := store.FindUserByUsername(c.Request.Context(), t.ID, strings.TrimSpace(body.Username))
		if err != nil {
			writeErr(c, err)
			return
		}
		if !found || u.SeedToken == "" {
			writeErr(c, errUnauthorized())
			return
		}
		// httpOnly + SameSite=Lax；secure=false 以兼容 localhost(http) 与 CF(https) 测试栈。
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(sessionCookie, u.SeedToken, int(7*24*time.Hour/time.Second), "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{
			"user":   gin.H{"id": u.ID, "username": u.Username, "role": u.Role},
			"tenant": gin.H{"slug": t.Slug, "site_name": t.Name},
		})
	}
}

// walletBalanceHandler 返回当前用户钱包余额：{balance_usd}（按 Principal 的 tenant+user scope）。
func walletBalanceHandler(repo wallet.WalletRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		bal, err := repo.Balance(c.Request.Context(), p.TenantID, p.UserID)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"balance_usd": bal})
	}
}

// walletRedeemHandler 兑换码入账，返回新余额。无效/已用 -> REDEEM_CODE_INVALID/REDEEM_CODE_USED。
func walletRedeemHandler(svc wallet.WalletService, repo wallet.WalletRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		code := ""
		if err := c.ShouldBindJSON(&body); err == nil {
			code = strings.TrimSpace(body.Code)
		}
		if code == "" {
			writeErr(c, wallet.ErrRedeemCodeInvalid)
			return
		}
		// Redeem 从 ctx Principal 解析 tenant（鉴权中间件已注入），按 CAS 状态机翻牌并入账。
		if err := svc.Redeem(c.Request.Context(), p.UserID, code); err != nil {
			writeErr(c, err)
			return
		}
		bal, err := repo.Balance(c.Request.Context(), p.TenantID, p.UserID)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"balance_usd": bal})
	}
}

// walletRechargeHandler 是充值下单 stub：仅生成订单号与占位支付参数，不入账、不接支付网关。
// Slice 3 接 payment 模块（下单 -> 二维码/跳转 -> 回调按 order_no 幂等入账）。
func walletRechargeHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		var body struct {
			Amount float64 `json:"amount"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || !(body.Amount > 0) {
			writeErr(c, wallet.ErrAmountInvalid)
			return
		}
		orderNo := fmt.Sprintf("rc_%d_%d_%d", p.TenantID, p.UserID, time.Now().UnixNano())
		c.JSON(http.StatusOK, gin.H{
			"order_no": orderNo,
			"pay": gin.H{
				"method": "stub",
				"status": "pending",
				"qr":     "",
				"note":   "Slice 3 接真实支付",
			},
		})
	}
}

// tenantFromCtx 读取租户中间件解析出的租户。
func tenantFromCtx(c *gin.Context) (*tenant.Tenant, bool) {
	v, ok := c.Get(ctxTenantKey)
	if !ok {
		return nil, false
	}
	t, ok := v.(*tenant.Tenant)
	return t, ok
}

// registerStatic 服务 webDist 下的静态资源，未命中文件回退 index.html（SPA），
// 但 /api、/v1 前缀的未匹配路由返回 JSON 404，避免把后端路径喂给前端路由。
func registerStatic(r *gin.Engine, webDist string) {
	indexPath := filepath.Join(webDist, "index.html")
	absDist, _ := filepath.Abs(webDist)

	r.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if strings.HasPrefix(reqPath, "/api/") || strings.HasPrefix(reqPath, "/v1/") {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "resource not found"})
			return
		}
		// 解析目标文件并做目录穿越防护（限制在 webDist 内）。
		target := filepath.Join(absDist, filepath.Clean("/"+strings.TrimPrefix(reqPath, "/")))
		if target == absDist || strings.HasPrefix(target, absDist+string(os.PathSeparator)) {
			if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
				c.File(target)
				return
			}
		}
		c.File(indexPath) // SPA fallback（index 不存在时由 http.ServeFile 自然 404）
	})
}

// errUnauthorized 构造统一的 401 未认证错误（Identity 命名空间）。
func errUnauthorized() error {
	return apperr.New(identity.CodeUnauthorized, "未登录或登录已失效", http.StatusUnauthorized)
}

// writeErr 按统一错误信封输出 {code, message}，HTTP 状态由 AppError 决定。
func writeErr(c *gin.Context, err error) {
	c.JSON(apperr.HTTPStatusOf(err), gin.H{
		"code":    apperr.CodeOf(err),
		"message": messageOf(err),
	})
}

// messageOf 提取 AppError 的人类可读描述；非 AppError 回退通用文案。
func messageOf(err error) string {
	var ae *apperr.AppError
	if errors.As(err, &ae) {
		return ae.Msg
	}
	return "internal error"
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// getenv 读环境变量，空则用默认值。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
