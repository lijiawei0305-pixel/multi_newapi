package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type responseAdmissionTaskAdaptor struct {
	providerCalls int
}

func (*responseAdmissionTaskAdaptor) Init(*relaycommon.RelayInfo) {}

func (*responseAdmissionTaskAdaptor) ValidateRequestAndSetAction(*gin.Context, *relaycommon.RelayInfo) *dto.TaskError {
	return nil
}

func (*responseAdmissionTaskAdaptor) EstimateBilling(*gin.Context, *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

func (*responseAdmissionTaskAdaptor) AdjustBillingOnSubmit(*relaycommon.RelayInfo, []byte) map[string]float64 {
	return nil
}

func (*responseAdmissionTaskAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func (*responseAdmissionTaskAdaptor) BuildRequestURL(*relaycommon.RelayInfo) (string, error) {
	return "https://provider.invalid/tasks", nil
}

func (*responseAdmissionTaskAdaptor) BuildRequestHeader(*gin.Context, *http.Request, *relaycommon.RelayInfo) error {
	return nil
}

func (*responseAdmissionTaskAdaptor) BuildRequestBody(*gin.Context, *relaycommon.RelayInfo) (io.Reader, error) {
	return strings.NewReader(`{}`), nil
}

func (a *responseAdmissionTaskAdaptor) DoRequest(*gin.Context, *relaycommon.RelayInfo, io.Reader) (*http.Response, error) {
	a.providerCalls++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

func (*responseAdmissionTaskAdaptor) DoResponse(*gin.Context, *http.Response, *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	return "upstream-task", nil, nil
}

func (*responseAdmissionTaskAdaptor) GetModelList() []string { return []string{"sora-2"} }

func (*responseAdmissionTaskAdaptor) GetChannelName() string { return "response-admission-test" }

func (*responseAdmissionTaskAdaptor) FetchTask(context.Context, string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}

func (*responseAdmissionTaskAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func configureResponseAdmissionTest(t *testing.T) {
	t.Helper()
	t.Setenv("RELAY_RESPONSE_BUFFER_TEMP_DIR", t.TempDir())
	t.Setenv("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", "4")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "0")
}

func TestTaskResponseAdmissionFailureNeverInvokesProvider(t *testing.T) {
	configureResponseAdmissionTest(t)
	database, err := gorm.Open(sqlite.Open("file:task-response-admission?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.TaskSubmissionRecovery{}))
	require.NoError(t, database.Exec("ALTER TABLE users ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 0").Error)
	require.NoError(t, database.Create(&model.User{Id: 1, Username: "response-admission-user"}).Error)
	originalDatabase := model.DB
	model.DB = database
	t.Cleanup(func() { model.DB = originalDatabase })

	held, err := common.AcquireBufferedResponseReservation()
	require.NoError(t, err)
	t.Cleanup(held.Release)

	adaptor := &responseAdmissionTaskAdaptor{}
	originalFactory := getTaskSubmitAdaptor
	getTaskSubmitAdaptor = func(constant.TaskPlatform) channel.TaskAdaptor { return adaptor }
	t.Cleanup(func() { getTaskSubmitAdaptor = originalFactory })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))
	c.Set("platform", "response-admission-test")
	info := &relaycommon.RelayInfo{
		UserId:          1,
		OriginModelName: "sora-2",
		StartTime:       time.Now(),
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		UserSetting:     dto.UserSetting{AcceptUnsetRatioModel: true},
	}

	result, taskErr := RelayTaskSubmit(c, info)

	assert.Nil(t, result)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode, taskErr.Message)
	assert.Equal(t, "response buffer capacity is temporarily unavailable", taskErr.Message)
	assert.NotContains(t, taskErr.Message, "reserved=")
	require.ErrorIs(t, taskErr.Error, common.ErrBufferedResponseCapacity)
	assert.Zero(t, adaptor.providerCalls)
}

func TestAcceptedDeliveryFailureIsSafeTerminalAndReleasesAdmission(t *testing.T) {
	configureResponseAdmissionTest(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	buffered, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	c.Writer = buffered
	service.MarkUpstreamAccepted(c)

	_, err = buffered.WriteString("12345")
	require.NoError(t, err)
	_, err = buffered.WriteString("6789")
	require.ErrorIs(t, err, common.ErrBufferedResponseTooLarge)

	apiErr := acceptedResponseDeliveryError(c)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Contains(t, apiErr.Error(), "do not retry")
	assert.NotContains(t, apiErr.Error(), "limit=8")

	_, err = common.AcquireBufferedResponseReservation()
	require.ErrorIs(t, err, common.ErrBufferedResponseCapacity, "failed delivery keeps admission reserved until lifecycle cleanup")
	buffered.Discard()
	replacement, err := common.AcquireBufferedResponseReservation()
	require.NoError(t, err)
	replacement.Release()
}

func TestAcceptedResponseUsageValidationNeverPanicsOrFabricatesUsage(t *testing.T) {
	var typedNil *dto.Usage
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "nil", value: nil},
		{name: "typed nil", value: typedNil},
		{name: "wrong type", value: dto.Usage{TotalTokens: 99}},
	} {
		t.Run(test.name, func(t *testing.T) {
			configureResponseAdmissionTest(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			buffered, err := common.NewBufferedResponseWriter(c.Writer)
			require.NoError(t, err)
			c.Writer = buffered
			service.MarkUpstreamAccepted(c)

			usage, apiErr := acceptedResponseUsage(c, test.value)

			assert.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
			require.Error(t, buffered.Err())
			buffered.Discard()
		})
	}
}

func TestOptionalHTTPResponseRejectsTypedNilAndWrongTypeWithoutPanic(t *testing.T) {
	var typedNil *http.Response
	for _, value := range []any{nil, typedNil, "not-an-http-response"} {
		response, apiErr := optionalHTTPResponse(value)
		assert.Nil(t, response)
		require.NotNil(t, apiErr)
		assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
		assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
		assert.NotContains(t, apiErr.Error(), "http.Response")
	}
}

func TestResolveSynchronousHTTPResponseRequiresExplicitNilCapability(t *testing.T) {
	awsSDKChat := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAws},
	}
	awsAPIKeyChat := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeAws,
			ChannelOtherSettings: dto.ChannelOtherSettings{AwsKeyType: dto.AwsKeyTypeApiKey},
		},
	}
	awsSDKEmbedding := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeEmbeddings,
		RelayFormat: types.RelayFormatEmbedding,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAws},
	}
	awsSDKClaude := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeUnknown,
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAws},
	}
	xunfeiChat := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeXunfei},
	}
	xunfeiCompletion := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeXunfei},
	}
	volcAudio := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeAudioSpeech,
		RelayFormat: types.RelayFormatOpenAIAudio,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVolcEngine},
	}
	var typedNil *http.Response

	tests := []struct {
		name      string
		value     any
		info      *relaycommon.RelayInfo
		wantError bool
	}{
		{name: "ordinary nil", wantError: true},
		{name: "AWS SDK chat nil", info: awsSDKChat},
		{name: "AWS API key chat nil", info: awsAPIKeyChat, wantError: true},
		{name: "AWS SDK embedding nil", info: awsSDKEmbedding, wantError: true},
		{name: "AWS SDK Claude nil", info: awsSDKClaude},
		{name: "Xunfei chat nil", info: xunfeiChat},
		{name: "Xunfei completion nil", info: xunfeiCompletion, wantError: true},
		{name: "Volc audio nil belongs to audio helper", info: volcAudio, wantError: true},
		{name: "typed nil remains invalid", value: typedNil, info: awsSDKChat, wantError: true},
		{name: "wrong type remains invalid", value: "not a response", info: awsSDKChat, wantError: true},
		{name: "HTTP response", value: &http.Response{StatusCode: http.StatusOK}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, apiErr := resolveSynchronousHTTPResponse(test.value, test.info)
			if test.wantError {
				assert.Nil(t, response)
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				return
			}
			require.Nil(t, apiErr)
			if test.value == nil {
				assert.Nil(t, response)
			} else {
				assert.Same(t, test.value, response)
			}
		})
	}
}

