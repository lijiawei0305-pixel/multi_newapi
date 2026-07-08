package mtwire

// 购买升级发子域名 gap 回归测试（2026-07-08 用户报「开通代理页子域名框没反应」牵出）：
// 已是 L0 的代理自助购买 OEM/API 档（GrantLevel≥1），provisionAgentFromOrder 升级分支此前只
// SetAgentType——不发子域名、不作废推广渠道，买家付费升「独立站代理」却没有站，须管理员手动补。
// 修复契约（镜像 HandleAdminUpdateAgent 促升序列，agent.go Fix 3 原子性教训）：
//   - GrantLevel≥1 → 先 EnsureSubdomain（幂等，失败则整体失败、level 绝不落库）；
//   - 仅「真促升」（当前 level<1 → ≥1）才 VoidChannelsByTenant——**年度续费（已是 L1）不得再作废渠道**
//     （否则每年续费都打断代理正在用的邀请链接，这是购买路径与 admin PATCH 语义的关键差异）；
//   - 之后才 SetAgentType + upsertMembership。

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// recordingTenantService 是 tenant.TenantService 测试桩：EnsureSubdomain 记录每次调用的 slug 并返回
// 可控 subErr；Get 回固定租户；Create/AddSubdomain/SetStatus 不该被升级分支调用，误调即报错暴露。
type recordingTenantService struct {
	tn      *tenant.Tenant
	subErr  error
	ensured []string
}

func (r *recordingTenantService) Get(_ context.Context, _ int64) (*tenant.Tenant, error) {
	return r.tn, nil
}

func (r *recordingTenantService) Create(_ context.Context, _ tenant.CreateTenantInput) (*tenant.Tenant, error) {
	return nil, errors.New("unexpected Create call in upgrade-branch test")
}

func (r *recordingTenantService) EnsureSubdomain(_ context.Context, _ int64, slug string) error {
	r.ensured = append(r.ensured, slug)
	return r.subErr
}

func (r *recordingTenantService) AddSubdomain(_ context.Context, _ int64, _ string) (string, []string, error) {
	return "", nil, errors.New("unexpected AddSubdomain call in upgrade-branch test")
}

func (r *recordingTenantService) SetStatus(_ context.Context, _ int64, _ tenant.TenantStatus) error {
	return errors.New("unexpected SetStatus call in upgrade-branch test")
}

var _ tenant.TenantService = (*recordingTenantService)(nil)

// newAgentPlanUpgradeTestApp 造升级分支最小 App：真实 sqlite tenants（owner 反查）+ agent_profiles +
// 推广渠道 + 会员台账表；TenantService 用记录桩。返回 (app, 已拥有租户的 owner 用户ID, 租户ID, 桩)。
func newAgentPlanUpgradeTestApp(t *testing.T, initialLevel int, subErr error) (*App, int64, int64, *recordingTenantService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	if err := promotionrepo.AutoMigrate(db); err != nil {
		t.Fatalf("promotion migrate: %v", err)
	}
	if err := db.AutoMigrate(&agentMembershipRow{}); err != nil {
		t.Fatalf("membership migrate: %v", err)
	}

	ar := agentrepo.New(db)
	pr := promotionrepo.New(db)
	app := &App{
		DB:            db,
		TenantRepo:    tenantrepo.New(db),
		AgentRepo:     ar,
		AgentService:  agent.NewService(ar, nil),
		PromotionRepo: pr,
		Promotion:     promotion.NewService(pr),
	}

	const ownerID = int64(501)
	tid := seedOwnedTenant(t, app, "upshop", ownerID)
	if err := ar.SetAgentType(context.Background(), tid, agent.AgentParams{
		UserID: ownerID, Level: initialLevel, PackageDiscount: 0.9, CommissionRatio: 0.1,
	}); err != nil {
		t.Fatalf("seed agent profile: %v", err)
	}
	rts := &recordingTenantService{
		tn:     &tenant.Tenant{ID: tid, Slug: "upshop", Name: "upshop", Status: tenant.StatusActive},
		subErr: subErr,
	}
	app.TenantService = rts
	return app, ownerID, tid, rts
}

// upgradeOrder 造一笔已支付的 OEM 档（GrantLevel=1）升级订单。
func upgradeOrder(ownerID int64) *agentPlanOrderRow {
	return &agentPlanOrderRow{
		OrderNo: "AGTTEST01", OwnerUserID: ownerID, PlanID: 2, PlanCode: "oem",
		GrantLevel: 1, GrantCanAPI: false, GrantDiscountRatio: 0.8, ValidDays: 365,
	}
}

