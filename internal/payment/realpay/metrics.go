package realpay

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// 进程内 SLI（payment_http 为主；warm 仅记 ok/fail/duration，不重复贡献 reuse/TLS）。
// TLS 成功率：仅 cold handshake 三态样本，滚动 30 分钟窗口，最少 30 个样本才 available。

const (
	metricsSampleCap       = 256
	tlsWindow              = 30 * time.Minute
	tlsMinSamples          = 30
	tlsSuccessThreshold    = 0.99
	tlsConsecutiveBadNeed  = 2
	tlsReminderInterval    = 6 * time.Hour
	prepayP95MinSamples    = 5
)

// TLSHandshakeResult is the cold-handshake outcome for one HTTP attempt.
type TLSHandshakeResult int

const (
	// TLSNotAttempted: reused connection, or DNS/TCP failed before TLS started.
	TLSNotAttempted TLSHandshakeResult = iota
	// TLSSuccess: TLSHandshakeDone(err == nil).
	TLSSuccess
	// TLSFailure: TLSHandshakeDone(err != nil) or handshake started but never completed.
	TLSFailure
)

type timedTLS struct {
	at time.Time
	ok bool // true = success
}

type timedDur struct {
	at time.Time
	d  time.Duration
}

type metricsState struct {
	mu sync.Mutex

	warmOK, warmFail int64
	warmDurations    []timedDur

	// TLS cold handshake samples in rolling window (success/fail only).
	tlsSamples []timedTLS
	// Diagnostic counters (process lifetime; not used for rate alone).
	tlsNotAttempted int64

	// Connection reuse from tracingRoundTripper only (authoritative).
	httpOK, httpFail     int64
	httpReused, httpCold int64
	reuseSamples         []timedTLS // ok=true means reused

	prepaySync []timedDur
	prepayBg   []timedDur

	breakerOpenTotalNs atomic.Int64
	breakerOpenSince   atomic.Int64 // unix nano；0=closed
	unknownStreak      atomic.Int64

	// TLS alert state machine (payment-only; does not touch global Dedup TTL).
	tlsAlert consecutiveTLSAlert
}

type consecutiveTLSAlert struct {
	consecutiveBad int
	degraded       bool
	lastNotify     time.Time
	lastReminder   time.Time
}

var payMetrics metricsState

// nowFn is overridable in tests.
var metricsNow = time.Now

// RecordWarm 记录保温探测结果（不贡献 TLS / 连接复用率，避免与 payment_http 双计）。
// reuseKnown=false 时不记 reuse（支付宝 SignedProbe 外层拿不到 trace → unknown，不是 cold）。
func RecordWarm(ok bool, d time.Duration) {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	if ok {
		payMetrics.warmOK++
	} else {
		payMetrics.warmFail++
	}
	payMetrics.warmDurations = appendTimedDur(payMetrics.warmDurations, d)
}

// RecordHTTP 记录业务/探测 HTTP 结果（由 tracingRoundTripper 调用）。
// tls 仅在 cold handshake 时为 Success/Failure；reused 与 pre-TLS 失败为 NotAttempted。
func RecordHTTP(ok, reused bool, tls TLSHandshakeResult, d time.Duration, operation string) {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	now := metricsNow()
	if ok {
		payMetrics.httpOK++
	} else {
		payMetrics.httpFail++
	}
	if reused {
		payMetrics.httpReused++
		payMetrics.reuseSamples = appendReuse(payMetrics.reuseSamples, now, true)
	} else {
		payMetrics.httpCold++
		payMetrics.reuseSamples = appendReuse(payMetrics.reuseSamples, now, false)
	}
	switch tls {
	case TLSSuccess:
		payMetrics.tlsSamples = appendTLS(payMetrics.tlsSamples, now, true)
	case TLSFailure:
		payMetrics.tlsSamples = appendTLS(payMetrics.tlsSamples, now, false)
	default:
		payMetrics.tlsNotAttempted++
	}
	// Attempt-level prepay durations are NOT written here — logical Prepay uses RecordPrepayDuration.
	_ = operation
	_ = d
}

// RecordPrepayDuration 在一次逻辑 createPay 结束时只记一次 sync/bg 耗时。
func RecordPrepayDuration(bg bool, d time.Duration) {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	if bg {
		payMetrics.prepayBg = appendTimedDur(payMetrics.prepayBg, d)
	} else {
		payMetrics.prepaySync = appendTimedDur(payMetrics.prepaySync, d)
	}
}

// RecordBreakerOpen / RecordBreakerClose 维护熔断开路时长。
func RecordBreakerOpen() {
	now := time.Now().UnixNano()
	payMetrics.breakerOpenSince.CompareAndSwap(0, now)
}

