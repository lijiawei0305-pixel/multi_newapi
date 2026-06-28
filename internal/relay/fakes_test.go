package relay

import (
	"context"
	"sync"

	"newapi-mt/internal/platform/appctx"
)

// recorder 记录跨 mock 的调用顺序，用于断言编排次序与短路（鉴权→…→扣费）。并发安全，支撑 -race。
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *recorder) seq() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

// ---- fakeAuthenticator ----

type fakeAuthenticator struct {
	rec       *recorder
	principal *appctx.Principal
	err       error

	mu      sync.Mutex
	calls   int
	gotRaws []string
}

func (f *fakeAuthenticator) AuthenticateToken(_ context.Context, raw string) (*appctx.Principal, error) {
	if f.rec != nil {
		f.rec.add("auth")
	}
	f.mu.Lock()
	f.calls++
	f.gotRaws = append(f.gotRaws, raw)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.principal, nil
}

func (f *fakeAuthenticator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ---- fakeAccessGuard ----

type fakeAccessGuard struct {
	rec *recorder
	err error

	mu          sync.Mutex
	calls       int
	gotTenantID int64
}

func (f *fakeAccessGuard) RequireTenantActive(_ context.Context, tenantID int64) error {
	if f.rec != nil {
		f.rec.add("tenant")
	}
	f.mu.Lock()
	f.calls++
	f.gotTenantID = tenantID
	f.mu.Unlock()
	return f.err
}

func (f *fakeAccessGuard) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ---- fakeRiskEngine ----

type fakeRiskEngine struct {
	rec *recorder
	err error

	mu          sync.Mutex
	calls       int
	gotCtx      CallContext
	gotUserID   int64
	gotTenantID int64
}

func (f *fakeRiskEngine) CheckCall(_ context.Context, p *appctx.Principal, rc CallContext) error {
	if f.rec != nil {
		f.rec.add("risk")
	}
	f.mu.Lock()
	f.calls++
	f.gotCtx = rc
	if p != nil {
		f.gotUserID = p.UserID
		f.gotTenantID = p.TenantID
	}
	f.mu.Unlock()
	return f.err
}

func (f *fakeRiskEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ---- fakeModelPermission ----

type fakeModelPermission struct {
	rec *recorder
	err error

	mu       sync.Mutex
	calls    int
	gotModel string
}

func (f *fakeModelPermission) Check(_ context.Context, _ *appctx.Principal, model string) error {
	if f.rec != nil {
		f.rec.add("model")
	}
	f.mu.Lock()
	f.calls++
	f.gotModel = model
	f.mu.Unlock()
	return f.err
}

func (f *fakeModelPermission) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ---- fakeUpstreamPool ----

type fakeUpstreamPool struct {
	rec   *recorder
	resp  RelayResponse
	usage Usage
	err   error

	mu     sync.Mutex
	calls  int
	gotReq RelayRequest
}

func (f *fakeUpstreamPool) Forward(_ context.Context, req RelayRequest) (RelayResponse, Usage, error) {
	if f.rec != nil {
		f.rec.add("forward")
	}
	f.mu.Lock()
	f.calls++
	f.gotReq = req
	f.mu.Unlock()
	if f.err != nil {
		return RelayResponse{}, Usage{}, f.err
	}
	return f.resp, f.usage, nil
}

func (f *fakeUpstreamPool) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ---- fakeBilling ----

type fakeBilling struct {
	rec    *recorder
	result *ChargeResult
	err    error

	mu     sync.Mutex
	calls  int
	gotReq ChargeRequest
}

func (f *fakeBilling) Charge(_ context.Context, req ChargeRequest) (*ChargeResult, error) {
	if f.rec != nil {
		f.rec.add("charge")
	}
	f.mu.Lock()
	f.calls++
	f.gotReq = req
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	res := f.result
	if res == nil {
		res = &ChargeResult{BucketKind: "wallet"}
	}
	return res, nil
}

func (f *fakeBilling) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeBilling) lastReq() ChargeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotReq
}
