package realpay

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTLSResultFromTrace_ThreeState(t *testing.T) {
	// reused → not attempted
	assert.Equal(t, TLSNotAttempted, tlsResultFromTrace(&traceTimings{reused: true, tlsStart: time.Now(), tlsDone: time.Now(), tlsDoneSeen: true}))

	// success
	assert.Equal(t, TLSSuccess, tlsResultFromTrace(&traceTimings{
		tlsStart: time.Now(), tlsDone: time.Now(), tlsDoneSeen: true, tlsErr: nil,
	}))

	// failure via err
	assert.Equal(t, TLSFailure, tlsResultFromTrace(&traceTimings{
		tlsStart: time.Now(), tlsDone: time.Now(), tlsDoneSeen: true, tlsErr: assert.AnError,
	}))

	// started but never completed
	assert.Equal(t, TLSFailure, tlsResultFromTrace(&traceTimings{tlsStart: time.Now()}))

	// DNS/TCP never reached TLS
	assert.Equal(t, TLSNotAttempted, tlsResultFromTrace(&traceTimings{}))
}

func TestRecordHTTP_TLSSemantics(t *testing.T) {
	ResetMetricsForTest()
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	SetMetricsNowForTest(func() time.Time { return base })
	t.Cleanup(func() { SetMetricsNowForTest(nil); ResetMetricsForTest() })

	// TLS failure must count TLSFail, not TLSOK
	RecordHTTP(false, false, TLSFailure, time.Second, "warm")
	// TLS success
	RecordHTTP(true, false, TLSSuccess, time.Second, "warm")
	// DNS/TCP: not attempted — no TLS sample
	RecordHTTP(false, false, TLSNotAttempted, time.Second, "warm")
	// reused: not attempted
	RecordHTTP(true, true, TLSNotAttempted, time.Millisecond, "warm")

	// Force availability with enough samples
	for i := 0; i < 28; i++ {
		RecordHTTP(true, false, TLSSuccess, time.Millisecond, "warm")
	}
	snap := SnapshotMetrics()
	require.True(t, snap.TLSAvailable)
	assert.Equal(t, int64(29), snap.TLSOK)  // 1 + 28
	assert.Equal(t, int64(1), snap.TLSFail) // only explicit failure
	assert.GreaterOrEqual(t, snap.TLSNotAttempted, int64(2))
	// rate = 29/30
	assert.InDelta(t, 29.0/30.0, snap.TLSSuccessRate, 1e-9)
}

func TestRecordHTTP_ReusedDoesNotEnterTLSDenominator(t *testing.T) {
	ResetMetricsForTest()
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	SetMetricsNowForTest(func() time.Time { return base })
	t.Cleanup(func() { SetMetricsNowForTest(nil); ResetMetricsForTest() })

	for i := 0; i < 40; i++ {
		RecordHTTP(true, true, TLSNotAttempted, time.Millisecond, "query")
	}
	snap := SnapshotMetrics()
	assert.False(t, snap.TLSAvailable)
	assert.Equal(t, int64(0), snap.TLSSampleCount)
	assert.True(t, snap.ConnReuseAvailable)
	assert.InDelta(t, 1.0, snap.ConnReuseRate, 1e-9)
}

func TestSnapshotMetrics_InsufficientSamplesUnavailable(t *testing.T) {
	ResetMetricsForTest()
	t.Cleanup(ResetMetricsForTest)
	for i := 0; i < 10; i++ {
		RecordHTTP(false, false, TLSFailure, time.Millisecond, "warm")
	}
	snap := SnapshotMetrics()
	assert.False(t, snap.TLSAvailable)
	assert.Equal(t, "n/a (n=10, need≥30)", FormatTLSRate(snap))
	// Alert must not fire Critical on insufficient samples
	dec := EvaluateTLSAlert(time.Time{})
	assert.Equal(t, TLSAlertNone, dec.Kind)
}

func TestSnapshotMetrics_RollingWindowDropsOldFailures(t *testing.T) {
	ResetMetricsForTest()
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	now := base
	SetMetricsNowForTest(func() time.Time { return now })
	t.Cleanup(func() { SetMetricsNowForTest(nil); ResetMetricsForTest() })

	// Old failures in window t0
	for i := 0; i < 30; i++ {
		RecordHTTP(false, false, TLSFailure, time.Millisecond, "warm")
	}
	snap := SnapshotMetrics()
	require.True(t, snap.TLSAvailable)
	assert.Equal(t, 0.0, snap.TLSSuccessRate)

	// Advance past window; fill with successes
	now = base.Add(tlsWindow + time.Minute)
	for i := 0; i < 30; i++ {
		RecordHTTP(true, false, TLSSuccess, time.Millisecond, "warm")
	}
	snap = SnapshotMetrics()
	require.True(t, snap.TLSAvailable)
	assert.InDelta(t, 1.0, snap.TLSSuccessRate, 1e-9)
	assert.Equal(t, int64(0), snap.TLSFail)
}