func RecordBreakerClose() {
	since := payMetrics.breakerOpenSince.Swap(0)
	if since > 0 {
		payMetrics.breakerOpenTotalNs.Add(time.Now().UnixNano() - since)
	}
}

// RecordUnknownOutcome 连续 unknown 计数（熔断用，亦供 SLI）。
func RecordUnknownOutcome() int64 {
	return payMetrics.unknownStreak.Add(1)
}

func ResetUnknownStreak() {
	payMetrics.unknownStreak.Store(0)
}

func UnknownStreak() int64 {
	return payMetrics.unknownStreak.Load()
}

func appendTLS(s []timedTLS, at time.Time, ok bool) []timedTLS {
	s = pruneTLS(s, at)
	s = append(s, timedTLS{at: at, ok: ok})
	if len(s) > metricsSampleCap*4 {
		s = s[len(s)-metricsSampleCap*2:]
	}
	return s
}

func appendReuse(s []timedTLS, at time.Time, reused bool) []timedTLS {
	s = pruneTLS(s, at)
	s = append(s, timedTLS{at: at, ok: reused})
	if len(s) > metricsSampleCap*4 {
		s = s[len(s)-metricsSampleCap*2:]
	}
	return s
}

func appendTimedDur(s []timedDur, d time.Duration) []timedDur {
	at := metricsNow()
	s = pruneDur(s, at)
	s = append(s, timedDur{at: at, d: d})
	if len(s) > metricsSampleCap {
		s = s[len(s)/2:]
	}
	return s
}

func pruneTLS(s []timedTLS, now time.Time) []timedTLS {
	cut := now.Add(-tlsWindow)
	i := 0
	for i < len(s) && s[i].at.Before(cut) {
		i++
	}
	if i == 0 {
		return s
	}
	return append([]timedTLS(nil), s[i:]...)
}

func pruneDur(s []timedDur, now time.Time) []timedDur {
	cut := now.Add(-tlsWindow)
	i := 0
	for i < len(s) && s[i].at.Before(cut) {
		i++
	}
	if i == 0 {
		return s
	}
	return append([]timedDur(nil), s[i:]...)
}

// MetricsSnapshot 导出 SLI 只读视图。
type MetricsSnapshot struct {
	WarmOK, WarmFail int64
	HTTPOK, HTTPFail int64
	HTTPReused, HTTPCold int64

	// TLS cold-handshake window
	TLSOK, TLSFail   int64
	TLSNotAttempted  int64
	TLSSampleCount   int64
	TLSAvailable     bool
	TLSSuccessRate   float64 // only valid when TLSAvailable
	TLSWindowStart   time.Time
	TLSWindowEnd     time.Time

	// Conn reuse from HTTP path only
	ConnReuseRate      float64
	ConnReuseAvailable bool
	ConnReuseSamples   int64

	PrepaySyncP95Ms     int64 // -1 = unavailable
	PrepayBgP95Ms       int64
	PrepaySyncSamples   int
	PrepayBgSamples     int
	PrepaySyncAvailable bool
	PrepayBgAvailable   bool

	BreakerOpenSeconds float64
	UnknownStreak      int64
}

// SnapshotMetrics 聚合当前滚动窗口 SLI。
func SnapshotMetrics() MetricsSnapshot {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	now := metricsNow()
	payMetrics.tlsSamples = pruneTLS(payMetrics.tlsSamples, now)
	payMetrics.reuseSamples = pruneTLS(payMetrics.reuseSamples, now)
	payMetrics.prepaySync = pruneDur(payMetrics.prepaySync, now)
	payMetrics.prepayBg = pruneDur(payMetrics.prepayBg, now)
	payMetrics.warmDurations = pruneDur(payMetrics.warmDurations, now)

	s := MetricsSnapshot{
		WarmOK:          payMetrics.warmOK,
		WarmFail:        payMetrics.warmFail,
		HTTPOK:          payMetrics.httpOK,
		HTTPFail:        payMetrics.httpFail,
		HTTPReused:      payMetrics.httpReused,
		HTTPCold:        payMetrics.httpCold,
		TLSNotAttempted: payMetrics.tlsNotAttempted,
		UnknownStreak:   payMetrics.unknownStreak.Load(),
		TLSWindowEnd:    now,
		TLSWindowStart:  now.Add(-tlsWindow),
	}
	var okN, failN int64
	for _, e := range payMetrics.tlsSamples {
		if e.ok {
			okN++
		} else {
			failN++
		}
	}
	s.TLSOK, s.TLSFail = okN, failN
	s.TLSSampleCount = okN + failN
	if s.TLSSampleCount >= tlsMinSamples {
		s.TLSAvailable = true
		s.TLSSuccessRate = float64(okN) / float64(s.TLSSampleCount)
	}

	var reusedN, coldN int64
	for _, e := range payMetrics.reuseSamples {
		if e.ok {
			reusedN++
		} else {
			coldN++
		}
	}
	s.ConnReuseSamples = reusedN + coldN
	if s.ConnReuseSamples >= tlsMinSamples {
		s.ConnReuseAvailable = true
		s.ConnReuseRate = float64(reusedN) / float64(s.ConnReuseSamples)
	}

	s.PrepaySyncSamples = len(payMetrics.prepaySync)
	s.PrepayBgSamples = len(payMetrics.prepayBg)
	s.PrepaySyncP95Ms, s.PrepaySyncAvailable = p95MsTimed(payMetrics.prepaySync)
	s.PrepayBgP95Ms, s.PrepayBgAvailable = p95MsTimed(payMetrics.prepayBg)

	openNs := payMetrics.breakerOpenTotalNs.Load()
	if since := payMetrics.breakerOpenSince.Load(); since > 0 {
		openNs += time.Now().UnixNano() - since
	}
	s.BreakerOpenSeconds = float64(openNs) / 1e9
	return s
}

