package ollama

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

type failingOllamaBody struct {
	closed atomic.Bool
}

func (b *failingOllamaBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (b *failingOllamaBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestOllamaHandlersCloseBodyOnReadError(t *testing.T) {
	handlers := []struct {
		name string
		call func(*gin.Context, *relaycommon.RelayInfo, *http.Response) error
	}{
		{name: "chat", call: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) error {
			_, apiErr := ollamaChatHandler(c, info, resp)
			return apiErr
		}},
		{name: "embedding", call: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) error {
			_, apiErr := ollamaEmbeddingHandler(c, info, resp)
			return apiErr
		}},
	}
	for _, handler := range handlers {
		t.Run(handler.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			body := &failingOllamaBody{}
			resp := &http.Response{StatusCode: http.StatusOK, Body: body}

			err := handler.call(c, &relaycommon.RelayInfo{}, resp)

			require.Error(t, err)
			assert.True(t, body.closed.Load())
		})
	}
}
