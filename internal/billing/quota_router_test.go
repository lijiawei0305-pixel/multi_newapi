package billing

import (
	"context"
	"errors"
	"testing"

	"newapi-mt/internal/platform/apperr"
	"newapi-mt/internal/platform/quota"
)

func TestNewQuotaRouterImplementsContract(t *testing.T) {
	var _ quota.Router = NewQuotaRouter(&fakeSubChecker{}, &fakeWalletFactory{}, &fakeSubFactory{})
}

// 有 active 套餐 → 套餐桶；钱包工厂不被触碰（独立计量不回退）。
func TestSelectActiveSubscriptionPicksSubscriptionBucket(t *testing.T) {
	subSrc := &fakeSource{kind: quota.KindSubscription}
	walletSrc := &fakeSource{kind: quota.KindWallet}
	subs := &fakeSubChecker{active: true}
	wallet := &fakeWalletFactory{src: walletSrc}
	sub := &fakeSubFactory{src: subSrc}
	r := NewQuotaRouter(subs, wallet, sub)

	got, err := r.Select(context.Background(), 7, 1001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != subSrc {
		t.Fatalf("expected subscription source, got %#v", got)
	}
	if sub.calls != 1 {
		t.Fatalf("subscription factory calls = %d, want 1", sub.calls)
	}
	if wallet.calls != 0 {
		t.Fatalf("wallet factory must not be called when active subscription exists, calls = %d", wallet.calls)
	}
	if subs.gotUser != 7 || sub.gotUser != 7 || sub.gotTenant != 1001 {
		t.Fatalf("identity not threaded through: subsUser=%d subUser=%d subTenant=%d", subs.gotUser, sub.gotUser, sub.gotTenant)
	}
}

// 无 active 套餐 → 钱包桶；套餐工厂不被触碰。
func TestSelectNoSubscriptionPicksWalletBucket(t *testing.T) {
	subSrc := &fakeSource{kind: quota.KindSubscription}
	walletSrc := &fakeSource{kind: quota.KindWallet}
	subs := &fakeSubChecker{active: false}
	wallet := &fakeWalletFactory{src: walletSrc}
	sub := &fakeSubFactory{src: subSrc}
	r := NewQuotaRouter(subs, wallet, sub)

	got, err := r.Select(context.Background(), 9, 2002)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != walletSrc {
		t.Fatalf("expected wallet source, got %#v", got)
	}
	if wallet.calls != 1 {
		t.Fatalf("wallet factory calls = %d, want 1", wallet.calls)
	}
	if sub.calls != 0 {
		t.Fatalf("subscription factory must not be called without active subscription, calls = %d", sub.calls)
	}
	if wallet.gotUser != 9 || wallet.gotTenant != 2002 {
		t.Fatalf("identity not threaded to wallet factory: user=%d tenant=%d", wallet.gotUser, wallet.gotTenant)
	}
}

// 套餐判定出错 → 原样上浮，两个工厂均不触碰。
func TestSelectSubscriptionCheckerErrorPropagates(t *testing.T) {
	sentinel := errors.New("db down")
	subs := &fakeSubChecker{err: sentinel}
	wallet := &fakeWalletFactory{}
	sub := &fakeSubFactory{}
	r := NewQuotaRouter(subs, wallet, sub)

	_, err := r.Select(context.Background(), 1, 1)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected checker error to propagate, got %v", err)
	}
	if wallet.calls != 0 || sub.calls != 0 {
		t.Fatalf("no factory should be called on checker error: wallet=%d sub=%d", wallet.calls, sub.calls)
	}
}

// 钱包工厂出错 → 原样上浮。
func TestSelectWalletFactoryErrorPropagates(t *testing.T) {
	sentinel := errors.New("no wallet")
	subs := &fakeSubChecker{active: false}
	wallet := &fakeWalletFactory{err: sentinel}
	sub := &fakeSubFactory{}
	r := NewQuotaRouter(subs, wallet, sub)

	if _, err := r.Select(context.Background(), 1, 1); !errors.Is(err, sentinel) {
		t.Fatalf("expected wallet factory error to propagate, got %v", err)
	}
}

// 套餐工厂出错 → 原样上浮。
func TestSelectSubscriptionFactoryErrorPropagates(t *testing.T) {
	appErr := apperr.New(CodeSubscriptionExpired, "套餐已过期", 402)
	subs := &fakeSubChecker{active: true}
	wallet := &fakeWalletFactory{}
	sub := &fakeSubFactory{err: appErr}
	r := NewQuotaRouter(subs, wallet, sub)

	_, err := r.Select(context.Background(), 1, 1)
	if !apperr.Is(err, CodeSubscriptionExpired) {
		t.Fatalf("expected SUBSCRIPTION_EXPIRED to propagate, got %v", err)
	}
}
