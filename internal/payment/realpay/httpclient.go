package realpay

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// 支付 HTTP 超时预算（P2 回调，在 P0/P1 之后）：
//
//	dial 3s + TLS 4s = 7s 冷连地板 ≤ 后台 per-attempt 8s；
//	同步路径由 context overall 2.5s 截断，不依赖过紧的 Transport 超时砍成功握手。
//	禁止回到 3×8s 同步长等待。
const (
	payDialTimeout           = 3 * time.Second
	payTLSHandshakeTimeout   = 4 * time.Second
	payResponseHeaderTimeout = 5 * time.Second
	payIdleConnTimeout       = 90 * time.Second
	payClientTimeout         = 22 * time.Second // ≥ 后台 overall(20s)
	payMaxIdleConns          = 32
	payMaxIdleConnsPerHost   = 8
	payMaxConnsPerHost       = 16
	payKeepAlive             = 30 * time.Second
)

// 微信官方 Native 主/备域名（仅 hostname，禁止固定 IP）。
// 文档：Native 支付同时提供主域名与异地接入备域名。
const (
	wxHostPrimary = "api.mch.weixin.qq.com"
	wxHostBackup  = "api2.mch.weixin.qq.com"
)

// paymentTransport 是与 http.DefaultTransport **完全隔离**的支付专用 Transport。
// 即使进程设置 TLS_INSECURE_SKIP_VERIFY，本 Transport 也永远校验证书与 hostname。
func newPaymentTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   payDialTimeout,
		KeepAlive: payKeepAlive,
	}
	return &http.Transport{
		Proxy: nil, // 禁止 HTTP_PROXY 静默接管支付流量
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// 强制 IPv4：当前出口无可用 IPv6 路由时双栈会挂到 TLS timeout
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          payMaxIdleConns,
		MaxIdleConnsPerHost:   payMaxIdleConnsPerHost,
		MaxConnsPerHost:       payMaxConnsPerHost,
		IdleConnTimeout:       payIdleConnTimeout,
		TLSHandshakeTimeout:   payTLSHandshakeTimeout,
		ResponseHeaderTimeout: payResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: false, // 永远校验；忽略全局 insecure 开关
		},
	}
}

// tracingRoundTripper 用 httptrace 记录 DNS/连接/TLS/TTFB；从 typed context 读 attempt（禁止 X-Pay Header）。
type tracingRoundTripper struct {
	base http.RoundTripper
	logf func(format string, args ...any)
}

