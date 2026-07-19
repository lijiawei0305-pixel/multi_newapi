package siliconflow

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingSiliconFlowBody struct {
	closed atomic.Bool
}

func (b *failingSiliconFlowBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (b *failingSiliconFlowBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestSiliconFlowRerankHandlerClosesBodyOnReadError(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := &failingSiliconFlowBody{}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	_, apiErr := siliconflowRerankHandler(c, &relaycommon.RelayInfo{}, resp)

	require.NotNil(t, apiErr)
	assert.True(t, body.closed.Load())
}
