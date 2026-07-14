/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package alert

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeChannel 是记录调用次数的假通道——测试绝不真发邮件/webhook。可配置固定错误或 panic。
type fakeChannel struct {
	name    string
	mu      sync.Mutex
	calls   int
	lastA   Alert
	failErr error
	doPanic bool
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) Send(_ context.Context, _ Config, a Alert) error {
	f.mu.Lock()
	f.calls++
	f.lastA = a
	f.mu.Unlock()
	if f.doPanic {
		panic("boom")
	}
	return f.failErr
}

func (f *fakeChannel) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func staticProvider(cfg Config) ConfigProvider {
	return func() Config { return cfg }
}

func silentLogger() SinkOption {
	return WithLogger(func(string, ...any) {})
}

// ---------------------------------------------------------------------------
// LoadConfig（契约解析）
// ---------------------------------------------------------------------------

func TestLoadConfig_NilGetter_ReturnsDisabledSafeDefault(t *testing.T) {
	cfg := LoadConfig(nil)
	require.False(t, cfg.Enabled, "nil getter 必须禁用（未配置即不告警）")
	require.Empty(t, cfg.Email)
	require.Empty(t, cfg.WebhookURL)
	require.Equal(t, DefaultThresholdPct, cfg.ThresholdPct)
}

func TestLoadConfig_ParsesAllKeys(t *testing.T) {
	store := map[string]string{
		OptEnabled:      "true",
		OptEmail:        "a@x.com, b@y.com\nc@z.com;d@w.com , ",
		OptWebhookURL:   "  https://hook.example.com/alert  ",
		OptThresholdPct: "88.5",
	}
	cfg := LoadConfig(func(k string) string { return store[k] })

	require.True(t, cfg.Enabled)
	require.Equal(t, []string{"a@x.com", "b@y.com", "c@z.com", "d@w.com"}, cfg.Email)
	require.Equal(t, "https://hook.example.com/alert", cfg.WebhookURL)
	require.Equal(t, 88.5, cfg.ThresholdPct)
}

func TestLoadConfig_ThresholdFallback(t *testing.T) {
	cases := map[string]string{
		"empty":    "",
		"garbage":  "abc",
		"zero":     "0",
		"negative": "-5",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := LoadConfig(func(k string) string {
				if k == OptThresholdPct {
					return raw
				}
				return ""
			})
			require.Equal(t, DefaultThresholdPct, cfg.ThresholdPct)
		})
	}
}

func TestLoadConfig_EnabledOnlyOnExactTrue(t *testing.T) {
	for _, raw := range []string{"false", "1", "yes", "TRUE", ""} {
		cfg := LoadConfig(func(k string) string {
			if k == OptEnabled {
				return raw
			}
			return ""
		})
		require.False(t, cfg.Enabled, "只有精确 'true' 才启用，got %q", raw)
	}
	cfg := LoadConfig(func(k string) string {
		if k == OptEnabled {
			return "  true  "
		}
		return ""
	})
	require.True(t, cfg.Enabled, "两侧空白应被 trim 后识别为 true")
}

// LoadConfig 满足 ConfigLoader 契约（编译期已由 config.go 的 var _ ConfigLoader 断言，这里再运行时确认）。
func TestLoadConfig_SatisfiesConfigLoaderContract(t *testing.T) {
	var loader ConfigLoader = LoadConfig
	require.NotNil(t, loader)
}

// ---------------------------------------------------------------------------
// Sink：禁用 no-op
// ---------------------------------------------------------------------------

func TestSink_Disabled_NoOp(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	s := NewSink(staticProvider(Config{Enabled: false}), WithChannels(ch), silentLogger())

	err := s.Dispatch(context.Background(), Alert{Level: LevelCritical, Subject: "x"})
	require.NoError(t, err)
	require.Equal(t, 0, ch.count(), "禁用时不得触达任何通道")
}

func TestSink_NilProvider_NoOp(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	s := NewSink(nil, WithChannels(ch), silentLogger())
	require.NoError(t, s.Dispatch(context.Background(), Alert{Subject: "x"}))
	require.Equal(t, 0, ch.count())
}

func TestSink_Enabled_DispatchesAllChannels(t *testing.T) {
	c1 := &fakeChannel{name: "email"}
	c2 := &fakeChannel{name: "webhook"}
	s := NewSink(staticProvider(Config{Enabled: true}), WithChannels(c1, c2), silentLogger())

	require.NoError(t, s.Dispatch(context.Background(), Alert{Level: LevelWarning, Subject: "满额"}))
	require.Equal(t, 1, c1.count())
	require.Equal(t, 1, c2.count())
}

// ---------------------------------------------------------------------------
// Sink：禁用时 Critical 绝不静默——留兜底日志（运维盲区根因修复）
// ---------------------------------------------------------------------------

