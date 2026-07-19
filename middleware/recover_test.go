package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRelayPanicRecoverDoesNotExposePanicPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const canary = "private-prompt-and-payment-token"
	output := &concurrentLogBuffer{}

	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultWriter
	gin.DefaultWriter = output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = originalWriter
		common.LogWriterMu.Unlock()
	})

	engine := gin.New()
	engine.Use(RelayPanicRecover())
	engine.GET("/panic", func(*gin.Context) { panic(canary) })
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "https://github.com/Calcium-Ion/new-api")
	assert.NotContains(t, recorder.Body.String(), canary)
	assert.Contains(t, output.String(), "panic detected: error_type=string")
	assert.Contains(t, output.String(), "sha256=")
	assert.NotContains(t, output.String(), canary)
}
