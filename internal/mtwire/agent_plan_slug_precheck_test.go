package mtwire

// AGT 代理套餐购买：slug 付款前校验/预留 + 软删复活的回归测试。
//
// 覆盖两条不变量：
//  1. precheckAndReserveAgentSlug 与 provisionAgentFromOrder 分支决策同构——「预检通过 ⇒ 激活必成（就
//     slug 而言）」。非法/保留/占用在付款前 fail-closed 拒绝；升级/复活路径忽略 slug；全新代理成功预检
//     即插入软删态预留行占位（预留语义详见 agent_plan_slug_reservation_test.go）。
//  2. 软删代理重购不再撞 tenants.slug 唯一键永久失败（付款黑洞的「无需用户输入」触发点），而是复活旧租户。

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// seedAgentTenant 造一个归属 ownerID、指定 slug/状态的代理租户（SkipSubdomain 免建域名噪声）。返回租户 id。
func seedAgentTenant(t *testing.T, app *App, ownerID int64, slug string, status tenant.TenantStatus) int64 {
	t.Helper()
	ctx := context.Background()
	tn, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug: slug, Name: "seed " + slug, TokenplanEnabled: true, SkipSubdomain: true,
	})
	if err != nil {
		t.Fatalf("seed tenant %q: %v", slug, err)
	}
	if err := app.TenantRepo.SetOwnerUserID(ctx, tn.ID, ownerID); err != nil {
		t.Fatalf("set owner %d: %v", ownerID, err)
	}
	if status != tenant.StatusActive {
		if err := app.TenantRepo.SetTenantStatus(ctx, tn.ID, status); err != nil {
			t.Fatalf("set status %q: %v", status, err)
		}
	}
	return tn.ID
}

// TestPrecheckAgentPurchaseSlug 断言付款前校验的三分支：全新代理走格式/保留词/状态盲查重（成功即预留）；
// 已是代理与软删 owner（复活）忽略 slug。用例覆盖付款黑洞的全部输入触发点。
func TestPrecheckAgentPurchaseSlug(t *testing.T) {
	app, _ := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	seedAgentTenant(t, app, 950, "taken-shop", tenant.StatusActive)   // 他人已占用的 slug
	seedAgentTenant(t, app, 951, "live-shop", tenant.StatusActive)    // 已是代理（升级路径）
	seedAgentTenant(t, app, 952, "gone-shop", tenant.StatusDeleted)   // 曾被软删（复活路径）
	seedAgentTenant(t, app, 953, "susp-shop", tenant.StatusSuspended) // 被管理员停用（付款前拒单）

	cases := []struct {
		name    string
		owner   int64
		slug    string
		wantErr string // "" = 期望 nil
	}{
		{"new/chinese", 960, "我的小店", "SLUG_INVALID"},
		{"new/too-short", 961, "ai", "SLUG_INVALID"},
		{"new/underscore", 962, "my_shop", "SLUG_INVALID"},
		{"new/dot", 963, "shop.cn", "SLUG_INVALID"},
		{"new/reserved", 964, "admin", "SLUG_RESERVED"},
		{"new/duplicate", 965, "taken-shop", "SLUG_DUPLICATE"},
		{"new/valid-unique", 966, "fresh-shop", ""},
		{"new/empty-derives-default", 967, "", ""},                       // 空 → 派生 agent967（唯一）
		{"new/reserved-boundary", 968, "api", "SLUG_RESERVED"},           // 边界长度 3 的保留词（锁定格式先过再判保留词）
		{"new/uppercase-reserved", 969, "ADMIN", "SLUG_RESERVED"},        // 归一 ToLower 后仍命中保留词
		{"new/uppercase-duplicate", 970, "TAKEN-SHOP", "SLUG_DUPLICATE"}, // 归一后撞占用（防大小写绕过查重→付款后撞键黑洞）
		{"new/spaces-trim-valid", 971, " Trim-Shop ", ""},                // 首尾空格 + 大写 → 归一 trim-shop（唯一，放行；不复用 966 的 slug——成功预检即预留占位）
		{"existing-agent/ignores-slug", 951, "admin", ""},                // 升级路径：连保留词都放行（slug 被忽略）
		{"soft-deleted/reactivate", 952, "我的小店", ""},                     // 复活路径：非法 slug 也放行（复用旧 slug）
		{"suspended-owner/reject", 953, "我的小店", "AGENT_SUSPENDED"},       // 停用态：suspended 守卫先于 slug 校验拒单
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := app.precheckAndReserveAgentSlug(ctx, c.owner, c.slug, "")
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("precheck(owner=%d, slug=%q) = %v, want nil", c.owner, c.slug, err)
				}
				return
			}
			if !apperr.Is(err, c.wantErr) {
				t.Fatalf("precheck(owner=%d, slug=%q) = %v, want code %s", c.owner, c.slug, err, c.wantErr)
			}
		})
	}
}

