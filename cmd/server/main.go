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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"newapi-mt/internal/billing"
	billingrepo "newapi-mt/internal/billing/gormrepo"
	"newapi-mt/internal/identity"
	idstore "newapi-mt/internal/identity/gormstore"
	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
	"newapi-mt/internal/platform/quota"
	"newapi-mt/internal/pricing"
	"newapi-mt/internal/relay"
	"newapi-mt/internal/relay/upstream"
	"newapi-mt/internal/tenant"
	"newapi-mt/internal/tenant/gormrepo"
	"newapi-mt/internal/tokenplan"
	tprepo "newapi-mt/internal/tokenplan/gormrepo"
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

// --- Slice 3 tokenplan 最小适配桩（SubscriptionService 的依赖；Slice 4 接真实模块）---

// tokenplanPayment 是 tokenplan.PaymentGateway 的测试栈桩：下单即生成订单号，
// 并由 purchase handler 立即 ActivateFromPayment（同步视为已支付）。
// Slice 4 接真实 payment 模块（二维码/跳转 + 异步回调按 order_no 幂等激活）。
type tokenplanPayment struct{}

func (tokenplanPayment) CreateOrder(_ context.Context, in tokenplan.OrderInput) (*tokenplan.PayOrder, error) {
	orderNo := fmt.Sprintf("sub_%d_%d_%d", in.TenantID, in.UserID, time.Now().UnixNano())
	return &tokenplan.PayOrder{OrderID: orderNo, PayURL: "stub://pay/" + orderNo}, nil
}

// tokenplanRisk 是 tokenplan.RiskEngine 的简化实现：仅 Trial 限购，按 **user 维度**查
// user_subscriptions——已有 active/exhausted/expired 的 trial 订阅 ≥1 即 PURCHASE_LIMIT_EXCEEDED。
// 实名维 ∪ 设备维限购 + Redis 频控 / 异常调用风控顺延 Slice 4（接 risk 模块）。
type tokenplanRisk struct{ db *gorm.DB }

func (r tokenplanRisk) CheckPurchaseLimit(ctx context.Context, in tokenplan.PurchaseLimitCheck) error {
	if in.PlanCode != "trial" {
		return nil // 一期仅对 Trial 限购，其余档不限
	}
	var count int64
	err := r.db.WithContext(ctx).
		Table("user_subscriptions AS us").
		Joins("JOIN token_plans AS p ON p.id = us.plan_id").
		Where("us.user_id = ? AND p.code = ? AND us.status IN ?",
			in.UserID, "trial", []string{
				string(tokenplan.SubActive), string(tokenplan.SubExhausted), string(tokenplan.SubExpired),
			}).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count >= 1 {
		return tokenplan.ErrPurchaseLimitExceeded
	}
	return nil
}

// tokenplanEarnings 是 tokenplan.EarningSink 的 noop 实现：丢弃 tokenplan_spread 收益。
// Slice 4 接真实 agent 模块（写 agent_earning_logs + 增代理可提现余额）。
type tokenplanEarnings struct{}

func (tokenplanEarnings) AddEarning(_ context.Context, _ tokenplan.EarningEntry) error { return nil }

// --- Slice 4 中继计费适配桩（billing 双桶 / 模型价 / 收益 / 租户活跃）---

// walletSourceAdapter 把 wallet.WalletQuotaFactory（For(Principal)）适配为
// billing.WalletSourceFactory（WalletSource(ctx,userID,tenantID)），供 QuotaRouter 选钱包桶。
type walletSourceAdapter struct{ f wallet.WalletQuotaFactory }

func (a walletSourceAdapter) WalletSource(_ context.Context, userID, tenantID int64) (quota.Source, error) {
	return a.f.For(appctx.Principal{UserID: userID, TenantID: tenantID}), nil
}

// staticModelCatalog 是 billing.ModelCatalog 的**占位**实现：对所有模型返回同一组默认单价
// （USD per token），单价由 env 可调（MODEL_PRICE_IN_PER_1M / MODEL_PRICE_OUT_PER_1M）。
// TODO(模型价)：真实实现应复用 New API 模型价表，按 model 精确定价（含分组倍率），见交付报告风险。
type staticModelCatalog struct {
	inPerToken  float64 // 输入单价（USD/token）
	outPerToken float64 // 输出单价（USD/token）
}

func (c staticModelCatalog) Price(_ context.Context, _ string) (float64, float64, error) {
	return c.inPerToken, c.outPerToken, nil
}

// noopBillingEarnings 是 billing.EarningSink 的 noop 实现：丢弃 consume_commission 分润。
// Slice 后续接真实 agent 模块（按代理分润比例入账可提现余额）。
type noopBillingEarnings struct{}

func (noopBillingEarnings) AddEarning(_ context.Context, _ billing.EarningEntry) error { return nil }

// tenantStatusChecker 把 tenant.TenantRepo 适配为 identity.TenantStatusChecker，
// 供 AccessGuard.RequireTenantActive 在 /v1 入口拦截冻结/删除租户。
type tenantStatusChecker struct{ repo tenant.TenantRepo }

