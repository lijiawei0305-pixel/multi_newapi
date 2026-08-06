package realpay

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// fakeTimeoutErr 实现 net.Error 且 Timeout()=true。
type fakeTimeoutErr struct{}

func (fakeTimeoutErr) Error() string   { return "fake timeout" }
func (fakeTimeoutErr) Timeout() bool   { return true }
func (fakeTimeoutErr) Temporary() bool { return true }

func TestIsTransientPayErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"deadline", context.DeadlineExceeded, true},
		{"net-timeout", fakeTimeoutErr{}, true},
		{"op-error", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"canceled", context.Canceled, false},
		{"definitive", payment.NewOutcomeError(payment.CreateOutcomeDefinitiveReject, "param_error", "response", errors.New("PARAM_ERROR")), false},
	}
	for _, c := range cases {
		if got := isTransientPayErr(c.err); got != c.want {
			t.Errorf("%s: isTransientPayErr=%v want %v", c.name, got, c.want)
		}
	}
}

func TestRetryCreatePay_SuccessFirstTry(t *testing.T) {
	calls := 0
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }}
	err := retryCreatePay(context.Background(), clock, func(context.Context, int, int) error {
		calls++
		return nil
	}, nil)
	if err != nil || calls != 1 {
		t.Fatalf("got err=%v calls=%d, want nil/1", err, calls)
	}
}

func TestRetryCreatePay_PreWriteThenSuccess(t *testing.T) {
	calls := 0
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }}
	err := retryCreatePay(context.Background(), clock, func(context.Context, int, int) error {
		calls++
		if calls < 2 {
			return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "connect", "connect", errors.New("connection refused"))
		}
		return nil
	}, nil)
	if err != nil || calls != 2 {
		t.Fatalf("got err=%v calls=%d, want nil/2", err, calls)
	}
}

func TestRetryCreatePay_AlwaysPreWrite_GivesUp(t *testing.T) {
	calls := 0
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }}
	err := retryCreatePay(context.Background(), clock, func(context.Context, int, int) error {
		calls++
		return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "connect", "connect", errors.New("connection refused"))
	}, nil)
	if err == nil || calls != payCreateMaxAttempts {
		t.Fatalf("got err=%v calls=%d, want error/%d", err, calls, payCreateMaxAttempts)
	}
}

func TestRetryCreatePay_PostWrite_NoRetry(t *testing.T) {
	calls := 0
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }}
	err := retryCreatePay(context.Background(), clock, func(context.Context, int, int) error {
		calls++
		return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "timeout", "ttfb", context.DeadlineExceeded)
	}, nil)
	if err == nil || calls != 1 {
		t.Fatalf("got err=%v calls=%d, want error/1", err, calls)
	}
	if !payment.IsOutcomeUnknown(err) {
		t.Fatalf("want outcome_unknown, got %v", err)
	}
}

func TestRetryCreatePay_ParentCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return context.Canceled }}
	calls := 0
	err := retryCreatePay(ctx, clock, func(tryCtx context.Context, attempt, max int) error {
		calls++
		return tryCtx.Err()
	}, nil)
	if err == nil {
		t.Fatal("want error")
	}
	if calls > 1 {
		t.Fatalf("calls=%d, want <=1", calls)
	}
}
