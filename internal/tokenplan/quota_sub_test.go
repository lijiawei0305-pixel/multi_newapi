package tokenplan

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/platform/quota"
)

func TestSubscriptionQuotaChargeSuccess(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	sub := activeSub(repo, clock, 11, 7, 100, 30)
	q, ok := NewQuotaFactory(repo, clock).For(appctx.Principal{UserID: 11, TenantID: 7})
	if !ok {
		t.Fatal("active user must yield a subscription source")
	}

	r, err := q.Charge(ctx, 30)
	if err != nil {
		t.Fatalf("Charge: %v", err)
	}
	if r.Kind != quota.KindSubscription || r.ChargedUSD != 30 || r.RemainingUSD != 70 {
		t.Fatalf("receipt = %+v, want {subscription 30 70}", r)
	}
	if b, _ := q.Balance(ctx); b != 70 {
		t.Fatalf("balance=%v want 70", b)
	}
	_ = sub
}

func TestSubscriptionQuotaChargeExhausted(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	activeSub(repo, clock, 11, 7, 100, 30)
	q, _ := NewQuotaFactory(repo, clock).For(appctx.Principal{UserID: 11, TenantID: 7})

	if _, err := q.Charge(ctx, 100); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if _, err := q.Charge(ctx, 1); apperr.CodeOf(err) != CodeSubscriptionExhausted {
		t.Fatalf("want SUBSCRIPTION_EXHAUSTED, got %v", err)
	}
}

func TestSubscriptionQuotaChargeExpired(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	activeSub(repo, clock, 11, 7, 100, 30)
	q, _ := NewQuotaFactory(repo, clock).For(appctx.Principal{UserID: 11, TenantID: 7})

	clock.advance(31 * 24 * time.Hour)
	if _, err := q.Charge(ctx, 1); apperr.CodeOf(err) != CodeSubscriptionExpired {
		t.Fatalf("want SUBSCRIPTION_EXPIRED, got %v", err)
	}
}

func TestSubscriptionQuotaChargeRejectsBadCost(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	activeSub(repo, clock, 11, 7, 100, 30)
	q, _ := NewQuotaFactory(repo, clock).For(appctx.Principal{UserID: 11, TenantID: 7})
	if _, err := q.Charge(ctx, -1); apperr.CodeOf(err) != CodeAmountInvalid {
		t.Fatalf("negative cost want TOKENPLAN_AMOUNT_INVALID, got %v", err)
	}
}

func TestNilClockDefaultsToSystem(t *testing.T) {
	// 未注入时钟时回退真实时钟（覆盖 orSystemClock 默认分支 + systemClock.Now）。
	repo := NewMemRepo()
	svc := NewSubscriptionService(repo, repo, newFakePayment(), &fakeRisk{}, newFakeEarnings(), nil)
	if has, err := svc.HasActive(context.Background(), 1); err != nil || has {
		t.Fatalf("empty repo with system clock: has=%v err=%v", has, err)
	}
	if q, ok := NewQuotaFactory(repo, nil).For(appctx.Principal{UserID: 1}); ok || q != nil {
		t.Fatalf("no active with system clock: want (nil,false)")
	}
}

func TestFactoryForNoActive(t *testing.T) {
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	if q, ok := NewQuotaFactory(repo, clock).For(appctx.Principal{UserID: 99, TenantID: 7}); ok || q != nil {
		t.Fatalf("no active sub must yield (nil,false), got (%v,%v)", q, ok)
	}
}

func TestFactorySubscriptionSource(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	activeSub(repo, clock, 11, 7, 100, 30)
	f := NewQuotaFactory(repo, clock)

	q, err := f.SubscriptionSource(ctx, 11, 7)
	if err != nil || q == nil {
		t.Fatalf("SubscriptionSource active: q=%v err=%v", q, err)
	}
	// 无 active → SUBSCRIPTION_EXPIRED（竞态保护）。
	if _, err := f.SubscriptionSource(ctx, 99, 7); apperr.CodeOf(err) != CodeSubscriptionExpired {
		t.Fatalf("no active want SUBSCRIPTION_EXPIRED, got %v", err)
	}
}

func TestBalanceNotFound(t *testing.T) {
	// 直接构造一个绑定到不存在订阅的桶，Balance 应上浮 SUBSCRIPTION_NOT_FOUND。
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	q := (&quotaFactory{repo: repo, clock: clock}).bind(&Subscription{ID: 777, MonthLimitUSD: 100})
	if _, err := q.Balance(context.Background()); apperr.CodeOf(err) != CodeSubscriptionNotFound {
		t.Fatalf("want SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
}

// TestSubscriptionQuotaConcurrentNoOverrun 是核心并发用例：N goroutine 并发计量，
// 断言不击穿 month_limit（§6.2）。配合 `go test -race` 检测数据竞争。
func TestSubscriptionQuotaConcurrentNoOverrun(t *testing.T) {
	ctx := context.Background()
	const (
		monthLimit = 100.0
		cost       = 1.0
		workers    = 500 // 远多于可成功次数(100)，制造激烈竞争
	)
	clock := newFakeClock(epoch)
	repo := NewMemRepo()
	sub := activeSub(repo, clock, 11, 7, monthLimit, 30)
	q, _ := NewQuotaFactory(repo, clock).For(appctx.Principal{UserID: 11, TenantID: 7})

	var (
		success   int64
		exhausted int64
		wg        sync.WaitGroup
		gate      = make(chan struct{})
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-gate
			_, err := q.Charge(ctx, cost)
			switch {
			case err == nil:
				atomic.AddInt64(&success, 1)
			case apperr.Is(err, CodeSubscriptionExhausted):
				atomic.AddInt64(&exhausted, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(gate)
	wg.Wait()

	got, _ := repo.GetByID(ctx, sub.ID)
	// 1) 不击穿：used 不超过 month_limit。
	if got.UsedUSD > monthLimit {
		t.Fatalf("breached! used=%v > limit=%v", got.UsedUSD, monthLimit)
	}
	// 2) 恰好 monthLimit/cost 次成功。
	if success != int64(monthLimit/cost) {
		t.Fatalf("success=%d want %d", success, int64(monthLimit/cost))
	}
	// 3) 守恒：成功累加 == used。
	if float64(success)*cost != got.UsedUSD {
		t.Fatalf("conservation broken: charged=%v used=%v", float64(success)*cost, got.UsedUSD)
	}
	// 4) 其余全部超额被拒。
	if success+exhausted != workers {
		t.Fatalf("accounted=%d want %d", success+exhausted, workers)
	}
	// 5) 用满后置 exhausted 终态。
	if got.Status != SubExhausted {
		t.Fatalf("after full use status=%s want exhausted", got.Status)
	}
	// 6) 计量日志条数==成功次数。
	if logs := repo.UsageLogs(sub.ID); int64(len(logs)) != success {
		t.Fatalf("usage logs=%d want %d", len(logs), success)
	}
}
