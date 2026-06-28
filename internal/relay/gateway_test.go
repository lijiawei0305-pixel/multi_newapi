package relay

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
)

// harness 聚合一套「全成功」mock 与据其装配的 Gateway，单测按需把某个依赖改为失败。
type harness struct {
	rec      *recorder
	auth     *fakeAuthenticator
	access   *fakeAccessGuard
	risk     *fakeRiskEngine
	models   *fakeModelPermission
	upstream *fakeUpstreamPool
	billing  *fakeBilling
	gw       *Gateway
}

func newHarness() *harness {
	rec := &recorder{}
	h := &harness{
		rec:    rec,
		auth:   &fakeAuthenticator{rec: rec, principal: &appctx.Principal{UserID: 7, TenantID: 1001, Role: appctx.RoleUser}},
		access: &fakeAccessGuard{rec: rec},
		risk:   &fakeRiskEngine{rec: rec},
		models: &fakeModelPermission{rec: rec},
		upstream: &fakeUpstreamPool{
			rec:   rec,
			resp:  RelayResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true}`), Headers: map[string]string{"Content-Type": "application/json"}},
			usage: Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500},
		},
		billing: &fakeBilling{rec: rec, result: &ChargeResult{BucketKind: "subscription", ChargedUSD: 2.0, RemainingUSD: 8.0}},
	}
	h.gw = NewGateway(h.auth, h.access, h.risk, h.models, h.billing, h.upstream)
	return h
}

func sampleRequest() RelayRequest {
	return RelayRequest{
		RawToken:  "Bearer sk-abc",
		Model:     "gpt-4o",
		Endpoint:  "/v1/chat/completions",
		Body:      []byte(`{"model":"gpt-4o","messages":[]}`),
		Stream:    false,
		RequestID: "req-1",
		ClientIP:  "1.2.3.4",
	}
}

func TestNewGatewayNotNil(t *testing.T) {
	h := newHarness()
	if h.gw == nil {
		t.Fatal("NewGateway returned nil")
	}
}

// 成功路径：断言编排顺序、上游响应透传，以及身份/上下文/计费请求被正确线程化。
func TestHandleSuccessOrderAndThreading(t *testing.T) {
	h := newHarness()
	req := sampleRequest()

	resp, err := h.gw.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 上游响应原样透传。
	if !reflect.DeepEqual(resp, h.upstream.resp) {
		t.Fatalf("response = %+v, want upstream resp %+v", resp, h.upstream.resp)
	}
	// 编排顺序。
	wantSeq := []string{"auth", "tenant", "risk", "model", "forward", "charge"}
	if seq := h.rec.seq(); !reflect.DeepEqual(seq, wantSeq) {
		t.Fatalf("orchestration order = %v, want %v", seq, wantSeq)
	}
	// 鉴权收到的原始令牌。
	if len(h.auth.gotRaws) != 1 || h.auth.gotRaws[0] != "Bearer sk-abc" {
		t.Fatalf("auth got raws = %v, want [\"Bearer sk-abc\"]", h.auth.gotRaws)
	}
	// 租户守卫收到的 tenantID（来自 Principal，非请求体）。
	if h.access.gotTenantID != 1001 {
		t.Fatalf("access tenantID = %d, want 1001", h.access.gotTenantID)
	}
	// 风控收到的 CallContext + 身份。
	wantCC := CallContext{Model: "gpt-4o", Endpoint: "/v1/chat/completions", ClientIP: "1.2.3.4", RequestID: "req-1"}
	if h.risk.gotCtx != wantCC {
		t.Fatalf("risk CallContext = %+v, want %+v", h.risk.gotCtx, wantCC)
	}
	if h.risk.gotUserID != 7 || h.risk.gotTenantID != 1001 {
		t.Fatalf("risk identity = (u=%d,t=%d), want (7,1001)", h.risk.gotUserID, h.risk.gotTenantID)
	}
	// 模型权限收到的模型名。
	if h.models.gotModel != "gpt-4o" {
		t.Fatalf("model checked = %q, want gpt-4o", h.models.gotModel)
	}
	// 上游转发收到完整请求（含 Body）。
	if !reflect.DeepEqual(h.upstream.gotReq, req) {
		t.Fatalf("forward req = %+v, want %+v", h.upstream.gotReq, req)
	}
	// 计费请求：身份取自 Principal、用量取自上游 usage。
	wantCharge := ChargeRequest{
		RequestID: "req-1", UserID: 7, TenantID: 1001, Model: "gpt-4o",
		PromptTokens: 1000, CompletionTokens: 500,
	}
	if got := h.billing.lastReq(); got != wantCharge {
		t.Fatalf("charge req = %+v, want %+v", got, wantCharge)
	}
}

// 短路：任一步失败立即返回，后续步骤不执行（用 recorder 记录的调用序列断言）。
func TestHandleShortCircuit(t *testing.T) {
	cases := []struct {
		name    string
		inject  func(h *harness, e error)
		code    string
		wantSeq []string
	}{
		{"auth fails", func(h *harness, e error) { h.auth.err = e }, "TOKEN_INVALID", []string{"auth"}},
		{"tenant inactive", func(h *harness, e error) { h.access.err = e }, "TENANT_INACTIVE", []string{"auth", "tenant"}},
		{"risk blocks", func(h *harness, e error) { h.risk.err = e }, "RATE_LIMITED", []string{"auth", "tenant", "risk"}},
		{"model not allowed", func(h *harness, e error) { h.models.err = e }, CodeModelNotAllowed, []string{"auth", "tenant", "risk", "model"}},
		{"upstream error", func(h *harness, e error) { h.upstream.err = e }, CodeUpstreamError, []string{"auth", "tenant", "risk", "model", "forward"}},
		{"charge rejected", func(h *harness, e error) { h.billing.err = e }, "QUOTA_INSUFFICIENT", []string{"auth", "tenant", "risk", "model", "forward", "charge"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness()
			c.inject(h, apperr.New(c.code, "blocked at "+c.name, http.StatusForbidden))

			resp, err := h.gw.Handle(context.Background(), sampleRequest())
			if !apperr.Is(err, c.code) {
				t.Fatalf("error code = %q, want %q (err=%v)", apperr.CodeOf(err), c.code, err)
			}
			// 失败一律返回零响应（含计费失败：扣费失败不回送上游响应）。
			if !reflect.DeepEqual(resp, RelayResponse{}) {
				t.Fatalf("want zero response on failure, got %+v", resp)
			}
			// 调用序列止于失败步骤，后续步骤未发生。
			if seq := h.rec.seq(); !reflect.DeepEqual(seq, c.wantSeq) {
				t.Fatalf("call sequence = %v, want %v", seq, c.wantSeq)
			}
		})
	}
}

// 计费失败（桶不足/超额/过期）原样返回客户端、不回退、不回送上游响应。
func TestHandleChargeFailurePropagatesNoRollback(t *testing.T) {
	for _, code := range []string{"QUOTA_INSUFFICIENT", "SUBSCRIPTION_EXHAUSTED", "SUBSCRIPTION_EXPIRED"} {
		t.Run(code, func(t *testing.T) {
			h := newHarness()
			h.billing.err = apperr.New(code, "bucket rejected", http.StatusPaymentRequired)

			resp, err := h.gw.Handle(context.Background(), sampleRequest())
			if !apperr.Is(err, code) {
				t.Fatalf("want %s propagated, got %v", code, err)
			}
			if apperr.HTTPStatusOf(err) != http.StatusPaymentRequired {
				t.Fatalf("want HTTP 402 from bucket error, got %d", apperr.HTTPStatusOf(err))
			}
			// 转发已发生但响应被丢弃（不回送），扣费仅尝试一次（不回退到其它桶）。
			if h.upstream.count() != 1 {
				t.Fatalf("forward should have run once, got %d", h.upstream.count())
			}
			if h.billing.count() != 1 {
				t.Fatalf("charge attempted %d times, want exactly 1 (no fallback)", h.billing.count())
			}
			if !reflect.DeepEqual(resp, RelayResponse{}) {
				t.Fatalf("want zero response when charge fails, got %+v", resp)
			}
		})
	}
}

// 上游转发错误标准化：非 AppError → UPSTREAM_ERROR(502) 且保留底层错误可解包。
func TestHandleUpstreamErrorWrapsPlainError(t *testing.T) {
	h := newHarness()
	sentinel := errors.New("dial tcp: connection refused")
	h.upstream.err = sentinel

	_, err := h.gw.Handle(context.Background(), sampleRequest())
	if !apperr.Is(err, CodeUpstreamError) {
		t.Fatalf("want UPSTREAM_ERROR, got code %q (%v)", apperr.CodeOf(err), err)
	}
	if apperr.HTTPStatusOf(err) != http.StatusBadGateway {
		t.Fatalf("want HTTP 502, got %d", apperr.HTTPStatusOf(err))
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("UPSTREAM_ERROR should wrap and preserve the underlying error")
	}
	if h.billing.count() != 0 {
		t.Fatalf("charge must not run when forward fails, got %d", h.billing.count())
	}
}

// 上游转发返回的 AppError（已带稳定错误码）原样透传，不被二次包成 UPSTREAM_ERROR。
func TestHandleUpstreamAppErrorPassesThrough(t *testing.T) {
	h := newHarness()
	h.upstream.err = apperr.New("CHANNEL_DISABLED", "渠道已停用", http.StatusServiceUnavailable)

	_, err := h.gw.Handle(context.Background(), sampleRequest())
	if !apperr.Is(err, "CHANNEL_DISABLED") {
		t.Fatalf("want CHANNEL_DISABLED preserved, got %q (%v)", apperr.CodeOf(err), err)
	}
	if apperr.HTTPStatusOf(err) != http.StatusServiceUnavailable {
		t.Fatalf("want HTTP 503 preserved, got %d", apperr.HTTPStatusOf(err))
	}
}

// 并发安全：多 goroutine 并发 Handle 全成功，配合 -race 验证编排器与 mock 无数据竞争。
func TestHandleConcurrent(t *testing.T) {
	// 不挂 recorder（跨 goroutine 顺序无意义），仅验证无竞争 + 全部成功。
	auth := &fakeAuthenticator{principal: &appctx.Principal{UserID: 7, TenantID: 1001, Role: appctx.RoleUser}}
	billing := &fakeBilling{}
	gw := NewGateway(auth, &fakeAccessGuard{}, &fakeRiskEngine{}, &fakeModelPermission{},
		billing, &fakeUpstreamPool{resp: RelayResponse{StatusCode: 200}, usage: Usage{PromptTokens: 10}})

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = gw.Handle(context.Background(), sampleRequest())
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d errored: %v", i, e)
		}
	}
	if billing.count() != n {
		t.Fatalf("charge count = %d, want %d", billing.count(), n)
	}
}
