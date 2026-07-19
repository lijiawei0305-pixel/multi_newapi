package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type realtimeHandlerResult struct {
	apiErr   *types.NewAPIError
	accepted bool
}

func exerciseRealtimeHandler(t *testing.T, upstreamMessage string) ([]byte, error, realtimeHandlerResult) {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade upstream websocket: %v", err)
			return
		}
		defer conn.Close()
		if err := conn.WriteMessage(websocket.TextMessage, []byte(upstreamMessage)); err != nil {
			t.Errorf("write upstream websocket message: %v", err)
			return
		}
		if err := conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(time.Second),
		); err != nil {
			t.Errorf("close upstream websocket: %v", err)
		}
	}))
	defer upstreamServer.Close()

	targetURL := "ws" + strings.TrimPrefix(upstreamServer.URL, "http")
	targetConn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	require.NoError(t, err)

	resultCh := make(chan realtimeHandlerResult, 1)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		clientConn, upgradeErr := upgrader.Upgrade(c.Writer, c.Request, nil)
		if upgradeErr != nil {
			t.Errorf("upgrade downstream websocket: %v", upgradeErr)
			return
		}
		apiErr, _ := OpenaiRealtimeHandler(c, &relaycommon.RelayInfo{
			ClientWs:          clientConn,
			TargetWs:          targetConn,
			InputAudioFormat:  "pcm16",
			OutputAudioFormat: "pcm16",
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: "gpt-realtime",
			},
		})
		resultCh <- realtimeHandlerResult{apiErr: apiErr, accepted: service.IsUpstreamAccepted(c)}
	})
	downstreamServer := httptest.NewServer(router)
	defer downstreamServer.Close()

	downstreamURL := "ws" + strings.TrimPrefix(downstreamServer.URL, "http")
	downstreamConn, _, err := websocket.DefaultDialer.Dial(downstreamURL, nil)
	require.NoError(t, err)
	defer downstreamConn.Close()

	_, forwarded, readErr := downstreamConn.ReadMessage()
	select {
	case result := <-resultCh:
		return forwarded, readErr, result
	case <-time.After(3 * time.Second):
		t.Fatal("realtime handler did not stop after the upstream closed")
		return nil, nil, realtimeHandlerResult{}
	}
}

func TestOpenaiRealtimeHandlerJoinsReadersAndMarksAccepted(t *testing.T) {
	message := `{"type":"session.created","session":{"input_audio_format":"pcm16","output_audio_format":"pcm16"}}`
	forwarded, readErr, result := exerciseRealtimeHandler(t, message)

	require.NoError(t, readErr)
	assert.JSONEq(t, message, string(forwarded))
	assert.Nil(t, result.apiErr)
	assert.True(t, result.accepted)
}

func TestOpenaiRealtimeHandlerRejectsMalformedAcceptedMessage(t *testing.T) {
	forwarded, readErr, result := exerciseRealtimeHandler(t, `{"type":`)

	assert.Error(t, readErr)
	assert.Empty(t, forwarded)
	require.NotNil(t, result.apiErr)
	assert.True(t, types.IsSkipRetryError(result.apiErr))
	assert.True(t, result.accepted)
}
