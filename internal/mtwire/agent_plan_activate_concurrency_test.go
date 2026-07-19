package mtwire

// 代理套餐激活并发回归测试(修 M5:provision 先于 CAS,并发双回调或致孤儿租户)。
// 契约:同一新代理订单被并发回调多次激活,只建一个租户、一条会员台账、订单幂等 activated,
// 且无回调因撞 tenants.slug 唯一键而报错(靠 owner 串行化,而非撞键+重试)。

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAgentPlanActivateTestApp 造「新代理激活」最小 App:真实 sqlite + 真实 TenantService(能真正建
// 租户并受 tenants.slug 唯一键约束),供并发建租户竞态测试。返回 (app, ownerID)。
func newAgentPlanActivateTestApp(t *testing.T) (*App, int64) {
	t.Helper()
	// TranslateError:true → gorm 把唯一键冲突翻成 tenant.ErrSlugDuplicate(Create 依赖)。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// :memory: 必须单连接,否则每连接是独立库;并发 goroutine 仍在语句间交错(竞态得以复现)。
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
	if err := db.AutoMigrate(&agentMembershipRow{}, &agentMembershipGrantRow{}, &agentPlanOrderRow{}); err != nil {
		t.Fatalf("agt migrate: %v", err)
	}
	// provisionAgentFromOrder 新代理分支会 UPDATE users.tenant_id=0(owner 自用落主站)。
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}

	ar := agentrepo.New(db)
	pr := promotionrepo.New(db)
	tr := tenantrepo.New(db)
	app := &App{
		DB:            db,
		TenantRepo:    tr,
		TenantService: tenant.NewService(tr, tenant.NewSlugValidator()), // 真实建租户(受唯一 slug 约束)
		AgentRepo:     ar,
		AgentService:  agent.NewService(ar, nil),
		PromotionRepo: pr,
		Promotion:     promotion.NewService(pr),
	}

	const ownerID = int64(901)
	if err := db.Exec(`INSERT INTO users (id, tenant_id) VALUES (?, 0)`, ownerID).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return app, ownerID
}

type failingNewAgentTenantService struct {
	err error
}

func (s failingNewAgentTenantService) Get(context.Context, int64) (*tenant.Tenant, error) {
	return nil, s.err
}
func (s failingNewAgentTenantService) Create(context.Context, tenant.CreateTenantInput) (*tenant.Tenant, error) {
	return nil, s.err
}
func (s failingNewAgentTenantService) EnsureSubdomain(context.Context, int64, string) error {
	return s.err
}
func (s failingNewAgentTenantService) AddSubdomain(context.Context, int64, string) (string, []string, error) {
	return "", nil, s.err
}
func (s failingNewAgentTenantService) SetStatus(context.Context, int64, tenant.TenantStatus) error {
	return s.err
}

func TestActivatePaidAgentPlanOrderClaimsBeforeProvisionAndRecoversStaleClaim(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	const orderNo = "AGT-claim-recovery"
	seedPendingNewAgentOrder(t, app, orderNo, ownerID)
	originalTenantService := app.TenantService
	provisionErr := errors.New("tenant provisioning unavailable")
	app.TenantService = failingNewAgentTenantService{err: provisionErr}

	err := app.ActivatePaidAgentPlanOrder(context.Background(), orderNo, 6.90)
	require.ErrorIs(t, err, provisionErr)
	var claimed agentPlanOrderRow
	require.NoError(t, app.DB.Take(&claimed, "order_no = ?", orderNo).Error)
	assert.Equal(t, agtOrderActivating, claimed.Status, "durable claim must precede provisioning side effects")
	var tenantCount int64
	require.NoError(t, app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).Count(&tenantCount).Error)
	assert.Zero(t, tenantCount)

	// 模拟进程在认领后崩溃：超过恢复租约后，新调用方先 CAS 回 pending，再重新认领并完整交付。
	require.NoError(t, app.DB.Model(&agentPlanOrderRow{}).Where("order_no = ?", orderNo).
		Update("updated_at", time.Now().Add(-2*reconcileMinAge)).Error)
	app.TenantService = originalTenantService
	require.NoError(t, app.ActivatePaidAgentPlanOrder(context.Background(), orderNo, 0))
	require.NoError(t, app.DB.Take(&claimed, "order_no = ?", orderNo).Error)
	assert.Equal(t, agtOrderActivated, claimed.Status)
	assert.NotZero(t, claimed.AgentTenantID)
}