func TestTextHelperRejectsUndeclaredNilBeforeProviderResponseHandling(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeXunfei)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "invalid-auth-that-DoResponse-would-reject")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "spark-test")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeCompletions,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "spark-test",
		RequestURLPath:  "/v1/completions",
		Request: &dto.GeneralOpenAIRequest{
			Model: "spark-test",
			Messages: []dto.Message{
				{Role: "user", Content: "hello"},
			},
		},
	}

	apiErr := TextHelper(c, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
	assert.EqualError(t, apiErr, "upstream returned no HTTP response")
	assert.NotContains(t, apiErr.Error(), "invalid auth", "DoResponse must not run after response validation fails")
	assert.False(t, service.IsUpstreamAccepted(c))
}

func TestStreamUsageRequestedPreservesContainerSemantics(t *testing.T) {
	assert.True(t, streamUsageRequested(nil), "omitting stream_options keeps legacy usage behavior")
	assert.False(t, streamUsageRequested(&dto.StreamOptions{}), "an object with include_usage omitted means false")
	assert.False(t, streamUsageRequested(&dto.StreamOptions{IncludeUsage: common.GetPointer(false)}))
	assert.True(t, streamUsageRequested(&dto.StreamOptions{IncludeUsage: common.GetPointer(true)}))
}

