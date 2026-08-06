package realpay

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 熔断：滑动窗口内连续 N 次 outcome_unknown 开路 T 秒。
// 开路期间同步 Prepay 直接跳过（<100ms 返回 queued）；半开放行单次探测。

const (
	defaultBreakerN = 5
	defaultBreakerT = 30 * time.Second
)

func breakerEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PAYMENT_BREAKER_ENABLED"))
	if v == "" {
		return true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}

func breakerN() int {
	v := strings.TrimSpace(os.Getenv("PAYMENT_BREAKER_N"))
	if v == "" {
		return defaultBreakerN
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultBreakerN
	}
	return n
}

func breakerCooldown() time.Duration {
	v := strings.TrimSpace(os.Getenv("PAYMENT_BREAKER_T_SEC"))
	if v == "" {
		return defaultBreakerT
	}
	sec, err := strconv.Atoi(v)
	if err != nil || sec < 1 {
		return defaultBreakerT
	}
	return time.Duration(sec) * time.Second
}

type circuitBreaker struct {
	mu            sync.Mutex
	unknownStreak int
	openUntil     time.Time
	halfOpen      bool // 冷却后半开：放行 1 次探测
	probeInFlight bool
}

var globalBreaker circuitBreaker

// BreakerAllowSync 同步路径是否允许发起 Prepay。
// 开路且非半开探测 → false（调用方应立即 queued）。
func BreakerAllowSync() bool {
	if !breakerEnabled() {
		return true
	}
	globalBreaker.mu.Lock()
	defer globalBreaker.mu.Unlock()
	now := time.Now()
	if globalBreaker.openUntil.IsZero() || now.After(globalBreaker.openUntil) {
		// closed 或 冷却结束 → 半开
		if !globalBreaker.openUntil.IsZero() && now.After(globalBreaker.openUntil) {
			globalBreaker.halfOpen = true
			globalBreaker.openUntil = time.Time{}
			RecordBreakerClose()
		}
		if globalBreaker.halfOpen {
			if globalBreaker.probeInFlight {
				return false // 半开只放行一个
			}
			globalBreaker.probeInFlight = true
			return true
		}
		return true
	}
	// open
	return false
}

// BreakerRecordSuccess 成功出码：重置 streak，关半开。
func BreakerRecordSuccess() {
	if !breakerEnabled() {
		return
	}
	globalBreaker.mu.Lock()
	defer globalBreaker.mu.Unlock()
	globalBreaker.unknownStreak = 0
	globalBreaker.halfOpen = false
	globalBreaker.probeInFlight = false
	if !globalBreaker.openUntil.IsZero() {
		globalBreaker.openUntil = time.Time{}
		RecordBreakerClose()
	}
	ResetUnknownStreak()
}

// BreakerRecordUnknown 记录 unknown；达 N 则开路 T 秒。
func BreakerRecordUnknown() {
	if !breakerEnabled() {
		return
	}
	globalBreaker.mu.Lock()
	defer globalBreaker.mu.Unlock()
	globalBreaker.probeInFlight = false
	globalBreaker.unknownStreak++
	_ = RecordUnknownOutcome()
	n := breakerN()
	if globalBreaker.unknownStreak >= n {
		globalBreaker.openUntil = time.Now().Add(breakerCooldown())
		globalBreaker.halfOpen = false
		globalBreaker.unknownStreak = 0
		RecordBreakerOpen()
	}
}

// BreakerRecordDefinitive 业务拒绝不计入 unknown 熔断。
func BreakerRecordDefinitive() {
	if !breakerEnabled() {
		return
	}
	globalBreaker.mu.Lock()
	defer globalBreaker.mu.Unlock()
	globalBreaker.probeInFlight = false
	// 确定性失败说明链路可达，重置 unknown streak
	globalBreaker.unknownStreak = 0
	globalBreaker.halfOpen = false
	ResetUnknownStreak()
}

// BreakerIsOpen 只读：是否处于开路（含半开探测占用）。
func BreakerIsOpen() bool {
	if !breakerEnabled() {
		return false
	}
	globalBreaker.mu.Lock()
	defer globalBreaker.mu.Unlock()
	if globalBreaker.openUntil.IsZero() {
		return false
	}
	return time.Now().Before(globalBreaker.openUntil)
}

// ResetBreakerForTest 测试重置。
func ResetBreakerForTest() {
	globalBreaker.mu.Lock()
	defer globalBreaker.mu.Unlock()
	globalBreaker.unknownStreak = 0
	globalBreaker.openUntil = time.Time{}
	globalBreaker.halfOpen = false
	globalBreaker.probeInFlight = false
	ResetUnknownStreak()
	RecordBreakerClose()
}
