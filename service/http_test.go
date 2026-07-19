package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCopyUpstreamResponseHeadersBlocksProviderOriginControl(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	source := http.Header{
		"Content-Type":                []string{"audio/mpeg"},
		"Connection":                  []string{"Content-Type, X-Request-Id"},
		"X-Request-Id":                []string{"provider-request"},
		"Set-Cookie":                  []string{"gateway_admin=true; Domain=example.com"},
		"Set-Cookie2":                 []string{"legacy=true"},
		"Proxy-Authenticate":          []string{"Basic realm=provider"},
		"Server":                      []string{"provider-internal"},
		"Access-Control-Allow-Origin": []string{"https://attacker.example"},
		"Strict-Transport-Security":   []string{"max-age=0"},
		"Cache-Control":               []string{"public, max-age=86400"},
		"X-RateLimit-Remaining":       []string{"9"},
		common.RequestIdKey:           []string{"captured-upstream-id"},
	}
	destination := make(http.Header)

	CopyUpstreamResponseHeaders(c, destination, source)

	assert.Empty(t, destination.Get("Content-Type"), "Connection-declared headers are hop-by-hop")
	assert.Empty(t, destination.Get("X-Request-Id"))
	assert.Empty(t, destination.Get("Set-Cookie"))
	assert.Empty(t, destination.Get("Set-Cookie2"))
	assert.Empty(t, destination.Get("Proxy-Authenticate"))
	assert.Empty(t, destination.Get("Server"))
	assert.Empty(t, destination.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, destination.Get("Strict-Transport-Security"))
	assert.Equal(t, "no-store, private", destination.Get("Cache-Control"))
	assert.Equal(t, "9", destination.Get("X-RateLimit-Remaining"))
	assert.Equal(t, "captured-upstream-id", c.GetString(common.UpstreamRequestIdKey))
}

func TestShouldCopyUpstreamHeaderUsesExplicitSafeAllowlist(t *testing.T) {
	assert.True(t, ShouldCopyUpstreamHeader(nil, "Content-Disposition", []string{`attachment; filename="audio.mp3"`}))
	assert.True(t, ShouldCopyUpstreamHeader(nil, "OpenAI-Processing-Ms", []string{"42"}))
	assert.False(t, ShouldCopyUpstreamHeader(nil, "Transfer-Encoding", []string{"chunked"}))
	assert.False(t, ShouldCopyUpstreamHeader(nil, "X-Provider-Secret", []string{"secret"}))
	assert.False(t, ShouldCopyUpstreamHeader(nil, "Content-Type", nil))
}
