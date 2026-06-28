package payment

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestProviderValidAndNotifyPath(t *testing.T) {
	cases := []struct {
		p     Provider
		valid bool
		path  string
	}{
		{ProviderWxpay, true, "/pay/wxpay/notify"},
		{ProviderAlipay, true, "/auth/alipay/notify"},
		{Provider("paypal"), false, ""},
		{Provider(""), false, ""},
	}
	for _, c := range cases {
		if c.p.Valid() != c.valid {
			t.Errorf("Provider(%q).Valid() = %v, want %v", c.p, c.p.Valid(), c.valid)
		}
		if c.p.NotifyPath() != c.path {
			t.Errorf("Provider(%q).NotifyPath() = %q, want %q", c.p, c.p.NotifyPath(), c.path)
		}
	}
}

func TestOrderTypeValid(t *testing.T) {
	for _, c := range []struct {
		t    OrderType
		want bool
	}{
		{OrderTypeRecharge, true},
		{OrderTypeSubscription, true},
		{OrderType("topup"), false},
	} {
		if c.t.Valid() != c.want {
			t.Errorf("OrderType(%q).Valid() = %v, want %v", c.t, c.t.Valid(), c.want)
		}
	}
}

func TestOrderStatusStateMachine(t *testing.T) {
	// Valid 集合。
	for _, s := range []OrderStatus{OrderCreated, OrderPaid, OrderCredited, OrderFailed} {
		if !s.Valid() {
			t.Errorf("status %q should be valid", s)
		}
	}
	if OrderStatus("settled").Valid() {
		t.Error("unknown status must be invalid")
	}

	// 终态。
	if OrderCreated.IsTerminal() || OrderPaid.IsTerminal() {
		t.Error("created/paid are not terminal")
	}
	if !OrderCredited.IsTerminal() || !OrderFailed.IsTerminal() {
		t.Error("credited/failed must be terminal")
	}

	// 合法迁移矩阵。
	type tr struct {
		from, to OrderStatus
		ok       bool
	}
	for _, c := range []tr{
		{OrderCreated, OrderPaid, true},
		{OrderCreated, OrderFailed, true},
		{OrderCreated, OrderCredited, false}, // 不能跳过 paid
		{OrderPaid, OrderCredited, true},
		{OrderPaid, OrderCreated, true}, // 入账失败回滚
		{OrderPaid, OrderFailed, true},
		{OrderCredited, OrderPaid, false}, // 终态不可迁出
		{OrderFailed, OrderCreated, false},
	} {
		if got := c.from.CanTransitionTo(c.to); got != c.ok {
			t.Errorf("CanTransitionTo(%q→%q) = %v, want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestOrderInputValidate(t *testing.T) {
	base := OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay,
		AmountUSD: 100, ActualPaid: 100, GroupID: 3,
	}
	if err := base.validate(); err != nil {
		t.Fatalf("valid recharge rejected: %v", err)
	}
	sub := OrderInput{Type: OrderTypeSubscription, TenantID: 1, UserID: 2, Provider: ProviderAlipay, PlanID: 9, ActualPaid: 30}
	if err := sub.validate(); err != nil {
		t.Fatalf("valid subscription rejected: %v", err)
	}

	bad := map[string]OrderInput{
		"no tenant":         {Type: OrderTypeRecharge, UserID: 2, Provider: ProviderWxpay, AmountUSD: 1},
		"no user":           {Type: OrderTypeRecharge, TenantID: 1, Provider: ProviderWxpay, AmountUSD: 1},
		"bad type":          {Type: "x", TenantID: 1, UserID: 2, Provider: ProviderWxpay, AmountUSD: 1},
		"bad provider":      {Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: "x", AmountUSD: 1},
		"neg amount":        {Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay, AmountUSD: -1},
		"recharge zero amt": {Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay, AmountUSD: 0},
		"sub no plan":       {Type: OrderTypeSubscription, TenantID: 1, UserID: 2, Provider: ProviderWxpay},
		"neg paid":          {Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay, AmountUSD: 1, ActualPaid: -1},
	}
	for name, in := range bad {
		if got := apperr.CodeOf(in.validate()); got != CodeOrderInvalid {
			t.Errorf("%s: code = %q, want %q", name, got, CodeOrderInvalid)
		}
	}
}
