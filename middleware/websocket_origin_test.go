package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsWebSocketOriginAllowed(t *testing.T) {
	t.Setenv(websocketAllowedOriginsEnv, "")
	t.Setenv(websocketAllowCrossOriginEnv, "")
	t.Setenv(websocketTrustProxyHeadersEnv, "")

	tests := []struct {
		name       string
		requestURL string
		host       string
		origin     string
		allowed    bool
	}{
		{name: "non-browser without origin", requestURL: "ws://api.example.test/v1/realtime", host: "api.example.test", allowed: true},
		{name: "same origin ws maps to http", requestURL: "ws://api.example.test/v1/realtime", host: "api.example.test", origin: "http://api.example.test", allowed: true},
		{name: "same origin wss maps to https", requestURL: "wss://api.example.test/v1/realtime", host: "api.example.test", origin: "https://api.example.test", allowed: true},
		{name: "default https port normalizes", requestURL: "wss://api.example.test/v1/realtime", host: "api.example.test:443", origin: "https://api.example.test", allowed: true},
		{name: "scheme mismatch", requestURL: "ws://api.example.test/v1/realtime", host: "api.example.test", origin: "https://api.example.test", allowed: false},
		{name: "unknown origin", requestURL: "wss://api.example.test/v1/realtime", host: "api.example.test", origin: "https://evil.example", allowed: false},
		{name: "opaque null origin", requestURL: "wss://api.example.test/v1/realtime", host: "api.example.test", origin: "null", allowed: false},
		{name: "origin with credentials", requestURL: "wss://api.example.test/v1/realtime", host: "api.example.test", origin: "https://user@api.example.test", allowed: false},
		{name: "origin with path", requestURL: "wss://api.example.test/v1/realtime", host: "api.example.test", origin: "https://api.example.test/path", allowed: false},
		{name: "non-default port mismatch", requestURL: "wss://api.example.test:8443/v1/realtime", host: "api.example.test:8443", origin: "https://api.example.test", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.requestURL, nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			assert.Equal(t, tt.allowed, IsWebSocketOriginAllowed(req))
		})
	}
}

func TestIsWebSocketOriginAllowed_ExplicitCrossOriginControls(t *testing.T) {
	t.Setenv(websocketTrustProxyHeadersEnv, "")

	t.Run("exact allowlist", func(t *testing.T) {
		t.Setenv(websocketAllowedOriginsEnv, "https://console.example.test/, https://admin.example.test:443")
		t.Setenv(websocketAllowCrossOriginEnv, "false")
		req := newOriginRequest("https://api.example.test", "https://console.example.test")
		require.True(t, IsWebSocketOriginAllowed(req))
	})

	t.Run("unknown origin stays denied with allowlist", func(t *testing.T) {
		t.Setenv(websocketAllowedOriginsEnv, "https://console.example.test")
		t.Setenv(websocketAllowCrossOriginEnv, "false")
		req := newOriginRequest("https://api.example.test", "https://evil.example")
		require.False(t, IsWebSocketOriginAllowed(req))
	})

	t.Run("deliberate cross-origin switch", func(t *testing.T) {
		t.Setenv(websocketAllowedOriginsEnv, "")
		t.Setenv(websocketAllowCrossOriginEnv, "true")
		req := newOriginRequest("https://api.example.test", "https://partner.example")
		require.True(t, IsWebSocketOriginAllowed(req))
	})

	t.Run("switch does not accept malformed browser origin", func(t *testing.T) {
		t.Setenv(websocketAllowedOriginsEnv, "")
		t.Setenv(websocketAllowCrossOriginEnv, "true")
		req := newOriginRequest("https://api.example.test", "null")
		require.False(t, IsWebSocketOriginAllowed(req))
	})
}

func TestIsWebSocketOriginAllowed_TLSProxyPreservingHost(t *testing.T) {
	t.Setenv(websocketAllowedOriginsEnv, "")
	t.Setenv(websocketAllowCrossOriginEnv, "false")
	t.Setenv(websocketTrustProxyHeadersEnv, "false")

	req := newOriginRequest("http://api.example.test", "https://api.example.test")
	req.Header.Set("X-Forwarded-Proto", "https")
	require.True(t, IsWebSocketOriginAllowed(req))
}

func TestIsWebSocketOriginAllowed_TrustedReverseProxy(t *testing.T) {
	t.Setenv(websocketAllowedOriginsEnv, "")
	t.Setenv(websocketAllowCrossOriginEnv, "false")

	req := newOriginRequest("http://internal:3000", "https://api.example.test")
	req.Header.Set("X-Forwarded-Host", "api.example.test")
	req.Header.Set("X-Forwarded-Proto", "https")

	t.Setenv(websocketTrustProxyHeadersEnv, "false")
	require.False(t, IsWebSocketOriginAllowed(req), "untrusted client-supplied forwarding headers must not widen the policy")

	t.Setenv(websocketTrustProxyHeadersEnv, "true")
	require.True(t, IsWebSocketOriginAllowed(req), "trusted TLS-terminating proxy should preserve the public origin")
}

func TestIsWebSocketOriginAllowed_StandardForwardedHeader(t *testing.T) {
	t.Setenv(websocketAllowedOriginsEnv, "")
	t.Setenv(websocketAllowCrossOriginEnv, "false")
	t.Setenv(websocketTrustProxyHeadersEnv, "true")

	req := newOriginRequest("http://internal:3000", "https://api.example.test:8443")
	req.Header.Set("Forwarded", `for=192.0.2.1;proto=https;host="api.example.test:8443"`)
	require.True(t, IsWebSocketOriginAllowed(req))
}

func TestWebSocketOriginGuard_RejectsBeforeCredentialProcessing(t *testing.T) {
	t.Setenv(websocketAllowedOriginsEnv, "")
	t.Setenv(websocketAllowCrossOriginEnv, "false")
	t.Setenv(websocketTrustProxyHeadersEnv, "false")
	gin.SetMode(gin.TestMode)

	var credentialHandlerReached atomic.Bool
	engine := gin.New()
	engine.Use(WebSocketOriginGuard())
	engine.Use(func(c *gin.Context) {
		credentialHandlerReached.Store(true)
		c.Next()
	})
	engine.GET("/v1/realtime", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	const secret = "sk-must-not-be-reflected"
	dialer := websocket.Dialer{Subprotocols: []string{"realtime", "openai-insecure-api-key." + secret}}
	header := http.Header{"Origin": []string{"https://evil.example"}}
	_, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/realtime", header)
	require.Error(t, err)
	require.NotNil(t, response)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.False(t, credentialHandlerReached.Load(), "unknown Origin must be rejected before token middleware")

	body, readErr := io.ReadAll(response.Body)
	require.NoError(t, readErr)
	assert.NotContains(t, string(body), secret)
	for name, values := range response.Header {
		assert.NotContains(t, name+strings.Join(values, ","), secret)
	}
}

func newOriginRequest(requestOrigin, browserOrigin string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, requestOrigin+"/v1/realtime", nil)
	req.Header.Set("Origin", browserOrigin)
	return req
}
