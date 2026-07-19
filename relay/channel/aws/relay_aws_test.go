package aws

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBedrockRuntime struct {
	invokeModel       func(context.Context, *bedrockruntime.InvokeModelInput) (*bedrockruntime.InvokeModelOutput, error)
	invokeModelStream func(context.Context, *bedrockruntime.InvokeModelWithResponseStreamInput) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error)
}

type fakeAWSResponseStream struct {
	events []bedrockruntimeTypes.ResponseStream
	err    error
}

func (f *fakeAWSResponseStream) Events() <-chan bedrockruntimeTypes.ResponseStream {
	events := make(chan bedrockruntimeTypes.ResponseStream, len(f.events))
	for _, event := range f.events {
		events <- event
	}
	close(events)
	return events
}

func (f *fakeAWSResponseStream) Err() error {
	return f.err
}

func (f *fakeBedrockRuntime) InvokeModel(ctx context.Context, input *bedrockruntime.InvokeModelInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	if f.invokeModel == nil {
		return nil, errors.New("unexpected InvokeModel call")
	}
	return f.invokeModel(ctx, input)
}

func (f *fakeBedrockRuntime) InvokeModelWithResponseStream(ctx context.Context, input *bedrockruntime.InvokeModelWithResponseStreamInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error) {
	if f.invokeModelStream == nil {
		return nil, errors.New("unexpected InvokeModelWithResponseStream call")
	}
	return f.invokeModelStream(ctx, input)
}

func newAWSHandlerTestContext(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		UpstreamModelName: "claude-3-5-sonnet-20240620",
	}}
}

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestAWSHandlerDistinguishesExplicitRejectionFromAcceptedDecodeFailure(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		invokeErr    error
		accepted     bool
		explicit     bool
		wantAPIError bool
	}{
		{name: "invoke failure remains retryable", invokeErr: errors.New("dial failed"), wantAPIError: true},
		{name: "explicit provider rejection remains retryable", body: `{"type":"error","error":{"type":"invalid_request_error","message":"rejected"}}`, explicit: true, wantAPIError: true},
		{name: "malformed accepted response is terminal", body: `{`, accepted: true, wantAPIError: true},
		{name: "successful response is accepted", body: `{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`, accepted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, info := newAWSHandlerTestContext(t)
			client := &fakeBedrockRuntime{invokeModel: func(ctx context.Context, input *bedrockruntime.InvokeModelInput) (*bedrockruntime.InvokeModelOutput, error) {
				require.NotNil(t, input)
				if test.invokeErr != nil {
					return nil, test.invokeErr
				}
				return &bedrockruntime.InvokeModelOutput{Body: []byte(test.body)}, nil
			}}
			adaptor := &Adaptor{AwsClient: client, AwsReq: &bedrockruntime.InvokeModelInput{}}

			apiErr, usage := awsHandler(c, info, adaptor)

			assert.Equal(t, test.accepted, service.IsUpstreamAccepted(c))
			assert.Equal(t, test.wantAPIError, apiErr != nil)
			if apiErr != nil {
				assert.Equal(t, test.explicit, service.IsExplicitUpstreamRejection(apiErr))
			}
			if test.wantAPIError {
				assert.Nil(t, usage)
			} else {
				assert.NotNil(t, usage)
			}
		})
	}
}

