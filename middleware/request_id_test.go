package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestIdTestContextKey struct{}

func TestRequestIdPropagatesWithoutStringContextKeyCollision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const callerValue = "caller-context-value"

	var ginRequestId string
	var contextRequestId string
	var contextHasRequestId bool
	var preservedCallerValue string
	var publicStringKeyValue any

	engine := gin.New()
	engine.Use(RequestId())
	engine.GET("/request-id", func(c *gin.Context) {
		ginRequestId = c.GetString(common.RequestIdKey)
		contextRequestId, contextHasRequestId = common.RequestIdFromContext(c.Request.Context())
		preservedCallerValue, _ = c.Request.Context().Value(requestIdTestContextKey{}).(string)
		publicStringKeyValue = c.Request.Context().Value(common.RequestIdKey)
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/request-id", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestIdTestContextKey{}, callerValue))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.NotEmpty(t, ginRequestId)
	assert.Equal(t, ginRequestId, contextRequestId)
	assert.True(t, contextHasRequestId)
	assert.Equal(t, ginRequestId, recorder.Header().Get(common.RequestIdKey))
	assert.Equal(t, callerValue, preservedCallerValue)
	assert.Nil(t, publicStringKeyValue, "request context must not use the public string header key")
}
