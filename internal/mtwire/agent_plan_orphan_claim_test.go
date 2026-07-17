package mtwire

// provisionAgentFromOrder 新代理分支六步非事务写的崩溃自愈（audit 2026-07-17 发现#6）：
// CreateTenant 成功后进程被部署重建/DB 抖动 → 留下「slug 已占、owner_user_id=0、active」的孤儿行；
// 重试的 owner 反查双查询（agentTenantByOwner / agentDeletedTenantByOwner）都看不见它，落新建分支
// CreateTenant 必撞 SLUG_DUPLICATE → 付了钱的订单永久卡 pending，对账每 5 分钟重试到永远。
// 修复：撞键时**认领**同 slug 的无归属 active 行（随后各步幂等续跑，L1+ 补域名）；升级分支幂等
// EnsureWallet 兜「归属后再崩」的半开通行。已归属的他人行绝不认领（真冲突保持卡单可见）。
// 预留式（发现#5）已让新单不走新建分支；本自愈兜底遗留 pending 单、历史孤儿行与释放竞态残余。

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// 审计发现#6 核心回归（子代理临时测试的正式化）：播种「CreateTenant 后崩溃」现场（派生 slug、
// owner=0、active、无域名行），断言重试激活成功自愈——孤儿行被认领归属买家、L1 域名补建、
// 钱包/会员齐、订单 activated 并回填认领行。
func TestActivate_ClaimsOrphanUnownedTenantRow(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	orphan, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug: "agent901", Name: "代理站 agent901", TokenplanEnabled: true, SkipSubdomain: true,
	})
	if err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	seedPendingAgentOrderLevel(t, app, "AGTORPH1", ownerID, 1, 2999) // 空 slug → 派生 agent901

	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTORPH1", 2999); err != nil {
		t.Fatalf("重试激活应认领孤儿行自愈，got %v（今日＝确定性 SLUG_DUPLICATE 永久循环）", err)
	}

	var row struct {
		Status      string
		OwnerUserID int64
	}
	if err := app.DB.Table("tenants").Where("id = ?", orphan.ID).
		Select("status, owner_user_id").Take(&row).Error; err != nil {
		t.Fatalf("reload orphan: %v", err)
	}
	if row.OwnerUserID != ownerID || row.Status != string(tenant.StatusActive) {
		t.Fatalf("孤儿行 = (owner %d, %q), want (%d, active)（认领未归属）", row.OwnerUserID, row.Status, ownerID)
	}
	var wallets int64
	if err := app.DB.Table("agent_wallets").Where("tenant_id = ?", orphan.ID).Count(&wallets).Error; err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	if wallets != 1 {
		t.Fatalf("agent_wallets = %d, want 1", wallets)
	}
	// 崩溃现场无域名行：L1 认领须补派生（审计 snippet 漏了这步——否则付独立档钱没有站）。
	if tn, derr := app.TenantRepo.GetTenantByDomain(ctx, tenant.DomainForSlug("agent901")); derr != nil || tn.ID != orphan.ID {
		t.Fatalf("认领后 L1 域名解析 = (%v, %v), want 命中 %d", tn, derr, orphan.ID)
	}
	var ord agentPlanOrderRow
	if err := app.DB.Take(&ord, "order_no = ?", "AGTORPH1").Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if ord.Status != agtOrderActivated || ord.AgentTenantID != orphan.ID {
		t.Fatalf("order = (%q, tenant %d), want (activated, %d)", ord.Status, ord.AgentTenantID, orphan.ID)
	}
}

// 次级崩溃现场：认领并写归属后再崩 → 行「已归属但未开通」（无钱包），重试走「已是代理→升级」分支。
// 该分支此前不 EnsureWallet → 无钱包代理（分润无处入账）。断言升级分支幂等补建钱包。
func TestActivate_HealsAttributedButUnprovisionedTenant(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	tid := seedAgentTenant(t, app, ownerID, "half-shop", tenant.StatusActive) // 已归属、active、无钱包
	seedPendingAgentOrderLevel(t, app, "AGTHEAL1", ownerID, 1, 2999)

	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTHEAL1", 2999); err != nil {
		t.Fatalf("半开通行重试激活: %v", err)
	}
	var wallets int64
	if err := app.DB.Table("agent_wallets").Where("tenant_id = ?", tid).Count(&wallets).Error; err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	if wallets != 1 {
		t.Fatalf("agent_wallets = %d, want 1（升级分支须幂等补建，兜『归属后崩溃』重试）", wallets)
	}
}

// 认领谓词负测试：slug 被**他人已归属**的行占用＝真冲突而非崩溃现场——绝不认领（不得篡改他人租户
// 归属），维持 SLUG_DUPLICATE 让订单留在卡单页可见。
func TestActivate_NeverClaimsOwnedTenantRow(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	other := seedAgentTenant(t, app, 977, "agent901", tenant.StatusActive) // 他人占着买家的派生 slug
	seedPendingNewAgentOrder(t, app, "AGTSTEAL1", ownerID)

	err := app.ActivatePaidAgentPlanOrder(ctx, "AGTSTEAL1", 6.90)
	if !apperr.Is(err, "SLUG_DUPLICATE") {
		t.Fatalf("他人已归属行 = %v, want SLUG_DUPLICATE（绝不认领）", err)
	}
	var owner int64
	if err := app.DB.Table("tenants").Where("id = ?", other).
		Select("owner_user_id").Take(&owner).Error; err != nil {
		t.Fatalf("reload other: %v", err)
	}
	if owner != 977 {
		t.Fatalf("他人租户归属被篡改为 %d, want 977", owner)
	}
	var ord agentPlanOrderRow
	if err := app.DB.Take(&ord, "order_no = ?", "AGTSTEAL1").Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if ord.Status == agtOrderActivated {
		t.Fatalf("真冲突订单不得 activated（应留 pending 卡单可见）")
	}
	_ = ctx
}
