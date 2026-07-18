package tokenplan

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// fakeClock 是可注入的假时钟（并发安全），支持设定与推进时间，便于到期/计量用例。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeGuard 是 PricingGuard 的内存假实现（复刻 pricing.guard 的纯逻辑）。
// 可注入 forceErr 强制返回错误，覆盖 SetListing 的保护线映射分支。
type fakeGuard struct {
	forceErr error
	calls    int
}

func (g *fakeGuard) ValidateRetailPrice(retail, cost, minMargin float64) error {
	g.calls++
	if g.forceErr != nil {
		return g.forceErr
	}
	if retail < cost*(1+minMargin) {
		return apperr.New("PRICE_BELOW_PROTECTION", "零售价击穿成本保护线", http.StatusBadRequest)
	}
	return nil
}

// fakePayment 是 PaymentGateway 的内存假实现：记录下单入参，可注入错误与固定订单号。
type fakePayment struct {
	mu      sync.Mutex
	err     error
	nextID  int
	fixedID string
	orders  []OrderInput
}

func newFakePayment() *fakePayment { return &fakePayment{} }

func (p *fakePayment) CreateOrder(_ context.Context, in OrderInput) (*PayOrder, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	p.orders = append(p.orders, in)
	id := p.fixedID
	if id == "" {
		p.nextID++
		id = "ord-" + itoa(p.nextID)
	}
	return &PayOrder{OrderID: id, PayURL: "https://pay.example/" + id}, nil
}

func (p *fakePayment) lastOrder() (OrderInput, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.orders) == 0 {
		return OrderInput{}, false
	}
	return p.orders[len(p.orders)-1], true
}

// fakeRisk 是 RiskEngine 的内存假实现：默认放行，deny=true 返回 PURCHASE_LIMIT_EXCEEDED。
type fakeRisk struct {
	deny         bool
	err          error
	calls        int
	last         PurchaseLimitCheck
	releaseCalls int                  // ReleasePurchaseClaim 被调用次数（补偿断言用，audit F2）
	released     []PurchaseLimitCheck // 每次归还的入参快照
}

func (r *fakeRisk) CheckPurchaseLimit(_ context.Context, in PurchaseLimitCheck) error {
	r.calls++
	r.last = in
	if r.err != nil {
		return r.err
	}
	if r.deny {
		return apperr.New(CodePurchaseLimitExceeded, "已达套餐限购次数", http.StatusConflict)
	}
	return nil
}

// ReleasePurchaseClaim 记录归还调用（供 Purchase 两级补偿的单测断言）。
func (r *fakeRisk) ReleasePurchaseClaim(_ context.Context, in PurchaseLimitCheck) error {
	r.releaseCalls++
	r.released = append(r.released, in)
	return nil
}

// fakeEarnings 是 EarningSink 的内存假实现（并发安全）：记录每次调用与去重后条目。
type fakeEarnings struct {
	mu      sync.Mutex
	err     error
	calls   int
	entries []EarningEntry
}

func newFakeEarnings() *fakeEarnings { return &fakeEarnings{} }

// errEarningInjected 供用例注入 AddEarning 失败（模拟瞬时 DB 错误），验证重试补记（安全审计 M1）。
var errEarningInjected = errors.New("fakeEarnings: injected failure")

func (e *fakeEarnings) AddEarning(_ context.Context, en EarningEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if e.err != nil {
		return e.err
	}
	// 模型化真实 EarningSink 的幂等（agent_earning_logs.idem_key UNIQUE(tenant,source_type,source_id)
	// + ON CONFLICT DO NOTHING）：同 (SourceType, SourceID) 只落一条，多次调用不重复记账。
	for _, x := range e.entries {
		if x.SourceType == en.SourceType && x.SourceID == en.SourceID {
			return nil
		}
	}
	e.entries = append(e.entries, en)
	return nil
}

// setErr 注入/清除后续 AddEarning 的失败（并发安全）。
func (e *fakeEarnings) setErr(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *fakeEarnings) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.entries)
}

func (e *fakeEarnings) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *fakeEarnings) last() (EarningEntry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.entries) == 0 {
		return EarningEntry{}, false
	}
	return e.entries[len(e.entries)-1], true
}

// itoa 是无 strconv 依赖的小整数转字符串（仅供假订单号）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// seedPlanInto 直接经 PlanRepo 落一个套餐并返回其 ID（购买/上架用例的前置）。
func seedPlanInto(repo PlanRepo, in PlanInput) *Plan {
	c := NewCatalog(repo)
	p, err := c.Create(context.Background(), in)
	if err != nil {
		panic(err)
	}
	return p
}

// basePlanInput 返回一份合法的套餐入参（测试基线，可按需改字段）。
func basePlanInput() PlanInput {
	return PlanInput{
		Code:           "solo",
		Name:           "Solo",
		BasePrice:      279,
		AnchorPrice:    3400,
		DiscountLabel:  "-92%",
		Multiplier:     1.0,
		MonthLimitUSD:  560,
		ValidDays:      30,
		AgentCostPrice: 200,
		MinPrice:       220,
		Status:         PlanEnabled,
	}
}