func (t *tracingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	tr := &traceTimings{}
	var wrote atomic.Bool
	var mu sync.Mutex // 保护 tr 字段（httptrace 回调可能并发）
	ctx := httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			mu.Lock()
			tr.dnsStart = time.Now()
			mu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			mu.Lock()
			tr.dnsDone = time.Now()
			mu.Unlock()
		},
		ConnectStart: func(_, _ string) {
			mu.Lock()
			tr.connectStart = time.Now()
			mu.Unlock()
		},
		ConnectDone: func(_, _ string, _ error) {
			mu.Lock()
			tr.connectDone = time.Now()
			mu.Unlock()
		},
		TLSHandshakeStart: func() {
			mu.Lock()
			tr.tlsStart = time.Now()
			mu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			mu.Lock()
			tr.tlsDone = time.Now()
			mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			tr.gotConn = time.Now()
			tr.reused = info.Reused
			tr.wasIdle = info.WasIdle
			tr.idleTime = info.IdleTime
			mu.Unlock()
		},
		WroteHeaders: func() {
			wrote.Store(true)
			mu.Lock()
			tr.wroteHeaders = time.Now()
			mu.Unlock()
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			wrote.Store(true)
			mu.Lock()
			tr.wroteRequest = time.Now()
			mu.Unlock()
		},
		GotFirstResponseByte: func() {
			mu.Lock()
			tr.ttfb = time.Now()
			mu.Unlock()
		},
	})
	req = req.WithContext(ctx)
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	total := time.Since(start)

	// 从 typed context 读取观测元数据（不读、不写任何 X-Pay-* Header）
	op, attempt, maxAttempts, provider := payMetaFromContext(req.Context())
	host := req.URL.Hostname()

	mu.Lock()
	snap := *tr
	mu.Unlock()

	if err != nil {
		oe := payment.ClassifyNetworkError(err, wrote.Load())
		// 用 trace 细化 stage（DNS/dial/TLS/header timeout）
		oe = refineStageFromTrace(oe, &snap, wrote.Load())
		tlsOK := !snap.tlsStart.IsZero() && !snap.tlsDone.IsZero() || snap.reused
		RecordHTTP(false, snap.reused, tlsOK, total, op)
		if t.logf != nil {
			t.logf("payment_http: provider=%s operation=%s attempt=%d max_attempts=%d host=%s duration_ms=%d dns_ms=%s connect_ms=%s tls_ms=%s ttfb_ms=%s conn_reused=%v outcome=%s error_class=%s stage=%s",
				provider, op, attempt, maxAttempts, host, total.Milliseconds(),
				msOrReused(snap.dnsStart, snap.dnsDone, snap.reused),
				msOrReused(snap.connectStart, snap.connectDone, snap.reused),
				msOrReused(snap.tlsStart, snap.tlsDone, snap.reused),
				msOrNA(snap.ttfb, start),
				snap.reused, oe.Outcome, oe.ErrorClass, oe.Stage)
		}
		return nil, oe
	}

	RecordHTTP(true, snap.reused, true, total, op)
	if t.logf != nil {
		t.logf("payment_http: provider=%s operation=%s attempt=%d max_attempts=%d host=%s duration_ms=%d dns_ms=%s connect_ms=%s tls_ms=%s ttfb_ms=%s conn_reused=%v outcome=success http_status=%d",
			provider, op, attempt, maxAttempts, host, total.Milliseconds(),
			msOrReused(snap.dnsStart, snap.dnsDone, snap.reused),
			msOrReused(snap.connectStart, snap.connectDone, snap.reused),
			msOrReused(snap.tlsStart, snap.tlsDone, snap.reused),
			msOrNA(snap.ttfb, start),
			snap.reused, resp.StatusCode)
	}
	return resp, nil
}

func payMetaFromContext(ctx context.Context) (op string, attempt, maxAttempts int, provider string) {
	op = "http"
	if v, ok := ctx.Value(ctxKeyPayOperation{}).(string); ok && v != "" {
		op = v
	}
	if v, ok := ctx.Value(ctxKeyPayAttempt{}).(int); ok {
		attempt = v
	}
	if v, ok := ctx.Value(ctxKeyPayMaxAttempts{}).(int); ok {
		maxAttempts = v
	}
	if v, ok := ctx.Value(ctxKeyPayProvider{}).(string); ok {
		provider = v
	}
	return
}

func refineStageFromTrace(oe *payment.OutcomeError, tr *traceTimings, wrote bool) *payment.OutcomeError {
	if oe == nil {
		return nil
	}
	cp := *oe
	if wrote {
		if tr.ttfb.IsZero() {
			cp.Stage = "ttfb"
		} else {
			cp.Stage = "response"
		}
		return &cp
	}
	// 未写出：按已发生的最远阶段
	if !tr.tlsStart.IsZero() && tr.tlsDone.IsZero() {
		cp.Stage = "tls"
		if cp.ErrorClass == "timeout" || cp.ErrorClass == "unknown" {
			cp.ErrorClass = "timeout"
		}
		return &cp
	}
	if !tr.connectStart.IsZero() && tr.connectDone.IsZero() {
		cp.Stage = "connect"
		return &cp
	}
	if !tr.dnsStart.IsZero() && tr.dnsDone.IsZero() {
		cp.Stage = "dns"
		return &cp
	}
	if tr.dnsStart.IsZero() && tr.connectStart.IsZero() && tr.tlsStart.IsZero() {
		// 可能在 dial 前
		if cp.Stage == "" || cp.Stage == "roundtrip" || cp.Stage == "unknown" {
			cp.Stage = "connect"
		}
	}
	return &cp
}

type traceTimings struct {
	dnsStart, dnsDone         time.Time
	connectStart, connectDone time.Time
	tlsStart, tlsDone         time.Time
	gotConn                   time.Time
	wroteHeaders              time.Time
	wroteRequest              time.Time
	ttfb                      time.Time
	reused, wasIdle           bool
	idleTime                  time.Duration
}

func msOrReused(start, end time.Time, reused bool) string {
	if reused {
		return "reused"
	}
	if start.IsZero() || end.IsZero() {
		return "not_run"
	}
	return strconv.FormatInt(end.Sub(start).Milliseconds(), 10)
}