// TestProvisionAgentFromOrder_ReactivatesSoftDeletedTenant 是付款黑洞「无需用户输入」触发点的核心回归：
// owner 的代理被软删后（tenants.slug 唯一索引仍占位），重购激活应**复活**旧租户，而非走新建分支撞唯一键
// 永久 ErrSlugDuplicate。断言：第二单激活无错、仍只一个租户、旧租户翻回 active、订单回填被复活的租户。
func TestProvisionAgentFromOrder_ReactivatesSoftDeletedTenant(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	// 第一笔：全新 L0 代理（空 slug → 派生 agent<owner>）。
	seedPendingNewAgentOrder(t, app, "AGTREACT1", ownerID)
	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTREACT1", 6.90); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	var tid1 int64
	if err := app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).
		Select("id").Take(&tid1).Error; err != nil {
		t.Fatalf("load tenant: %v", err)
	}

	// 软删该租户（模拟 admin 删除代理：翻 status=deleted，slug 唯一索引仍占位）。
	if err := app.TenantRepo.SetTenantStatus(ctx, tid1, tenant.StatusDeleted); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// 第二笔：同一 owner 重购（空 slug → 又派生 agent<owner>，撞软删占位）。
	seedPendingNewAgentOrder(t, app, "AGTREACT2", ownerID)
	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTREACT2", 6.90); err != nil {
		t.Fatalf("重购激活应复活软删租户而非撞 slug 唯一键，got %v", err)
	}

	// 只一个租户（复活同一个，而非新建孤儿）。
	var count int64
	if err := app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).Count(&count).Error; err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant count = %d, want 1（复活应复用旧租户，不得新建孤儿）", count)
	}

	// 旧租户已翻回 active。
	var status string
	if err := app.DB.Table("tenants").Where("id = ?", tid1).
		Select("status").Take(&status).Error; err != nil {
		t.Fatalf("reload status: %v", err)
	}
	if status != string(tenant.StatusActive) {
		t.Fatalf("tenant status = %q, want active（复活未翻回状态）", status)
	}

	// 第二单激活且回填的正是被复活的租户。
	var ord2 agentPlanOrderRow
	if err := app.DB.Take(&ord2, "order_no = ?", "AGTREACT2").Error; err != nil {
		t.Fatalf("reload order2: %v", err)
	}
	if ord2.Status != agtOrderActivated {
		t.Fatalf("order2 status = %q, want activated", ord2.Status)
	}
	if ord2.AgentTenantID != tid1 {
		t.Fatalf("order2 agent_tenant_id = %d, want reactivated %d", ord2.AgentTenantID, tid1)
	}
}

// seedPendingAgentOrderLevel 造一笔指定档位的 pending 新代理订单（空 slug → 派生 agent<owner>）。
func seedPendingAgentOrderLevel(t *testing.T, app *App, orderNo string, ownerID int64, grantLevel int, amount float64) {
	t.Helper()
	ord := &agentPlanOrderRow{
		OrderNo: orderNo, OwnerUserID: ownerID, PlanID: 2, PlanCode: "oem",
		AmountCNY: amount, Status: agtOrderPending,
		GrantLevel: grantLevel, GrantCanAPI: false, GrantDiscountRatio: 0, ValidDays: 365,
	}
	if err := app.DB.Create(ord).Error; err != nil {
		t.Fatalf("seed L%d order %s: %v", grantLevel, orderNo, err)
	}
}

