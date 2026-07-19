package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeHandlerMessageOnlyErrorIsExplicitRejection(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"message":"provider rejected"}}`)),
	}

	usage, apiErr := ClaudeHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, service.IsExplicitUpstreamRejection(apiErr))
	assert.False(t, service.IsUpstreamAccepted(c))
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Empty(t, recorder.Body.String())
}

func TestClaudeStreamErrorFirstVersusAfterValidEvent(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	tests := []struct {
		name         string
		body         string
		wantError    bool
		wantExplicit bool
		wantAccept   bool
	}{
		{
			name:         "error first",
			body:         "data: {\"type\":\"error\",\"error\":{\"message\":\"rejected\"}}\n\n",
			wantError:    true,
			wantExplicit: true,
		},
		{
			name:       "error after valid event",
			wantError:  true,
			wantAccept: true,
			body: strings.Join([]string{
				`data: {"type":"message_start","message":{"model":"claude-test"}}`,
				`data: {"type":"error","error":{"message":"failed later"}}`,
				``,
			}, "\n"),
		},
		{
			name:       "malformed event",
			body:       "data: {bad\n\n",
			wantError:  true,
			wantAccept: true,
		},
		{
			name:       "empty successful response",
			body:       "",
			wantError:  true,
			wantAccept: true,
		},
		{
			name:       "truncated after message start",
			body:       "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-test\"}}\n\n",
			wantError:  true,
			wantAccept: true,
		},
		{
			name:       "message stop without message start",
			body:       "data: {\"type\":\"message_stop\"}\n\n",
			wantError:  true,
			wantAccept: true,
		},
		{
			name: "complete stream",
			body: strings.Join([]string{
				`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":1,"output_tokens":0}}}`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				`data: {"type":"message_stop"}`,
				``,
			}, "\n"),
			wantAccept: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatClaude,
				DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body))}

			usage, apiErr := ClaudeStreamHandler(c, resp, info)

			if test.wantError {
				require.Nil(t, usage)
				require.NotNil(t, apiErr)
			} else {
				require.Nil(t, apiErr, "unexpected stream error: %#v", apiErr)
				require.NotNil(t, usage)
			}
			assert.Equal(t, test.wantExplicit, service.IsExplicitUpstreamRejection(apiErr))
			assert.Equal(t, test.wantAccept, service.IsUpstreamAccepted(c))
			if !test.wantAccept {
				assert.Empty(t, recorder.Body.String())
			}
		})
	}
}

func TestClaudeMessageStopFinishesWithoutWaitingForConnectionClose(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader}
	result := make(chan *types.NewAPIError, 1)
	go func() {
		_, apiErr := ClaudeStreamHandler(c, resp, info)
		result <- apiErr
	}()

	_, err := io.WriteString(writer, strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test"}}`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n"))
	require.NoError(t, err)
	select {
	case apiErr := <-result:
		require.Nil(t, apiErr)
		assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	case <-time.After(time.Second):
		t.Fatal("Claude stream waited for EOF after message_stop")
	}
}
