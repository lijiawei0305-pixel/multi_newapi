package cohere

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cohereTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *cohereTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

type cohereCancelBody struct {
	ctx    context.Context
	closed atomic.Bool
}

func (b *cohereCancelBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *cohereCancelBody) Close() error {
	b.closed.Store(true)
	return nil
}

func newCohereStreamContext(body io.ReadCloser) (*gin.Context, *relaycommon.RelayInfo, *http.Response) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/chat", nil)
	info := &relaycommon.RelayInfo{
		StartTime:   time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "command-test"},
	}
	return c, info, &http.Response{StatusCode: http.StatusOK, Body: body}
}

func TestCohereStreamAcceptanceStatesAndBodyClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		body         string
		wantError    bool
		wantExplicit bool
		wantAccepted bool
	}{
		{
			name:         "first stream error remains retryable",
			body:         "{\"event_type\":\"stream-error\",\"finish_reason\":\"ERROR\"}\n",
			wantError:    true,
			wantExplicit: true,
		},
		{
			name:         "malformed event is acceptance unknown",
			body:         "{bad\n",
			wantError:    true,
			wantAccepted: true,
		},
		{
			name:         "completed response succeeds",
			body:         "{\"event_type\":\"text-generation\",\"text\":\"hi\"}\n{\"event_type\":\"stream-end\",\"is_finished\":true,\"finish_reason\":\"COMPLETE\",\"response\":{\"meta\":{\"billed_units\":{\"input_tokens\":1,\"output_tokens\":1}}}}\n",
			wantAccepted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &cohereTrackingBody{Reader: strings.NewReader(test.body)}
			c, info, resp := newCohereStreamContext(body)

			usage, apiErr := cohereStreamHandler(c, info, resp)

			if test.wantError {
				require.NotNil(t, apiErr)
			} else {
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
			}
			assert.Equal(t, test.wantExplicit, service.IsExplicitUpstreamRejection(apiErr))
			assert.Equal(t, test.wantAccepted, service.IsUpstreamAccepted(c))
			assert.True(t, body.closed.Load(), "response body must close on every exit")
		})
	}
}

func TestCohereStreamCancellationReturnsAndClosesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	body := &cohereCancelBody{ctx: ctx}
	c, info, resp := newCohereStreamContext(body)
	c.Request = c.Request.WithContext(ctx)
	result := make(chan *types.NewAPIError, 1)
	go func() {
		_, apiErr := cohereStreamHandler(c, info, resp)
		result <- apiErr
	}()

	cancel()
	select {
	case apiErr := <-result:
		require.NotNil(t, apiErr)
		assert.True(t, service.IsUpstreamAccepted(c))
		assert.True(t, body.closed.Load())
	case <-time.After(time.Second):
		t.Fatal("canceled Cohere stream did not return")
	}
}
