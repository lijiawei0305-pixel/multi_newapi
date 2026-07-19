package mtwire

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/model"
)

const demoSeedTestPassword = "LocalSeedPass!234"

func newDemoSeedSecurityTestApp(t *testing.T) (*App, int64) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, tenantrepo.AutoMigrate(db))
	require.NoError(t, agentrepo.AutoMigrate(db))
	require.NoError(t, db.AutoMigrate(&model.User{}))

	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	previousNewUserQuota := common.QuotaForNewUser
	model.DB = db
	common.RedisEnabled = false
	common.QuotaForNewUser = 0
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		common.QuotaForNewUser = previousNewUserQuota
	})

	tenantRepo := tenantrepo.New(db)
	agentRepo := agentrepo.New(db)
	app := &App{
		DB:            db,
		TenantRepo:    tenantRepo,
		TenantService: tenant.NewService(tenantRepo, tenant.NewSlugValidator()),
		AgentRepo:     agentRepo,
		AgentService:  agent.NewService(agentRepo, nil),
	}
	demoTenant, err := app.TenantService.Create(context.Background(), tenant.CreateTenantInput{
		Slug:             demoSlug,
		Name:             demoName,
		TokenplanEnabled: true,
	})
	require.NoError(t, err)
	return app, demoTenant.ID
}

