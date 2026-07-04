package agent

import (
	"context"
	"testing"
)

func TestMemRepo_GetAgentTypeNotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	if _, found, err := repo.GetAgentType(ctx, 123); err != nil || found {
		t.Fatalf("GetAgentType(unknown): found=%v err=%v, want false/nil", found, err)
	}
}

func TestMemRepo_SeedAPIBalanceReflectedInWallet(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	repo.SeedAPIBalance(7, 250)

	w, err := repo.GetWallet(ctx, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.APIBalance != 250 {
		t.Fatalf("APIBalance = %v, want 250", w.APIBalance)
	}
}

func TestMemRepo_GetWithdrawalNotFound(t *testing.T) {
	ctx := context.Background()
	if _, err := NewMemRepo().GetWithdrawal(ctx, 1); err == nil {
		t.Fatal("expected ErrWithdrawNotFound, got nil")
	} else {
		assertCode(t, err, CodeWithdrawNotFound)
	}
}

// TestMemRepo_GetWalletReturnsCopy 确认返回值是副本，外部改动不污染内部状态。
func TestMemRepo_GetWalletReturnsCopy(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	repo.SeedAPIBalance(1, 10)

	w1, _ := repo.GetWallet(ctx, 1)
	w1.APIBalance = 9999 // 改动副本

	w2, _ := repo.GetWallet(ctx, 1)
	if w2.APIBalance != 10 {
		t.Fatalf("internal wallet leaked: APIBalance = %v, want 10", w2.APIBalance)
	}
}

// ---- 收款账户（提现闭环补强 #1）----

func TestMemRepo_GetPayoutAccountNotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	p, found, err := repo.GetPayoutAccount(ctx, 7)
	if err != nil || found {
		t.Fatalf("GetPayoutAccount(unset): found=%v err=%v, want false/nil", found, err)
	}
	if !p.IsZero() {
		t.Fatalf("GetPayoutAccount(unset) = %+v, want zero value", p)
	}
}

func TestMemRepo_SetPayoutAccountRoundTrips(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	want := PayoutAccount{Method: PayoutBank, Account: "6222000000", Name: "Alice", Bank: "ICBC"}
	if err := repo.SetPayoutAccount(ctx, 7, want); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, found, err := repo.GetPayoutAccount(ctx, 7)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestMemRepo_SetPayoutAccountDoesNotClobberAgentParams 确认收款账户与代理业务参数（AgentParams）
// 是同一份 profile 记录里互不干扰的两组字段：先后以任意顺序写入，互不清空对方。
func TestMemRepo_SetPayoutAccountDoesNotClobberAgentParams(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	params := AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 1}
	if err := repo.SetAgentType(ctx, 7, params); err != nil {
		t.Fatalf("set agent type: %v", err)
	}
	payout := PayoutAccount{Method: PayoutAlipay, Account: "a@example.com", Name: "Alice"}
	if err := repo.SetPayoutAccount(ctx, 7, payout); err != nil {
		t.Fatalf("set payout account: %v", err)
	}

	gotParams, _, err := repo.GetAgentType(ctx, 7)
	if err != nil || gotParams != params {
		t.Fatalf("AgentParams clobbered by SetPayoutAccount: got %+v, want %+v (err %v)", gotParams, params, err)
	}
	gotPayout, found, err := repo.GetPayoutAccount(ctx, 7)
	if err != nil || !found || gotPayout != payout {
		t.Fatalf("payout account not persisted: got %+v found=%v err=%v", gotPayout, found, err)
	}
}
