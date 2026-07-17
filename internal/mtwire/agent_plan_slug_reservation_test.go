package mtwire

// AGT slug 预留式闭环（audit 2026-07-17 发现#5）：预检不再是 check-then-act 的快照查重，而是在
// **下单收钱前**直接插入软删态 tenants 占位行，让 idx_tenants_slug 唯一索引在钱动之前仲裁——
// 「预检通过 ⇒ 激活必成（就 slug 而言）」从注释愿望变成索引担保。覆盖：
//   触发(a) 两买家同 slug：第二人付款前即拒（此前双双扣款、仅一人能激活）；
//   触发(b) slug 的派生域名已被他租户占用（AddSubdomain 解耦 slug 与域名）：付款前即拒
//          （此前付款后 CreateDomain 撞 tenant_domains 唯一键 + 先插的 tenants 行成毒丸孤儿）；
//   预留行激活＝完整开通（钱包/owner 自用归位——revive 分支此前只服务「曾完整开通过的旧站」）；
//   未付款订单对账过期 → 预留释放、slug 可再售；同 owner 弃单重试的兄弟单仍 pending 时不提前释放。

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// stubAgtUnpaid 把对账查单桩成「确认未付」，测试结束自动还原。
func stubAgtUnpaid(t *testing.T) {
	t.Helper()
	orig := agtOrderPaidQuery
	agtOrderPaidQuery = func(*App, context.Context, string, string) (bool, error) { return false, nil }
	t.Cleanup(func() { agtOrderPaidQuery = orig })
}

// 触发(a)：owner1 预检成功即预留占位；owner2 同 slug 预检必须在下单收钱前被拒——
// 此前两人都过检、都下单扣款、仅一人激活，另一人 ¥9990 打水漂（audit 发现#5 主触发）。
func TestAgentSlugReservation_SecondBuyerRejectedBeforePayment(t *testing.T) {
	app, _ := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	rid, err := app.precheckAndReserveAgentSlug(ctx, 910, "hot-shop", "")
	if err != nil {
		t.Fatalf("owner1 precheck: %v", err)
	}
	if rid <= 0 {
		t.Fatalf("owner1 应获得预留租户行（id>0），got %d", rid)
	}
	if _, err := app.precheckAndReserveAgentSlug(ctx, 911, "hot-shop", ""); !apperr.Is(err, "SLUG_DUPLICATE") {
		t.Fatalf("owner2 同 slug 预检 = %v, want SLUG_DUPLICATE（付款前拒绝，不得下单扣款）", err)
	}
}

// 触发(b)：管理员经 AddSubdomain 把 beta-shop.wedreamhub.com 指给 slug=alpha-shop 的租户——域名与
// slug 解耦。预检只查 tenants.slug 时该单放行收款，激活 CreateDomain 必撞 tenant_domains.domain
// 唯一索引（且先插的 tenants 行成无主毒丸）。预检域名口径必须与激活一致、付款前拒绝。
func TestAgentSlugReservation_DomainConflictRejectedBeforePayment(t *testing.T) {
	app, _ := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	tid := seedAgentTenant(t, app, 915, "alpha-shop", tenant.StatusActive)
	if _, _, err := app.TenantService.AddSubdomain(ctx, tid, "beta-shop"); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	if _, err := app.precheckAndReserveAgentSlug(ctx, 916, "beta-shop", ""); !apperr.Is(err, "SLUG_DUPLICATE") {
		t.Fatalf("派生域名已被他租户占用的 slug 预检 = %v, want SLUG_DUPLICATE（否则付款后必撞 tenant_domains 唯一键）", err)
	}
}