// seedPendingNewAgentOrder 造一笔 pending 的 L0 新代理套餐订单(空 slug → 派生 agent<owner>)。
func seedPendingNewAgentOrder(t *testing.T, app *App, orderNo string, ownerID int64) {
	t.Helper()
	ord := &agentPlanOrderRow{
		OrderNo: orderNo, OwnerUserID: ownerID, PlanID: 1, PlanCode: "starter",
		AmountCNY: 6.90, Status: agtOrderPending,
		GrantLevel: 0, GrantCanAPI: false, GrantDiscountRatio: 0, ValidDays: 365,
	}
	if err := app.DB.Create(ord).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

// TestActivatePaidAgentPlanOrder_ConcurrentCallbacksProvisionOnce 是 M5 的核心回归:
// 同一新代理订单被 8 个并发回调激活,断言只建一个租户、一条会员、订单 activated、且无回调报错。
func TestActivatePaidAgentPlanOrder_ConcurrentCallbacksProvisionOnce(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	const orderNo = "AGTCONC01"
	seedPendingNewAgentOrder(t, app, orderNo, ownerID)

	const n = 8
	start := make(chan struct{}) // 屏障:让 8 个 goroutine 尽量同时冲入,放大竞态窗口
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			errs[idx] = app.ActivatePaidAgentPlanOrder(context.Background(), orderNo, 6.90)
		}(i)
	}
	close(start)
	wg.Wait()

	// 全部回调应返回 nil:赢家 provision,其余在临界区内重读 activated 幂等短路——不得有撞唯一键的错误。
	for i, e := range errs {
		if e != nil {
			t.Fatalf("callback %d returned error %v; 并发回调不应撞 slug 唯一键(应被 owner 串行化拦下)", i, e)
		}
	}

	// 只建一个租户。
	var tenantCount int64
	if err := app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).Count(&tenantCount).Error; err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if tenantCount != 1 {
		t.Fatalf("tenant count = %d, want 1(并发双 provision 产生了孤儿租户)", tenantCount)
	}

	// 只一条会员台账。
	var membCount int64
	if err := app.DB.Model(&agentMembershipRow{}).Count(&membCount).Error; err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if membCount != 1 {
		t.Fatalf("membership count = %d, want 1", membCount)
	}

	// 订单幂等 activated。
	var ord agentPlanOrderRow
	if err := app.DB.Take(&ord, "order_no = ?", orderNo).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if ord.Status != agtOrderActivated {
		t.Fatalf("order status = %q, want activated", ord.Status)
	}
	if ord.AgentTenantID == 0 {
		t.Fatalf("order.agent_tenant_id 未回填")
	}
}

// TestActivatePaidAgentPlanOrder_ConcurrentSameOwnerTwoOrders 覆盖 finding 提到的另一面:
// 同一 owner 的**两笔不同订单**并发激活(空 slug 都派生同一 agent<owner>)。owner 锁串行后:
// 赢家建租户、输家走 agentTenantByOwner 命中→升级同一租户,断言仍只一个租户、两单皆 activated、无撞键错误。
func TestActivatePaidAgentPlanOrder_ConcurrentSameOwnerTwoOrders(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	startedAt := time.Now()
	seedPendingNewAgentOrder(t, app, "AGTSAME01", ownerID)
	seedPendingNewAgentOrder(t, app, "AGTSAME02", ownerID)

	orders := []string{"AGTSAME01", "AGTSAME02"}
	start := make(chan struct{})
	errs := make([]error, len(orders))
	var wg sync.WaitGroup
	wg.Add(len(orders))
	for i, no := range orders {
		go func(idx int, orderNo string) {
			defer wg.Done()
			<-start
			errs[idx] = app.ActivatePaidAgentPlanOrder(context.Background(), orderNo, 6.90)
		}(i, no)
	}
	close(start)
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("order %s 激活报错 %v; 同 owner 多单并发不应撞 slug 唯一键", orders[i], e)
		}
	}
	var tenantCount int64
	if err := app.DB.Table("tenants").Where("owner_user_id = ?", ownerID).Count(&tenantCount).Error; err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if tenantCount != 1 {
		t.Fatalf("tenant count = %d, want 1(同 owner 多单并发产生了孤儿租户)", tenantCount)
	}
	for _, no := range orders {
		var ord agentPlanOrderRow
		if err := app.DB.Take(&ord, "order_no = ?", no).Error; err != nil {
			t.Fatalf("reload %s: %v", no, err)
		}
		if ord.Status != agtOrderActivated {
			t.Fatalf("order %s status = %q, want activated", no, ord.Status)
		}
	}
	var membership agentMembershipRow
	require.NoError(t, app.DB.Take(&membership).Error)
	assert.WithinDuration(t, startedAt.AddDate(0, 0, 730), membership.ExpireAt, 5*time.Second,
		"two distinct 365-day orders must accumulate instead of overwriting expiry")
	var grants int64
	require.NoError(t, app.DB.Model(&agentMembershipGrantRow{}).Count(&grants).Error)
	assert.Equal(t, int64(2), grants)
}

// TestLockAgentActivation_SerializesSameOwner 确定性验证条带锁对同一 owner 互斥:
// 100 个 goroutine 在锁内自增非原子计数,若锁失效则 -race 报数据竞争 / 计数丢失。
func TestLockAgentActivation_SerializesSameOwner(t *testing.T) {
	const ownerID = int64(5)
	const n = 100
	counter := 0
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			unlock := lockAgentActivation(ownerID)
			counter++ // 受锁保护的非原子自增
			unlock()
		}()
	}
	wg.Wait()
	if counter != n {
		t.Fatalf("counter = %d, want %d(锁未能串行化同一 owner)", counter, n)
	}
}
