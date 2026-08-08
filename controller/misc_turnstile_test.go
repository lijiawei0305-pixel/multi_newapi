package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setStatusTurnstileRuntimeForTest(t *testing.T, enabled bool) {
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

func getStatusTurnstileCheck(t *testing.T, host string) bool {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	ctx.Request.Host = host

	GetStatus(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data struct {
			TurnstileCheck bool `json:"turnstile_check"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload.Data.TurnstileCheck
}

func TestGetStatusUsesTurnstileHostPolicy(t *testing.T) {
	setStatusTurnstileRuntimeForTest(t, true)
	t.Setenv(common.EnvTurnstileExtraHosts, "portal.example.com")

	assert.True(t, getStatusTurnstileCheck(t, "www.wedreamhub.com"))
	assert.True(t, getStatusTurnstileCheck(t, "portal.example.com"))
	assert.False(t, getStatusTurnstileCheck(t, "agent.wedreamhub.com"))
	assert.False(t, getStatusTurnstileCheck(t, "oem.example.com"))
}

func TestGetStatusHonorsDisabledTurnstileSwitch(t *testing.T) {
	setStatusTurnstileRuntimeForTest(t, false)
	t.Setenv(common.EnvTurnstileExtraHosts, "")

	assert.False(t, getStatusTurnstileCheck(t, "www.wedreamhub.com"))
}