func TestAWSHandlersRejectInvalidSDKStateWithoutPanicOrAcceptance(t *testing.T) {
	var nilClient *bedrockruntime.Client
	var nilRequest *bedrockruntime.InvokeModelInput
	var nilStreamRequest *bedrockruntime.InvokeModelWithResponseStreamInput
	tests := []struct {
		name string
		run  func(*gin.Context, *relaycommon.RelayInfo) *types.NewAPIError
	}{
		{
			name: "nil relay metadata",
			run: func(c *gin.Context, _ *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := awsHandler(c, nil, &Adaptor{AwsClient: &fakeBedrockRuntime{}, AwsReq: &bedrockruntime.InvokeModelInput{}})
				return apiErr
			},
		},
		{
			name: "nil client",
			run: func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := awsHandler(c, info, &Adaptor{AwsReq: &bedrockruntime.InvokeModelInput{}})
				return apiErr
			},
		},
		{
			name: "typed nil client",
			run: func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := awsHandler(c, info, &Adaptor{AwsClient: nilClient, AwsReq: &bedrockruntime.InvokeModelInput{}})
				return apiErr
			},
		},
		{
			name: "typed nil nonstream request",
			run: func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := awsHandler(c, info, &Adaptor{AwsClient: &fakeBedrockRuntime{}, AwsReq: nilRequest})
				return apiErr
			},
		},
		{
			name: "typed nil nova request",
			run: func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := handleNovaRequest(c, info, &Adaptor{AwsClient: &fakeBedrockRuntime{}, AwsReq: nilRequest})
				return apiErr
			},
		},
		{
			name: "typed nil stream request",
			run: func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := awsStreamHandler(c, info, &Adaptor{AwsClient: &fakeBedrockRuntime{}, AwsReq: nilStreamRequest})
				return apiErr
			},
		},
		{
			name: "wrong nonstream request type",
			run: func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := awsHandler(c, info, &Adaptor{AwsClient: &fakeBedrockRuntime{}, AwsReq: &bedrockruntime.InvokeModelWithResponseStreamInput{}})
				return apiErr
			},
		},
		{
			name: "wrong stream request type",
			run: func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				apiErr, _ := awsStreamHandler(c, info, &Adaptor{AwsClient: &fakeBedrockRuntime{}, AwsReq: &bedrockruntime.InvokeModelInput{}})
				return apiErr
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, info := newAWSHandlerTestContext(t)
			assert.NotPanics(t, func() {
				assert.NotNil(t, test.run(c, info))
			})
			assert.False(t, service.IsUpstreamAccepted(c))
		})
	}
}

func TestAWSNovaAcceptedMalformedAndEmptyResponsesNeverPanic(t *testing.T) {
	for _, body := range []string{`{`, `{"output":{"message":{"content":[]}},"usage":{}}`} {
		c, info := newAWSHandlerTestContext(t)
		client := &fakeBedrockRuntime{invokeModel: func(context.Context, *bedrockruntime.InvokeModelInput) (*bedrockruntime.InvokeModelOutput, error) {
			return &bedrockruntime.InvokeModelOutput{Body: []byte(body)}, nil
		}}
		adaptor := &Adaptor{AwsClient: client, AwsReq: &bedrockruntime.InvokeModelInput{}}

		assert.NotPanics(t, func() {
			apiErr, usage := handleNovaRequest(c, info, adaptor)
			assert.NotNil(t, apiErr)
			assert.Nil(t, usage)
		})
		assert.True(t, service.IsUpstreamAccepted(c))
	}
}

func TestAWSInvokeUsesDownstreamCancellationAndFiniteNonstreamDeadline(t *testing.T) {
	t.Setenv("RELAY_NON_STREAM_TIMEOUT_SECONDS", "5")
	c, info := newAWSHandlerTestContext(t)
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	cancel()
	client := &fakeBedrockRuntime{invokeModel: func(ctx context.Context, _ *bedrockruntime.InvokeModelInput) (*bedrockruntime.InvokeModelOutput, error) {
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.LessOrEqual(t, time.Until(deadline), 5*time.Second)
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
		return nil, ctx.Err()
	}}

	apiErr, _ := awsHandler(c, info, &Adaptor{AwsClient: client, AwsReq: &bedrockruntime.InvokeModelInput{}})

	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c))
}

