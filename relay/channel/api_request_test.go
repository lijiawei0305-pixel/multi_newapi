package channel

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

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestDoRequestCancelsProviderWhenDownstreamContextEnds(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	service.InitHttpClient()

	providerStarted := make(chan struct{})
	providerCanceled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(providerStarted)
		<-r.Context().Done()
		close(providerCanceled)
	}))
	defer provider.Close()

	downstreamCtx, cancelDownstream := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(downstreamCtx)

	// Deliberately construct the provider request with a background context.
	// doRequest must still bind it to the caller before client.Do.
	providerRequest, err := http.NewRequestWithContext(context.Background(), http.MethodPost, provider.URL, nil)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, requestErr := DoRequest(c, providerRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
		result <- requestErr
	}()

	select {
	case <-providerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("provider request did not start")
	}
	cancelDownstream()

	select {
	case <-providerCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not observe downstream cancellation")
	}
	select {
	case requestErr := <-result:
		require.Error(t, requestErr)
		require.EqualError(t, requestErr, "upstream error: do request failed")
	case <-time.After(2 * time.Second):
		t.Fatal("DoRequest did not return after downstream cancellation")
	}
}

func TestDoRequestNonStreamDeadlineBeforeResponseHeaders(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	t.Setenv("RELAY_NON_STREAM_TIMEOUT_SECONDS", "1")
	service.InitHttpClient()

	providerStarted := make(chan struct{})
	providerCanceled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(providerStarted)
		<-r.Context().Done()
		close(providerCanceled)
	}))
	defer provider.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	providerRequest, err := http.NewRequest(http.MethodPost, provider.URL+"?api_key=must-not-leak", nil)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, requestErr := DoRequest(c, providerRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
		result <- requestErr
	}()
	select {
	case <-providerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("provider request did not start")
	}
	select {
	case requestErr := <-result:
		require.EqualError(t, requestErr, "upstream error: do request failed")
		require.NotContains(t, requestErr.Error(), provider.URL)
		require.NotContains(t, requestErr.Error(), "must-not-leak")
	case <-time.After(7 * time.Second):
		t.Fatal("non-stream header deadline did not fire")
	}
	select {
	case <-providerCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not observe non-stream header deadline")
	}
}

func TestDoRequestNonStreamDeadlineRemainsActiveForResponseBody(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	t.Setenv("RELAY_NON_STREAM_TIMEOUT_SECONDS", "1")
	service.InitHttpClient()

	bodyStarted := make(chan struct{})
	providerCanceled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"partial":"`)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(bodyStarted)
		<-r.Context().Done()
		close(providerCanceled)
	}))
	defer provider.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	providerRequest, err := http.NewRequest(http.MethodPost, provider.URL, nil)
	require.NoError(t, err)
	resp, err := DoRequest(c, providerRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	defer resp.Body.Close()
	select {
	case <-bodyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("provider body did not start")
	}

	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(resp.Body)
		readDone <- readErr
	}()
	select {
	case readErr := <-readDone:
		require.Error(t, readErr)
	case <-time.After(7 * time.Second):
		t.Fatal("non-stream body deadline did not remain active after headers")
	}
	select {
	case <-providerCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not observe non-stream body deadline")
	}
}

func TestDoRequestBodyCloseReleasesNonStreamContext(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	t.Setenv("RELAY_NON_STREAM_TIMEOUT_SECONDS", "30")
	service.InitHttpClient()

	providerCanceled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "x")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
		close(providerCanceled)
	}))
	defer provider.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	providerRequest, err := http.NewRequest(http.MethodPost, provider.URL, nil)
	require.NoError(t, err)
	resp, err := DoRequest(c, providerRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	select {
	case <-providerCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("closing response body did not release its non-stream context")
	}
}

func TestDoRequestCancellationInterruptsSlowResponseBody(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	service.InitHttpClient()

	bodyStarted := make(chan struct{})
	providerCanceled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"partial":"`)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(bodyStarted)
		<-r.Context().Done()
		close(providerCanceled)
	}))
	defer provider.Close()

	downstreamCtx, cancelDownstream := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(downstreamCtx)
	providerRequest, err := http.NewRequest(http.MethodPost, provider.URL, nil)
	require.NoError(t, err)

	resp, err := DoRequest(c, providerRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	defer resp.Body.Close()
	select {
	case <-bodyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not start the response body")
	}

	cancelDownstream()
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(resp.Body)
		readDone <- readErr
	}()

	select {
	case <-providerCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("provider body remained active after downstream cancellation")
	}
	select {
	case readErr := <-readDone:
		require.Error(t, readErr)
	case <-time.After(2 * time.Second):
		t.Fatal("response body read did not unblock after cancellation")
	}
}

type blockingPreHeaderPingWriter struct {
	gin.ResponseWriter

	writeStarted chan struct{}
	releaseWrite chan struct{}
	writeErr     error
	panicOnWrite bool
	startOnce    sync.Once
	activeWrites atomic.Int32
	maxActive    atomic.Int32
	writeCount   atomic.Int32
}

