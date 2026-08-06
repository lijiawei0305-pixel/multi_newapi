package realpay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarmer_NoTargets_SilentNoErrorLogs(t *testing.T) {
	var logs atomic.Int32
	w := NewWarmer(func() []WarmTarget { return nil }, func(string, ...any) {
		logs.Add(1)
	})
	w.interval = 20 * time.Millisecond
	w.Start()
	time.Sleep(60 * time.Millisecond)
	w.Stop()
	// tick 会跑，但 targets 空 → 不写 payment_warm 日志
	assert.Equal(t, int32(0), logs.Load())
	assert.Greater(t, w.ticks.Load(), int64(0))
}

func TestWarmer_DisabledEnv_NoStart(t *testing.T) {
	t.Setenv("PAYMENT_WARMER_ENABLED", "false")
	var logs atomic.Int32
	w := NewWarmer(func() []WarmTarget {
		return []WarmTarget{{Provider: "wxpay", Host: "x", URL: "http://127.0.0.1/"}}
	}, func(string, ...any) { logs.Add(1) })
	w.Start()
	time.Sleep(30 * time.Millisecond)
	w.Stop()
	assert.False(t, w.running.Load())
	assert.Equal(t, int32(0), logs.Load())
	assert.Equal(t, int64(0), w.ticks.Load())
}

func TestWarmer_Configured_EmitsProbeLogs(t *testing.T) {
	t.Setenv("PAYMENT_WARMER_ENABLED", "true")
	var mu atomic.Value
	mu.Store([]string{})
	w := NewWarmer(func() []WarmTarget {
		return []WarmTarget{{Provider: "wxpay", Host: "api.mch.weixin.qq.com", URL: "https://example.invalid/"}}
	}, func(format string, args ...any) {
		s := format
		if len(args) > 0 {
			s = sprintf(format, args...)
		}
		cur := mu.Load().([]string)
		mu.Store(append(cur, s))
	})
	w.interval = 30 * time.Millisecond
	// 注入不触网 probe
	w.doProbe = func(ctx context.Context, tgt WarmTarget) WarmResult {
		return WarmResult{Provider: tgt.Provider, Host: tgt.Host, OK: true, Duration: 5 * time.Millisecond, ConnReused: false, TLSMs: 3}
	}
	w.Start()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if w.ticks.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.Stop()
	require.GreaterOrEqual(t, w.ticks.Load(), int64(2))
	got := mu.Load().([]string)
	require.NotEmpty(t, got)
	assert.Contains(t, got[0], "payment_warm:")
	assert.Contains(t, got[0], "provider=wxpay")
}

func TestWarmer_ConnReusedOnSecondProbe(t *testing.T) {
	t.Setenv("PAYMENT_WARMER_ENABLED", "true")
	// httptest + 显式 Keep-Alive Transport：断言第二次探测 conn_reused。
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(401) // 模拟未签名 certificates
		_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED"}`))
	}))
	t.Cleanup(srv.Close)

	tr := &http.Transport{
		Proxy:               nil,
		DisableKeepAlives:   false,
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
	cli := &http.Client{Transport: tr, Timeout: 2 * time.Second}

	w := NewWarmer(func() []WarmTarget {
		return []WarmTarget{{Provider: "wxpay", Host: "127.0.0.1", URL: srv.URL + "/v3/certificates"}}
	}, func(string, ...any) {})
	w.client = func() *http.Client { return cli }

	tgt := WarmTarget{Provider: "wxpay", Host: "127.0.0.1", URL: srv.URL + "/v3/certificates"}
	r1 := w.defaultProbe(context.Background(), tgt)
	r2 := w.defaultProbe(context.Background(), tgt)
	require.True(t, r1.OK, "first probe ok: %s", r1.Err)
	require.True(t, r2.OK, "second probe ok: %s", r2.Err)
	assert.False(t, r1.ConnReused, "first probe is cold")
	assert.True(t, r2.ConnReused, "warmer second probe conn_reused=true")
	assert.GreaterOrEqual(t, hits.Load(), int32(2))
}

func TestBuildWxWarmTargets_Hosts(t *testing.T) {
	ts := BuildWxWarmTargets()
	require.Len(t, ts, 2)
	assert.Equal(t, wxHostPrimary, ts[0].Host)
	assert.Equal(t, wxHostBackup, ts[1].Host)
	assert.Contains(t, ts[0].URL, "/v3/certificates")
}

func TestWarmer_StopIdempotent(t *testing.T) {
	w := NewWarmer(func() []WarmTarget { return nil }, nil)
	w.interval = time.Hour
	w.Start()
	w.Stop()
	w.Stop() // 不 panic
}

// --- helpers ---

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func withGotConnTrace(ctx context.Context, reused *bool) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			*reused = info.Reused
		},
	})
}
