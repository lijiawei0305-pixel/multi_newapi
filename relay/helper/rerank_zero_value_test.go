package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRerankExplicitZeroTopNIsRejectedInsteadOfDefaulted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(`{
		"model":"rerank","query":"q","documents":["d"],"top_n":0
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateRerankRequest(c)
	assert.Nil(t, request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "top_n must be greater than zero")
}
