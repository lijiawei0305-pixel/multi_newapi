package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTurnstileRuntimeForTest(t *testing.T, enabled bool) {
	t.Helper()
	original := common.GetAuthRuntimeConfig()
	_, err := common.ApplyAuthRuntimeOptions(map[string]string{
		"TurnstileCheckEnabled": strconv.FormatBool(enabled),
		"TurnstileSiteKey":      "test-site-key",
		"TurnstileSecretKey":    "test-secret-key",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := common.ApplyAuthRuntimeOptions(map[string]string{
			"TurnstileCheckEnabled": strconv.FormatBool(original.TurnstileCheckEnabled),
			"TurnstileSiteKey":      original.TurnstileSiteKey,
			"TurnstileSecretKey":    original.TurnstileSecretKey,
		})
		require.NoError(t, restoreErr)
	})
}

func performTurnstileRequest(t *testing.T, host string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("turnstile-host-test"))))
	router.GET("/test", TurnstileCheck(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"passed": true})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	request.Host = host
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestTurnstileCheckUsesHostPolicy(t *testing.T) {
	setTurnstileRuntimeForTest(t, true)
	t.Setenv(common.EnvTurnstileExtraHosts, "portal.example.com")

	mainSite := performTurnstileRequest(t, "www.wedreamhub.com")
	assert.Equal(t, false, mainSite["success"])
	assert.Contains(t, mainSite["message"], "token")

	extraHost := performTurnstileRequest(t, "portal.example.com")
	assert.Equal(t, false, extraHost["success"])

	assert.Equal(t, true, performTurnstileRequest(t, "agent.wedreamhub.com")["passed"])
	assert.Equal(t, true, performTurnstileRequest(t, "oem.example.com")["passed"])
}

func TestTurnstileCheckGlobalSwitchStillDisablesMainSite(t *testing.T) {
	setTurnstileRuntimeForTest(t, false)
	t.Setenv(common.EnvTurnstileExtraHosts, "")

	assert.Equal(t, true, performTurnstileRequest(t, "www.wedreamhub.com")["passed"])
}