func msOrNA(t, base time.Time) string {
	if t.IsZero() {
		return "not_run"
	}
	return strconv.FormatInt(t.Sub(base).Milliseconds(), 10)
}

// hostRewriteTransport 按 **request-local** context 选择主/备域名（P1-2）。
// 禁止进程级永久粘滞 useBackup：每次逻辑操作从主域开始；仅当前 attempt 的 pre-write 重试可走备域。
type hostRewriteTransport struct {
	base http.RoundTripper
}

func (h *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	host := r.URL.Hostname()
	if host == wxHostPrimary || host == wxHostBackup {
		preferBackup, _ := req.Context().Value(ctxKeyPayPreferBackup{}).(bool)
		target := wxHostPrimary
		if preferBackup {
			target = wxHostBackup
		}
		if host != target {
			// 保留原端口（若有）
			if port := r.URL.Port(); port != "" {
				r.URL.Host = net.JoinHostPort(target, port)
			} else {
				r.URL.Host = target
			}
			r.Host = r.URL.Host
		}
	}
	// 剥离任何误注入的内部调试 Header，绝不发给微信
	r.Header.Del("X-Pay-Operation")
	r.Header.Del("X-Pay-Attempt")
	r.Header.Del("X-Pay-Provider")
	r.Header.Del("X-Pay-Max-Attempts")
	return h.base.RoundTrip(r)
}

// paymentHTTPClient 构造长期复用的支付 HTTP 客户端。
func paymentHTTPClient(logf func(string, ...any)) *http.Client {
	base := newPaymentTransport()
	rt := http.RoundTripper(&tracingRoundTripper{base: base, logf: logf})
	rt = &hostRewriteTransport{base: rt}
	return &http.Client{
		Timeout:   payClientTimeout,
		Transport: rt,
		// P1-2：拒绝全部 redirect（支付 API 不得跟随跨域或同域跳转）。
		// P2：归 definitive_reject——重定向是确定性配置/劫持信号，不该进核实空转。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return payment.NewOutcomeError(payment.CreateOutcomeDefinitiveReject, "redirect_denied", "redirect",
				fmt.Errorf("payment http: redirects denied"))
		},
	}
}

// shared clients：微信/支付宝可共用同一安全 Transport 策略；凭据轮换时 CloseIdleConnections。
var (
	payClientOnce sync.Once
	payClient     *http.Client
	payClientLogf atomic.Value // func(string, ...any)
)

// SetPayClientLogf 注入支付 HTTP 分段日志（主站 SysLog）；nil 安全。
func SetPayClientLogf(logf func(string, ...any)) {
	if logf != nil {
		payClientLogf.Store(logf)
	}
}

func getPayClientLogf() func(string, ...any) {
	if v := payClientLogf.Load(); v != nil {
		if f, ok := v.(func(string, ...any)); ok {
			return f
		}
	}
	return func(string, ...any) {}
}

// paymentHTTPClientShared 返回进程内单例支付 client（SDK 长期复用）。
func paymentHTTPClientShared() *http.Client {
	payClientOnce.Do(func() {
		payClient = paymentHTTPClient(func(format string, args ...any) {
			getPayClientLogf()(format, args...)
		})
	})
	return payClient
}

// ClosePaymentIdleConnections 凭据轮换时关闭旧 idle 连接。
// 必须先经 paymentHTTPClientShared() 初始化，禁止裸读 payClient（Once.Do 竞态）。
func ClosePaymentIdleConnections() {
	c := paymentHTTPClientShared()
	if c == nil || c.Transport == nil {
		return
	}
	if tr, ok := unwrapTransport(c.Transport).(*http.Transport); ok && tr != nil {
		tr.CloseIdleConnections()
	}
}

func unwrapTransport(rt http.RoundTripper) http.RoundTripper {
	for {
		switch t := rt.(type) {
		case *tracingRoundTripper:
			rt = t.base
		case *hostRewriteTransport:
			rt = t.base
		default:
			return rt
		}
	}
}

// ipv4OnlyHTTPClient 兼容旧调用名：返回安全支付 client（不再 Clone DefaultTransport）。
func ipv4OnlyHTTPClient() *http.Client {
	return paymentHTTPClientShared()
}
