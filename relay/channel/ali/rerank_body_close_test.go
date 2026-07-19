package ali

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

type failingAliRerankBody struct {
	closed atomic.Bool
}

func (b *failingAliRerankBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (b *failingAliRerankBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestRerankHandlerClosesBodyOnReadError(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := &failingAliRerankBody{}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	apiErr, _ := RerankHandler(c, resp, &relaycommon.RelayInfo{})

	require.NotNil(t, apiErr)
	assert.True(t, body.closed.Load())
}