// 预留行激活必须**完整开通**：revive 分支此前只服务「曾完整开通过的旧站」（不 EnsureWallet、不归位
// owner 自用 tenant_id）——预留行从未开通过，直走 revive 会产出无钱包代理。断言激活后：预留行翻
// active、钱包已建、owner 自用落主站（users.tenant_id=0）、L1 子域名派生、订单回填预留行 id。
func TestAgentSlugReservation_ActivationFullyProvisionsReservedTenant(t *testing.T) {
	app, _ := newAgentPlanActivateTestApp(t)
	ctx := context.Background()

	const owner = int64(920)
	if err := app.DB.Exec(`INSERT INTO users (id, tenant_id) VALUES (?, 5)`, owner).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	rid, err := app.precheckAndReserveAgentSlug(ctx, owner, "resv-shop", "预留小店")
	if err != nil || rid <= 0 {
		t.Fatalf("reserve = (%d, %v), want (>0, nil)", rid, err)
	}
	if err := app.DB.Create(&agentPlanOrderRow{
		OrderNo: "AGTRESV1", OwnerUserID: owner, PlanID: 2, PlanCode: "oem",
		AmountCNY: 2999, Status: agtOrderPending, GrantLevel: 1, ValidDays: 365,
		Slug: "resv-shop", Name: "预留小店", AgentTenantID: rid,
	}).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if err := app.ActivatePaidAgentPlanOrder(ctx, "AGTRESV1", 2999); err != nil {
		t.Fatalf("activate reserved: %v", err)
	}

	var row struct {
		Status      string
		OwnerUserID int64
	}
	if err := app.DB.Table("tenants").Where("id = ?", rid).
		Select("status, owner_user_id").Take(&row).Error; err != nil {
		t.Fatalf("reload reservation: %v", err)
	}
	if row.Status != string(tenant.StatusActive) || row.OwnerUserID != owner {
		t.Fatalf("预留行 = (%q, owner %d)，want (active, %d)", row.Status, row.OwnerUserID, owner)
	}
	var wallets int64
	if err := app.DB.Table("agent_wallets").Where("tenant_id = ?", rid).Count(&wallets).Error; err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	if wallets != 1 {
		t.Fatalf("agent_wallets = %d 行, want 1（预留行激活必须建钱包，否则分润无处入账）", wallets)
	}
	var userTenant int64
	if err := app.DB.Table("users").Where("id = ?", owner).
		Select("tenant_id").Take(&userTenant).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if userTenant != 0 {
		t.Fatalf("users.tenant_id = %d, want 0（owner 自用落主站基准）", userTenant)
	}
	if tn, derr := app.TenantRepo.GetTenantByDomain(ctx, tenant.DomainForSlug("resv-shop")); derr != nil || tn.ID != rid {
		t.Fatalf("L1 子域名解析 = (%v, %v), want 命中预留行 %d", tn, derr, rid)
	}
	var ord agentPlanOrderRow
	if err := app.DB.Take(&ord, "order_no = ?", "AGTRESV1").Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if ord.Status != agtOrderActivated || ord.AgentTenantID != rid {
		t.Fatalf("order = (%q, tenant %d), want (activated, %d)", ord.Status, ord.AgentTenantID, rid)
	}
}

// 未付款订单对账过期必须释放预留——否则「点开收银台没付钱」即永久占用 slug、无任何释放通道
// （C4 家族「只增不删」）。释放后 slug 立即可再售。
func TestAgentSlugReservation_ExpiredOrderReleasesSlug(t *testing.T) {
	app, _ := newAgentPlanActivateTestApp(t)
	ctx := context.Background()
	stubAgtUnpaid(t)

	rid, err := app.precheckAndReserveAgentSlug(ctx, 925, "left-shop", "")
	if err != nil || rid <= 0 {
		t.Fatalf("reserve = (%d, %v), want (>0, nil)", rid, err)
	}
	old := time.Now().Add(-3 * time.Hour) // 超 agtReconcileExpireAge(2h)：确认未付 → 终态过期
	if err := app.DB.Create(&agentPlanOrderRow{
		OrderNo: "AGTLEFT1", OwnerUserID: 925, PlanID: 2, PlanCode: "oem",
		AmountCNY: 2999, Status: agtOrderPending, GrantLevel: 1, ValidDays: 365,
		Slug: "left-shop", AgentTenantID: rid, CreatedAt: old, UpdatedAt: old,
	}).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}

	res, err := app.ReconcileStuckAgentPlans(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Expired) != 1 || res.Expired[0] != "AGTLEFT1" {
		t.Fatalf("Expired = %v, want [AGTLEFT1]", res.Expired)
	}
	var n int64
	if err := app.DB.Table("tenants").Where("slug = ?", "left-shop").Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("过期后预留行仍在（%d 行）——弃单永久占用 slug", n)
	}
	if rid2, rerr := app.precheckAndReserveAgentSlug(ctx, 926, "left-shop", ""); rerr != nil || rid2 <= 0 {
		t.Fatalf("释放后再售 = (%d, %v), want (>0, nil)", rid2, rerr)
	}
}

