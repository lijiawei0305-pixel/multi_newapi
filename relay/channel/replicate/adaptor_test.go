package replicate

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadImagesToBase64StopsBeforeCumulativeBudgetOverrun(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "1000")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "300")
	service.InitHttpClient()
	fetchSetting := system_setting.GetFetchSetting()
	original := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = original })
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(make([]byte, 75))
	}))
	t.Cleanup(server.Close)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	images, err := downloadImagesToBase64(c, []string{server.URL, server.URL}, service.NewImageResponseEncodedBudget())

	assert.Nil(t, images)
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrImageResponseBudgetExceeded))
	assert.Equal(t, int32(1), requests.Load(), "the second URL must not be fetched after the aggregate budget is exhausted")
}
