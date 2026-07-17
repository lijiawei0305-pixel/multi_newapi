package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 钉死 part① 客户端 IP 信任模型（audit 2026-07-17 · High）：
// 全仓从未 SetTrustedProxies → gin 默认信任 0.0.0.0/0 → ClientIP() 取攻击者自填的最左
// X-Forwarded-For。这既让 IP 限流可被换头绕过，又可反向武器化（拿受害者 IP 把其踢下线）。
// 源站前是 Cloudflare（CF Origin CA 在用），故信任 CF-Connecting-IP 权威头取真实客户端。

func newConfiguredEngine(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(w)
	ConfigureTrustedClientIP(engine)
	return c, w
}

// 有 CF-Connecting-IP 时：ClientIP() 取该头，**无视伪造的 X-Forwarded-For**。
func TestConfigureTrustedClientIP_TrustsCloudflareHeaderOverSpoofedXFF(t *testing.T) {
	c, _ := newConfiguredEngine(t)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/tenant/redeem", nil)
	c.Request.RemoteAddr = "127.0.0.1:1234"
	c.Request.Header.Set("CF-Connecting-IP", "203.0.113.9")
	c.Request.Header.Set("X-Forwarded-For", "9.9.9.9, 10.0.0.1") // 攻击者自填，必须被无视

	require.Equal(t, "203.0.113.9", c.ClientIP(),
		"应取 CF-Connecting-IP 权威头，而非攻击者自填的最左 XFF")
}

// 无 CF 头时：优雅回退（不 panic、返回非空），保证不比现状更糟。
func TestConfigureTrustedClientIP_FallsBackWhenNoCloudflareHeader(t *testing.T) {
	c, _ := newConfiguredEngine(t)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	c.Request.RemoteAddr = "127.0.0.1:1234"

	require.NotEmpty(t, c.ClientIP(), "无 CF 头应回退到既有解析逻辑，不应返回空")
}

// 守卫：配置后 TrustedPlatform 必须是 CF-Connecting-IP，防止有人静默改回/移除。
func TestConfigureTrustedClientIP_SetsCloudflarePlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, engine := gin.CreateTestContext(httptest.NewRecorder())
	ConfigureTrustedClientIP(engine)
	require.Equal(t, gin.PlatformCloudflare, engine.TrustedPlatform)
	require.Equal(t, "CF-Connecting-IP", engine.TrustedPlatform)
}
