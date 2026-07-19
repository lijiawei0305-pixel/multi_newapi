package vidu

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoResponseRejectsOversizedProviderBodyWithoutProcessingPartialData(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "4")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("secret"))}

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{})

	require.NotNil(t, taskErr)
	assert.Empty(t, taskID)
	assert.Nil(t, taskData)
	assert.Equal(t, "read_response_body_failed", taskErr.Code)
	assert.True(t, errors.Is(taskErr.Error, common.ErrReadLimitExceeded))
	assert.NotContains(t, taskErr.Message, "secret")
	assert.Empty(t, recorder.Body.String())
}