// 总开关关闭时，Critical 仍不触达通道，但必须留一行兜底日志（否则支付/对账严重告警无声消失）。
func TestSink_Disabled_CriticalStillLogs(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	var logged int
	s := NewSink(staticProvider(Config{Enabled: false}), WithChannels(ch),
		WithLogger(func(string, ...any) { logged++ }))

	err := s.Dispatch(context.Background(), Alert{Level: LevelCritical, Subject: "支付卡单", Body: "3 笔未入账"})
	require.NoError(t, err)
	require.Equal(t, 0, ch.count(), "禁用时仍不得触达任何通道")
	require.Equal(t, 1, logged, "Critical 在总开关关闭时必须留一行兜底日志，绝不静默")
}

// 非 Critical（Warning/Info）在禁用时保持完全静默，不制造日志噪声。
func TestSink_Disabled_NonCriticalStaysSilent(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	var logged int
	s := NewSink(staticProvider(Config{Enabled: false}), WithChannels(ch),
		WithLogger(func(string, ...any) { logged++ }))

	require.NoError(t, s.Dispatch(context.Background(), Alert{Level: LevelWarning, Subject: "满额"}))
	require.Equal(t, 0, ch.count())
	require.Equal(t, 0, logged, "非 Critical 在禁用时保持静默，不制造日志噪声")
}

// 禁用时的 Critical 兜底日志同受 DedupKey 窗口节流（失败每 5min 复发不刷爆日志）。
func TestSink_Disabled_CriticalLogDeduped(t *testing.T) {
	var logged int
	s := NewSink(staticProvider(Config{Enabled: false}), WithChannels(&fakeChannel{name: "fake"}),
		WithDedupTTL(time.Hour), WithLogger(func(string, ...any) { logged++ }))

	a := Alert{Level: LevelCritical, Subject: "对账失败", DedupKey: "reconcile_failed"}
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.Equal(t, 1, logged, "同 DedupKey 窗口内兜底日志只记一次，不每轮轰炸")
}

// ---------------------------------------------------------------------------
// Sink：去重（同 key 窗口内只发一次）
// ---------------------------------------------------------------------------

func TestSink_Dedup_SameKeyOnceWithinWindow(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	s := NewSink(
		staticProvider(Config{Enabled: true}),
		WithChannels(ch),
		WithDedupTTL(time.Hour),
		silentLogger(),
	)

	a := Alert{Level: LevelCritical, Subject: "卡单", DedupKey: "breakage_anomaly:20260708"}
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.Equal(t, 1, ch.count(), "同 DedupKey 窗口内只应分发一次")
}

func TestSink_Dedup_EmptyKeyNeverDeduped(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	s := NewSink(staticProvider(Config{Enabled: true}), WithChannels(ch), silentLogger())

	a := Alert{Subject: "无 key", DedupKey: ""}
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.Equal(t, 2, ch.count(), "空 DedupKey 不去重，每次都发")
}

func TestSink_Dedup_DifferentKeysBothSent(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	s := NewSink(staticProvider(Config{Enabled: true}), WithChannels(ch), WithDedupTTL(time.Hour), silentLogger())

	require.NoError(t, s.Dispatch(context.Background(), Alert{Subject: "1", DedupKey: "k1"}))
	require.NoError(t, s.Dispatch(context.Background(), Alert{Subject: "2", DedupKey: "k2"}))
	require.Equal(t, 2, ch.count())
}

func TestSink_Dedup_ResendsAfterWindowExpires(t *testing.T) {
	ch := &fakeChannel{name: "fake"}
	var nowNS atomic.Int64
	nowNS.Store(time.Unix(1_700_000_000, 0).UnixNano())
	clock := func() time.Time { return time.Unix(0, nowNS.Load()) }

	s := NewSink(
		staticProvider(Config{Enabled: true}),
		WithChannels(ch),
		WithDedupTTL(10*time.Minute),
		WithClock(clock),
		silentLogger(),
	)

	a := Alert{Subject: "过期重发", DedupKey: "k"}
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.Equal(t, 1, ch.count())

	// 窗口内：抑制。
	nowNS.Add((5 * time.Minute).Nanoseconds())
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.Equal(t, 1, ch.count())

	// 越过窗口：重发。
	nowNS.Add((6 * time.Minute).Nanoseconds())
	require.NoError(t, s.Dispatch(context.Background(), a))
	require.Equal(t, 2, ch.count())
}

// ---------------------------------------------------------------------------
// Sink：通道失败/ panic 不阻断、不 panic
// ---------------------------------------------------------------------------

func TestSink_ChannelFailure_DoesNotBlockOthers(t *testing.T) {
	failing := &fakeChannel{name: "email", failErr: errors.New("smtp down")}
	ok := &fakeChannel{name: "webhook"}
	var logged int
	s := NewSink(
		staticProvider(Config{Enabled: true}),
		WithChannels(failing, ok),
		WithLogger(func(string, ...any) { logged++ }),
	)

	err := s.Dispatch(context.Background(), Alert{Subject: "x"})
	require.Error(t, err, "失败通道的错误应回传供调用方记日志")
	require.Equal(t, 1, failing.count())
	require.Equal(t, 1, ok.count(), "一个通道失败不得阻断其余通道")
	require.GreaterOrEqual(t, logged, 1, "失败应记日志")
}

