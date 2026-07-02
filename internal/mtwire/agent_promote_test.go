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
	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
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

// newPromoteTestApp 造最小 App：真实 GORM agent_profiles（sqlite，验证 level 是否真落库）+ 真实 GORM
// 推广渠道（sqlite，验证升档是否真作废渠道，见 agent_promote_void_test.go）+ 测试桩 TenantService
// （EnsureSubdomain 结果可控）。tenantID 固定，起点 L0 且已有非零成本/折扣/分润参数，用来同时验证
// 「失败时这些字段也不该被 SetAgentType 覆盖」。
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
	if err := promotionrepo.AutoMigrate(db); err != nil {
		t.Fatalf("promotion migrate: %v", err)
	}
	ar := agentrepo.New(db)
	const tenantID = int64(42)
	if err := ar.SetAgentType(context.Background(), tenantID, agent.AgentParams{
		Level: 0, CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.1,
	}); err != nil {
		t.Fatalf("seed initial L0 profile: %v", err)
	}
	pr := promotionrepo.New(db)
	app := &App{
		DB:            db,
		AgentRepo:     ar,
		AgentService:  agent.NewService(ar, nil), // guard=nil：本测试不需要折扣保护线
		PromotionRepo: pr,
		Promotion:     promotion.NewService(pr),
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

// ----------------------------------------------------------------------------
// 邀请链接（推广渠道）自动作废：代理升级为独立档（level>=1）时，该代理名下的全部推广渠道应自动
// 作废——已作废渠道不再向*新*注册归属（见 attribution_test.go 的 attributeByChannel 覆盖），
// 已归属的历史用户不受影响。幂等：重复对已是 L1 的代理下发 level=1 是 no-op。
// ----------------------------------------------------------------------------

// TestHandleAdminUpdateAgent_PromoteVoidsChannels 核心验收：升档 L0→L1 时自动作废该代理名下的
// 全部推广渠道。
func TestHandleAdminUpdateAgent_PromoteVoidsChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tenantID := newPromoteTestApp(t, nil)
	ctx := context.Background()

	chA := &promotion.Channel{TenantID: tenantID, ChannelCode: "promo_a"}
	chB := &promotion.Channel{TenantID: tenantID, ChannelCode: "promo_b"}
	if err := app.PromotionRepo.CreateChannel(ctx, chA); err != nil {
		t.Fatalf("seed channel a: %v", err)
	}
	if err := app.PromotionRepo.CreateChannel(ctx, chB); err != nil {
		t.Fatalf("seed channel b: %v", err)
	}

	c, w := adminUpdateAgentCtx(tenantID, `{"level":1}`)
	app.HandleAdminUpdateAgent(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	gotA, err := app.PromotionRepo.GetChannelByCode(ctx, "promo_a")
	if err != nil {
		t.Fatalf("get channel a: %v", err)
	}
	gotB, err := app.PromotionRepo.GetChannelByCode(ctx, "promo_b")
	if err != nil {
		t.Fatalf("get channel b: %v", err)
	}
	if !gotA.Voided || !gotB.Voided {
		t.Fatalf("channels not voided after promotion: a.Voided=%v b.Voided=%v", gotA.Voided, gotB.Voided)
	}
}

// TestHandleAdminUpdateAgent_PromoteDoesNotVoidOtherTenantChannels 只作废*该*代理自己的渠道，
// 不动别的租户（越权/误伤防线，镜像 gormrepo 的 scopeByTenant 契约）。
func TestHandleAdminUpdateAgent_PromoteDoesNotVoidOtherTenantChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tenantID := newPromoteTestApp(t, nil)
	ctx := context.Background()

	const otherTenantID = int64(999)
	own := &promotion.Channel{TenantID: tenantID, ChannelCode: "promo_own"}
	other := &promotion.Channel{TenantID: otherTenantID, ChannelCode: "promo_other"}
	if err := app.PromotionRepo.CreateChannel(ctx, own); err != nil {
		t.Fatalf("seed own channel: %v", err)
	}
	if err := app.PromotionRepo.CreateChannel(ctx, other); err != nil {
		t.Fatalf("seed other channel: %v", err)
	}

	c, w := adminUpdateAgentCtx(tenantID, `{"level":1}`)
	app.HandleAdminUpdateAgent(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	gotOther, err := app.PromotionRepo.GetChannelByCode(ctx, "promo_other")
	if err != nil {
		t.Fatalf("get other channel: %v", err)
	}
	if gotOther.Voided {
		t.Fatal("promoting tenant must not void another tenant's channel")
	}
}

// TestHandleAdminUpdateAgent_RepromoteIsIdempotent 再次对已是 L1 的代理下发 level=1（重复升档）
// 必须是 no-op：不报错，渠道保持已作废状态。
func TestHandleAdminUpdateAgent_RepromoteIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tenantID := newPromoteTestApp(t, nil)
	ctx := context.Background()

	ch := &promotion.Channel{TenantID: tenantID, ChannelCode: "promo_once"}
	if err := app.PromotionRepo.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	// 首次升档。
	c1, w1 := adminUpdateAgentCtx(tenantID, `{"level":1}`)
	app.HandleAdminUpdateAgent(c1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first promote code = %d, want 200; body=%s", w1.Code, w1.Body.String())
	}

	// 再次对已是 L1 的代理下发 level=1。
	c2, w2 := adminUpdateAgentCtx(tenantID, `{"level":1}`)
	app.HandleAdminUpdateAgent(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("re-promote code = %d, want 200 (idempotent no-op); body=%s", w2.Code, w2.Body.String())
	}

	got, err := app.PromotionRepo.GetChannelByCode(ctx, "promo_once")
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if !got.Voided {
		t.Fatal("channel must stay voided after idempotent re-promote")
	}
}