func TestAWSStreamSuccessfulInvokeWithMissingOutputIsAcceptedTerminal(t *testing.T) {
	c, info := newAWSHandlerTestContext(t)
	client := &fakeBedrockRuntime{invokeModelStream: func(ctx context.Context, _ *bedrockruntime.InvokeModelWithResponseStreamInput) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error) {
		_, hasDeadline := ctx.Deadline()
		assert.False(t, hasDeadline)
		return nil, nil
	}}

	apiErr, usage := awsStreamHandler(c, info, &Adaptor{AwsClient: client, AwsReq: &bedrockruntime.InvokeModelWithResponseStreamInput{}})

	require.NotNil(t, apiErr)
	assert.Nil(t, usage)
	assert.True(t, service.IsUpstreamAccepted(c))
}

func TestConsumeAWSResponseStreamPreservesThreeStateAcceptance(t *testing.T) {
	validChunk := func() bedrockruntimeTypes.ResponseStream {
		return &bedrockruntimeTypes.ResponseStreamMemberChunk{Value: bedrockruntimeTypes.PayloadPart{
			Bytes: []byte(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`),
		}}
	}
	explicitChunk := &bedrockruntimeTypes.ResponseStreamMemberChunk{Value: bedrockruntimeTypes.PayloadPart{
		Bytes: []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"rejected"}}`),
	}}
	malformedChunk := &bedrockruntimeTypes.ResponseStreamMemberChunk{Value: bedrockruntimeTypes.PayloadPart{Bytes: []byte(`{`)}}
	var nilChunk *bedrockruntimeTypes.ResponseStreamMemberChunk
	validationMessage := "invalid request"
	validationErr := &bedrockruntimeTypes.ValidationException{Message: &validationMessage}
	tests := []struct {
		name         string
		events       []bedrockruntimeTypes.ResponseStream
		streamErr    error
		accepted     bool
		explicit     bool
		wantAPIError bool
	}{
		{name: "explicit first chunk remains retryable", events: []bedrockruntimeTypes.ResponseStream{explicitChunk}, explicit: true, wantAPIError: true},
		{name: "malformed first chunk is accepted unknown", events: []bedrockruntimeTypes.ResponseStream{malformedChunk}, accepted: true, wantAPIError: true},
		{name: "typed nil first chunk is accepted unknown", events: []bedrockruntimeTypes.ResponseStream{nilChunk}, accepted: true, wantAPIError: true},
		{name: "validation stream error before output is explicit", streamErr: validationErr, explicit: true, wantAPIError: true},
		{name: "generic stream error before output is accepted unknown", streamErr: errors.New("connection reset"), accepted: true, wantAPIError: true},
		{name: "empty stream is accepted unknown", accepted: true, wantAPIError: true},
		{name: "valid output succeeds", events: []bedrockruntimeTypes.ResponseStream{validChunk()}, accepted: true},
		{name: "validation after valid output stays accepted", events: []bedrockruntimeTypes.ResponseStream{validChunk()}, streamErr: validationErr, accepted: true, wantAPIError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, info := newAWSHandlerTestContext(t)
			apiErr, usage := consumeAWSResponseStream(c, info, &fakeAWSResponseStream{events: test.events, err: test.streamErr})

			assert.Equal(t, test.accepted, service.IsUpstreamAccepted(c))
			assert.Equal(t, test.wantAPIError, apiErr != nil)
			if apiErr != nil {
				assert.Equal(t, test.explicit, service.IsExplicitUpstreamRejection(apiErr))
			}
			if test.wantAPIError {
				assert.Nil(t, usage)
			} else {
				assert.NotNil(t, usage)
			}
		})
	}
}