func TestSink_ChannelPanic_Recovered(t *testing.T) {
	panicking := &fakeChannel{name: "email", doPanic: true}
	ok := &fakeChannel{name: "webhook"}
	s := NewSink(
		staticProvider(Config{Enabled: true}),
		WithChannels(panicking, ok),
		silentLogger(),
	)

	require.NotPanics(t, func() {
		err := s.Dispatch(context.Background(), Alert{Subject: "x"})
		require.Error(t, err)
	})
	require.Equal(t, 1, ok.count(), "panic 通道不得打断其余通道")
}

func TestSink_AllChannelsOK_NilError(t *testing.T) {
	c1 := &fakeChannel{name: "email"}
	c2 := &fakeChannel{name: "webhook"}
	s := NewSink(staticProvider(Config{Enabled: true}), WithChannels(c1, c2), silentLogger())
	require.NoError(t, s.Dispatch(context.Background(), Alert{Subject: "x"}))
}

// ---------------------------------------------------------------------------
// email 通道：无收件人跳过（不算失败）
// ---------------------------------------------------------------------------

func TestEmailChannel_NoRecipients_SkipsWithoutSend(t *testing.T) {
	var called int
	ch := NewEmailChannel(func(_, _, _ string) error {
		called++
		return nil
	})
	err := ch.Send(context.Background(), Config{Email: nil}, Alert{Subject: "x"})
	require.NoError(t, err)
	require.Equal(t, 0, called, "无收件人应跳过发送")
}

func TestEmailChannel_SendsToJoinedRecipients(t *testing.T) {
	var gotReceiver, gotSubject, gotContent string
	var called int
	ch := NewEmailChannel(func(subject, receiver, content string) error {
		called++
		gotSubject, gotReceiver, gotContent = subject, receiver, content
		return nil
	})
	err := ch.Send(context.Background(), Config{Email: []string{"a@x.com", " b@y.com "}},
		Alert{Level: LevelCritical, Subject: "系统卡单", Body: "第一行\n第二行"})
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Equal(t, "a@x.com;b@y.com", gotReceiver, "多收件人以 ';' 连接，供 common.SendEmail 拆分")
	require.Equal(t, "系统卡单", gotSubject)
	require.Contains(t, gotContent, "第一行")
	require.Contains(t, gotContent, "<br/>", "正文换行渲染为 <br/>")
}

func TestEmailChannel_EscapesHTML(t *testing.T) {
	var gotContent string
	ch := NewEmailChannel(func(_, _, content string) error {
		gotContent = content
		return nil
	})
	err := ch.Send(context.Background(), Config{Email: []string{"a@x.com"}},
		Alert{Subject: "x", Body: "<script>alert(1)</script>"})
	require.NoError(t, err)
	require.NotContains(t, gotContent, "<script>", "用户/系统文本必须转义防注入")
	require.Contains(t, gotContent, "&lt;script&gt;")
}

func TestEmailChannel_PropagatesSenderError(t *testing.T) {
	ch := NewEmailChannel(func(_, _, _ string) error { return errors.New("smtp fail") })
	err := ch.Send(context.Background(), Config{Email: []string{"a@x.com"}}, Alert{Subject: "x"})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// webhook 通道：无 URL 跳过；POST JSON；非 2xx 报错
// ---------------------------------------------------------------------------

func TestWebhookChannel_NoURL_Skips(t *testing.T) {
	ch := NewWebhookChannel(nil, time.Second)
	require.NoError(t, ch.Send(context.Background(), Config{WebhookURL: ""}, Alert{Subject: "x"}))
}

func TestWebhookChannel_PostsJSON(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(srv.Client(), 2*time.Second)
	err := ch.Send(context.Background(), Config{WebhookURL: srv.URL},
		Alert{Level: LevelWarning, Subject: "满额", Body: "订阅 18 用量 96%", DedupKey: "sub_exhausted:18:1700"})
	require.NoError(t, err)
	require.Equal(t, "application/json", gotContentType)
	require.Contains(t, string(gotBody), `"level":"warning"`)
	require.Contains(t, string(gotBody), `"subject":"满额"`)
	require.Contains(t, string(gotBody), `"dedup_key":"sub_exhausted:18:1700"`)
}

func TestWebhookChannel_Non2xx_IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(srv.Client(), 2*time.Second)
	err := ch.Send(context.Background(), Config{WebhookURL: srv.URL}, Alert{Subject: "x"})
	require.Error(t, err)
}

func TestWebhookChannel_AppliesTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block // 永不返回，直到测试结束。
	}))
	defer srv.Close()
	defer close(block)

	ch := NewWebhookChannel(nil, 100*time.Millisecond)
	start := time.Now()
	err := ch.Send(context.Background(), Config{WebhookURL: srv.URL}, Alert{Subject: "x"})
	require.Error(t, err, "超时应报错")
	require.Less(t, time.Since(start), 3*time.Second, "应在超时窗口内返回，不无限等待")
}