func (w *blockingPreHeaderPingWriter) Write(data []byte) (int, error) {
	w.writeCount.Add(1)
	active := w.activeWrites.Add(1)
	defer w.activeWrites.Add(-1)
	for {
		maximum := w.maxActive.Load()
		if active <= maximum || w.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	w.startOnce.Do(func() { close(w.writeStarted) })
	if w.releaseWrite != nil {
		<-w.releaseWrite
	}
	if w.panicOnWrite {
		panic("test response writer panic")
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseWriter.Write(data)
}

func newPreHeaderPingTestContext(t *testing.T, writer *blockingPreHeaderPingWriter) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	writer.ResponseWriter = c.Writer
	c.Writer = writer
	return c
}

func TestDoRequestWithPreHeaderPingsSerializesResponseWriterHandoff(t *testing.T) {
	writer := &blockingPreHeaderPingWriter{
		writeStarted: make(chan struct{}),
		releaseWrite: make(chan struct{}),
	}
	c := newPreHeaderPingTestContext(t, writer)

	allowUpstreamResponse := make(chan struct{})
	upstreamFinished := make(chan struct{})
	expectedResponse := &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
	requestResult := make(chan upstreamRequestResult, 1)
	go func() {
		response, err := doRequestWithPreHeaderPings(c, time.Millisecond, func() (*http.Response, error) {
			<-allowUpstreamResponse
			close(upstreamFinished)
			return expectedResponse, nil
		})
		requestResult <- upstreamRequestResult{response: response, err: err}
	}()

	select {
	case <-writer.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("pre-header ping did not start")
	}
	close(allowUpstreamResponse)
	select {
	case <-upstreamFinished:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not finish")
	}

	// The provider response is ready, but the handoff cannot occur while a
	// pre-header ping still owns the writer.
	select {
	case <-requestResult:
		t.Fatal("request returned while a pre-header ping write was still active")
	default:
	}

	close(writer.releaseWrite)
	select {
	case result := <-requestResult:
		require.NoError(t, result.err)
		require.Same(t, expectedResponse, result.response)
	case <-time.After(2 * time.Second):
		t.Fatal("request did not return after the ping write completed")
	}

	_, err := c.Writer.Write([]byte("data: downstream handler\n\n"))
	require.NoError(t, err)
	require.Equal(t, int32(1), writer.maxActive.Load(), "ping and protocol handler writes must never overlap")
}

func TestDoRequestWithPreHeaderPingsJoinsInFlightWriteAfterCancellation(t *testing.T) {
	writer := &blockingPreHeaderPingWriter{
		writeStarted: make(chan struct{}),
		releaseWrite: make(chan struct{}),
	}
	c := newPreHeaderPingTestContext(t, writer)
	downstreamContext, cancelDownstream := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(downstreamContext)

	upstreamCanceled := make(chan struct{})
	requestResult := make(chan upstreamRequestResult, 1)
	go func() {
		response, err := doRequestWithPreHeaderPings(c, time.Millisecond, func() (*http.Response, error) {
			<-c.Request.Context().Done()
			close(upstreamCanceled)
			return nil, c.Request.Context().Err()
		})
		requestResult <- upstreamRequestResult{response: response, err: err}
	}()

	select {
	case <-writer.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("pre-header ping did not start")
	}
	cancelDownstream()
	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not observe downstream cancellation")
	}
	select {
	case <-requestResult:
		t.Fatal("canceled request returned while its ping write was still active")
	default:
	}

	close(writer.releaseWrite)
	select {
	case result := <-requestResult:
		require.Nil(t, result.response)
		require.ErrorIs(t, result.err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("canceled request did not return after its ping write completed")
	}
	require.Zero(t, writer.activeWrites.Load())
	require.Equal(t, int32(1), writer.writeCount.Load(), "cancellation must leave no detached ping writer")
}

func TestDoRequestWithPreHeaderPingsStopsAfterWriteFailure(t *testing.T) {
	writeFailure := errors.New("downstream write failed")
	writer := &blockingPreHeaderPingWriter{
		writeStarted: make(chan struct{}),
		writeErr:     writeFailure,
	}
	c := newPreHeaderPingTestContext(t, writer)

	allowUpstreamResponse := make(chan struct{})
	expectedResponse := &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
	requestResult := make(chan upstreamRequestResult, 1)
	go func() {
		response, err := doRequestWithPreHeaderPings(c, time.Millisecond, func() (*http.Response, error) {
			<-allowUpstreamResponse
			return expectedResponse, nil
		})
		requestResult <- upstreamRequestResult{response: response, err: err}
	}()

	select {
	case <-writer.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("pre-header ping did not start")
	}
	close(allowUpstreamResponse)
	select {
	case result := <-requestResult:
		require.NoError(t, result.err)
		require.Same(t, expectedResponse, result.response)
	case <-time.After(2 * time.Second):
		t.Fatal("provider response was lost after a downstream ping failure")
	}
	require.Equal(t, int32(1), writer.writeCount.Load(), "a failed ping must disable later pre-header writes")
}

func TestSendPreHeaderPingRecoversWriterPanic(t *testing.T) {
	writer := &blockingPreHeaderPingWriter{
		writeStarted: make(chan struct{}),
		panicOnWrite: true,
	}
	c := newPreHeaderPingTestContext(t, writer)

	err := sendPreHeaderPing(c)
	require.Error(t, err)
	require.Contains(t, err.Error(), "panic recovered")
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}
