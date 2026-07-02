package mtwire

// Fix 3（重要）回归测试：HandleAdminUpdateAgent 升档 L0→L1 时，此前先 SetAgentType（落 level=1）
// 后调 EnsureSubdomain（派生子域名）。若 EnsureSubdomain 失败，代理已经停在「level=1 但没有子域名」
// ——独立档自助能力（RequireAgentLevel(1) 门禁的站点装修/自定义域名等）被解锁，却没有可用站点。
// 修复：调换顺序——先 EnsureSubdomain，成功后才 SetAgentType；失败则直接返回错误，level 绝不落库。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// failingSubdomainTenantService 是 tenant.TenantService 的测试桩：Get 回一个固定租户；EnsureSubdomain
// 返回可控的 subErr（nil=成功）；Create/SetStatus 本测试用不到，占位报错以便一旦误调用即暴露。
type failingSubdomainTenantService struct {
	tn     *tenant.Tenant
	subErr error
}

func (f *failingSubdomainTenantService) Get(_ context.Context, _ int64) (*tenant.Tenant, error) {
	return f.tn, nil
}

func (f *failingSubdomainTenantService) Create(_ context.Context, _ tenant.CreateTenantInput) (*tenant.Tenant, error) {
	return nil, errors.New("unexpected Create call in test stub")
}

func (f *failingSubdomainTenantService) EnsureSubdomain(_ context.Context, _ int64, _ string) error {
	return f.subErr
}

func (f *failingSubdomainTenantService) SetStatus(_ context.Context, _ int64, _ tenant.TenantStatus) error {
	return errors.New("unexpected SetStatus call in test stub")
}

var _ tenant.TenantService = (*failingSubdomainTenantService)(nil)

// newPromoteTestApp 造最小 App：真实 GORM agent_profiles（sqlite，验证 level 是否真落库）+ 测试桩
// TenantService（EnsureSubdomain 结果可控）。tenantID 固定，起点 L0 且已有非零成本/折扣/分润参数，
// 用来同时验证「失败时这些字段也不该被 SetAgentType 覆盖」。
func newPromoteTestApp(t *testing.T, subErr error) (*App, int64) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	ar := agentrepo.New(db)
	const tenantID = int64(42)
	if err := ar.SetAgentType(context.Background(), tenantID, agent.AgentParams{
		Level: 0, CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.1,
	}); err != nil {
		t.Fatalf("seed initial L0 profile: %v", err)
	}
	app := &App{
		DB:           db,
		AgentRepo:    ar,
		AgentService: agent.NewService(ar, nil), // guard=nil：本测试不需要折扣保护线
		TenantService: &failingSubdomainTenantService{
			tn:     &tenant.Tenant{ID: tenantID, Slug: "promo-shop", Name: "promo", Status: tenant.StatusActive},
			subErr: subErr,
		},
	}
	return app, tenantID
}

func adminUpdateAgentCtx(tenantID int64, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/agents/"+strconv.FormatInt(tenantID, 10), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(tenantID, 10)}}
	return c, w
}

// TestHandleAdminUpdateAgent_EnsureSubdomainFailureDoesNotAdvanceLevel 是 Fix 3 的核心回归测试：
// 升档 L0→L1 时若 EnsureSubdomain 失败，代理的 level 必须保持不变（不落成 1），且原有成本/折扣/
// 分润参数也不该被半途覆盖（因为 SetAgentType 整体没有被调用——原子性）。
func TestHandleAdminUpdateAgent_EnsureSubdomainFailureDoesNotAdvanceLevel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wantErr := errors.New("boom: subdomain provisioning failed")
	app, tenantID := newPromoteTestApp(t, wantErr)

	c, w := adminUpdateAgentCtx(tenantID, `{"level":1}`)
	app.HandleAdminUpdateAgent(c)

	if w.Code == http.StatusOK {
		t.Fatalf("code = 200, want an error status when EnsureSubdomain fails; body=%s", w.Body.String())
	}
	params, found, err := app.AgentRepo.GetAgentType(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if !found {
		t.Fatalf("agent profile disappeared entirely, want original L0 profile intact")
	}
	if params.Level != 0 {
		t.Fatalf("level = %d after EnsureSubdomain failure, want unchanged 0 (Fix 3 regression: non-atomic promotion)", params.Level)
	}
	// 折扣保护线字段也不该被半途落库（SetAgentType 整体未被调用）。
	if params.CostPrice != 10 || params.PackageDiscount != 0.9 || params.CommissionRatio != 0.1 {
		t.Fatalf("params mutated despite EnsureSubdomain failure: %+v", params)
	}
}

// TestHandleAdminUpdateAgent_PromoteSucceedsWhenSubdomainOK 正向对照：EnsureSubdomain 成功时，
// level 正常落为 1，确认重排序未破坏既有成功路径。
func TestHandleAdminUpdateAgent_PromoteSucceedsWhenSubdomainOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tenantID := newPromoteTestApp(t, nil) // EnsureSubdomain 成功

	c, w := adminUpdateAgentCtx(tenantID, `{"level":1}`)
	app.HandleAdminUpdateAgent(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	params, found, err := app.AgentRepo.GetAgentType(context.Background(), tenantID)
	if err != nil || !found {
		t.Fatalf("GetAgentType: found=%v err=%v", found, err)
	}
	if params.Level != 1 {
		t.Fatalf("level = %d, want 1 after successful promotion", params.Level)
	}
}
