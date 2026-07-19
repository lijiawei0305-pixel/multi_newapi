package volcengine

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolcengineTTSStructured200ErrorRemainsUnacceptedRejection(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "1024")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
		"code":3001,"message":"voice rejected","data":""
	}`))}

	usage, apiErr := handleTTSResponse(c, resp, &relaycommon.RelayInfo{}, "mp3")

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c))
	assert.False(t, types.IsSkipRetryError(apiErr))
}

func TestVolcengineFirstAudioFrameMarksAcceptedBeforeLaterFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	require.NoError(t, writeAcceptedVolcengineAudio(c, []byte("first-audio-frame")))

	assert.True(t, service.IsUpstreamAccepted(c), "later websocket errors must not retry or refund generated audio")
	assert.Equal(t, "first-audio-frame", recorder.Body.String())
}

func TestVolcenginePostSendDisconnectIsTerminalUnknown(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	apiErr := acceptedVolcengineWebSocketFailure(c, errors.New("private network disconnect"))

	require.NotNil(t, apiErr)
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.NotContains(t, apiErr.Error(), "private network disconnect")
}

func TestUsesNativeTTSWebSocketRequiresExactServerCapability(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		relayMode   int
		baseURL     string
		want        bool
	}{
		{name: "default Volc base", channelType: constant.ChannelTypeVolcEngine, relayMode: relayconstant.RelayModeAudioSpeech, want: true},
		{name: "explicit Volc base", channelType: constant.ChannelTypeVolcEngine, relayMode: relayconstant.RelayModeAudioSpeech, baseURL: constant.ChannelBaseURLs[constant.ChannelTypeVolcEngine], want: true},
		{name: "trailing slash Volc base", channelType: constant.ChannelTypeVolcEngine, relayMode: relayconstant.RelayModeAudioSpeech, baseURL: constant.ChannelBaseURLs[constant.ChannelTypeVolcEngine] + "/", want: true},
		{name: "custom HTTP base", channelType: constant.ChannelTypeVolcEngine, relayMode: relayconstant.RelayModeAudioSpeech, baseURL: "https://proxy.example", want: false},
		{name: "wrong channel", channelType: constant.ChannelTypeMiniMax, relayMode: relayconstant.RelayModeAudioSpeech, want: false},
		{name: "wrong relay mode", channelType: constant.ChannelTypeVolcEngine, relayMode: relayconstant.RelayModeImagesGenerations, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayMode: test.relayMode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    test.channelType,
					ChannelBaseUrl: test.baseURL,
				},
			}
			assert.Equal(t, test.want, UsesNativeTTSWebSocket(info))
		})
	}
	assert.False(t, UsesNativeTTSWebSocket(nil))
}

func TestConvertAudioRequestDoesNotOverrideClientStreamMode(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		IsStream:  false,
		RelayMode: relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeVolcEngine,
			ChannelBaseUrl: "https://proxy.example",
			ApiKey:         "app-id|access-token",
		},
	}

	requestBody, err := (&Adaptor{}).ConvertAudioRequest(c, info, dto.AudioRequest{
		Input:          "hello",
		Voice:          "alloy",
		ResponseFormat: "mp3",
	})

	require.NoError(t, err)
	require.NotNil(t, requestBody)
	assert.False(t, info.IsStream)
}

func TestNativeVolcNormalCloseBeforeAudioIsTerminalUnknown(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	requestReceived := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		requestReceived <- struct{}{}
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "complete without audio"),
			time.Now().Add(time.Second),
		)
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeVolcEngine,
			ApiKey:      "app-id|access-token",
		},
	}
	requestURL := "ws" + strings.TrimPrefix(server.URL, "http")

	usage, apiErr := handleTTSWebSocketResponse(c, requestURL, VolcengineTTSRequest{}, info, "mp3")

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Empty(t, recorder.Body.Bytes())
	select {
	case <-requestReceived:
	default:
		t.Fatal("native websocket request was not sent")
	}
}

func TestNativeVolcEmptyFinalAudioFrameIsTerminalUnknown(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		message, err := NewMessage(MsgTypeAudioOnlyServer, MsgTypeFlagNegativeSeq)
		if err != nil {
			return
		}
		message.Sequence = -1
		frame, err := message.Marshal()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.BinaryMessage, frame)
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeVolcEngine,
			ApiKey:      "app-id|access-token",
		},
	}

	usage, apiErr := handleTTSWebSocketResponse(
		c,
		"ws"+strings.TrimPrefix(server.URL, "http"),
		VolcengineTTSRequest{},
		info,
		"mp3",
	)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Empty(t, recorder.Body.Bytes())
}

type trackingVolcengineBody struct {
	io.Reader
	closed bool
}

func (b *trackingVolcengineBody) Close() error {
	b.closed = true
	return nil
}

func TestCustomVolcAudioUsesAndClosesSingleHTTPResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(contextKeyResponseFormat, "mp3")
	encoded := base64.StdEncoding.EncodeToString([]byte("audio-bytes"))
	body := &trackingVolcengineBody{Reader: strings.NewReader(`{"code":3000,"message":"ok","data":"` + encoded + `"}`)}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeVolcEngine,
			ChannelBaseUrl: "https://proxy.example",
			ApiKey:         "app-id|access-token",
		},
	}

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.Nil(t, apiErr)
	require.IsType(t, &dto.Usage{}, usage)
	assert.True(t, body.closed)
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Equal(t, "audio-bytes", recorder.Body.String())
}

func newVolcengineWebSocketTestClient(t *testing.T, serve func(*websocket.Conn)) *websocket.Conn {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		serve(conn)
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestVolcengineWebSocketRejectsOversizedFrameBeforeAllocation(t *testing.T) {
	t.Setenv("RELAY_VOLCENGINE_TTS_WS_MAX_FRAME_BYTES", "32")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "64")
	conn := newVolcengineWebSocketTestClient(t, func(serverConn *websocket.Conn) {
		_ = serverConn.WriteMessage(websocket.BinaryMessage, []byte(strings.Repeat("x", 64)))
	})
	refresh := prepareVolcengineTTSWebSocket(conn)
	require.NoError(t, refresh())

	_, err := ReceiveMessage(conn)

	require.ErrorIs(t, err, websocket.ErrReadLimit)
}

func TestVolcengineWebSocketContextCancellationClosesBlockedRead(t *testing.T) {
	serverDone := make(chan struct{})
	conn := newVolcengineWebSocketTestClient(t, func(*websocket.Conn) { <-serverDone })
	ctx, cancel := context.WithCancel(context.Background())
	stop := closeVolcengineTTSWebSocketOnCancel(ctx, conn)
	t.Cleanup(stop)
	readDone := make(chan error, 1)
	go func() {
		_, _, err := conn.ReadMessage()
		readDone <- err
	}()
	cancel()

	select {
	case err := <-readDone:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not release the blocked websocket read")
	}
	close(serverDone)
}