// L0 购买 OEM 档：升级分支必须幂等发子域名（用租户既有 slug）并落 level=1——gap 的核心回归。
func TestProvisionAgentFromOrder_UpgradeL0_ProvisionsSubdomain(t *testing.T) {
	app, ownerID, tid, rts := newAgentPlanUpgradeTestApp(t, 0, nil)
	ctx := context.Background()

	gotTid, err := app.provisionAgentFromOrder(ctx, upgradeOrder(ownerID))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if gotTid != tid {
		t.Fatalf("tenantID = %d, want %d (升级既有租户,不新建)", gotTid, tid)
	}
	if len(rts.ensured) != 1 || rts.ensured[0] != "upshop" {
		t.Fatalf("EnsureSubdomain calls = %v, want [upshop] —— 购买升级必须发子域名(gap 回归)", rts.ensured)
	}
	params, found, err := app.AgentRepo.GetAgentType(ctx, tid)
	if err != nil || !found {
		t.Fatalf("GetAgentType: found=%v err=%v", found, err)
	}
	if params.Level != 1 {
		t.Fatalf("level = %d, want 1", params.Level)
	}
}

// 子域名派生失败 → 整体失败,level 绝不落 1（原子性,镜像 admin 促升 Fix 3）。
func TestProvisionAgentFromOrder_SubdomainFailure_LevelStaysL0(t *testing.T) {
	app, ownerID, tid, _ := newAgentPlanUpgradeTestApp(t, 0, errors.New("boom: subdomain failed"))
	ctx := context.Background()

	if _, err := app.provisionAgentFromOrder(ctx, upgradeOrder(ownerID)); err == nil {
		t.Fatal("provision succeeded despite EnsureSubdomain failure, want error (原子性)")
	}
	params, found, err := app.AgentRepo.GetAgentType(ctx, tid)
	if err != nil || !found {
		t.Fatalf("GetAgentType: found=%v err=%v", found, err)
	}
	if params.Level != 0 {
		t.Fatalf("level = %d after subdomain failure, want unchanged 0（不得留下 level=1 无站）", params.Level)
	}
}

// 真促升（L0→L1）作废该代理全部推广渠道（镜像 admin 促升语义:邀请链接不再向新注册归属）。
func TestProvisionAgentFromOrder_UpgradeL0_VoidsChannels(t *testing.T) {
	app, ownerID, tid, _ := newAgentPlanUpgradeTestApp(t, 0, nil)
	ctx := context.Background()
	if err := app.PromotionRepo.CreateChannel(ctx, &promotion.Channel{TenantID: tid, ChannelCode: "up_ch"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	if _, err := app.provisionAgentFromOrder(ctx, upgradeOrder(ownerID)); err != nil {
		t.Fatalf("provision: %v", err)
	}
	got, err := app.PromotionRepo.GetChannelByCode(ctx, "up_ch")
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if !got.Voided {
		t.Fatal("channel not voided after purchase-upgrade L0→L1, want voided（镜像 admin 促升）")
	}
}

// 年度续费（已是 L1 再买 GrantLevel=1 档）：**不得**作废渠道（购买路径与 admin PATCH 的关键语义差异——
// 否则每年续费都打断正在用的邀请链接）；EnsureSubdomain 仍幂等执行（顺带自愈历史「L1 无站」数据）。
func TestProvisionAgentFromOrder_RenewalL1_DoesNotVoidChannels(t *testing.T) {
	app, ownerID, tid, rts := newAgentPlanUpgradeTestApp(t, 1, nil)
	ctx := context.Background()
	if err := app.PromotionRepo.CreateChannel(ctx, &promotion.Channel{TenantID: tid, ChannelCode: "renew_ch"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	if _, err := app.provisionAgentFromOrder(ctx, upgradeOrder(ownerID)); err != nil {
		t.Fatalf("provision: %v", err)
	}
	got, err := app.PromotionRepo.GetChannelByCode(ctx, "renew_ch")
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if got.Voided {
		t.Fatal("renewal (already L1) must NOT void channels——续费不打断邀请链接")
	}
	if len(rts.ensured) != 1 {
		t.Fatalf("EnsureSubdomain calls = %v, want 1（续费幂等自愈子域名）", rts.ensured)
	}
	params, _, err := app.AgentRepo.GetAgentType(ctx, tid)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if params.Level != 1 {
		t.Fatalf("level = %d, want 1", params.Level)
	}
}
