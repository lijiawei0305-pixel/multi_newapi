package realpay

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 连接保温：周期 < IdleConnTimeout，维持支付主机 idle 连接，避免稀疏下单永远冷连。
// 探测必须幂等、不涉资金、不产生真实可支付单。
const (
	payWarmInterval     = 60 * time.Second
	payWarmProbeTimeout = 8 * time.Second
	// 支付宝查单用确定不存在的 out_trade_no；接受 TRADE_NOT_EXIST。
	warmAliOutTradeNo = "WARM_PROBE_ORDER_DOES_NOT_EXIST_0000"
)

// warmerEnabled 默认开启；PAYMENT_WARMER_ENABLED=0/false 关闭（可回退）。
func warmerEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PAYMENT_WARMER_ENABLED"))
	if v == "" {
		return true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}

// WarmTarget 描述一次保温探测目标。
type WarmTarget struct {
	Provider string // wxpay | alipay
	Host     string // hostname only
	// URL 完整探测 URL（微信 certificates）；支付宝可为空，改走 SignedProbe。
	URL string
	// SignedProbe 可选：已签名幂等请求（支付宝 trade.query）。非 nil 时优先。
	SignedProbe func(ctx context.Context) error
}

// WarmResult 单次探测结果（可观测）。
type WarmResult struct {
	Provider string
	Host     string
	OK       bool
	Duration time.Duration
	TLSMs    int64 // -1 = not_run / reused
	// ConnReused is meaningful only when ConnReuseKnown.
	ConnReused     bool
	ConnReuseKnown bool // false for SignedProbe outer path (unknown, not cold)
	Err            string
}

// Warmer 后台连接保温器。
type Warmer struct {
	interval time.Duration
	logf     func(format string, args ...any)
	// targets 每次 tick 动态取（渠道启停可变化）
	targets func() []WarmTarget
	// doProbe 可注入（单测）；默认走 shared HTTP client
	doProbe func(ctx context.Context, t WarmTarget) WarmResult
	// client 业务同一池；默认 paymentHTTPClientShared
	client func() *http.Client

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	running  atomic.Bool
	// ticks 单测观测
	ticks atomic.Int64
}

// NewWarmer 构造保温器。targets 在每次 tick 调用；未配置渠道应返回空切片。
func NewWarmer(targets func() []WarmTarget, logf func(string, ...any)) *Warmer {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	w := &Warmer{
		interval: payWarmInterval,
		logf:     logf,
		targets:  targets,
		client:   paymentHTTPClientShared,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	w.doProbe = w.defaultProbe
	return w
}

// Start 启动后台循环；未启用 env / 已在跑则 no-op。
func (w *Warmer) Start() {
	if w == nil || !warmerEnabled() {
		return
	}
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	go w.loop()
}

// Stop 停止循环并等待退出。
func (w *Warmer) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		if w.running.Load() {
			close(w.stop)
			<-w.done
		} else {
			// 从未 Start：直接关掉 done，避免泄漏
			select {
			case <-w.done:
			default:
				close(w.done)
			}
		}
	})
}

func (w *Warmer) loop() {
	defer func() {
		w.running.Store(false)
		close(w.done)
	}()
	// 启动立即探测一次（尽快建连）
	w.tick()
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
			w.tick()
		}
	}
}

func (w *Warmer) tick() {
	w.ticks.Add(1)
	if w.targets == nil {
		return
	}
	list := w.targets()
	if len(list) == 0 {
		// 未配置：静默跳过，不刷错误日志
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), payWarmProbeTimeout*time.Duration(len(list)+1))
	defer cancel()
	for _, tgt := range list {
		res := w.doProbe(ctx, tgt)
		tlsStr := "not_run"
		if res.ConnReused {
			tlsStr = "reused"
		} else if res.TLSMs >= 0 {
			tlsStr = strconv.FormatInt(res.TLSMs, 10)
		}
		reuseStr := "unknown"
		if res.ConnReuseKnown {
			reuseStr = strconv.FormatBool(res.ConnReused)
		}
		w.logf("payment_warm: provider=%s host=%s ok=%v duration_ms=%d tls_ms=%s conn_reused=%s",
			res.Provider, res.Host, res.OK, res.Duration.Milliseconds(), tlsStr, reuseStr)
		// Warm 只记 ok/fail/duration；reuse/TLS 以 tracingRoundTripper 的 payment_http 为准，避免双计。
		RecordWarm(res.OK, res.Duration)
	}
}