func TestSynchronousUpstreamResultClassification(t *testing.T) {
	tests := []struct {
		name       string
		response   *http.Response
		apiErr     *types.NewAPIError
		wantAccept bool
	}{
		{name: "http success", response: &http.Response{StatusCode: http.StatusOK}, wantAccept: true},
		{name: "http nonstandard success", response: &http.Response{StatusCode: http.StatusCreated}, wantAccept: true},
		{name: "http local decode failure", response: &http.Response{StatusCode: http.StatusOK}, apiErr: types.NewError(errors.New("decode failed"), types.ErrorCodeBadResponseBody), wantAccept: true},
		{name: "http nonstandard success decode failure", response: &http.Response{StatusCode: http.StatusNoContent}, apiErr: types.NewError(errors.New("decode failed"), types.ErrorCodeBadResponseBody), wantAccept: true},
		{name: "http explicit rejection", response: &http.Response{StatusCode: http.StatusOK}, apiErr: service.MarkExplicitUpstreamRejection(types.NewError(errors.New("rejected"), types.ErrorCodeBadResponse)), wantAccept: false},
		{name: "http redirect", response: &http.Response{StatusCode: http.StatusMultipleChoices}, wantAccept: false},
		{name: "sdk success", wantAccept: true},
		{name: "sdk failure", apiErr: types.NewError(errors.New("invoke failed"), types.ErrorCodeDoRequestFailed), wantAccept: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			recordSynchronousUpstreamResult(c, test.response, test.apiErr)
			assert.Equal(t, test.wantAccept, service.IsUpstreamAccepted(c))
		})
	}
}

func TestRealtimeResponseValidationRejectsTypedNilAndWrongTypes(t *testing.T) {
	var typedNilConn *websocket.Conn
	for _, value := range []any{nil, typedNilConn, "wrong"} {
		connection, apiErr := requiredWebSocketConnection(value)
		assert.Nil(t, connection)
		require.NotNil(t, apiErr)
		assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	}

	connection, apiErr := requiredWebSocketConnection(&websocket.Conn{})
	require.Nil(t, apiErr)
	assert.NotNil(t, connection)

	var typedNilUsage *dto.RealtimeUsage
	for _, value := range []any{nil, typedNilUsage, dto.RealtimeUsage{}, "wrong"} {
		usage, usageErr := acceptedRealtimeUsage(value)
		assert.Nil(t, usage)
		require.NotNil(t, usageErr)
		assert.True(t, types.IsSkipRetryError(usageErr))
	}
}