// 同 owner 弃单重试（O1 带预留、O2 经复活路径同锚）：O1 先过期时**不得**释放预留——O2 仍 pending
// 且要靠它复活，若提前释放，他人抢注后 O2 已付款却撞键成付款黑洞。O2 也过期后才释放。
func TestAgentSlugReservation_NotReleasedWhileSiblingOrderPending(t *testing.T) {
	app, _ := newAgentPlanActivateTestApp(t)
	ctx := context.Background()
	stubAgtUnpaid(t)

	const owner = int64(930)
	rid, err := app.precheckAndReserveAgentSlug(ctx, owner, "twice-shop", "")
	if err != nil || rid <= 0 {
		t.Fatalf("reserve = (%d, %v)", rid, err)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := app.DB.Create(&agentPlanOrderRow{
		OrderNo: "AGTTWICE1", OwnerUserID: owner, PlanID: 2, PlanCode: "oem", AmountCNY: 2999,
		Status: agtOrderPending, GrantLevel: 1, ValidDays: 365, Slug: "twice-shop",
		AgentTenantID: rid, CreatedAt: old, UpdatedAt: old,
	}).Error; err != nil {
		t.Fatalf("seed O1: %v", err)
	}
	// 买家重试：复活路径（owner 名下已有预留占位）→ 必须返回同一预留锚点，令 O2 也引用它。
	rid2, err := app.precheckAndReserveAgentSlug(ctx, owner, "whatever-else", "")
	if err != nil {
		t.Fatalf("retry precheck: %v", err)
	}
	if rid2 != rid {
		t.Fatalf("重试预检锚点 = %d, want 同预留 %d（否则 O1 过期即释放、O2 已付却可能撞键）", rid2, rid)
	}
	if err := app.DB.Create(&agentPlanOrderRow{
		OrderNo: "AGTTWICE2", OwnerUserID: owner, PlanID: 2, PlanCode: "oem", AmountCNY: 2999,
		Status: agtOrderPending, GrantLevel: 1, ValidDays: 365, Slug: "twice-shop",
		AgentTenantID: rid2,
	}).Error; err != nil {
		t.Fatalf("seed O2: %v", err)
	}

	// 第一轮：只有 O1 落进扫描窗（O2 刚建、updated_at 晚于 before）→ O1 过期，但预留必须存活。
	if _, err := app.ReconcileStuckAgentPlans(ctx, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("reconcile #1: %v", err)
	}
	var n int64
	if err := app.DB.Table("tenants").Where("id = ?", rid).Count(&n).Error; err != nil {
		t.Fatalf("count #1: %v", err)
	}
	if n != 1 {
		t.Fatalf("O1 过期后预留被提前释放（兄弟单 O2 仍 pending、已付将撞键）")
	}

	// 把 O2 也推入过期窗 → 第二轮才释放。
	if err := app.DB.Model(&agentPlanOrderRow{}).Where("order_no = ?", "AGTTWICE2").
		Updates(map[string]any{"created_at": old, "updated_at": old}).Error; err != nil {
		t.Fatalf("backdate O2: %v", err)
	}
	if _, err := app.ReconcileStuckAgentPlans(ctx, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("reconcile #2: %v", err)
	}
	if err := app.DB.Table("tenants").Where("id = ?", rid).Count(&n).Error; err != nil {
		t.Fatalf("count #2: %v", err)
	}
	if n != 0 {
		t.Fatalf("全部订单过期后预留仍未释放——弃单永久占用 slug")
	}
}
