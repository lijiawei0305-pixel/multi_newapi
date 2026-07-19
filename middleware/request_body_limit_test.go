package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnonymousRequestBodyLimitRejectsOversizedBodyBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLimit := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 1
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = originalLimit })

	for _, test := range []struct {
		name       string
		bodyBytes  int
		wantStatus int
		wantCalled bool
	}{
		{name: "exact limit reaches handler", bodyBytes: 1024, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "one byte over is rejected", bodyBytes: 1025, wantStatus: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			engine := gin.New()
			called := false
			engine.POST("/limited", AnonymousRequestBodyLimit(), func(c *gin.Context) {
				called = true
				data, err := c.GetRawData()
				require.NoError(t, err)
				assert.Len(t, data, test.bodyBytes)
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, "/limited", strings.NewReader(strings.Repeat("x", test.bodyBytes)))

			engine.ServeHTTP(recorder, request)

			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantCalled, called)
		})
	}
}