func (c tenantStatusChecker) StatusOf(ctx context.Context, tenantID int64) (identity.TenantStatus, bool, error) {
	t, err := c.repo.GetTenant(ctx, tenantID)
	if err != nil {
		if apperr.Is(err, "TENANT_NOT_FOUND") {
			return "", false, nil // 租户不存在视为非 active
		}
		return "", false, err // 基础设施错误原样上浮
	}
	return identity.TenantStatus(t.Status), true, nil
}

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
	if err := tprepo.AutoMigrate(db); err != nil {
		log.Fatalf("[server] migrate tokenplan tables: %v", err)
	}
	if err := billingrepo.AutoMigrate(db); err != nil {
		log.Fatalf("[server] migrate billing tables: %v", err)
	}

	repo := gormrepo.New(db)
	svc := tenant.NewService(repo, tenant.NewSlugValidator())
	resolver := tenant.NewResolver(repo, tenant.NewMemCache())

	store := idstore.New(db)
	authn := identity.NewAuthenticator(store)
	// RequireAdmin / RequireTenantOwner 仅看 Principal；RequireTenantActive（/v1 入口用）需
	// TenantStatusChecker，故注入 tenant repo 适配器（冻结/删除租户在中继入口被拦截）。
	guard := identity.NewAccessGuard(tenantStatusChecker{repo: repo})

	wRepo := walletrepo.New(db)
	wSvc := wallet.NewService(wRepo, fixedRatioPricing{}, noopEarningSink{})

	// tokenplan 装配：catalog/retail/subscription；retail 用真实 pricing.NewGuard()，
	// payment/risk/earnings 为本切桩（见上）；clock=nil 回退真实时钟。
	tpRepo := tprepo.New(db)
	catalog := tokenplan.NewCatalog(tpRepo)
	retailSvc := tokenplan.NewRetailService(tpRepo, pricing.NewGuard())
	subSvc := tokenplan.NewSubscriptionService(tpRepo, tpRepo, tokenplanPayment{}, tokenplanRisk{db: db}, tokenplanEarnings{}, nil)

	// billing 双桶计费装配（doc/api-contract §2.9 / §4）：
	//   QuotaRouter：SubscriptionChecker=tokenplan(HasActive)，钱包桶=wallet，套餐桶=tokenplan；
	//   BillingService：算上游成本→选桶→原子扣减→写计费日志→消耗分润（noop）。
	billingLogs := billingrepo.New(db)
	walletQF := wallet.NewQuotaFactory(wRepo)
	subQF := tokenplan.NewQuotaFactory(tpRepo, nil)
	quotaRouter := billing.NewQuotaRouter(subSvc, walletSourceAdapter{f: walletQF}, subQF)
	modelCatalog := staticModelCatalog{
		// 默认占位价：in $1.25/1M、out $10/1M（env 可调，注释见 staticModelCatalog）。
		inPerToken:  parsePricePer1M("MODEL_PRICE_IN_PER_1M", 1.25),
		outPerToken: parsePricePer1M("MODEL_PRICE_OUT_PER_1M", 10),
	}
	billingSvc := billing.NewService(modelCatalog, quotaRouter, billingLogs, noopBillingEarnings{})

	// 上游渠道池（OpenAI 兼容）：凭据从 env 注入，禁止入库；缺省时 /v1 转发返回 UPSTREAM_ERROR。
	upstreamBase := os.Getenv("UPSTREAM_BASE_URL")
	upstreamPool := upstream.New(upstreamBase, os.Getenv("UPSTREAM_API_KEY"))
	if upstreamBase == "" {
		log.Printf("[server] WARN UPSTREAM_BASE_URL unset; /v1 forwarding will fail until configured")
	}

	if err := seedAll(context.Background(), db, svc, repo, store, wRepo, tpRepo); err != nil {
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
		tpRepo:   tpRepo,
		catalog:  catalog,
		retail:   retailSvc,
		subSvc:   subSvc,
		guard:    guard,
		// Slice 4 中继计费
		billingSvc:   billingSvc,
		billingLogs:  billingLogs,
		upstream:     upstreamPool,
		modelCatalog: modelCatalog,
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

// seededPlan 是 seed 后回填了主键的套餐摘要（供建租户上架记录 + 演示订阅快照）。
type seededPlan struct {
	id            int64
	code          string
	basePrice     float64
	monthLimitUSD float64
}

// seedAll 幂等地建立全部演示数据：6 档主站套餐 + 每租户（用户/钱包/兑换码 + admin/agent 角色用户 +
// 6 档上架记录）。已存在则仅补缺，不覆盖人工改动。
func seedAll(ctx context.Context, db *gorm.DB, svc tenant.TenantService, repo tenant.TenantRepo, store *idstore.Store, wRepo *walletrepo.Repo, tpRepo *tprepo.Repo) error {
	plans, err := seedPlans(ctx, tpRepo)
	if err != nil {
		return err
	}
	for _, spec := range tenantSeeds() {
		tid, err := ensureTenant(ctx, svc, repo, db, spec)
		if err != nil {
			return err
		}
		uid, err := seedIdentityWallet(ctx, store, wRepo, tid, spec)
		if err != nil {
			return err
		}
		if err := seedTenantRoles(ctx, store, tid, spec.slug); err != nil {
			return err
		}
		if err := seedTenantListings(ctx, tpRepo, tid, spec.slug, plans); err != nil {
			return err
		}
		// 演示主用户（demo@<slug>）幂等激活一份 mini 套餐 → /v1 走「套餐桶」（独立计量）。
		if mini, ok := findSeededPlan(plans, "mini"); ok {
			if err := seedSubscription(ctx, tpRepo, tid, uid, mini, spec.slug); err != nil {
				return err
			}
		}
		// payg 用户（payg@<slug>，无套餐，钱包余额）→ /v1 走「钱包桶」，演示双桶分流。
		if err := seedPaygUser(ctx, store, wRepo, tid, spec.slug); err != nil {
			return err
		}
	}
	return nil
}

// seedPlans 幂等地写入 proposal §8.2 的 6 档主站套餐（按 code 去重），返回回填主键的摘要。
func seedPlans(ctx context.Context, tpRepo *tprepo.Repo) ([]seededPlan, error) {
	out := make([]seededPlan, 0, 6)
	for _, p := range tokenplan.SeedPlans() {
		pp := p
		id, err := tpRepo.EnsurePlan(ctx, &pp)
		if err != nil {
			return nil, fmt.Errorf("seed plan %s: %w", p.Code, err)
		}
		out = append(out, seededPlan{id: id, code: p.Code, basePrice: p.BasePrice, monthLimitUSD: p.MonthLimitUSD})
		log.Printf("[seed] plan code=%s id=%d base=%.2f¥ month_limit=%.0fUSD", p.Code, id, p.BasePrice, p.MonthLimitUSD)
	}
	return out, nil
}

// seedTenantListings 幂等地为某租户把 6 档套餐全部上架（enabled，零售价=主站基准价）。
func seedTenantListings(ctx context.Context, tpRepo *tprepo.Repo, tid int64, slug string, plans []seededPlan) error {
	for _, p := range plans {
		if err := tpRepo.EnsureListing(ctx, tid, p.id, true, p.basePrice); err != nil {
			return fmt.Errorf("seed listing tenant=%s plan=%s: %w", slug, p.code, err)
		}
	}
	log.Printf("[seed] tenant=%s(id=%d) listed %d token-plans (retail=base)", slug, tid, len(plans))
	return nil
}

// seedTenantRoles 幂等地为某租户建 admin / agent 角色用户（各带 seed Token，dev-login 可登），便于 E2E 切角色。
func seedTenantRoles(ctx context.Context, store *idstore.Store, tid int64, slug string) error {
	roles := []struct {
		username string
		token    string
		role     appctx.Role
	}{
		{"admin@" + slug, "sk-td-admin-" + slug, appctx.RoleAdmin},
		{"agent@" + slug, "sk-td-agent-" + slug, appctx.RoleAgentOwner},
	}
	for _, rr := range roles {
		u, err := store.EnsureUser(ctx, &idstore.User{
			TenantID:  tid,
			Username:  rr.username,
			Role:      rr.role,
			SeedToken: rr.token,
		})
		if err != nil {
			return fmt.Errorf("seed role user %s: %w", rr.username, err)
		}
		if err := store.EnsureToken(ctx, tid, u.ID, "default", rr.token); err != nil {
			return fmt.Errorf("seed role token %s: %w", rr.username, err)
		}
		log.Printf("[seed] tenant=%s(id=%d) user=%s(id=%d, role=%s) token=%q", slug, tid, rr.username, u.ID, rr.role, rr.token)
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
// 返回演示主用户的 ID（供后续 seed 其演示订阅）。
func seedIdentityWallet(ctx context.Context, store *idstore.Store, wRepo *walletrepo.Repo, tid int64, spec tenantSeed) (int64, error) {
	u, err := store.EnsureUser(ctx, &idstore.User{
		TenantID:  tid,
		Username:  spec.username,
		Role:      appctx.RoleUser,
		SeedToken: spec.rawToken,
	})
	if err != nil {
		return 0, fmt.Errorf("seed user %s: %w", spec.username, err)
	}
	if err := store.EnsureToken(ctx, tid, u.ID, "default", spec.rawToken); err != nil {
		return 0, fmt.Errorf("seed token for %s: %w", spec.username, err)
	}
	if err := wRepo.EnsureBalance(ctx, tid, u.ID, spec.balanceUSD); err != nil {
		return 0, fmt.Errorf("seed balance for %s: %w", spec.username, err)
	}
	if err := wRepo.EnsureRedemption(ctx, &wallet.RedemptionCode{
		TenantID:  tid,
		Code:      spec.redeemCode,
		AmountUSD: spec.redeemUSD,
		Status:    wallet.RedemptionEnabled,
	}); err != nil {
		return 0, fmt.Errorf("seed redemption for %s: %w", spec.username, err)
	}
	log.Printf("[seed] tenant=%s(id=%d) user=%s(id=%d) token=%q balance=%.2fUSD redeem=%s(%.0fUSD)",
		spec.slug, tid, spec.username, u.ID, spec.rawToken, spec.balanceUSD, spec.redeemCode, spec.redeemUSD)
	return u.ID, nil
}

// paygBalanceUSD 是 payg 演示用户的初始钱包余额（USD）；用于钱包桶后付费计费演示。
const paygBalanceUSD = 50

// seedPaygUser 幂等地为租户建 payg 用户（payg@<slug>，role user）+ API Token + 钱包余额（无套餐）。
// 该用户无 active 套餐，故 /v1 调用经 QuotaRouter 选「钱包桶」，与主用户的「套餐桶」形成双桶演示。
func seedPaygUser(ctx context.Context, store *idstore.Store, wRepo *walletrepo.Repo, tid int64, slug string) error {
	username := "payg@" + slug
	rawToken := "sk-td-payg-" + slug
	u, err := store.EnsureUser(ctx, &idstore.User{
		TenantID:  tid,
		Username:  username,
		Role:      appctx.RoleUser,
		SeedToken: rawToken,
	})
	if err != nil {
		return fmt.Errorf("seed payg user %s: %w", username, err)
	}
	if err := store.EnsureToken(ctx, tid, u.ID, "default", rawToken); err != nil {
		return fmt.Errorf("seed payg token %s: %w", username, err)
	}
	if err := wRepo.EnsureBalance(ctx, tid, u.ID, paygBalanceUSD); err != nil {
		return fmt.Errorf("seed payg balance %s: %w", username, err)
	}
	log.Printf("[seed] tenant=%s(id=%d) user=%s(id=%d, role=user) token=%q balance=%.2fUSD (wallet-bucket, no plan)",
		slug, tid, username, u.ID, rawToken, float64(paygBalanceUSD))
	return nil
}

// seedSubscription 幂等地为某用户激活一份套餐订阅（固定 order_no → INSERT IGNORE，重启不重复建）。
// 走 SubscriptionRepo.ActivateFromOrder（仅插订阅行，不读待支付订单），有效期 30 天。
func seedSubscription(ctx context.Context, tpRepo *tprepo.Repo, tid, userID int64, plan seededPlan, slug string) error {
	if userID <= 0 {
		return nil // 主用户回读失败时跳过（不致命）
	}
	now := time.Now()
	sub := &tokenplan.Subscription{
		TenantID:       tid,
		UserID:         userID,
		PlanID:         plan.id,
		PurchasedPrice: plan.basePrice,
		MonthLimitUSD:  plan.monthLimitUSD,
		UsedUSD:        0,
		Status:         tokenplan.SubActive,
		StartAt:        now,
		ExpireAt:       now.AddDate(0, 0, 30),
		SourceOrderID:  fmt.Sprintf("seed_sub_%d_%s", tid, plan.code),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := tpRepo.ActivateFromOrder(ctx, sub)
	if err != nil {
		return fmt.Errorf("seed subscription tenant=%s user=%d plan=%s: %w", slug, userID, plan.code, err)
	}
	log.Printf("[seed] tenant=%s(id=%d) user=%d subscription plan=%s month_limit=%.0fUSD created=%v (subscription-bucket)",
		slug, tid, userID, plan.code, plan.monthLimitUSD, created)
	return nil
}

// findSeededPlan 在 seed 套餐摘要中按 code 查找。
func findSeededPlan(plans []seededPlan, code string) (seededPlan, bool) {
	for _, p := range plans {
		if p.code == code {
			return p, true
		}
	}
	return seededPlan{}, false
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
	// Slice 3 tokenplan
	tpRepo  *tprepo.Repo
	catalog tokenplan.PlanCatalog
	retail  tokenplan.PlanRetailService
	subSvc  tokenplan.SubscriptionService
	guard   identity.AccessGuard
	// Slice 4 中继计费
	billingSvc   billing.BillingService
	billingLogs  *billingrepo.Repo
	upstream     relay.UpstreamPool
	modelCatalog billing.ModelCatalog
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
	authed.GET("/tenant/billing-logs", billingLogsHandler(d.billingLogs)) // 🅤 我的计费流水（近 N 条）

	// tokenplan 套餐（api-contract §2.4）。角色守卫在 handler 内（admin/agent_owner）。
	authed.GET("/tenant/token-plans", tenantTokenPlansHandler(d.retail))                  // 🅤 本租户上架套餐
	authed.POST("/tenant/token-plans/:id/purchase", purchaseHandler(d.subSvc, d.catalog)) // 🅤 购买+激活
	authed.GET("/tenant/subscriptions", subscriptionsHandler(d.tpRepo, d.catalog))        // 🅤 我的套餐
	authed.GET("/tenant/token-plans/manage", agentListPlansHandler(d.retail, d.guard))    // 🅖 代理上架视图
	authed.PATCH("/tenant/token-plans/manage", agentSetListingHandler(d.retail, d.guard)) // 🅖 上架/改价
	authed.GET("/admin/token-plans", adminListPlansHandler(d.catalog, d.guard))           // 🅐 套餐列表
	authed.GET("/admin/token-plans/:id", adminGetPlanHandler(d.catalog, d.guard))         // 🅐 单套餐
	authed.POST("/admin/token-plans", adminCreatePlanHandler(d.catalog, d.guard))         // 🅐 新建
	authed.PATCH("/admin/token-plans/:id", adminUpdatePlanHandler(d.catalog, d.guard))    // 🅐 改价/改档

	// /v1 模型调用入口（OpenAI 兼容，api-contract §2.9）：Bearer=用户租户 API Token。
	// 不挂 tenantMiddleware——租户/用户由 Token 反查得出（Principal 携带 TenantID）。
	v1 := r.Group("/v1")
	v1.Use(authMiddleware(d.authn))
	v1.POST("/chat/completions", chatCompletionsHandler(d.guard, d.upstream, d.billingSvc, d.billingLogs, d.modelCatalog))

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
		// api_token 明文回传（**仅测试栈，生产删除**）：供前端 playground 作 /v1 Bearer 调用。
		c.JSON(http.StatusOK, gin.H{
			"user":      gin.H{"id": u.ID, "username": u.Username, "role": u.Role},
			"tenant":    gin.H{"slug": t.Slug, "site_name": t.Name},
			"api_token": u.SeedToken,
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

// ---- Slice 4 中继计费 handlers（api-contract §2.9 /v1 + 计费流水）----

// chatCompletionsHandler 是 POST /v1/chat/completions（OpenAI 兼容）入口。
// 鉴权由 authMiddleware 完成（Bearer=用户租户 API Token → Principal）。
//
// 流程（后付费，api-contract §2.9 / §4；桶路由下沉 billing.Charge 内部）：
//
//	租户活跃校验 → 转发上游 → 仅 2xx 才计费（usage×模型价 → 选桶 → 原子扣减 → 写日志 → 分润）。
//
// 双桶（独立计量不回退）：有 active 套餐→套餐桶 Meter；否则钱包桶。
// 后付费豁免：扣费失败（QUOTA_INSUFFICIENT / SUBSCRIPTION_EXHAUSTED / SUBSCRIPTION_EXPIRED）
// 时已转发上游、产生真实成本，故仅落一条 unpaid 流水留痕，**仍原样回送上游响应**。
// TODO(预扣)：高额请求应在转发前按预算预扣，扣不动直接拒绝，防止欠费滥用（见交付报告风险）。
// 若改判为预检语义，则把扣费失败改为返回相应错误码、不回送响应。
func chatCompletionsHandler(guard identity.AccessGuard, pool relay.UpstreamPool, billingSvc billing.BillingService, logs *billingrepo.Repo, modelCatalog billing.ModelCatalog) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		// 租户活跃校验（冻结/删除租户在此拦截 → TENANT_INACTIVE）。
		if err := guard.RequireTenantActive(c.Request.Context(), p.TenantID); err != nil {
			writeErr(c, err)
			return
		}
		// 读原始请求体（原样透传上游；编排层不改写）。
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeErr(c, apperr.New(relay.CodeUpstreamError, "读取请求体失败", http.StatusBadRequest))
			return
		}
		model := extractModel(body)
		if model == "" {
			writeErr(c, apperr.New("MODEL_REQUIRED", "缺少 model 字段", http.StatusBadRequest))
			return
		}
		reqID := requestID(c)

		// 转发上游。传输层失败 → UPSTREAM_ERROR；有响应（含 4xx/5xx）则在 resp 携带。
		resp, usage, ferr := pool.Forward(c.Request.Context(), relay.RelayRequest{
			Model:     model,
			Endpoint:  "/v1/chat/completions",
			Body:      body,
			RequestID: reqID,
			ClientIP:  c.ClientIP(),
		})
		if ferr != nil {
			writeErr(c, apperr.New(relay.CodeUpstreamError, "上游转发失败", http.StatusBadGateway).Wrap(ferr))
			return
		}
		// 上游非 2xx：原样回送错误响应，不计费、不记日志（未产生有效用量）。
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeUpstream(c, resp)
			return
		}

		// 计费（选桶/扣减/日志/分润下沉 billing.Charge 内部）。
		result, cerr := billingSvc.Charge(c.Request.Context(), billing.ChargeRequest{
			RequestID:        reqID,
			UserID:           p.UserID,
			TenantID:         p.TenantID,
			Model:            model,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
		})
		if cerr != nil {
			if isBucketError(cerr) {
				// 后付费豁免：留痕 unpaid 流水，仍回送上游响应。
				_ = logs.WriteUnpaid(c.Request.Context(), billing.CallLogEntry{
					RequestID:        reqID,
					TenantID:         p.TenantID,
					UserID:           p.UserID,
					Model:            model,
					PromptTokens:     usage.PromptTokens,
					CompletionTokens: usage.CompletionTokens,
					BucketKind:       bucketKindForError(cerr),
					UpstreamCostUSD:  upstreamCostUSD(c.Request.Context(), modelCatalog, model, usage),
				})
				c.Header("X-TD-Billing", "unpaid:"+apperr.CodeOf(cerr))
				writeUpstream(c, resp)
				return
			}
			// 非桶错误（模型未定价 / 基础设施）：真实失败，返回错误。
			writeErr(c, cerr)
			return
		}

		// 计费成功：回写计费元信息头（不污染 OpenAI 响应体）+ 原样回送上游响应。
		c.Header("X-TD-Bucket", string(result.BucketKind))
		c.Header("X-TD-Charged-USD", formatUSD(result.ChargedUSD))
		c.Header("X-TD-Remaining-USD", formatUSD(result.RemainingUSD))
		writeUpstream(c, resp)
	}
}

// billingLogsHandler 返回当前用户在本租户的近 N 条计费流水（鉴权，本人本租户 scope）。
// 查询参数 limit（默认 50，上限 200）。
func billingLogsHandler(logs *billingrepo.Repo) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		limit := 50
		if v := c.Query("limit"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n > 0 {
				if n > 200 {
					n = 200
				}
				limit = n
			}
		}
		views, err := logs.ListByUser(c.Request.Context(), p.TenantID, p.UserID, limit)
		if err != nil {
			writeErr(c, err)
			return
		}
		out := make([]gin.H, 0, len(views))
		for _, v := range views {
			out = append(out, gin.H{
				"id":                v.ID,
				"request_id":        v.RequestID,
				"model":             v.Model,
				"prompt_tokens":     v.PromptTokens,
				"completion_tokens": v.CompletionTokens,
				"bucket":            v.BucketKind,
				"upstream_cost_usd": v.UpstreamCostUSD,
				"charged_usd":       v.ChargedUSD,
				"gross_profit_usd":  v.GrossProfitUSD,
				"status":            v.Status,
				"created_at":        v.CreatedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

// ---- /v1 handler 辅助 ----

// extractModel 仅解析请求体的 model 字段（其余字段原样透传上游，不在此解析）。
func extractModel(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &m)
	return strings.TrimSpace(m.Model)
}

// requestID 取调用链 ID：优先 X-Request-Id 头，缺省按时间生成（落计费日志/幂等键）。
func requestID(c *gin.Context) string {
	if id := strings.TrimSpace(c.GetHeader("X-Request-Id")); id != "" {
		return id
	}
	return fmt.Sprintf("v1_%d", time.Now().UnixNano())
}

// writeUpstream 原样回送上游响应（状态码 + Content-Type + 响应体）。
func writeUpstream(c *gin.Context, resp relay.RelayResponse) {
	ct := resp.Headers["Content-Type"]
	if ct == "" {
		ct = "application/json"
	}
	c.Data(resp.StatusCode, ct, resp.Body)
}

// isBucketError 判定是否为「桶不足/超额/过期」类后付费可豁免错误。
func isBucketError(err error) bool {
	switch apperr.CodeOf(err) {
	case billing.CodeQuotaInsufficient, billing.CodeSubscriptionExhausted, billing.CodeSubscriptionExpired:
		return true
	default:
		return false
	}
}

// bucketKindForError 由桶错误码反推扣费桶来源（供 unpaid 流水标注实际拒绝的桶）。
func bucketKindForError(err error) quota.Kind {
	switch apperr.CodeOf(err) {
	case billing.CodeSubscriptionExhausted, billing.CodeSubscriptionExpired:
		return quota.KindSubscription
	case billing.CodeQuotaInsufficient:
		return quota.KindWallet
	default:
		return ""
	}
}

// upstreamCostUSD 按模型价×用量算上游成本（USD），供 unpaid 流水留痕；未定价返回 0。
func upstreamCostUSD(ctx context.Context, catalog billing.ModelCatalog, model string, usage relay.Usage) float64 {
	in, out, err := catalog.Price(ctx, model)
	if err != nil {
		return 0
	}
	return float64(usage.PromptTokens)*in + float64(usage.CompletionTokens)*out
}

// formatUSD 以定点 8 位小数序列化金额（与 decimal(20,8) 计量口径一致）。
func formatUSD(v float64) string {
	return strconv.FormatFloat(v, 'f', 8, 64)
}

// parsePricePer1M 读取「每百万 token 美元价」env 并转为单 token 单价（USD/token）；非法/缺省回退 def。
func parsePricePer1M(key string, def float64) float64 {
	per1M := def
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			per1M = f
		}
	}
	return per1M / 1_000_000
}

// ---- tokenplan handlers（api-contract §2.4）----

// tenantTokenPlansHandler 返回本租户「已上架且启用」的套餐列表（零售价 + 营销字段，见 §3 Plan）。
func tenantTokenPlansHandler(retail tokenplan.PlanRetailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		views, err := retail.ListForTenant(c.Request.Context(), p.TenantID)
		if err != nil {
			writeErr(c, err)
			return
		}
		out := make([]gin.H, 0, len(views))
		for _, v := range views {
			if !(v.Listed && v.Enabled) {
				continue // 仅向用户展示本租户已上架启用的套餐
			}
			out = append(out, planCardView(v.Plan, v.RetailPrice))
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

// purchaseHandler 购买套餐：限购校验 → stub 支付 → 立即激活 30 天订阅 → 返回订阅。
// 失败码：PLAN_NOT_FOUND / PLAN_DISABLED / PLAN_NOT_LISTED / PURCHASE_LIMIT_EXCEEDED。
func purchaseHandler(subSvc tokenplan.SubscriptionService, catalog tokenplan.PlanCatalog) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || planID <= 0 {
			writeErr(c, tokenplan.ErrPlanNotFound)
			return
		}
		// 设备/实名维度本切未启用（Slice 4 接 risk+Redis）；如前端透传则带上供未来风控。
		var body struct {
			DeviceID   string `json:"device_id"`
			RealNameID string `json:"real_name_id"`
		}
		_ = c.ShouldBindJSON(&body)

		ticket, err := subSvc.Purchase(c.Request.Context(), tokenplan.PurchaseInput{
			TenantID:   p.TenantID,
			UserID:     p.UserID,
			PlanID:     planID,
			DeviceID:   body.DeviceID,
			RealNameID: body.RealNameID,
		})
		if err != nil {
			writeErr(c, err)
			return
		}
		// stub 支付：下单即视为已支付，立即按 order_no 幂等激活订阅。
		sub, err := subSvc.ActivateFromPayment(c.Request.Context(), ticket.OrderID)
		if err != nil {
			writeErr(c, err)
			return
		}
		planCode, validDays := "", 0
		if pl, e := catalog.Get(c.Request.Context(), planID); e == nil {
			planCode, validDays = pl.Code, pl.ValidDays
		}
		c.JSON(http.StatusOK, gin.H{
			"order_no":        ticket.OrderID,
			"plan_code":       planCode,
			"month_limit_usd": sub.MonthLimitUSD,
			"valid_days":      validDays,
			"pay":             gin.H{"method": "stub", "status": "paid", "qr": ""},
			"subscription":    subscriptionView(sub, planCode),
		})
	}
}

// subscriptionsHandler 返回当前用户在本租户的全部套餐（含历史 exhausted/expired；惰性过期已落库）。
func subscriptionsHandler(tpRepo *tprepo.Repo, catalog tokenplan.PlanCatalog) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := principalFrom(c)
		if !ok {
			writeErr(c, errUnauthorized())
			return
		}
		subs, err := tpRepo.ListSubscriptionsByUser(c.Request.Context(), p.TenantID, p.UserID, time.Now())
		if err != nil {
			writeErr(c, err)
			return
		}
		codeByID, err := planCodeMap(c.Request.Context(), catalog)
		if err != nil {
			writeErr(c, err)
			return
		}
		out := make([]gin.H, 0, len(subs))
		for i := range subs {
			out = append(out, subscriptionView(&subs[i], codeByID[subs[i].PlanID]))
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

// agentListPlansHandler（🅖）返回代理可上架套餐与当前定价（含保护线/进货价，供改价参考）。
func agentListPlansHandler(retail tokenplan.PlanRetailService, guard identity.AccessGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := requireTenantOwner(c, guard)
		if !ok {
			return
		}
		views, err := retail.ListForTenant(c.Request.Context(), p.TenantID)
		if err != nil {
			writeErr(c, err)
			return
		}
		out := make([]gin.H, 0, len(views))
		for _, v := range views {
			out = append(out, gin.H{
				"id":                   v.Plan.ID,
				"code":                 v.Plan.Code,
				"name":                 v.Plan.Name,
				"base_price_cny":       v.Plan.BasePrice,
				"anchor_price_cny":     v.Plan.AnchorPrice,
				"min_price_cny":        v.Plan.MinPrice,
				"agent_cost_price_cny": v.Plan.AgentCostPrice,
				"month_limit_usd":      v.Plan.MonthLimitUSD,
				"valid_days":           v.Plan.ValidDays,
				"sort":                 v.Plan.Sort,
				"listed":               v.Listed,
				"enabled":              v.Enabled,
				"retail_price_cny":     v.RetailPrice,
			})
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

// agentSetListingHandler（🅖）上架/退出 + 改价；retail<min_price → RETAIL_BELOW_MIN。
func agentSetListingHandler(retail tokenplan.PlanRetailService, guard identity.AccessGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := requireTenantOwner(c, guard)
		if !ok {
			return
		}
		var body struct {
			PlanID      int64   `json:"plan_id"`
			Enabled     bool    `json:"enabled"`
			RetailPrice float64 `json:"retail_price"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.PlanID <= 0 {
			writeErr(c, tokenplan.ErrPlanNotFound)
			return
		}
		if err := retail.SetListing(c.Request.Context(), p.TenantID, body.PlanID, body.Enabled, body.RetailPrice); err != nil {
			writeErr(c, err) // RETAIL_BELOW_MIN / PLAN_DISABLED / PLAN_NOT_FOUND
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"plan_id":          body.PlanID,
			"enabled":          body.Enabled,
			"retail_price_cny": body.RetailPrice,
		})
	}
}

// adminListPlansHandler（🅐）返回全部套餐（全字段，含成本价/保护线）。
func adminListPlansHandler(catalog tokenplan.PlanCatalog, guard identity.AccessGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := requireAdmin(c, guard); !ok {
			return
		}
		plans, err := catalog.List(c.Request.Context())
		if err != nil {
			writeErr(c, err)
			return
		}
		out := make([]gin.H, 0, len(plans))
		for i := range plans {
			out = append(out, adminPlanView(plans[i]))
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

// adminGetPlanHandler（🅐）读取单个套餐。
func adminGetPlanHandler(catalog tokenplan.PlanCatalog, guard identity.AccessGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := requireAdmin(c, guard); !ok {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			writeErr(c, tokenplan.ErrPlanNotFound)
			return
		}
		pl, err := catalog.Get(c.Request.Context(), id)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, adminPlanView(*pl))
	}
}

// adminCreatePlanHandler（🅐）新建套餐（缺省 multiplier=1/valid_days=30/status=enabled）；非法 → PLAN_INPUT_INVALID。
func adminCreatePlanHandler(catalog tokenplan.PlanCatalog, guard identity.AccessGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := requireAdmin(c, guard); !ok {
			return
		}
		var req planReq
		if err := c.ShouldBindJSON(&req); err != nil {
			writeErr(c, tokenplan.ErrPlanInputInvalid)
			return
		}
		in := req.applyTo(tokenplan.PlanInput{Multiplier: 1.0, ValidDays: 30, Status: tokenplan.PlanEnabled})
		pl, err := catalog.Create(c.Request.Context(), in)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, adminPlanView(*pl))
	}
}

// adminUpdatePlanHandler（🅐）改价/改档：在既有套餐上叠加 body 提供的字段（PATCH 语义）后全量更新。
func adminUpdatePlanHandler(catalog tokenplan.PlanCatalog, guard identity.AccessGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := requireAdmin(c, guard); !ok {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			writeErr(c, tokenplan.ErrPlanNotFound)
			return
		}
		cur, err := catalog.Get(c.Request.Context(), id)
		if err != nil {
			writeErr(c, err) // PLAN_NOT_FOUND
			return
		}
		var req planReq
		if err := c.ShouldBindJSON(&req); err != nil {
			writeErr(c, tokenplan.ErrPlanInputInvalid)
			return
		}
		in := req.applyTo(planInputFromPlan(cur))
		if err := catalog.Update(c.Request.Context(), id, in); err != nil {
			writeErr(c, err) // PLAN_INPUT_INVALID / PLAN_NOT_FOUND
			return
		}
		updated, err := catalog.Get(c.Request.Context(), id)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, adminPlanView(*updated))
	}
}

// ---- tokenplan handler 辅助 ----

// requireAdmin 取 Principal 并施加 RequireAdmin；失败已写错误并返回 ok=false。
func requireAdmin(c *gin.Context, guard identity.AccessGuard) (*appctx.Principal, bool) {
	p, ok := principalFrom(c)
	if !ok {
		writeErr(c, errUnauthorized())
		return nil, false
	}
	if err := guard.RequireAdmin(p); err != nil {
		writeErr(c, err)
		return nil, false
	}
	return p, true
}

// requireTenantOwner 取 Principal 并施加 RequireTenantOwner（按其自身租户）；失败已写错误并返回 ok=false。
func requireTenantOwner(c *gin.Context, guard identity.AccessGuard) (*appctx.Principal, bool) {
	p, ok := principalFrom(c)
	if !ok {
		writeErr(c, errUnauthorized())
		return nil, false
	}
	if err := guard.RequireTenantOwner(p, p.TenantID); err != nil {
		writeErr(c, err)
		return nil, false
	}
	return p, true
}

// planCardView 是用户购买页卡片（§3 Plan）：含零售价 + 营销字段。
func planCardView(p tokenplan.Plan, retail float64) gin.H {
	return gin.H{
		"id":               p.ID,
		"code":             p.Code,
		"name":             p.Name,
		"base_price_cny":   p.BasePrice,
		"anchor_price_cny": p.AnchorPrice,
		"discount_label":   p.DiscountLabel,
		"retail_price_cny": retail,
		"month_limit_usd":  p.MonthLimitUSD,
		"multiplier":       p.Multiplier,
		"valid_days":       p.ValidDays,
		"is_recommended":   p.IsRecommended,
		"badge":            p.Badge,
		"sort":             p.Sort,
	}
}

// adminPlanView 是管理员视角的套餐全字段（含成本/保护线/时间戳）。
func adminPlanView(p tokenplan.Plan) gin.H {
	return gin.H{
		"id":                    p.ID,
		"code":                  p.Code,
		"name":                  p.Name,
		"base_price_cny":        p.BasePrice,
		"anchor_price_cny":      p.AnchorPrice,
		"discount_label":        p.DiscountLabel,
		"multiplier":            p.Multiplier,
		"month_limit_usd":       p.MonthLimitUSD,
		"valid_days":            p.ValidDays,
		"upstream_cost_est_cny": p.UpstreamCostEst,
		"agent_cost_price_cny":  p.AgentCostPrice,
		"min_price_cny":         p.MinPrice,
		"is_recommended":        p.IsRecommended,
		"badge":                 p.Badge,
		"sort":                  p.Sort,
		"status":                p.Status,
		"created_at":            p.CreatedAt,
		"updated_at":            p.UpdatedAt,
	}
}

// subscriptionView 是「我的套餐」单项（§3 Subscription）。
func subscriptionView(s *tokenplan.Subscription, planCode string) gin.H {
	return gin.H{
		"id":              s.ID,
		"plan_code":       planCode,
		"month_limit_usd": s.MonthLimitUSD,
		"used_usd":        s.UsedUSD,
		"remaining_usd":   s.RemainingUSD(),
		"status":          s.Status,
		"start_at":        s.StartAt,
		"expire_at":       s.ExpireAt,
	}
}

// planCodeMap 返回 plan_id -> code 映射（订阅列表回显套餐代码用）。
func planCodeMap(ctx context.Context, catalog tokenplan.PlanCatalog) (map[int64]string, error) {
	plans, err := catalog.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(plans))
	for _, p := range plans {
		m[p.ID] = p.Code
	}
	return m, nil
}

// planInputFromPlan 把既有套餐物化为可叠加的入参基线（PATCH 用）。
func planInputFromPlan(p *tokenplan.Plan) tokenplan.PlanInput {
	return tokenplan.PlanInput{
		Code:            p.Code,
		Name:            p.Name,
		BasePrice:       p.BasePrice,
		AnchorPrice:     p.AnchorPrice,
		DiscountLabel:   p.DiscountLabel,
		Multiplier:      p.Multiplier,
		MonthLimitUSD:   p.MonthLimitUSD,
		ValidDays:       p.ValidDays,
		UpstreamCostEst: p.UpstreamCostEst,
		AgentCostPrice:  p.AgentCostPrice,
		MinPrice:        p.MinPrice,
		IsRecommended:   p.IsRecommended,
		Badge:           p.Badge,
		Sort:            p.Sort,
		Status:          p.Status,
	}
}

// planReq 是 admin 套餐创建/更新的请求 DTO；全部指针字段以支持 PATCH 仅叠加传入项。
type planReq struct {
	Code            *string  `json:"code"`
	Name            *string  `json:"name"`
	BasePrice       *float64 `json:"base_price_cny"`
	AnchorPrice     *float64 `json:"anchor_price_cny"`
	DiscountLabel   *string  `json:"discount_label"`
	Multiplier      *float64 `json:"multiplier"`
	MonthLimitUSD   *float64 `json:"month_limit_usd"`
	ValidDays       *int     `json:"valid_days"`
	UpstreamCostEst *float64 `json:"upstream_cost_est_cny"`
	AgentCostPrice  *float64 `json:"agent_cost_price_cny"`
	MinPrice        *float64 `json:"min_price_cny"`
	IsRecommended   *bool    `json:"is_recommended"`
	Badge           *string  `json:"badge"`
	Sort            *int     `json:"sort"`
	Status          *string  `json:"status"`
}

// applyTo 把请求中显式提供的字段叠加到基线入参上（nil 表示不变）。
func (r planReq) applyTo(in tokenplan.PlanInput) tokenplan.PlanInput {
	if r.Code != nil {
		in.Code = *r.Code
	}
	if r.Name != nil {
		in.Name = *r.Name
	}
	if r.BasePrice != nil {
		in.BasePrice = *r.BasePrice
	}
	if r.AnchorPrice != nil {
		in.AnchorPrice = *r.AnchorPrice
	}
	if r.DiscountLabel != nil {
		in.DiscountLabel = *r.DiscountLabel
	}
	if r.Multiplier != nil {
		in.Multiplier = *r.Multiplier
	}
	if r.MonthLimitUSD != nil {
		in.MonthLimitUSD = *r.MonthLimitUSD
	}
	if r.ValidDays != nil {
		in.ValidDays = *r.ValidDays
	}
	if r.UpstreamCostEst != nil {
		in.UpstreamCostEst = *r.UpstreamCostEst
	}
	if r.AgentCostPrice != nil {
		in.AgentCostPrice = *r.AgentCostPrice
	}
	if r.MinPrice != nil {
		in.MinPrice = *r.MinPrice
	}
	if r.IsRecommended != nil {
		in.IsRecommended = *r.IsRecommended
	}
	if r.Badge != nil {
		in.Badge = *r.Badge
	}
	if r.Sort != nil {
		in.Sort = *r.Sort
	}
	if r.Status != nil {
		in.Status = tokenplan.PlanStatus(*r.Status)
	}
	return in
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
