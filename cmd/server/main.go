// Command server 是「租户管道」集成 Demo 的薄 gin 服务：
//
//	请求 Host -> TenantResolver 解析租户 -> 读站点装修 -> 返回品牌信息
//
// 目标是用真实 GORM(MySQL) 打通 DB -> 端点 -> 前端页 的最小链路，证明分层装配可用。
// 计费/鉴权/渠道等其余模块顺延后续 Slice，本服务不引入。
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

	"newapi-mt/internal/platform/apperr"
	"newapi-mt/internal/tenant"
	"newapi-mt/internal/tenant/gormrepo"
)

const (
	// ctxTenantKey 是中间件解析出的租户在 gin.Context 中的存放键。
	ctxTenantKey = "tenant"
	// demoSlug 是演示租户的 slug（同时是其二级域名 label）。
	demoSlug = "demo"
	// demoSiteName 是演示站点名（site_config.site_name）。
	demoSiteName = "Demo 代理站"
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
	if err := gormrepo.AutoMigrate(db); err != nil {
		log.Fatalf("[server] migrate tenant tables: %v", err)
	}
	if err := db.AutoMigrate(&siteConfigRow{}); err != nil {
		log.Fatalf("[server] migrate site_configs: %v", err)
	}

	repo := gormrepo.New(db)
	svc := tenant.NewService(repo, tenant.NewSlugValidator())
	resolver := tenant.NewResolver(repo, tenant.NewMemCache())

	if err := seedDemo(context.Background(), db, svc, repo); err != nil {
		log.Fatalf("[server] seed demo tenant: %v", err)
	}

	r := buildRouter(resolver, db, webDist)
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

// seedDemo 幂等地建立演示租户：slug=demo、域名 demo.wedreamhub.com（由 service 自动写）
// 与 localhost（本地调试），并写入演示站点装修。已存在则仅补齐缺失部分，不覆盖人工改动。
func seedDemo(ctx context.Context, db *gorm.DB, svc tenant.TenantService, repo tenant.TenantRepo) error {
	existing, err := repo.GetTenantBySlug(ctx, demoSlug)
	switch {
	case err == nil:
		return ensureLocalhost(ctx, repo, existing.ID, db)
	case apperr.Is(err, "TENANT_NOT_FOUND"):
		// 落到下面创建分支
	default:
		return fmt.Errorf("lookup demo tenant: %w", err)
	}

	t, err := svc.Create(ctx, tenant.CreateTenantInput{Slug: demoSlug, Name: demoSiteName})
	if err != nil {
		return fmt.Errorf("create demo tenant: %w", err)
	}
	return ensureLocalhost(ctx, repo, t.ID, db)
}

// ensureLocalhost 为指定租户补写 localhost 域名（已存在则跳过）并 upsert 演示站点装修。
func ensureLocalhost(ctx context.Context, repo tenant.TenantRepo, tenantID int64, db *gorm.DB) error {
	d := &tenant.TenantDomain{TenantID: tenantID, Domain: "localhost"}
	if err := repo.CreateDomain(ctx, d); err != nil && !apperr.Is(err, "SLUG_DUPLICATE") {
		return fmt.Errorf("seed localhost domain: %w", err)
	}
	cfg := siteConfigRow{
		TenantID:   tenantID,
		SiteName:   demoSiteName,
		LogoURL:    "",
		ThemeColor: "#2F54EB",
		FooterText: "© 2026 Demo 代理站 · Powered by newapi628",
	}
	// OnConflict DoNothing：幂等写入，不覆盖后续人工修改。
	if err := db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&cfg).Error; err != nil {
		return fmt.Errorf("seed site config: %w", err)
	}
	return nil
}

// buildRouter 装配路由：健康检查、租户中间件 + /api、静态资源（SPA fallback）。
func buildRouter(resolver tenant.TenantResolver, db *gorm.DB, webDist string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	api.Use(tenantMiddleware(resolver))
	api.GET("/tenant/current", currentTenantHandler(db))

	registerStatic(r, webDist)
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

// currentTenantHandler 返回当前站点品牌：{slug, site_name, logo_url, theme_color, footer_text}。
// 未解析到租户 -> 404 + {"code":"TENANT_NOT_FOUND","message":...}。
func currentTenantHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get(ctxTenantKey)
		if !ok {
			writeErr(c, tenant.ErrTenantNotFound)
			return
		}
		t := v.(*tenant.Tenant)

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
