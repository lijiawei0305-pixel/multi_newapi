package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 这些测试钉死 money 端点限流的核心安全属性：**按认证用户计桶，而非按 c.ClientIP()**。
// 背景（audit 2026-07-17 · High）：CriticalRateLimit 的桶 key 含 c.ClientIP()，而全仓从未
// SetTrustedProxies → gin 默认信任 0.0.0.0/0 → ClientIP() 返回攻击者自填的最左 X-Forwarded-For。
// 于是登录态攻击者每请求换一个 XFF 即落进全新桶，20/20min 闸门形同虚设（兑换码即钱）。
// 修复：money 端点全在 UserAuth 之后，改用 CriticalUserRateLimit（按 c.GetInt("id") 计桶），
// 与伪造 XFF 完全无关。

func runLimiter(h gin.HandlerFunc, userID int, xff string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/tenant/redeem", nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	req.RemoteAddr = "127.0.0.1:12345"
	c.Request = req
	if userID != 0 {
		c.Set("id", userID)
	}
	h(c)
	return c
}

// withCriticalLimit 临时把 CriticalRateLimit 配成小额度快速触顶，并强制走内存限流器（无 Redis）。
func withCriticalLimit(t *testing.T, num int, durationSec int64) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	save := struct {
		enabled  bool
		num      int
		duration int64
		redis    bool
	}{common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration, common.RedisEnabled}
	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = num
	common.CriticalRateLimitDuration = durationSec
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.CriticalRateLimitEnable = save.enabled
		common.CriticalRateLimitNum = save.num
		common.CriticalRateLimitDuration = save.duration
		common.RedisEnabled = save.redis
	})
}

// 同一认证用户换不同伪造 XFF，仍共用同一个桶：第 num+1 次必被拦。
func TestCriticalUserRateLimit_SameUserDifferentSpoofedIPShareBucket(t *testing.T) {
	withCriticalLimit(t, 2, 600)
	h := CriticalUserRateLimit()
	const uid = 900101

	require.False(t, runLimiter(h, uid, "9.9.9.9").IsAborted(), "第1次应放行")
	require.False(t, runLimiter(h, uid, "8.8.8.8").IsAborted(), "第2次（换IP）应放行")
	c := runLimiter(h, uid, "7.7.7.7") // 又换一个 IP
	require.True(t, c.IsAborted(), "第3次应被拦——伪造 XFF 无法逃出按用户计的桶")
	require.Equal(t, http.StatusTooManyRequests, c.Writer.Status())
}

// 不同用户各自独立计桶：A 触顶不影响 B。
func TestCriticalUserRateLimit_DifferentUsersIsolated(t *testing.T) {
	withCriticalLimit(t, 2, 600)
	h := CriticalUserRateLimit()
	const uidA, uidB = 900201, 900202

	require.False(t, runLimiter(h, uidA, "1.1.1.1").IsAborted())
	require.False(t, runLimiter(h, uidA, "1.1.1.1").IsAborted())
	require.True(t, runLimiter(h, uidA, "1.1.1.1").IsAborted(), "A 已触顶")

	require.False(t, runLimiter(h, uidB, "1.1.1.1").IsAborted(), "B 用同一 IP 仍独立计桶，不受 A 影响")
}

// 缺认证（无 user id）必须拒绝：防止被挂到 UserAuth 之前而退化成不设防。
func TestCriticalUserRateLimit_RequiresAuth(t *testing.T) {
	withCriticalLimit(t, 20, 600)
	c := runLimiter(CriticalUserRateLimit(), 0, "1.1.1.1")
	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusUnauthorized, c.Writer.Status())
}

// 开关关闭时为直通（与其余限流器一致的 defNext 语义）。
func TestCriticalUserRateLimit_DisabledIsPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	save := common.CriticalRateLimitEnable
	common.CriticalRateLimitEnable = false
	t.Cleanup(func() { common.CriticalRateLimitEnable = save })

	h := CriticalUserRateLimit()
	for i := 0; i < 50; i++ {
		require.False(t, runLimiter(h, 900301, "1.1.1.1").IsAborted(), "关闭时应始终直通")
	}
}