func TestRecordWarm_DoesNotDoubleCountReuse(t *testing.T) {
	ResetMetricsForTest()
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	SetMetricsNowForTest(func() time.Time { return base })
	t.Cleanup(func() { SetMetricsNowForTest(nil); ResetMetricsForTest() })

	// HTTP path: 30 reused
	for i := 0; i < 30; i++ {
		RecordHTTP(true, true, TLSNotAttempted, time.Millisecond, "warm")
	}
	// Warm outer path must not inflate reuse denominator
	for i := 0; i < 100; i++ {
		RecordWarm(true, time.Millisecond)
	}
	snap := SnapshotMetrics()
	require.True(t, snap.ConnReuseAvailable)
	assert.Equal(t, int64(30), snap.ConnReuseSamples)
	assert.InDelta(t, 1.0, snap.ConnReuseRate, 1e-9)
	assert.Equal(t, int64(100), snap.WarmOK)
}

func TestRecordPrepayDuration_LogicalOnce(t *testing.T) {
	ResetMetricsForTest()
	t.Cleanup(ResetMetricsForTest)
	RecordPrepayDuration(false, 100*time.Millisecond)
	RecordPrepayDuration(false, 200*time.Millisecond)
	RecordPrepayDuration(true, 300*time.Millisecond)
	// hedge attempts must not use RecordHTTP for prepay p95
	RecordHTTP(true, false, TLSSuccess, 50*time.Millisecond, "prepay_sync")
	snap := SnapshotMetrics()
	assert.Equal(t, 2, snap.PrepaySyncSamples)
	assert.Equal(t, 1, snap.PrepayBgSamples)
	assert.False(t, snap.PrepaySyncAvailable) // need 5
	assert.Equal(t, "n/a (n=2)", FormatP95(snap.PrepaySyncP95Ms, snap.PrepaySyncSamples, snap.PrepaySyncAvailable))

	for i := 0; i < 5; i++ {
		RecordPrepayDuration(false, time.Duration(i+1)*10*time.Millisecond)
	}
	snap = SnapshotMetrics()
	assert.True(t, snap.PrepaySyncAvailable)
	assert.Greater(t, snap.PrepaySyncP95Ms, int64(0))
}

func TestEvaluateTLSAlert_StateMachine(t *testing.T) {
	ResetMetricsForTest()
	base := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	now := base
	SetMetricsNowForTest(func() time.Time { return now })
	t.Cleanup(func() { SetMetricsNowForTest(nil); ResetMetricsForTest() })

	fillBad := func() {
		// 30 failures → rate 0
		for i := 0; i < 30; i++ {
			RecordHTTP(false, false, TLSFailure, time.Millisecond, "warm")
		}
	}
	fillGood := func() {
		for i := 0; i < 30; i++ {
			RecordHTTP(true, false, TLSSuccess, time.Millisecond, "warm")
		}
	}

	fillBad()
	// first bad window: no alert
	dec := EvaluateTLSAlert(now)
	assert.Equal(t, TLSAlertNone, dec.Kind)

	// second consecutive bad evaluation with still-bad window → degraded
	// (samples still in window)
	dec = EvaluateTLSAlert(now)
	require.Equal(t, TLSAlertDegraded, dec.Kind)
	assert.False(t, dec.Critical, "warm-only should be Warning severity (Critical=false)")
	assert.Contains(t, dec.Body, "severity_reason=warm_only")
	assert.Contains(t, dec.Body, "n/a (n=0)")

	// sustained bad before reminder: none
	dec = EvaluateTLSAlert(now.Add(time.Hour))
	assert.Equal(t, TLSAlertNone, dec.Kind)

	// reminder after 6h
	now = base.Add(tlsReminderInterval + time.Minute)
	// keep window bad: re-add failures in new time so window still has samples
	for i := 0; i < 30; i++ {
		RecordHTTP(false, false, TLSFailure, time.Millisecond, "warm")
	}
	dec = EvaluateTLSAlert(now)
	assert.Equal(t, TLSAlertReminder, dec.Kind)

	// recovery
	now = now.Add(time.Minute)
	// expire old fails by jumping window then fill good
	now = now.Add(tlsWindow + time.Minute)
	fillGood()
	dec = EvaluateTLSAlert(now)
	assert.Equal(t, TLSAlertRecovery, dec.Kind)

	// subsequent healthy: none
	dec = EvaluateTLSAlert(now)
	assert.Equal(t, TLSAlertNone, dec.Kind)
}

func TestEvaluateTLSAlert_BusinessPrepayIsCritical(t *testing.T) {
	ResetMetricsForTest()
	base := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	SetMetricsNowForTest(func() time.Time { return base })
	t.Cleanup(func() { SetMetricsNowForTest(nil); ResetMetricsForTest() })

	for i := 0; i < 30; i++ {
		RecordHTTP(false, false, TLSFailure, time.Millisecond, "prepay_sync")
	}
	RecordPrepayDuration(false, 100*time.Millisecond)

	_ = EvaluateTLSAlert(base) // first bad
	dec := EvaluateTLSAlert(base)
	require.Equal(t, TLSAlertDegraded, dec.Kind)
	assert.True(t, dec.Critical)
	assert.Contains(t, dec.Body, "business_or_mixed")
}

func TestPreferBackupFromWarmTarget(t *testing.T) {
	// pure unit: backup host detection used by defaultProbe
	assert.True(t, wxHostBackup == "api2.mch.weixin.qq.com")
	tgt := BuildWxWarmTargets()
	require.Len(t, tgt, 2)
	assert.Equal(t, wxHostPrimary, tgt[0].Host)
	assert.Equal(t, wxHostBackup, tgt[1].Host)
	assert.Contains(t, tgt[1].URL, wxHostBackup)
}
