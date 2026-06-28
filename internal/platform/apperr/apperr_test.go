package apperr

import (
	"errors"
	"net/http"
	"testing"
)

func TestNewAndError(t *testing.T) {
	e := New("TENANT_NOT_FOUND", "租户不存在", http.StatusNotFound)
	if e.Code != "TENANT_NOT_FOUND" || e.HTTP != 404 {
		t.Fatalf("unexpected fields: %+v", e)
	}
	if got := e.Error(); got != "TENANT_NOT_FOUND: 租户不存在" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestWrapUnwrap(t *testing.T) {
	base := errors.New("boom")
	e := New("PAY_SIGN_INVALID", "验签失败", 400).Wrap(base)
	if !errors.Is(e, base) {
		t.Fatal("Unwrap chain broken")
	}
	if e.Error() == "" || !contains(e.Error(), "boom") {
		t.Fatalf("wrapped error message missing cause: %q", e.Error())
	}
}

func TestCodeOfAndHTTP(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
		wantHTTP int
	}{
		{"app", New("QUOTA_INSUFFICIENT", "余额不足", 402), "QUOTA_INSUFFICIENT", 402},
		{"plain", errors.New("x"), "INTERNAL", http.StatusInternalServerError},
		{"nil-http", &AppError{Code: "X", Msg: "y"}, "X", http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CodeOf(c.err); got != c.wantCode {
				t.Errorf("CodeOf = %q want %q", got, c.wantCode)
			}
			if got := HTTPStatusOf(c.err); got != c.wantHTTP {
				t.Errorf("HTTPStatusOf = %d want %d", got, c.wantHTTP)
			}
		})
	}
}

func TestIs(t *testing.T) {
	if !Is(New("SUBSCRIPTION_EXHAUSTED", "套餐用尽", 402), "SUBSCRIPTION_EXHAUSTED") {
		t.Fatal("Is should match code")
	}
	if Is(errors.New("x"), "SUBSCRIPTION_EXHAUSTED") {
		t.Fatal("Is should not match plain error")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