func p95MsTimed(samples []timedDur) (int64, bool) {
	n := len(samples)
	if n < prepayP95MinSamples {
		return -1, false
	}
	cp := make([]time.Duration, n)
	for i := range samples {
		cp[i] = samples[i].d
	}
	for i := 1; i < n; i++ {
		j := i
		for j > 0 && cp[j] < cp[j-1] {
			cp[j], cp[j-1] = cp[j-1], cp[j]
			j--
		}
	}
	idx := int(float64(n-1) * 0.95)
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return cp[idx].Milliseconds(), true
}

// FormatP95 人类可读 P95：样本不足为 n/a (n=k)。
func FormatP95(ms int64, n int, available bool) string {
	if !available || n < prepayP95MinSamples || ms < 0 {
		return fmt.Sprintf("n/a (n=%d)", n)
	}
	return fmt.Sprintf("%d", ms)
}

// FormatTLSRate 人类可读 TLS 成功率。
func FormatTLSRate(s MetricsSnapshot) string {
	if !s.TLSAvailable {
		return fmt.Sprintf("n/a (n=%d, need≥%d)", s.TLSSampleCount, tlsMinSamples)
	}
	return fmt.Sprintf("%.3f", s.TLSSuccessRate)
}

// FormatConnReuse 人类可读连接复用率（仅 HTTP 权威路径）。
func FormatConnReuse(s MetricsSnapshot) string {
	if !s.ConnReuseAvailable {
		return fmt.Sprintf("n/a (n=%d)", s.ConnReuseSamples)
	}
	return fmt.Sprintf("%.3f", s.ConnReuseRate)
}

// WarmOnly reports whether there are no logical prepay samples in the window.
func (s MetricsSnapshot) WarmOnly() bool {
	return s.PrepaySyncSamples == 0 && s.PrepayBgSamples == 0
}

// TLSAlertKind is the payment-TLS alert state-machine action.
type TLSAlertKind string

const (
	TLSAlertNone     TLSAlertKind = ""
	TLSAlertDegraded TLSAlertKind = "degraded"
	TLSAlertReminder TLSAlertKind = "reminder"
	TLSAlertRecovery TLSAlertKind = "recovery"
)

// TLSAlertDecision is returned by EvaluateTLSAlert for the orchestrator to Dispatch.
type TLSAlertDecision struct {
	Kind     TLSAlertKind
	Critical bool // false => Warning (warm-only)
	Subject  string
	Body     string
	// ConsecutiveBad is diagnostic.
	ConsecutiveBad int
}

