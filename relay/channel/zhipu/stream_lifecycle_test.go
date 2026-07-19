package zhipu

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

type zhipuTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *zhipuTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

type zhipuCancelBody struct {
	ctx    context.Context
	closed atomic.Bool
}

func (b *zhipuCancelBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *zhipuCancelBody) Close() error {
	b.closed.Store(true)
	return nil
}

func newZhipuStreamContext(body io.ReadCloser) (*gin.Context, *relaycommon.RelayInfo, *http.Response) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/invoke", nil)
	info := &relaycommon.RelayInfo{
		StartTime:   time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "chatglm-test"},
	}
	return c, info, &http.Response{StatusCode: http.StatusOK, Body: body}
}

func TestZhipuStreamAcceptanceStatesAndBodyClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		body         string
		wantError    bool
		wantExplicit bool
		wantAccepted bool
	}{
		{
			name:         "first failed meta remains retryable",
			body:         "meta:{\"task_status\":\"failed\"}\n",
			wantError:    true,
			wantExplicit: true,
		},
		{
			name:         "malformed meta is acceptance unknown",
			body:         "meta:{bad\n",
			wantError:    true,
			wantAccepted: true,
		},
		{
			name:         "completed response succeeds",
			body:         "data:hi\nmeta:{\"request_id\":\"r1\",\"task_status\":\"success\",\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n",
			wantAccepted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &zhipuTrackingBody{Reader: strings.NewReader(test.body)}
			c, info, resp := newZhipuStreamContext(body)

			usage, apiErr := zhipuStreamHandler(c, info, resp)

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

func TestZhipuStreamCancellationReturnsAndClosesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	body := &zhipuCancelBody{ctx: ctx}
	c, info, resp := newZhipuStreamContext(body)
	c.Request = c.Request.WithContext(ctx)
	result := make(chan *types.NewAPIError, 1)
	go func() {
		_, apiErr := zhipuStreamHandler(c, info, resp)
		result <- apiErr
	}()

	cancel()
	select {
	case apiErr := <-result:
		require.NotNil(t, apiErr)
		assert.True(t, service.IsUpstreamAccepted(c))
		assert.True(t, body.closed.Load())
	case <-time.After(time.Second):
		t.Fatal("canceled Zhipu stream did not return")
	}
}
