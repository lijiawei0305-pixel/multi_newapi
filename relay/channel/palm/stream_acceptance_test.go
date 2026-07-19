package palm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type palmTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *palmTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

type palmCancelBody struct {
	ctx    context.Context
	closed atomic.Bool
}

func (b *palmCancelBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *palmCancelBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestPaLMStreamAcceptanceStatesAndBodyClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		body         string
		wantError    bool
		wantExplicit bool
		wantAccepted bool
	}{
		{
			name:         "provider error remains retryable",
			body:         `{"error":{"code":400,"message":"denied","status":"INVALID_ARGUMENT"}}`,
			wantError:    true,
			wantExplicit: true,
		},
		{
			name:         "malformed body is acceptance unknown",
			body:         `{bad`,
			wantError:    true,
			wantAccepted: true,
		},
		{
			name:         "complete response succeeds",
			body:         `{"candidates":[{"author":"1","content":"hi"}]}`,
			wantAccepted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/generateMessage", nil)
			body := &palmTrackingBody{Reader: strings.NewReader(test.body)}
			resp := &http.Response{StatusCode: http.StatusOK, Body: body}

			apiErr, _ := palmStreamHandler(c, resp)

			if test.wantError {
				require.NotNil(t, apiErr)
			} else {
				require.Nil(t, apiErr)
			}
			assert.Equal(t, test.wantExplicit, service.IsExplicitUpstreamRejection(apiErr))
			assert.Equal(t, test.wantAccepted, service.IsUpstreamAccepted(c))
			assert.True(t, body.closed.Load(), "response body must close on every exit")
		})
	}
}

func TestPaLMStreamCancellationReturnsAndClosesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/generateMessage", nil).WithContext(ctx)
	body := &palmCancelBody{ctx: ctx}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	result := make(chan *types.NewAPIError, 1)
	go func() {
		apiErr, _ := palmStreamHandler(c, resp)
		result <- apiErr
	}()

	cancel()
	select {
	case apiErr := <-result:
		require.NotNil(t, apiErr)
		assert.True(t, service.IsUpstreamAccepted(c))
		assert.True(t, body.closed.Load())
	case <-time.After(time.Second):
		t.Fatal("canceled PaLM stream did not return")
	}
}