// EvaluateTLSAlert applies the payment-only TLS alert state machine.
// Does not use global DedupTTL; enforces consecutive windows + 6h reminder + recovery.
func EvaluateTLSAlert(now time.Time) TLSAlertDecision {
	if now.IsZero() {
		now = metricsNow()
	}
	snap := SnapshotMetrics()
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	st := &payMetrics.tlsAlert

	bad := snap.TLSAvailable && snap.TLSSuccessRate < tlsSuccessThreshold
	healthy := snap.TLSAvailable && snap.TLSSuccessRate >= tlsSuccessThreshold

	if bad {
		st.consecutiveBad++
	} else if healthy {
		// good window resets consecutive counter
		if st.degraded {
			// recovery
			st.degraded = false
			st.consecutiveBad = 0
			st.lastNotify = now
			return TLSAlertDecision{
				Kind:           TLSAlertRecovery,
				Critical:       false,
				Subject:        "支付 TLS 成功率已恢复",
				Body:           formatTLSAlertBody(snap, st.consecutiveBad, "recovered"),
				ConsecutiveBad: 0,
			}
		}
		st.consecutiveBad = 0
		return TLSAlertDecision{Kind: TLSAlertNone}
	} else {
		// insufficient samples: do not escalate; do not clear degraded immediately
		return TLSAlertDecision{Kind: TLSAlertNone}
	}

	// bad path
	if !st.degraded {
		if st.consecutiveBad < tlsConsecutiveBadNeed {
			// first bad window: record only
			return TLSAlertDecision{Kind: TLSAlertNone, ConsecutiveBad: st.consecutiveBad}
		}
		st.degraded = true
		st.lastNotify = now
		st.lastReminder = now
		crit := !snap.WarmOnly()
		return TLSAlertDecision{
			Kind:           TLSAlertDegraded,
			Critical:       crit,
			Subject:        "支付 TLS 成功率低于门禁",
			Body:           formatTLSAlertBody(snap, st.consecutiveBad, severityReason(snap)),
			ConsecutiveBad: st.consecutiveBad,
		}
	}
	// already degraded: reminder only every 6h
	if now.Sub(st.lastReminder) >= tlsReminderInterval {
		st.lastReminder = now
		st.lastNotify = now
		crit := !snap.WarmOnly()
		return TLSAlertDecision{
			Kind:           TLSAlertReminder,
			Critical:       crit,
			Subject:        "支付 TLS 成功率持续低于门禁（提醒）",
			Body:           formatTLSAlertBody(snap, st.consecutiveBad, severityReason(snap)),
			ConsecutiveBad: st.consecutiveBad,
		}
	}
	return TLSAlertDecision{Kind: TLSAlertNone, ConsecutiveBad: st.consecutiveBad}
}

func severityReason(s MetricsSnapshot) string {
	if s.WarmOnly() {
		return "warm_only"
	}
	return "business_or_mixed"
}

func formatTLSAlertBody(s MetricsSnapshot, consecutive int, reason string) string {
	return fmt.Sprintf(
		"window=%s..%s\ntls_success_rate=%s tls_ok=%d tls_fail=%d tls_not_attempted=%d tls_samples=%d min_samples=%d\nwarm_ok=%d warm_fail=%d\nconn_reuse_rate=%s\nprepay_sync_p95_ms=%s prepay_sync_samples=%d\nprepay_bg_p95_ms=%s prepay_bg_samples=%d\nconsecutive_bad_windows=%d\nseverity_reason=%s\nthreshold=%.2f window=%s",
		s.TLSWindowStart.Format(time.RFC3339),
		s.TLSWindowEnd.Format(time.RFC3339),
		FormatTLSRate(s),
		s.TLSOK, s.TLSFail, s.TLSNotAttempted, s.TLSSampleCount, tlsMinSamples,
		s.WarmOK, s.WarmFail,
		FormatConnReuse(s),
		FormatP95(s.PrepaySyncP95Ms, s.PrepaySyncSamples, s.PrepaySyncAvailable), s.PrepaySyncSamples,
		FormatP95(s.PrepayBgP95Ms, s.PrepayBgSamples, s.PrepayBgAvailable), s.PrepayBgSamples,
		consecutive,
		reason,
		tlsSuccessThreshold,
		tlsWindow,
	)
}

// ResetMetricsForTest 测试重置。
func ResetMetricsForTest() {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	payMetrics.warmOK, payMetrics.warmFail = 0, 0
	payMetrics.warmDurations = nil
	payMetrics.tlsSamples = nil
	payMetrics.tlsNotAttempted = 0
	payMetrics.httpOK, payMetrics.httpFail = 0, 0
	payMetrics.httpReused, payMetrics.httpCold = 0, 0
	payMetrics.reuseSamples = nil
	payMetrics.prepaySync = nil
	payMetrics.prepayBg = nil
	payMetrics.breakerOpenTotalNs.Store(0)
	payMetrics.breakerOpenSince.Store(0)
	payMetrics.unknownStreak.Store(0)
	payMetrics.tlsAlert = consecutiveTLSAlert{}
}

// SetMetricsNowForTest injects clock for window tests; pass nil to restore.
func SetMetricsNowForTest(fn func() time.Time) {
	if fn == nil {
		metricsNow = time.Now
		return
	}
	metricsNow = fn
}
