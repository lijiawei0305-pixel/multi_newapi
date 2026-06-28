package wallet

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
	"newapi-mt/internal/platform/quota"
)

func walletFor(t *testing.T, repo WalletRepo, tenantID, userID int64) quota.Source {
	t.Helper()
	q := NewQuotaFactory(repo).For(appctx.Principal{TenantID: tenantID, UserID: userID})
	if q == nil {
		t.Fatal("factory returned nil source")
	}
	return q
}

func TestWalletQuotaChargeSuccess(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	_ = repo.AddBalance(ctx, 1, 1, 100)
	q := walletFor(t, repo, 1, 1)

	r, err := q.Charge(ctx, 30)
	if err != nil {
		t.Fatalf("Charge: %v", err)
	}
	if r.Kind != quota.KindWallet || r.ChargedUSD != 30 || r.RemainingUSD != 70 {
		t.Fatalf("receipt = %+v, want {wallet 30 70}", r)
	}
	if b, _ := q.Balance(ctx); b != 70 {
		t.Fatalf("balance = %v, want 70", b)
	}
}

func TestWalletQuotaChargeExactBalance(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	_ = repo.AddBalance(ctx, 1, 1, 50)
	q := walletFor(t, repo, 1, 1)

	r, err := q.Charge(ctx, 50) // 恰好扣空，余额 0，放行
	if err != nil {
		t.Fatalf("Charge exact: %v", err)
	}
	if r.RemainingUSD != 0 {
		t.Fatalf("remaining = %v, want 0", r.RemainingUSD)
	}
}

func TestWalletQuotaChargeInsufficient(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	_ = repo.AddBalance(ctx, 1, 1, 20)
	q := walletFor(t, repo, 1, 1)

	if _, err := q.Charge(ctx, 20.01); apperr.CodeOf(err) != CodeQuotaInsufficient {
		t.Fatalf("code = %q, want %q", apperr.CodeOf(err), CodeQuotaInsufficient)
	}
	// 余额不变。
	if b, _ := q.Balance(ctx); b != 20 {
		t.Fatalf("balance after failed charge = %v, want 20 (unchanged)", b)
	}
}

func TestWalletQuotaChargeRejectsBadCost(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	_ = repo.AddBalance(ctx, 1, 1, 20)
	q := walletFor(t, repo, 1, 1)

	if _, err := q.Charge(ctx, -1); apperr.CodeOf(err) != CodeAmountInvalid {
		t.Fatalf("negative cost code = %q, want %q", apperr.CodeOf(err), CodeAmountInvalid)
	}
	if b, _ := q.Balance(ctx); b != 20 {
		t.Fatalf("balance must be unchanged after rejected charge, got %v", b)
	}
}

// TestWalletQuotaConcurrentNoOverdraw 是核心并发用例：N goroutine 并发扣减，断言不透支。
// 配合 `go test -race` 检测数据竞争；断言总扣减 ≤ 初始余额且余额守恒。
func TestWalletQuotaConcurrentNoOverdraw(t *testing.T) {
	ctx := context.Background()
	const (
		initial = 100.0
		cost    = 1.0
		workers = 500 // 远多于可成功次数(100)，制造激烈竞争
	)
	repo := NewMemRepo()
	_ = repo.AddBalance(ctx, 1, 1, initial)
	q := walletFor(t, repo, 1, 1)

	var (
		success   int64
		insuff    int64
		wg        sync.WaitGroup
		startGate = make(chan struct{})
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-startGate // 同时起跑，最大化竞争
			_, err := q.Charge(ctx, cost)
			switch {
			case err == nil:
				atomic.AddInt64(&success, 1)
			case apperr.Is(err, CodeQuotaInsufficient):
				atomic.AddInt64(&insuff, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(startGate)
	wg.Wait()

	final, _ := q.Balance(ctx)
	// 1) 不透支：余额非负。
	if final < 0 {
		t.Fatalf("overdraw! final balance = %v", final)
	}
	// 2) 恰好 initial/cost 次成功（一分不多一分不少）。
	if success != int64(initial/cost) {
		t.Fatalf("success = %d, want %d", success, int64(initial/cost))
	}
	// 3) 守恒：初始 = 已扣 + 剩余。
	if got := float64(success)*cost + final; got != initial {
		t.Fatalf("conservation broken: charged+remaining = %v, want %v", got, initial)
	}
	// 4) 其余全部因不足被拒。
	if success+insuff != workers {
		t.Fatalf("accounted = %d, want %d", success+insuff, workers)
	}
}