// TestProvisionAgentFromOrder_ReactivatesL1RebuildsSubdomain 覆盖复活块对 L1+ 独立档的核心承诺：
// 软删（翻 deleted + 删 tenant_domains 域名记录，复刻 HandleAdminDeleteAgent）后重购，复活须落入升级分支、
// GrantLevel>=1 时 EnsureSubdomain **重建被删的 <slug>.wedreamhub.com**——否则买家付独立档钱却打不开自己的站。
func TestProvisionAgentFromOrder_ReactivatesL1RebuildsSubdomain(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	// 第一笔：L1 独立档新代理（GrantLevel=1 → TenantService.Create 自动派生 <slug>.wedreamhub.com）。
	seedPendingAgentOrderLevel(t, app, "AGTL1REACT1", ownerID, 1, 2999)
	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTL1REACT1", 2999); err != nil {
		t.Fatalf("first L1 activation: %v", err)
	}
	var row struct {
		ID   int64
		Slug string
	}
	if err := app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).
		Select("id, slug").Take(&row).Error; err != nil {
		t.Fatalf("load tenant: %v", err)
	}
	tid, domain := row.ID, tenant.DomainForSlug(row.Slug)

	// 首次开通即派生子域名并解析回本租户。
	if tn, err := app.TenantRepo.GetTenantByDomain(ctx, domain); err != nil || tn.ID != tid {
		t.Fatalf("首开后子域名解析 = (%v, %v)，want 命中租户 %d", tn, err, tid)
	}

	// 模拟 admin 软删：翻 deleted + 删域名记录（HandleAdminDeleteAgent 的两步）。
	if err := app.TenantRepo.SetTenantStatus(ctx, tid, tenant.StatusDeleted); err != nil {
		t.Fatalf("soft delete status: %v", err)
	}
	if _, err := app.TenantRepo.DeleteDomainsByTenant(ctx, tid); err != nil {
		t.Fatalf("soft delete domains: %v", err)
	}
	if _, err := app.TenantRepo.GetTenantByDomain(ctx, domain); !errors.Is(err, tenant.ErrTenantNotFound) {
		t.Fatalf("软删后子域名应不可解析，got err %v", err)
	}

	// 第二笔：L1 重购 → 复活 → 升级分支 EnsureSubdomain 重建被删域名。
	seedPendingAgentOrderLevel(t, app, "AGTL1REACT2", ownerID, 1, 2999)
	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTL1REACT2", 2999); err != nil {
		t.Fatalf("L1 重购应复活并重建子域名，got %v", err)
	}

	// 复活后：单租户、子域名重建并解析回本租户（隐含 status=active，否则 getActiveTenant 判 NotFound）。
	var count int64
	if err := app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).Count(&count).Error; err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant count = %d, want 1（复活应复用旧租户）", count)
	}
	tn, err := app.TenantRepo.GetTenantByDomain(ctx, domain)
	if err != nil {
		t.Fatalf("复活后子域名应重建并解析回本租户，got err %v", err)
	}
	if tn.ID != tid {
		t.Fatalf("子域名解析到租户 %d，want 复活的 %d", tn.ID, tid)
	}
}

// TestProvisionAgentFromOrder_RefusesSuspendedOwner 锁定 suspended「软款版付款黑洞」的兜底：被管理员停用的
// 代理，其 owner 重购激活时 provision 必须拒绝（agentplan.ErrAgentSuspended），订单不激活、租户不被购买静默翻回
// active——否则收钱+台账已激活但 agent 鉴权仍拒 suspended，付了钱登不进后台。与 precheck 付款前拒单同构。
func TestProvisionAgentFromOrder_RefusesSuspendedOwner(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	seedPendingNewAgentOrder(t, app, "AGTSUSP1", ownerID)
	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTSUSP1", 6.90); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	var tid int64
	if err := app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).
		Select("id").Take(&tid).Error; err != nil {
		t.Fatalf("load tenant: %v", err)
	}
	// 管理员停用该代理。
	if err := app.TenantRepo.SetTenantStatus(ctx, tid, tenant.StatusSuspended); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	// 停用态 owner 重购激活：provision 应拒（AGENT_SUSPENDED），订单不激活、不静默升级。
	seedPendingNewAgentOrder(t, app, "AGTSUSP2", ownerID)
	err := app.ActivatePaidAgentPlanOrder(ctx, "AGTSUSP2", 6.90)
	if !apperr.Is(err, "AGENT_SUSPENDED") {
		t.Fatalf("suspended owner 激活 = %v, want AGENT_SUSPENDED（不得静默升级停用代理）", err)
	}

	var ord2 agentPlanOrderRow
	if err := app.DB.Take(&ord2, "order_no = ?", "AGTSUSP2").Error; err != nil {
		t.Fatalf("reload order2: %v", err)
	}
	if ord2.Status == agtOrderActivated {
		t.Fatalf("order2 不应 activated（应留 pending 待管理员解封/退款）")
	}
	// 租户仍 suspended（购买不得解封）。
	var status string
	if err := app.DB.Table("tenants").Where("id = ?", tid).Select("status").Take(&status).Error; err != nil {
		t.Fatalf("reload status: %v", err)
	}
	if status != string(tenant.StatusSuspended) {
		t.Fatalf("tenant status = %q, want suspended（购买不得静默解封）", status)
	}
}
