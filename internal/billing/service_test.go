package billing

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"newapi-mt/internal/platform/apperr"
	"newapi-mt/internal/platform/quota"
)

func TestNewServiceNotNil(t *testing.T) {
	svc := NewService(fakeModelCatalog{}, &fakeRouter{}, &fakeCallLogWriter{}, &fakeEarningSink{})
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

// 成功路径：断言扣费金额、毛利、回执透传，以及「扣费 → 日志 → 分润」顺序与各条目内容。
func TestChargeSuccessOrderAndNumbers(t *testing.T) {
	rec := &recorder{}
	// upstream = 1000*0.001 + 500*0.002 = 1.0 + 1.0 = 2.0；默认倍率 1.0 → charged 2.0。
	catalog := fakeModelCatalog{in: 0.001, out: 0.002}
	src := &fakeSource{rec: rec, kind: quota.KindSubscription, remaining: 8.0}
	router := &fakeRouter{src: src}
	logs := &fakeCallLogWriter{rec: rec}
	earn := &fakeEarningSink{rec: rec}
	svc := NewService(catalog, router, logs, earn)

	res, err := svc.Charge(context.Background(), ChargeRequest{
		RequestID:        "req-1",
		UserID:           7,
		TenantID:         1001,
		Model:            "gpt-4o",
		PromptTokens:     1000,
		CompletionTokens: 500,
		GroupKey:         "grp-a",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 结果数值。
	if res.UpstreamCostUSD != 2.0 || res.ChargedUSD != 2.0 || res.GrossProfitUSD != 0.0 {
		t.Fatalf("result numbers wrong: %+v", res)
	}
	if res.RemainingUSD != 8.0 || res.BucketKind != quota.KindSubscription {
		t.Fatalf("receipt fields not threaded through: %+v", res)
	}
	// 桶收到的请求扣费额。
	if got, _ := src.lastRequest(); got != 2.0 {
		t.Fatalf("source charged with %v, want 2.0", got)
	}
	// 编排顺序。
	if seq := rec.seq(); !reflect.DeepEqual(seq, []string{"charge", "log", "earn"}) {
		t.Fatalf("orchestration order = %v, want [charge log earn]", seq)
	}
	// 路由收到的身份。
	if router.gotUser != 7 || router.gotTenant != 1001 {
		t.Fatalf("router identity wrong: user=%d tenant=%d", router.gotUser, router.gotTenant)
	}
	// 计费日志内容。
	logEntry, ok := logs.last()
	if !ok {
		t.Fatal("expected a call log entry")
	}
	want := CallLogEntry{
		RequestID: "req-1", TenantID: 1001, UserID: 7, Model: "gpt-4o",
		PromptTokens: 1000, CompletionTokens: 500, BucketKind: quota.KindSubscription,
		UpstreamCostUSD: 2.0, ChargedUSD: 2.0, GrossProfitUSD: 0.0, GroupKey: "grp-a",
	}
	if logEntry != want {
		t.Fatalf("log entry = %+v, want %+v", logEntry, want)
	}
	// 分润条目内容。
	earnEntry, ok := earn.last()
	if !ok {
		t.Fatal("expected an earning entry")
	}
	wantEarn := EarningEntry{
		TenantID: 1001, UserID: 7, Source: SourceConsumeCommission,
		AmountUSD: 2.0, Model: "gpt-4o", RefKey: "req-1",
	}
	if earnEntry != wantEarn {
		t.Fatalf("earning entry = %+v, want %+v", earnEntry, wantEarn)
	}
}

// 倍率 > 1 → 毛利非零，验证 gross_profit = charged - upstream_cost 公式数值正确。
func TestChargeGrossProfitWithMultiplier(t *testing.T) {
	catalog := fakeModelCatalog{in: 0.001, out: 0.002} // upstream = 2.0
	src := &fakeSource{kind: quota.KindWallet}
	svc := NewService(catalog, &fakeRouter{src: src}, &fakeCallLogWriter{}, &fakeEarningSink{})

	res, err := svc.Charge(context.Background(), ChargeRequest{
		Model: "m", PromptTokens: 1000, CompletionTokens: 500, Multiplier: 1.5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UpstreamCostUSD != 2.0 {
		t.Fatalf("upstream = %v, want 2.0", res.UpstreamCostUSD)
	}
	if res.ChargedUSD != 3.0 { // 2.0 * 1.5
		t.Fatalf("charged = %v, want 3.0", res.ChargedUSD)
	}
	if res.GrossProfitUSD != 1.0 { // 3.0 - 2.0
		t.Fatalf("gross profit = %v, want 1.0", res.GrossProfitUSD)
	}
}

// 桶回执为权威扣费额：BillingResult/日志/分润均取 receipt.ChargedUSD，而非请求额。
func TestChargeUsesReceiptChargedValue(t *testing.T) {
	catalog := fakeModelCatalog{in: 0.001, out: 0.002} // upstream = 2.0, 请求扣费 2.0
	src := &fakeSource{kind: quota.KindWallet, chargedReturn: 1.75, remaining: 5.0}
	logs := &fakeCallLogWriter{}
	earn := &fakeEarningSink{}
	svc := NewService(catalog, &fakeRouter{src: src}, logs, earn)

	res, err := svc.Charge(context.Background(), ChargeRequest{Model: "m", PromptTokens: 1000, CompletionTokens: 500})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ChargedUSD != 1.75 {
		t.Fatalf("result charged = %v, want receipt value 1.75", res.ChargedUSD)
	}
	if res.GrossProfitUSD != 1.75-2.0 { // 允许负毛利
		t.Fatalf("gross profit = %v, want %v", res.GrossProfitUSD, 1.75-2.0)
	}
	if le, _ := logs.last(); le.ChargedUSD != 1.75 || le.GrossProfitUSD != 1.75-2.0 {
		t.Fatalf("log entry not using receipt value: %+v", le)
	}
	if ee, _ := earn.last(); ee.AmountUSD != 1.75 {
		t.Fatalf("earning base not using receipt value: %+v", ee)
	}
}

// 零用量：扣费 0 仍成功落日志与分润（amount 0），结果全零。
func TestChargeZeroTokens(t *testing.T) {
	catalog := fakeModelCatalog{in: 0.001, out: 0.002}
	src := &fakeSource{kind: quota.KindWallet}
	logs := &fakeCallLogWriter{}
	earn := &fakeEarningSink{}
	svc := NewService(catalog, &fakeRouter{src: src}, logs, earn)

	res, err := svc.Charge(context.Background(), ChargeRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UpstreamCostUSD != 0 || res.ChargedUSD != 0 || res.GrossProfitUSD != 0 {
		t.Fatalf("expected all-zero result, got %+v", res)
	}
	if logs.count() != 1 || earn.count() != 1 {
		t.Fatalf("zero-cost call should still log+earn: log=%d earn=%d", logs.count(), earn.count())
	}
}

// 桶扣减失败（不足/超额/过期）：原样上浮、不吞码，且不写日志、不分润（独立计量不回退）。
func TestChargeBucketErrorsPropagateWithoutLogOrEarn(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"wallet insufficient", CodeQuotaInsufficient},
		{"subscription exhausted", CodeSubscriptionExhausted},
		{"subscription expired", CodeSubscriptionExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			catalog := fakeModelCatalog{in: 0.001, out: 0.002}
			src := &fakeSource{err: apperr.New(c.code, "bucket rejected", 402)}
			logs := &fakeCallLogWriter{}
			earn := &fakeEarningSink{}
			svc := NewService(catalog, &fakeRouter{src: src}, logs, earn)

			_, err := svc.Charge(context.Background(), ChargeRequest{Model: "m", PromptTokens: 100, CompletionTokens: 50})
			if !apperr.Is(err, c.code) {
				t.Fatalf("want code %q propagated, got %v", c.code, err)
			}
			if logs.count() != 0 {
				t.Fatalf("must not write call log when charge fails, count=%d", logs.count())
			}
			if earn.count() != 0 {
				t.Fatalf("must not add earning when charge fails, count=%d", earn.count())
			}
		})
	}
}

// ModelCatalog 出错：原样上浮，不选桶、不扣费、不写日志/分润。
func TestChargeModelCatalogErrorShortCircuits(t *testing.T) {
	sentinel := errors.New("model not priced")
	src := &fakeSource{}
	router := &fakeRouter{src: src}
	logs := &fakeCallLogWriter{}
	earn := &fakeEarningSink{}
	svc := NewService(fakeModelCatalog{err: sentinel}, router, logs, earn)

	_, err := svc.Charge(context.Background(), ChargeRequest{Model: "ghost"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected catalog error to propagate, got %v", err)
	}
	if router.calls != 0 || src.chargeCount() != 0 || logs.count() != 0 || earn.count() != 0 {
		t.Fatalf("nothing downstream should run: router=%d charge=%d log=%d earn=%d",
			router.calls, src.chargeCount(), logs.count(), earn.count())
	}
}

// 选桶出错：原样上浮，不扣费、不写日志/分润。
func TestChargeRouterErrorShortCircuits(t *testing.T) {
	sentinel := errors.New("router boom")
	logs := &fakeCallLogWriter{}
	earn := &fakeEarningSink{}
	svc := NewService(fakeModelCatalog{in: 0.001, out: 0.002}, &fakeRouter{err: sentinel}, logs, earn)

	_, err := svc.Charge(context.Background(), ChargeRequest{Model: "m", PromptTokens: 10})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected router error to propagate, got %v", err)
	}
	if logs.count() != 0 || earn.count() != 0 {
		t.Fatalf("no log/earn on router error: log=%d earn=%d", logs.count(), earn.count())
	}
}

// 日志写失败：上浮错误，且分润不写（日志在分润之前；真实 txn 下整笔回滚）。
func TestChargeLogWriterErrorPropagatesAndSkipsEarn(t *testing.T) {
	sentinel := errors.New("log db down")
	src := &fakeSource{kind: quota.KindWallet}
	logs := &fakeCallLogWriter{err: sentinel}
	earn := &fakeEarningSink{}
	svc := NewService(fakeModelCatalog{in: 0.001, out: 0.002}, &fakeRouter{src: src}, logs, earn)

	_, err := svc.Charge(context.Background(), ChargeRequest{Model: "m", PromptTokens: 100, CompletionTokens: 50})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected log error to propagate, got %v", err)
	}
	if src.chargeCount() != 1 {
		t.Fatalf("charge should have happened once, got %d", src.chargeCount())
	}
	if earn.count() != 0 {
		t.Fatalf("earning must not run if log write fails, count=%d", earn.count())
	}
}

// 分润写失败：上浮错误（真实 txn 下整笔回滚）。
func TestChargeEarningErrorPropagates(t *testing.T) {
	sentinel := errors.New("earning db down")
	src := &fakeSource{kind: quota.KindWallet}
	logs := &fakeCallLogWriter{}
	earn := &fakeEarningSink{err: sentinel}
	svc := NewService(fakeModelCatalog{in: 0.001, out: 0.002}, &fakeRouter{src: src}, logs, earn)

	_, err := svc.Charge(context.Background(), ChargeRequest{Model: "m", PromptTokens: 100, CompletionTokens: 50})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected earning error to propagate, got %v", err)
	}
	if logs.count() != 1 {
		t.Fatalf("log should have been written before earning, count=%d", logs.count())
	}
}

// 端到端：真实 QuotaRouter + 套餐桶超额 → SUBSCRIPTION_EXHAUSTED 上浮，钱包桶不被触碰（独立计量不回退）。
func TestChargeNoFallbackWhenSubscriptionExhausted(t *testing.T) {
	exhausted := &fakeSource{kind: quota.KindSubscription, err: apperr.New(CodeSubscriptionExhausted, "用尽", 402)}
	walletSrc := &fakeSource{kind: quota.KindWallet} // 若被回退会成功扣费
	walletFactory := &fakeWalletFactory{src: walletSrc}
	subFactory := &fakeSubFactory{src: exhausted}
	router := NewQuotaRouter(&fakeSubChecker{active: true}, walletFactory, subFactory)

	logs := &fakeCallLogWriter{}
	earn := &fakeEarningSink{}
	svc := NewService(fakeModelCatalog{in: 0.001, out: 0.002}, router, logs, earn)

	_, err := svc.Charge(context.Background(), ChargeRequest{UserID: 7, TenantID: 1, Model: "m", PromptTokens: 100})
	if !apperr.Is(err, CodeSubscriptionExhausted) {
		t.Fatalf("want SUBSCRIPTION_EXHAUSTED, got %v", err)
	}
	if walletFactory.calls != 0 || walletSrc.chargeCount() != 0 {
		t.Fatalf("wallet must never be used when subscription is active (no fallback): factory=%d charge=%d",
			walletFactory.calls, walletSrc.chargeCount())
	}
	if logs.count() != 0 || earn.count() != 0 {
		t.Fatalf("no log/earn on exhausted: log=%d earn=%d", logs.count(), earn.count())
	}
}
