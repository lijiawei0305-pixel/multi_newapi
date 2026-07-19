package mtwire

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

func newAgentAdminTransactionTestApp(t *testing.T) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, tenantrepo.AutoMigrate(db))
	require.NoError(t, agentrepo.AutoMigrate(db))
	require.NoError(t, promotionrepo.AutoMigrate(db))
	require.NoError(t, db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		username TEXT NOT NULL DEFAULT '',
		tenant_id INTEGER NOT NULL DEFAULT 0
	)`).Error)

	tr := tenantrepo.New(db)
	ar := agentrepo.New(db)
	pr := promotionrepo.New(db)
	cache := tenant.NewMemCache()
	return &App{
		DB:             db,
		TenantRepo:     tr,
		TenantResolver: tenant.NewResolver(tr, cache),
		TenantService:  tenant.NewService(tr, tenant.NewSlugValidator()),
		CustomDomains:  tenant.NewCustomDomainService(tr, nil),
		tenantCache:    cache,
		AgentRepo:      ar,
		AgentService:   agent.NewService(ar, pricing.NewGuard()),
		PromotionRepo:  pr,
		Promotion:      promotion.NewService(pr),
	}
}

func agentAdminMutationContext(method, path, id, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = gin.Params{{Key: "id", Value: id}}
	return c, w
}

func TestHandleAdminCreateAgent_WalletFailureRollsBackWholeAggregate(t *testing.T) {
	app := newAgentAdminTransactionTestApp(t)
	ctx := context.Background()
	require.NoError(t, app.DB.Exec(
		"INSERT INTO users (id, username, tenant_id) VALUES (?, ?, ?)", 101, "owner", 77,
	).Error)
	require.NoError(t, app.DB.Exec(`CREATE TRIGGER fail_agent_wallet_insert
		BEFORE INSERT ON agent_wallets
		BEGIN SELECT RAISE(ABORT, 'forced wallet failure'); END`).Error)

	c, w := agentAdminMutationContext(
		http.MethodPost,
		"/api/admin/agents",
		"",
		`{"slug":"atomic-create","name":"Atomic","owner_user_id":101,"level":1,"package_discount":1}`,
	)
	app.HandleAdminCreateAgent(c)

	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	var tenants, domains, profiles, wallets int64
	require.NoError(t, app.DB.Table("tenants").Where("slug = ?", "atomic-create").Count(&tenants).Error)
	require.NoError(t, app.DB.Table("tenant_domains").Where("domain = ?", tenant.DomainForSlug("atomic-create")).Count(&domains).Error)
	require.NoError(t, app.DB.Table("agent_profiles").Count(&profiles).Error)
	require.NoError(t, app.DB.Table("agent_wallets").Count(&wallets).Error)
	assert.Zero(t, tenants)
	assert.Zero(t, domains)
	assert.Zero(t, profiles)
	assert.Zero(t, wallets)

	var ownerTenantID int64
	require.NoError(t, app.DB.Table("users").Select("tenant_id").Where("id = ?", 101).Scan(&ownerTenantID).Error)
	assert.Equal(t, int64(77), ownerTenantID, "owner 归属也必须随创建失败回滚")
	_, err := app.TenantService.Get(ctx, 1)
	assert.Error(t, err)
}

func TestHandleAdminDeleteAgent_UserMigrationFailureRollsBackStatusAndDomains(t *testing.T) {
	app := newAgentAdminTransactionTestApp(t)
	ctx := context.Background()
	tn, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug: "atomic-delete", Name: "Atomic delete", TokenplanEnabled: true,
	})
	require.NoError(t, err)
	require.NoError(t, app.TenantRepo.SetOwnerUserID(ctx, tn.ID, 202))
	require.NoError(t, app.AgentService.SetAgentType(ctx, tn.ID, agent.AgentParams{
		UserID: 202, Level: 1, PackageDiscount: 1,
	}))
	require.NoError(t, app.AgentRepo.EnsureWallet(ctx, tn.ID, 202))
	require.NoError(t, app.DB.Exec(
		"INSERT INTO users (id, username, tenant_id) VALUES (?, ?, ?)", 203, "downstream", tn.ID,
	).Error)
	custom := &tenant.CustomDomain{
		TenantID: tn.ID, Domain: "atomic-delete.example.com", Status: tenant.CustomDomainActive,
		VerifyToken: "token", CertStatus: "issued",
	}
	require.NoError(t, app.TenantRepo.CreateCustomDomain(ctx, custom))
	require.NoError(t, app.DB.Exec(fmt.Sprintf(`CREATE TRIGGER fail_agent_user_migration
		BEFORE UPDATE OF tenant_id ON users WHEN OLD.tenant_id = %d
		BEGIN SELECT RAISE(ABORT, 'forced user migration failure'); END`, tn.ID)).Error)

	id := strconv.FormatInt(tn.ID, 10)
	c, w := agentAdminMutationContext(http.MethodDelete, "/api/admin/agents/"+id, id, "")
	app.HandleAdminDeleteAgent(c)

	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	gotTenant, err := app.TenantService.Get(ctx, tn.ID)
	require.NoError(t, err)
	assert.Equal(t, tenant.StatusActive, gotTenant.Status)
	assert.Equal(t, tenant.DomainForSlug("atomic-delete"), app.TenantRepo.GetPrimaryDomain(ctx, tn.ID))
	gotCustom, err := app.TenantRepo.GetCustomDomainByTenant(ctx, tn.ID)
	require.NoError(t, err)
	assert.Equal(t, custom.Domain, gotCustom.Domain)
	var downstreamTenantID int64
	require.NoError(t, app.DB.Table("users").Select("tenant_id").Where("id = ?", 203).Scan(&downstreamTenantID).Error)
	assert.Equal(t, tn.ID, downstreamTenantID)
}

func TestHandleAdminDeleteAgent_WalletReadFailureFailsClosed(t *testing.T) {
	app := newAgentAdminTransactionTestApp(t)
	ctx := context.Background()
	tn, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug: "wallet-read", Name: "Wallet read", TokenplanEnabled: true,
	})
	require.NoError(t, err)
	require.NoError(t, app.AgentService.SetAgentType(ctx, tn.ID, agent.AgentParams{
		UserID: 303, PackageDiscount: 1,
	}))
	require.NoError(t, app.DB.Migrator().DropTable("agent_wallets"))

	id := strconv.FormatInt(tn.ID, 10)
	c, w := agentAdminMutationContext(http.MethodDelete, "/api/admin/agents/"+id, id, "")
	app.HandleAdminDeleteAgent(c)

	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	gotTenant, err := app.TenantService.Get(ctx, tn.ID)
	require.NoError(t, err)
	assert.Equal(t, tenant.StatusActive, gotTenant.Status)
	assert.Equal(t, tenant.DomainForSlug("wallet-read"), app.TenantRepo.GetPrimaryDomain(ctx, tn.ID))
}

func TestHandleAdminUpdateAgent_LateNameFailureRollsBackPromotion(t *testing.T) {
	app := newAgentAdminTransactionTestApp(t)
	ctx := context.Background()
	tn, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug: "atomic-upgrade", Name: "Before", TokenplanEnabled: true, SkipSubdomain: true,
	})
	require.NoError(t, err)
	require.NoError(t, app.TenantRepo.SetOwnerUserID(ctx, tn.ID, 404))
	require.NoError(t, app.AgentService.SetAgentType(ctx, tn.ID, agent.AgentParams{
		UserID: 404, Level: 0, CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.1,
	}))
	channel := &promotion.Channel{TenantID: tn.ID, ChannelCode: "atomic_upgrade_channel"}
	require.NoError(t, app.PromotionRepo.CreateChannel(ctx, channel))
	require.NoError(t, app.DB.Exec(fmt.Sprintf(`CREATE TRIGGER fail_agent_name_update
		BEFORE UPDATE OF name ON tenants WHEN OLD.id = %d
		BEGIN SELECT RAISE(ABORT, 'forced name failure'); END`, tn.ID)).Error)

	id := strconv.FormatInt(tn.ID, 10)
	c, w := agentAdminMutationContext(
		http.MethodPatch,
		"/api/admin/agents/"+id,
		id,
		`{"level":1,"name":"After"}`,
	)
	app.HandleAdminUpdateAgent(c)

	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	params, found, err := app.AgentRepo.GetAgentType(ctx, tn.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Zero(t, params.Level)
	assert.Equal(t, float64(10), params.CostPrice)
	assert.Empty(t, app.TenantRepo.GetPrimaryDomain(ctx, tn.ID))
	gotChannel, err := app.PromotionRepo.GetChannelByCode(ctx, channel.ChannelCode)
	require.NoError(t, err)
	assert.False(t, gotChannel.Voided)
	gotTenant, err := app.TenantService.Get(ctx, tn.ID)
	require.NoError(t, err)
	assert.Equal(t, "Before", gotTenant.Name)
}

func TestHandleAdminSetAgentDomain_CreateFailureKeepsOldDomain(t *testing.T) {
	app := newAgentAdminTransactionTestApp(t)
	ctx := context.Background()
	tn, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug: "old-domain", Name: "Old domain", TokenplanEnabled: true,
	})
	require.NoError(t, err)
	require.NoError(t, app.AgentService.SetAgentType(ctx, tn.ID, agent.AgentParams{
		UserID: 505, PackageDiscount: 1,
	}))
	newDomain := tenant.DomainForSlug("new-domain")
	require.NoError(t, app.DB.Exec(fmt.Sprintf(`CREATE TRIGGER fail_replacement_domain
		BEFORE INSERT ON tenant_domains WHEN NEW.domain = '%s'
		BEGIN SELECT RAISE(ABORT, 'forced domain insert failure'); END`, newDomain)).Error)

	id := strconv.FormatInt(tn.ID, 10)
	c, w := agentAdminMutationContext(
		http.MethodPut,
		"/api/admin/agents/"+id+"/domain",
		id,
		`{"label":"new-domain"}`,
	)
	app.HandleAdminSetAgentDomain(c)

	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, tenant.DomainForSlug("old-domain"), app.TenantRepo.GetPrimaryDomain(ctx, tn.ID))
	_, err = app.TenantRepo.GetTenantByDomain(ctx, newDomain)
	assert.Error(t, err)
}

func TestHandleAdminDeleteAgent_NonAgentTenantIsNotMutable(t *testing.T) {
	app := newAgentAdminTransactionTestApp(t)
	ctx := context.Background()
	tn, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug: "plain-tenant", Name: "Plain", TokenplanEnabled: true,
	})
	require.NoError(t, err)

	id := strconv.FormatInt(tn.ID, 10)
	c, w := agentAdminMutationContext(http.MethodDelete, "/api/admin/agents/"+id, id, "")
	app.HandleAdminDeleteAgent(c)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"AGENT_NOT_FOUND"`)
	gotTenant, err := app.TenantService.Get(ctx, tn.ID)
	require.NoError(t, err)
	assert.Equal(t, tenant.StatusActive, gotTenant.Status)
	assert.Equal(t, tenant.DomainForSlug("plain-tenant"), app.TenantRepo.GetPrimaryDomain(ctx, tn.ID))
}