func seedDemoSecurityUser(t *testing.T, db *gorm.DB, username, password string, status int) model.User {
	t.Helper()
	hash, err := common.Password2Hash(password)
	require.NoError(t, err)
	user := model.User{
		Username: username,
		Password: hash,
		AffCode:  username + "-aff",
		Role:     common.RoleCommonUser,
		Status:   status,
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func withProductionDeployment(t *testing.T, production bool) {
	t.Helper()
	previous := common.ProductionDeployment
	common.ProductionDeployment = production
	t.Cleanup(func() { common.ProductionDeployment = previous })
}

func TestDemoAgentSeedPasswordRequiresExplicitSafeDevelopmentOptIn(t *testing.T) {
	testCases := []struct {
		name        string
		seedValue   string
		password    string
		production  bool
		wantPass    string
		wantEnabled bool
		wantError   string
	}{
		{name: "unset is disabled"},
		{name: "explicit false is disabled", seedValue: "false"},
		{name: "valid development opt-in", seedValue: "true", password: demoSeedTestPassword, wantPass: demoSeedTestPassword, wantEnabled: true},
		{name: "production rejects opt-in", seedValue: "true", password: demoSeedTestPassword, production: true, wantError: "forbidden in production"},
		{name: "malformed flag is rejected", seedValue: "yes", password: demoSeedTestPassword, wantError: "exactly true or false"},
		{name: "missing password is rejected", seedValue: "true", wantError: "16-20 characters"},
		{name: "short password is rejected", seedValue: "true", password: "short-password", wantError: "16-20 characters"},
		{name: "long password is rejected", seedValue: "true", password: "123456789012345678901", wantError: "16-20 characters"},
		{name: "unicode characters are counted correctly", seedValue: "true", password: strings.Repeat("界", 20), wantEnabled: true, wantPass: strings.Repeat("界", 20)},
		{name: "bcrypt byte limit is enforced", seedValue: "true", password: strings.Repeat("🗝", 20), wantError: "72-byte"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(demoAgentSeedEnabledEnv, testCase.seedValue)
			t.Setenv(demoAgentPasswordEnv, testCase.password)
			withProductionDeployment(t, testCase.production)

			password, enabled, err := demoAgentSeedPassword()
			if testCase.wantError != "" {
				require.ErrorContains(t, err, testCase.wantError)
				assert.False(t, enabled)
				assert.Empty(t, password)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantEnabled, enabled)
			assert.Equal(t, testCase.wantPass, password)
		})
	}
}

func TestReconcileDemoAgentSeedDefaultDoesNotCreateLogin(t *testing.T) {
	app, tenantID := newDemoSeedSecurityTestApp(t)
	t.Setenv(demoAgentSeedEnabledEnv, "")
	t.Setenv(demoAgentPasswordEnv, "")
	withProductionDeployment(t, false)

	require.NoError(t, app.reconcileDemoAgentSeed(context.Background(), tenantID))
	var count int64
	require.NoError(t, app.DB.Model(&model.User{}).Where("username = ?", demoAgentUsername).Count(&count).Error)
	assert.Zero(t, count)
	ownerID, err := app.TenantRepo.OwnerUserID(context.Background(), tenantID)
	require.NoError(t, err)
	assert.Zero(t, ownerID)
}

func TestSeedFailsClosedWhenLegacyDemoRetirementCannotBeVerified(t *testing.T) {
	app, _ := newDemoSeedSecurityTestApp(t)
	t.Setenv(demoAgentSeedEnabledEnv, "false")
	withProductionDeployment(t, true)

	sqlDB, err := app.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = app.Seed()
	require.ErrorIs(t, err, ErrDemoAgentSecurity)
}

func TestReconcileDemoAgentSeedProductionRejectsCreation(t *testing.T) {
	app, tenantID := newDemoSeedSecurityTestApp(t)
	t.Setenv(demoAgentSeedEnabledEnv, "true")
	t.Setenv(demoAgentPasswordEnv, demoSeedTestPassword)
	withProductionDeployment(t, true)

	err := app.reconcileDemoAgentSeed(context.Background(), tenantID)
	require.ErrorContains(t, err, "forbidden in production")
	var count int64
	require.NoError(t, app.DB.Model(&model.User{}).Where("username = ?", demoAgentUsername).Count(&count).Error)
	assert.Zero(t, count)
}

func TestReconcileDemoAgentSeedExplicitDevelopmentOptIn(t *testing.T) {
	app, tenantID := newDemoSeedSecurityTestApp(t)
	t.Setenv(demoAgentSeedEnabledEnv, "true")
	t.Setenv(demoAgentPasswordEnv, demoSeedTestPassword)
	withProductionDeployment(t, false)

	require.NoError(t, app.reconcileDemoAgentSeed(context.Background(), tenantID))
	var user model.User
	require.NoError(t, app.DB.Where("username = ?", demoAgentUsername).First(&user).Error)
	assert.Equal(t, common.UserStatusEnabled, user.Status)
	assert.True(t, common.ValidatePasswordAndHash(demoSeedTestPassword, user.Password))

	ownerID, err := app.TenantRepo.OwnerUserID(context.Background(), tenantID)
	require.NoError(t, err)
	assert.Equal(t, int64(user.Id), ownerID)
	profiles, err := app.AgentRepo.ListProfiles(context.Background())
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	assert.Equal(t, int64(user.Id), profiles[0].UserID)
}

func TestReconcileDemoAgentSeedPreservesManualTenantOwner(t *testing.T) {
	app, tenantID := newDemoSeedSecurityTestApp(t)
	const manualOwnerID = int64(9001)
	require.NoError(t, app.TenantRepo.SetOwnerUserID(context.Background(), tenantID, manualOwnerID))
	t.Setenv(demoAgentSeedEnabledEnv, "true")
	t.Setenv(demoAgentPasswordEnv, demoSeedTestPassword)
	withProductionDeployment(t, false)

	require.NoError(t, app.reconcileDemoAgentSeed(context.Background(), tenantID))
	ownerID, err := app.TenantRepo.OwnerUserID(context.Background(), tenantID)
	require.NoError(t, err)
	assert.Equal(t, manualOwnerID, ownerID)
	var count int64
	require.NoError(t, app.DB.Model(&model.User{}).Where("username = ?", demoAgentUsername).Count(&count).Error)
	assert.Zero(t, count, "manual ownership must not cause an unused demo login to be created")
	profiles, err := app.AgentRepo.ListProfiles(context.Background())
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestReconcileDemoAgentSeedDisabledReservesDemoUsername(t *testing.T) {
	t.Run("demo account is disabled without affecting another user", func(t *testing.T) {
		app, tenantID := newDemoSeedSecurityTestApp(t)
		demoUser := seedDemoSecurityUser(t, app.DB, demoAgentUsername, "HistoricalSeed!42", common.UserStatusEnabled)
		seedDemoSecurityUser(t, app.DB, "another-user", "HistoricalSeed!42", common.UserStatusEnabled)
		t.Setenv(demoAgentSeedEnabledEnv, "false")
		withProductionDeployment(t, false)

		require.NoError(t, app.reconcileDemoAgentSeed(context.Background(), tenantID))
		var gotDemo, gotOther model.User
		require.NoError(t, app.DB.First(&gotDemo, demoUser.Id).Error)
		require.NoError(t, app.DB.Where("username = ?", "another-user").First(&gotOther).Error)
		assert.Equal(t, common.UserStatusDisabled, gotDemo.Status)
		assert.Equal(t, common.UserStatusEnabled, gotOther.Status)
	})

	t.Run("manually rotated reserved demo account is also disabled", func(t *testing.T) {
		app, tenantID := newDemoSeedSecurityTestApp(t)
		customUser := seedDemoSecurityUser(t, app.DB, demoAgentUsername, "ManuallyRotated!42", common.UserStatusEnabled)
		t.Setenv(demoAgentSeedEnabledEnv, "false")
		withProductionDeployment(t, false)

		require.NoError(t, app.reconcileDemoAgentSeed(context.Background(), tenantID))
		var got model.User
		require.NoError(t, app.DB.First(&got, customUser.Id).Error)
		assert.Equal(t, common.UserStatusDisabled, got.Status)
		assert.Equal(t, customUser.Password, got.Password)
	})
}
