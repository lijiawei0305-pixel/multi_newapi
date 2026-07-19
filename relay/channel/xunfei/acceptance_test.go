package xunfei

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func newXunfeiTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func TestResolveXunfeiEventAcceptanceStates(t *testing.T) {
	t.Run("first explicit rejection remains unaccepted", func(t *testing.T) {
		c := newXunfeiTestContext()
		response, apiErr := resolveXunfeiEvent(c, xunfeiEvent{
			err:               errors.New("provider rejected"),
			explicitRejection: true,
		}, false)
		require.Nil(t, response)
		require.NotNil(t, apiErr)
		require.True(t, service.IsExplicitUpstreamRejection(apiErr))
		require.False(t, service.IsUpstreamAccepted(c))
	})

	t.Run("post-send ambiguity is accepted", func(t *testing.T) {
		c := newXunfeiTestContext()
		response, apiErr := resolveXunfeiEvent(c, xunfeiEvent{err: errors.New("read failed")}, false)
		require.Nil(t, response)
		require.NotNil(t, apiErr)
		require.False(t, service.IsExplicitUpstreamRejection(apiErr))
		require.True(t, service.IsUpstreamAccepted(c))
		require.NotContains(t, apiErr.Error(), "read failed")
	})

	t.Run("first valid event is accepted before output", func(t *testing.T) {
		c := newXunfeiTestContext()
		expected := &XunfeiChatResponse{}
		response, apiErr := resolveXunfeiEvent(c, xunfeiEvent{response: expected}, false)
		require.Nil(t, apiErr)
		require.Same(t, expected, response)
		require.True(t, service.IsUpstreamAccepted(c))
	})

	t.Run("rejection after valid output stays accepted", func(t *testing.T) {
		c := newXunfeiTestContext()
		service.MarkUpstreamAccepted(c)
		response, apiErr := resolveXunfeiEvent(c, xunfeiEvent{
			err:               errors.New("late rejection"),
			explicitRejection: true,
		}, true)
		require.Nil(t, response)
		require.NotNil(t, apiErr)
		require.False(t, service.IsExplicitUpstreamRejection(apiErr))
		require.True(t, service.IsUpstreamAccepted(c))
	})
}

func TestAdaptorDoRequestDoesNotSynthesizeHTTPAcceptance(t *testing.T) {
	response, err := (&Adaptor{}).DoRequest(newXunfeiTestContext(), nil, nil)
	require.NoError(t, err)
	require.Nil(t, response)
}

func TestXunfeiWriteAttemptFailureIsAcceptedButDialFailureIsNot(t *testing.T) {
	t.Run("write attempted", func(t *testing.T) {
		c := newXunfeiTestContext()
		apiErr := xunfeiStartError(c, true, errors.New("partial write containing secret"))
		require.NotNil(t, apiErr)
		require.True(t, service.IsUpstreamAccepted(c))
		require.NotContains(t, apiErr.Error(), "secret")
	})

	t.Run("dial failed", func(t *testing.T) {
		c := newXunfeiTestContext()
		apiErr := xunfeiStartError(c, false, errors.New("Xunfei websocket dial failed"))
		require.NotNil(t, apiErr)
		require.False(t, service.IsUpstreamAccepted(c))
	})
}

func TestXunfeiWebSocketEventClassification(t *testing.T) {
	tests := []struct {
		name              string
		serve             func(*websocket.Conn)
		wantError         bool
		wantExplicit      bool
		wantValidResponse bool
	}{
		{
			name: "error first",
			serve: func(conn *websocket.Conn) {
				response := XunfeiChatResponse{}
				response.Header.Code = 10013
				response.Header.Message = "invalid authorization secret"
				payload, _ := common.Marshal(response)
				_ = conn.WriteMessage(websocket.TextMessage, payload)
			},
			wantError:    true,
			wantExplicit: true,
		},
		{
			name: "connection closes after send",
			serve: func(conn *websocket.Conn) {
				_ = conn.Close()
			},
			wantError: true,
		},
		{
			name: "valid terminal event",
			serve: func(conn *websocket.Conn) {
				response := XunfeiChatResponse{}
				response.Header.Code = 0
				response.Header.Sid = "sid"
				response.Payload.Choices.Status = 2
				response.Payload.Choices.Text = []XunfeiChatResponseTextItem{{Content: "ok"}}
				payload, _ := common.Marshal(response)
				_ = conn.WriteMessage(websocket.TextMessage, payload)
			},
			wantValidResponse: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_, _, err = conn.ReadMessage()
				if err != nil {
					return
				}
				test.serve(conn)
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			request := dto.GeneralOpenAIRequest{
				Model:    "SparkDesk-v1.1",
				Messages: []dto.Message{{Role: "user", Content: "hello"}},
			}
			events, sent, err := xunfeiMakeRequest(ctx, request, "lite", "ws"+strings.TrimPrefix(server.URL, "http"), "app-id")
			require.NoError(t, err)
			require.True(t, sent)

			select {
			case event, ok := <-events:
				require.True(t, ok)
				require.Equal(t, test.wantError, event.err != nil)
				require.Equal(t, test.wantExplicit, event.explicitRejection)
				require.Equal(t, test.wantValidResponse, event.response != nil)
			case <-ctx.Done():
				t.Fatal("timed out waiting for Xunfei event")
			}
		})
	}
}
