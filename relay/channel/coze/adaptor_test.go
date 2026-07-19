package coze

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertCozeChatRequestMarshalsDefaultAndExplicitUser(t *testing.T) {
	for _, test := range []struct {
		name     string
		request  string
		wantUser string
	}{
		{
			name:     "default user",
			request:  `{"model":"coze-test","messages":[{"role":"user","content":"hello"}]}`,
			wantUser: "chatcmpl-request-1",
		},
		{
			name:     "explicit user",
			request:  `{"model":"coze-test","user":"customer-42","messages":[{"role":"user","content":"hello"}]}`,
			wantUser: "customer-42",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set(common.RequestIdKey, "request-1")
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(test.request), &request))

			converted, err := convertCozeChatRequest(c, request)
			require.NoError(t, err)
			encoded, err := common.Marshal(converted)
			require.NoError(t, err)

			user := gjson.GetBytes(encoded, "user_id")
			require.True(t, user.Exists())
			assert.Equal(t, test.wantUser, user.String())
			assert.Equal(t, gjson.String, user.Type)
		})
	}
}

func TestCozeDoRequestKeepsDetailBodyReadableAfterPolling(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	service.InitHttpClient()
	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = originalFetchSetting })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/chat":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"id":"chat-1","conversation_id":"conversation-1"}}`)
		case "/v3/chat/retrieve":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"status":"completed","usage":{"token_count":3,"input_count":1,"output_count":2}}}`)
		case "/v3/chat/message/list":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(30 * time.Millisecond)
			_, _ = fmt.Fprint(w, `{"code":0,"data":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: server.URL,
		ApiKey:         "test-key",
	}}

	result, err := (&Adaptor{}).DoRequest(c, info, nil)
	require.NoError(t, err)
	response, ok := result.(*http.Response)
	require.True(t, ok)
	require.NotNil(t, response)
	defer service.CloseResponseBodyGracefully(response)
	body, err := common.ReadAllWithLimit(response.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"code":0,"data":[]}`, string(body))
	assert.True(t, service.IsUpstreamAccepted(c))
}

func TestWaitForCozeChatIsBoundedAndCancellationAware(t *testing.T) {
	t.Run("completes", func(t *testing.T) {
		calls := 0
		err := waitForCozeChat(context.Background(), time.Millisecond, func(context.Context) (bool, error) {
			calls++
			return calls == 3, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 3, calls)
	})

	t.Run("caller cancellation interrupts long interval", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		started := time.Now()
		err := waitForCozeChat(ctx, time.Hour, func(context.Context) (bool, error) {
			cancel()
			return false, nil
		})
		require.ErrorIs(t, err, context.Canceled)
		assert.Less(t, time.Since(started), time.Second)
	})

	t.Run("deadline interrupts long interval", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		started := time.Now()
		err := waitForCozeChat(ctx, time.Hour, func(context.Context) (bool, error) {
			return false, nil
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(started), time.Second)
	})

	t.Run("poll error stops immediately", func(t *testing.T) {
		pollErr := errors.New("poll failed")
		err := waitForCozeChat(context.Background(), time.Hour, func(context.Context) (bool, error) {
			return false, pollErr
		})
		require.ErrorIs(t, err, pollErr)
	})
}

func TestCozeCreateResponseAcceptanceClassification(t *testing.T) {
	for _, test := range []struct {
		name          string
		createBody    string
		pollError     error
		wantAccepted  bool
		wantExplicit  bool
		wantErrorText string
	}{
		{
			name:          "malformed first response is conservatively accepted",
			createBody:    `{`,
			wantAccepted:  true,
			wantErrorText: "unexpected end",
		},
		{
			name:          "parsed code rejection stays unaccepted",
			createBody:    `{"code":4001,"msg":"invalid bot"}`,
			wantExplicit:  true,
			wantErrorText: "invalid bot",
		},
		{
			name:          "polling failure after successful creation stays accepted",
			createBody:    `{"code":0,"data":{"id":"chat-1","conversation_id":"conversation-1"}}`,
			pollError:     errors.New("chat status: failed"),
			wantAccepted:  true,
			wantErrorText: "chat status: failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			response, err := parseCozeCreateResponse(c, []byte(test.createBody))
			if err == nil && test.pollError != nil {
				err = test.pollError
			}

			if test.pollError == nil {
				assert.Nil(t, response)
			} else {
				require.NotNil(t, response)
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErrorText)
			assert.Equal(t, test.wantAccepted, service.IsUpstreamAccepted(c))
			assert.Equal(t, test.wantExplicit, service.IsExplicitUpstreamRejection(err))
		})
	}
}
