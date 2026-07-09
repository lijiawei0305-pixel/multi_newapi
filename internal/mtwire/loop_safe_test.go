package mtwire

import "testing"

// TestSafeLoopRun_RecoversPanicAndNextIterationRuns 证明单轮 panic 被隔离、不上抛，
// 且调用方随后仍能再跑一轮（对应 High 发现的回归判据「注入会 panic 的 runOnce 桩，
// 断言 ticker 下一轮仍被调用」）。若 panic 逃逸出 safeLoopRun，本测试自身即崩溃。
func TestSafeLoopRun_RecoversPanicAndNextIterationRuns(t *testing.T) {
	calls := 0
	run := func() {
		calls++
		if calls == 1 {
			panic("boom") // 首轮 panic，模拟 runXxxOnce 内 nil deref/断言失败
		}
	}

	safeLoopRun("test", run) // 首轮：panic 应被 recover，不上抛
	safeLoopRun("test", run) // 次轮：证明"循环存活"，仍被调用并正常完成

	if calls != 2 {
		t.Fatalf("expected 2 calls (loop survived panicking iteration), got %d", calls)
	}
}

// TestSafeLoopRun_LoopSurvivesRepeatedPanics 证明最坏情形——每一轮都 panic——循环仍逐轮存活，
// 不会因任一轮 panic 展开而终结承载 `for range ticker.C` 的 gopool 任务。
func TestSafeLoopRun_LoopSurvivesRepeatedPanics(t *testing.T) {
	iters := 0
	for i := 0; i < 3; i++ {
		safeLoopRun("test", func() {
			iters++
			panic("always")
		})
	}
	if iters != 3 {
		t.Fatalf("expected loop to survive 3 panicking iterations, got %d", iters)
	}
}

// TestSafeLoopRun_NoPanicPassThrough 证明无 panic 时行为透明（正常执行一次，无副作用）。
func TestSafeLoopRun_NoPanicPassThrough(t *testing.T) {
	calls := 0
	safeLoopRun("test", func() { calls++ })
	if calls != 1 {
		t.Fatalf("expected fn to run exactly once, got %d", calls)
	}
}