func (w *Warmer) defaultProbe(ctx context.Context, t WarmTarget) (res WarmResult) {
	res = WarmResult{Provider: t.Provider, Host: t.Host, TLSMs: -1}
	start := time.Now()
	// Named return so defer updates the actual returned Duration (F8/F7 in audit).
	defer func() { res.Duration = time.Since(start) }()

	if t.SignedProbe != nil {
		// Outer SignedProbe has no httptrace here; reuse unknown (not cold).
		// Inner SDK call still records payment_http via shared client.
		err := t.SignedProbe(ctx)
		res.OK = err == nil || isWarmAcceptableErr(err)
		res.ConnReuseKnown = false
		if err != nil && !res.OK {
			res.Err = err.Error()
		}
		return res
	}

	if t.URL == "" {
		res.Err = "empty warm url"
		return res
	}
	cli := w.client()
	if cli == nil {
		res.Err = "nil http client"
		return res
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	// 备域 target 必须真实访问 api2；按 Host/URL 设置 preferBackup，避免 hostRewrite 改回主域。
	preferBackup := t.Host == wxHostBackup || strings.Contains(t.URL, wxHostBackup)
	req = req.WithContext(withPayMeta(req.Context(), "warm", t.Provider, 1, 1, preferBackup))

	tr := &traceTimings{}
	var mu sync.Mutex
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		TLSHandshakeStart: func() {
			mu.Lock()
			tr.tlsStart = time.Now()
			mu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			mu.Lock()
			tr.tlsDone = time.Now()
			tr.tlsErr = err
			tr.tlsDoneSeen = true
			mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			tr.reused = info.Reused
			mu.Unlock()
		},
	}))

	resp, err := cli.Do(req)
	// 实际请求 host（经 hostRewrite 后）写入结果，便于对账主/备
	if req.URL != nil && req.URL.Hostname() != "" {
		res.Host = req.URL.Hostname()
	}
	mu.Lock()
	res.ConnReused = tr.reused
	res.ConnReuseKnown = true
	if !tr.tlsStart.IsZero() && !tr.tlsDone.IsZero() {
		res.TLSMs = tr.tlsDone.Sub(tr.tlsStart).Milliseconds()
	}
	mu.Unlock()

	if err != nil {
		res.Err = err.Error()
		res.OK = false
		return res
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	// 任意 HTTP 响应均视为连接保温成功（含 401 未签名、404 无平台证书）
	res.OK = true
	return res
}

func isWarmAcceptableErr(err error) bool {
	if err == nil {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "TRADE_NOT_EXIST") ||
		strings.Contains(s, "not exist") ||
		strings.Contains(s, "ORDER_NOT_EXIST") ||
		strings.Contains(s, "RESOURCE_NOT_EXISTS")
}

// withPayMeta 写入支付观测 context keys。
func withPayMeta(ctx context.Context, op, provider string, attempt, maxAttempts int, preferBackup bool) context.Context {
	ctx = context.WithValue(ctx, ctxKeyPayOperation{}, op)
	ctx = context.WithValue(ctx, ctxKeyPayProvider{}, provider)
	ctx = context.WithValue(ctx, ctxKeyPayAttempt{}, attempt)
	ctx = context.WithValue(ctx, ctxKeyPayMaxAttempts{}, maxAttempts)
	ctx = context.WithValue(ctx, ctxKeyPayPreferBackup{}, preferBackup)
	return ctx
}

// BuildWxWarmTargets 微信主/备 certificates 探测（无签名亦可：401/404 仍保温连接）。
func BuildWxWarmTargets() []WarmTarget {
	return []WarmTarget{
		{Provider: "wxpay", Host: wxHostPrimary, URL: "https://" + wxHostPrimary + "/v3/certificates"},
		{Provider: "wxpay", Host: wxHostBackup, URL: "https://" + wxHostBackup + "/v3/certificates"},
	}
}

// BuildAliWarmTarget 支付宝 trade.query 探测（须 SignedProbe）。
func BuildAliWarmTarget(host string, probe func(ctx context.Context) error) WarmTarget {
	if host == "" {
		host = "openapi.alipay.com"
	}
	return WarmTarget{
		Provider:    "alipay",
		Host:        host,
		SignedProbe: probe,
	}
}

// AliWarmOutTradeNo 导出探测单号（测试断言用）。
func AliWarmOutTradeNo() string { return warmAliOutTradeNo }

// ---- SDK 集成 ----

var (
	globalWarmerMu sync.Mutex
	globalWarmer   *Warmer
)

// StartSDKWarmer 在渠道已配置时启动全局保温器；未配置 targets 为空则静默。
// 须与业务共用 paymentHTTPClientShared。
func (s *SDK) StartSDKWarmer(logf func(string, ...any)) {
	if s == nil || !warmerEnabled() {
		return
	}
	if logf == nil {
		logf = getPayClientLogf()
	}
	// 两边都未配置：不启动
	if s.wx == nil && s.ali == nil {
		return
	}
	targets := func() []WarmTarget {
		var out []WarmTarget
		if s.wx != nil {
			out = append(out, BuildWxWarmTargets()...)
		}
		if s.ali != nil {
			host := "openapi.alipay.com"
			if !s.ali.prod {
				host = "openapi.alipaydev.com"
			}
			ali := s.ali
			out = append(out, BuildAliWarmTarget(host, func(ctx context.Context) error {
				return ali.warmTradeQuery(ctx)
			}))
		}
		return out
	}
	globalWarmerMu.Lock()
	defer globalWarmerMu.Unlock()
	if globalWarmer != nil {
		globalWarmer.Stop()
		globalWarmer = nil
	}
	w := NewWarmer(targets, logf)
	globalWarmer = w
	w.Start()
	logf("payment_warm: started interval=%s", payWarmInterval)
}

// StopSDKWarmer 停止全局保温器（凭据轮换/测试）。
func StopSDKWarmer() {
	globalWarmerMu.Lock()
	defer globalWarmerMu.Unlock()
	if globalWarmer != nil {
		globalWarmer.Stop()
		globalWarmer = nil
	}
}

// warmTradeQuery 幂等探测：查确定不存在的单，接受 TRADE_NOT_EXIST。
func (a *alipayAdapter) warmTradeQuery(ctx context.Context) error {
	ctx = withPayMeta(ctx, "warm", "alipay", 1, 1, false)
	qr, qerr := a.queryOrder(ctx, warmAliOutTradeNo)
	if qerr != nil {
		if qr != nil && qr.NotExist {
			return nil
		}
		if isWarmAcceptableErr(qerr) {
			return nil
		}
		return qerr
	}
	return nil
}