func TestAWSNonstreamDeadlineIsLiveAndRespectsEarlierParentDeadline(t *testing.T) {
	t.Setenv("RELAY_NON_STREAM_TIMEOUT_SECONDS", "5")
	previousRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	t.Cleanup(func() { common.RelayTimeout = previousRelayTimeout })

	t.Run("configured total deadline", func(t *testing.T) {
		c, info := newAWSHandlerTestContext(t)
		client := &fakeBedrockRuntime{invokeModel: func(ctx context.Context, _ *bedrockruntime.InvokeModelInput) (*bedrockruntime.InvokeModelOutput, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			remaining := time.Until(deadline)
			assert.Greater(t, remaining, 4*time.Second)
			assert.LessOrEqual(t, remaining, 5*time.Second)
			return nil, errors.New("stop after inspecting deadline")
		}}
		apiErr, _ := awsHandler(c, info, &Adaptor{AwsClient: client, AwsReq: &bedrockruntime.InvokeModelInput{}})
		require.NotNil(t, apiErr)
		assert.False(t, service.IsUpstreamAccepted(c))
	})

	t.Run("earlier parent deadline wins", func(t *testing.T) {
		c, info := newAWSHandlerTestContext(t)
		parent, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		parentDeadline, ok := parent.Deadline()
		require.True(t, ok)
		c.Request = c.Request.WithContext(parent)
		client := &fakeBedrockRuntime{invokeModel: func(ctx context.Context, _ *bedrockruntime.InvokeModelInput) (*bedrockruntime.InvokeModelOutput, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			assert.WithinDuration(t, parentDeadline, deadline, 10*time.Millisecond)
			return nil, errors.New("stop after inspecting deadline")
		}}
		apiErr, _ := awsHandler(c, info, &Adaptor{AwsClient: client, AwsReq: &bedrockruntime.InvokeModelInput{}})
		require.NotNil(t, apiErr)
		assert.False(t, service.IsUpstreamAccepted(c))
	})
}

func TestAWSStreamInvokeUsesDownstreamCancellation(t *testing.T) {
	c, info := newAWSHandlerTestContext(t)
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	cancel()
	client := &fakeBedrockRuntime{invokeModelStream: func(ctx context.Context, _ *bedrockruntime.InvokeModelWithResponseStreamInput) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error) {
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
		return nil, ctx.Err()
	}}

	apiErr, _ := awsStreamHandler(c, info, &Adaptor{AwsClient: client, AwsReq: &bedrockruntime.InvokeModelWithResponseStreamInput{}})

	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c))
}

func TestNovaInferenceConfigPreservesExplicitZerosAndUintRange(t *testing.T) {
	zeroUint := uint(0)
	zeroFloat := 0.0
	zeroInt := 0
	request := &dto.GeneralOpenAIRequest{
		MaxTokens:   &zeroUint,
		Temperature: &zeroFloat,
		TopP:        &zeroFloat,
		TopK:        &zeroInt,
	}
	nova := convertToNovaRequest(request)
	require.NotNil(t, nova.InferenceConfig)
	require.NotNil(t, nova.InferenceConfig.MaxTokens)
	require.NotNil(t, nova.InferenceConfig.Temperature)
	require.NotNil(t, nova.InferenceConfig.TopP)
	require.NotNil(t, nova.InferenceConfig.TopK)
	assert.Zero(t, *nova.InferenceConfig.MaxTokens)
	assert.Zero(t, *nova.InferenceConfig.Temperature)
	assert.Zero(t, *nova.InferenceConfig.TopP)
	assert.Zero(t, *nova.InferenceConfig.TopK)

	payload, err := common.Marshal(nova)
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"maxTokens":0`)
	assert.Contains(t, string(payload), `"temperature":0`)
	assert.Contains(t, string(payload), `"topP":0`)
	assert.Contains(t, string(payload), `"topK":0`)

	maxTokens := ^uint(0)
	nova = convertToNovaRequest(&dto.GeneralOpenAIRequest{MaxTokens: &maxTokens})
	payload, err = common.Marshal(nova)
	require.NoError(t, err)
	var roundTrip NovaRequest
	require.NoError(t, common.Unmarshal(payload, &roundTrip))
	require.NotNil(t, roundTrip.InferenceConfig)
	require.NotNil(t, roundTrip.InferenceConfig.MaxTokens)
	assert.Equal(t, maxTokens, *roundTrip.InferenceConfig.MaxTokens)
}
