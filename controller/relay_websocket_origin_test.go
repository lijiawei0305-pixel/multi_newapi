package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeUpgraderRejectsUnknownBrowserOrigin(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "")
	t.Setenv("WEBSOCKET_ALLOW_CROSS_ORIGIN", "false")
	t.Setenv("WEBSOCKET_TRUST_PROXY_HEADERS", "false")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = upgrader.Upgrade(w, r, nil)
	}))
	defer server.Close()

	const secret = "sk-never-reflect-this"
	dialer := websocket.Dialer{Subprotocols: []string{"realtime", "openai-insecure-api-key." + secret}}
	header := http.Header{"Origin": []string{"https://evil.example"}}
	_, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	require.Error(t, err)
	require.NotNil(t, response)
	defer response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode)

	body, readErr := io.ReadAll(response.Body)
	require.NoError(t, readErr)
	assert.NotContains(t, string(body), secret)
	assert.Empty(t, response.Header.Get("Sec-WebSocket-Protocol"))
}

func TestRealtimeUpgraderAllowsSameOriginAndOriginlessClients(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "")
	t.Setenv("WEBSOCKET_ALLOW_CROSS_ORIGIN", "false")
	t.Setenv("WEBSOCKET_TRUST_PROXY_HEADERS", "false")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("same origin browser", func(t *testing.T) {
		header := http.Header{"Origin": []string{server.URL}}
		conn, response, err := websocket.DefaultDialer.Dial(wsURL, header)
		require.NoError(t, err)
		if response != nil {
			defer response.Body.Close()
		}
		require.NotNil(t, conn)
		require.NoError(t, conn.Close())
	})

	t.Run("originless SDK", func(t *testing.T) {
		conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		if response != nil {
			defer response.Body.Close()
		}
		require.NotNil(t, conn)
		require.NoError(t, conn.Close())
	})
}
