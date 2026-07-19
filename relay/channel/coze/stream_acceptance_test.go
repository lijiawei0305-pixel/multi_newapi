package coze

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cozeTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *cozeTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestCozeStreamAcceptanceStatesAndBodyClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		body         string
		wantError    bool
		wantExplicit bool
		wantAccepted bool
	}{
		{
			name:         "first provider rejection remains retryable",
			body:         "event: error\ndata: {\"code\":4001,\"message\":\"denied\"}\n\n",
			wantError:    true,
			wantExplicit: true,
		},
		{
			name:         "malformed event is acceptance unknown",
			body:         "event: conversation.message.delta\ndata: {bad\n\n",
			wantError:    true,
			wantAccepted: true,
		},
		{
			name:         "rejection after output is terminal accepted",
			body:         "event: conversation.message.delta\ndata: {\"content\":\"hi\"}\n\nevent: error\ndata: {\"code\":4001,\"message\":\"late\"}\n\n",
			wantError:    true,
			wantAccepted: true,
		},
		{
			name:         "completed response succeeds",
			body:         "event: conversation.message.delta\ndata: {\"content\":\"hi\"}\n\nevent: conversation.chat.completed\ndata: {\"usage\":{\"input_count\":1,\"output_count\":1,\"token_count\":2}}\n\n",
			wantAccepted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v3/chat", nil)
			body := &cozeTrackingBody{Reader: strings.NewReader(test.body)}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}
			info := &relaycommon.RelayInfo{
				StartTime:   time.Now(),
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "coze-test"},
			}

			usage, apiErr := cozeChatStreamHandler(c, info, resp)

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
