package realpay

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// fakeTimeoutErr 实现 net.Error 且 Timeout()=true，模拟网络超时错误。
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
		{"wrapped-deadline", errors.New("x"), false},
		{"net-timeout", fakeTimeoutErr{}, true},
		{"op-error-conn-refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"business", errors.New("PARAM_ERROR"), false},
		{"canceled", context.Canceled, false},
	}
	for _, c := range cases {
		if got := isTransientPayErr(c.err); got != c.want {
			t.Errorf("%s: isTransientPayErr=%v want %v", c.name, got, c.want)
		}
	}
}

func TestRetryTransientPay_SuccessFirstTry(t *testing.T) {
	calls := 0
	err := retryTransientPay(context.Background(), 3, time.Second, 0, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("got err=%v calls=%d, want nil/1", err, calls)
	}
}

func TestRetryTransientPay_TransientThenSuccess(t *testing.T) {
	calls := 0
	err := retryTransientPay(context.Background(), 3, time.Second, 0, func(context.Context) error {
		calls++
		if calls < 3 {
			return context.DeadlineExceeded // transient
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("got err=%v calls=%d, want nil/3", err, calls)
	}
}

func TestRetryTransientPay_AlwaysTransient_GivesUp(t *testing.T) {
	calls := 0
	err := retryTransientPay(context.Background(), 3, time.Second, 0, func(context.Context) error {
		calls++
		return context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 3 {
		t.Fatalf("got err=%v calls=%d, want deadline/3", err, calls)
	}
}

func TestRetryTransientPay_NonTransient_NoRetry(t *testing.T) {
	calls := 0
	businessErr := errors.New("PARAM_ERROR")
	err := retryTransientPay(context.Background(), 3, time.Second, 0, func(context.Context) error {
		calls++
		return businessErr
	})
	if !errors.Is(err, businessErr) || calls != 1 {
		t.Fatalf("got err=%v calls=%d, want business/1", err, calls)
	}
}

func TestRetryTransientPay_ParentCanceled_StopsAtBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled
	calls := 0
	err := retryTransientPay(ctx, 5, time.Second, 200*time.Millisecond, func(context.Context) error {
		calls++
		return context.DeadlineExceeded // transient → would retry, but backoff sees ctx canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err=%v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1 (stop at first backoff)", calls)
	}
}
