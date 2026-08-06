package realpay

import (
	"sync"
	"sync/atomic"
	"time"
)

// 进程内 SLI 计数（payment_http / payment_warm 聚合），供 Go/No-Go 与 alertReconcileHealth。
// 无外部依赖；读侧 SnapshotMetrics。

type metricsState struct {
	mu sync.Mutex

	warmOK, warmFail     int64
	warmReused, warmCold int64
	warmDurations        []time.Duration // 环形样本，最多 256

	prepaySyncDurations []time.Duration
	prepayBgDurations   []time.Duration

	tlsOK, tlsFail int64

	httpOK, httpFail     int64
	httpReused, httpCold int64

	breakerOpenTotalNs atomic.Int64
	breakerOpenSince   atomic.Int64 // unix nano；0=closed
	unknownStreak      atomic.Int64
}

var payMetrics metricsState

const metricsSampleCap = 256

// RecordWarm 记录保温探测。
func RecordWarm(ok, reused bool, d time.Duration) {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	if ok {
		payMetrics.warmOK++
	} else {
		payMetrics.warmFail++
	}
	if reused {
		payMetrics.warmReused++
	} else {
		payMetrics.warmCold++
	}
	payMetrics.warmDurations = appendSample(payMetrics.warmDurations, d)
}

// RecordHTTP 记录业务 HTTP 结果（由 tracingRoundTripper 调用）。
func RecordHTTP(ok, reused, tlsOK bool, d time.Duration, operation string) {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	if ok {
		payMetrics.httpOK++
	} else {
		payMetrics.httpFail++
	}
	if reused {
		payMetrics.httpReused++
	} else {
		payMetrics.httpCold++
	}
	if tlsOK {
		payMetrics.tlsOK++
	} else if !ok && !reused {
		// 冷连失败可能含 TLS 失败；粗分：非 ok 且非 reused 计 tls 观测失败风险
		payMetrics.tlsFail++
	} else if reused || ok {
		payMetrics.tlsOK++
	}
	switch operation {
	case "prepay", "prepay_sync":
		payMetrics.prepaySyncDurations = appendSample(payMetrics.prepaySyncDurations, d)
	case "prepay_bg":
		payMetrics.prepayBgDurations = appendSample(payMetrics.prepayBgDurations, d)
	}
}

// RecordPrepayDuration 显式记录 sync/bg Prepay 耗时。
func RecordPrepayDuration(bg bool, d time.Duration) {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	if bg {
		payMetrics.prepayBgDurations = appendSample(payMetrics.prepayBgDurations, d)
	} else {
		payMetrics.prepaySyncDurations = appendSample(payMetrics.prepaySyncDurations, d)
	}
}

// RecordBreakerOpen / RecordBreakerClose 维护熔断开路时长。
func RecordBreakerOpen() {
	now := time.Now().UnixNano()
	if payMetrics.breakerOpenSince.CompareAndSwap(0, now) {
		// newly opened
	}
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

func appendSample(s []time.Duration, d time.Duration) []time.Duration {
	if len(s) >= metricsSampleCap {
		// 丢弃最旧一半
		s = s[len(s)/2:]
	}
	return append(s, d)
}

// MetricsSnapshot 导出 SLI 只读视图。
type MetricsSnapshot struct {
	WarmOK, WarmFail     int64
	WarmReused, WarmCold int64
	HTTPOK, HTTPFail     int64
	HTTPReused, HTTPCold int64
	TLSOK, TLSFail       int64
	TLSSuccessRate       float64 // 0–1；样本不足为 -1
	ConnReuseRate        float64 // 0–1；样本不足为 -1
	PrepaySyncP95Ms      int64   // -1 = 不足
	PrepayBgP95Ms        int64
	BreakerOpenSeconds   float64
	UnknownStreak        int64
}

// SnapshotMetrics 聚合当前 SLI。
func SnapshotMetrics() MetricsSnapshot {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	s := MetricsSnapshot{
		WarmOK:        payMetrics.warmOK,
		WarmFail:      payMetrics.warmFail,
		WarmReused:    payMetrics.warmReused,
		WarmCold:      payMetrics.warmCold,
		HTTPOK:        payMetrics.httpOK,
		HTTPFail:      payMetrics.httpFail,
		HTTPReused:    payMetrics.httpReused,
		HTTPCold:      payMetrics.httpCold,
		TLSOK:         payMetrics.tlsOK,
		TLSFail:       payMetrics.tlsFail,
		UnknownStreak: payMetrics.unknownStreak.Load(),
	}
	tlsTotal := s.TLSOK + s.TLSFail
	if tlsTotal >= 5 {
		s.TLSSuccessRate = float64(s.TLSOK) / float64(tlsTotal)
	} else {
		s.TLSSuccessRate = -1
	}
	connTotal := s.HTTPReused + s.HTTPCold + s.WarmReused + s.WarmCold
	if connTotal >= 5 {
		s.ConnReuseRate = float64(s.HTTPReused+s.WarmReused) / float64(connTotal)
	} else {
		s.ConnReuseRate = -1
	}
	s.PrepaySyncP95Ms = p95Ms(payMetrics.prepaySyncDurations)
	s.PrepayBgP95Ms = p95Ms(payMetrics.prepayBgDurations)

	openNs := payMetrics.breakerOpenTotalNs.Load()
	if since := payMetrics.breakerOpenSince.Load(); since > 0 {
		openNs += time.Now().UnixNano() - since
	}
	s.BreakerOpenSeconds = float64(openNs) / 1e9
	return s
}

func p95Ms(samples []time.Duration) int64 {
	n := len(samples)
	if n < 5 {
		return -1
	}
	// 拷贝排序
	cp := make([]time.Duration, n)
	copy(cp, samples)
	// 简单插入排序（n≤256）
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
	return cp[idx].Milliseconds()
}

// ResetMetricsForTest 测试重置。
func ResetMetricsForTest() {
	payMetrics.mu.Lock()
	defer payMetrics.mu.Unlock()
	payMetrics.warmOK, payMetrics.warmFail = 0, 0
	payMetrics.warmReused, payMetrics.warmCold = 0, 0
	payMetrics.warmDurations = nil
	payMetrics.prepaySyncDurations = nil
	payMetrics.prepayBgDurations = nil
	payMetrics.tlsOK, payMetrics.tlsFail = 0, 0
	payMetrics.httpOK, payMetrics.httpFail = 0, 0
	payMetrics.httpReused, payMetrics.httpCold = 0, 0
	payMetrics.breakerOpenTotalNs.Store(0)
	payMetrics.breakerOpenSince.Store(0)
	payMetrics.unknownStreak.Store(0)
}
