package baidu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type baiduTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *baiduTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestBaiduStreamAcceptanceStatesAndBodyClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	tests := []struct {
		name         string
		body         string
		wantError    bool
		wantExplicit bool
		wantAccepted bool
	}{
		{
			name:         "first provider rejection remains retryable",
			body:         "data: {\"error_code\":17,\"error_msg\":\"denied\"}\n\n",
			wantError:    true,
			wantExplicit: true,
		},
		{
			name:         "malformed event is acceptance unknown",
			body:         "data: {bad\n\n",
			wantError:    true,
			wantAccepted: true,
		},
		{
			name:         "rejection after output is terminal accepted",
			body:         "data: {\"result\":\"hi\",\"is_end\":false}\n\ndata: {\"error_code\":17,\"error_msg\":\"late\"}\n\n",
			wantError:    true,
			wantAccepted: true,
		},
		{
			name:         "completed response succeeds",
			body:         "data: {\"result\":\"hi\",\"is_end\":true,\"usage\":{\"prompt_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n",
			wantAccepted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			body := &baiduTrackingBody{Reader: strings.NewReader(test.body)}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}
			info := &relaycommon.RelayInfo{
				StartTime:   time.Now(),
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "ernie-test"},
			}

			apiErr, usage := baiduStreamHandler(c, info, resp)

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
