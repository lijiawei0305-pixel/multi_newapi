package aws

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/auth/bearer"
)

// getAwsErrorStatusCode extracts HTTP status code from AWS SDK error
func getAwsErrorStatusCode(err error) int {
	// Check for HTTP response error which contains status code
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		return httpErr.HTTPStatusCode()
	}
	// Default to 500 if we can't determine the status code
	return http.StatusInternalServerError
}

func bedrockRuntimeAPIAvailable(client bedrockRuntimeAPI) bool {
	if client == nil {
		return false
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func newAwsInvokeContext(c *gin.Context, stream bool) (context.Context, context.CancelFunc, error) {
	if c == nil || c.Request == nil {
		return nil, nil, errors.New("missing downstream request context")
	}
	base := c.Request.Context()
	if stream {
		ctx, cancel := context.WithCancel(base)
		return ctx, cancel, nil
	}
	timeout := channel.NonStreamUpstreamTimeout()
	if common.RelayTimeout > 0 {
		relayTimeout := time.Duration(common.RelayTimeout) * time.Second
		if relayTimeout < timeout {
			timeout = relayTimeout
		}
	}
	ctx, cancel := context.WithTimeout(base, timeout)
	return ctx, cancel, nil
}

func newAwsClient(c *gin.Context, info *relaycommon.RelayInfo) (*bedrockruntime.Client, error) {
	var (
		httpClient *http.Client
		err        error
	)
	if info.ChannelSetting.Proxy != "" {
		httpClient, err = service.NewProxyHttpClient(info.ChannelSetting.Proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	} else {
		httpClient = service.GetHttpClient()
	}

	awsSecret := strings.Split(info.ApiKey, "|")
	var client *bedrockruntime.Client
	switch len(awsSecret) {
	case 2:
		apiKey := awsSecret[0]
		region := awsSecret[1]
		client = bedrockruntime.New(bedrockruntime.Options{
			Region:                  region,
			BearerAuthTokenProvider: bearer.StaticTokenProvider{Token: bearer.Token{Value: apiKey}},
			HTTPClient:              httpClient,
		})
	case 3:
		ak := awsSecret[0]
		sk := awsSecret[1]
		region := awsSecret[2]
		client = bedrockruntime.New(bedrockruntime.Options{
			Region:      region,
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(ak, sk, "")),
			HTTPClient:  httpClient,
		})
	default:
		return nil, errors.New("invalid aws secret key")
	}

	return client, nil
}

func doAwsClientRequest(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor, requestBody io.Reader) (any, error) {
	awsCli, err := newAwsClient(c, info)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	a.AwsClient = awsCli

	// 获取对应的AWS模型ID
	awsModelId := getAwsModelID(info.UpstreamModelName)

	awsRegionPrefix := getAwsRegionPrefix(awsCli.Options().Region)
	canCrossRegion := awsModelCanCrossRegion(awsModelId, awsRegionPrefix)
	if canCrossRegion {
		awsModelId = awsModelCrossRegion(awsModelId, awsRegionPrefix)
	}

	// init empty request.header
	requestHeader := http.Header{}
	a.SetupRequestHeader(c, &requestHeader, info)
	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	for key, value := range headerOverride {
		requestHeader.Set(key, value)
	}

	if isNovaModel(awsModelId) {
		var novaReq *NovaRequest
		err = common.DecodeJson(requestBody, &novaReq)
		if err != nil {
			return nil, types.NewError(errors.Wrap(err, "decode nova request fail"), types.ErrorCodeBadRequestBody)
		}

		// 使用InvokeModel API，但使用Nova格式的请求体
		awsReq := &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(awsModelId),
			Accept:      aws.String("application/json"),
			ContentType: aws.String("application/json"),
		}

		reqBody, err := common.Marshal(novaReq)
		if err != nil {
			return nil, types.NewError(errors.Wrap(err, "marshal nova request"), types.ErrorCodeBadResponseBody)
		}
		awsReq.Body = reqBody
		a.AwsReq = awsReq
		return nil, nil
	} else {
		awsClaudeReq, err := formatRequest(requestBody, requestHeader)
		if err != nil {
			return nil, types.NewError(errors.Wrap(err, "format aws request fail"), types.ErrorCodeBadRequestBody)
		}

		if info.IsStream {
			awsReq := &bedrockruntime.InvokeModelWithResponseStreamInput{
				ModelId:     aws.String(awsModelId),
				Accept:      aws.String("application/json"),
				ContentType: aws.String("application/json"),
			}
			awsReq.Body, err = buildAwsRequestBody(c, info, awsClaudeReq)
			if err != nil {
				return nil, types.NewError(errors.Wrap(err, "marshal aws request fail"), types.ErrorCodeBadRequestBody)
			}
			a.AwsReq = awsReq
			return nil, nil
		} else {
			awsReq := &bedrockruntime.InvokeModelInput{
				ModelId:     aws.String(awsModelId),
				Accept:      aws.String("application/json"),
				ContentType: aws.String("application/json"),
			}
			awsReq.Body, err = buildAwsRequestBody(c, info, awsClaudeReq)
			if err != nil {
				return nil, types.NewError(errors.Wrap(err, "marshal aws request fail"), types.ErrorCodeBadRequestBody)
			}
			a.AwsReq = awsReq
			return nil, nil
		}
	}
}

// buildAwsRequestBody prepares the payload for AWS requests, applying passthrough rules when enabled.
func buildAwsRequestBody(c *gin.Context, info *relaycommon.RelayInfo, awsClaudeReq any) ([]byte, error) {
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, errors.Wrap(err, "get request body for pass-through fail")
		}
		body, err := storage.Bytes()
		if err != nil {
			return nil, errors.Wrap(err, "get request body bytes fail")
		}
		var data map[string]interface{}
		if err := common.Unmarshal(body, &data); err != nil {
			return nil, errors.Wrap(err, "pass-through unmarshal request body fail")
		}
		delete(data, "model")
		delete(data, "stream")
		return common.Marshal(data)
	}
	return common.Marshal(awsClaudeReq)
}

func getAwsRegionPrefix(awsRegionId string) string {
	parts := strings.Split(awsRegionId, "-")
	regionPrefix := ""
	if len(parts) > 0 {
		regionPrefix = parts[0]
	}
	return regionPrefix
}

func awsModelCanCrossRegion(awsModelId, awsRegionPrefix string) bool {
	regionSet, exists := awsModelCanCrossRegionMap[awsModelId]
	return exists && regionSet[awsRegionPrefix]
}

func awsModelCrossRegion(awsModelId, awsRegionPrefix string) string {
	modelPrefix, find := awsRegionCrossModelPrefixMap[awsRegionPrefix]
	if !find {
		return awsModelId
	}
	return modelPrefix + "." + awsModelId
}

func getAwsModelID(requestModel string) string {
	if awsModelIDName, ok := awsModelIDMap[requestModel]; ok {
		return awsModelIDName
	}
	return requestModel
}

func awsHandler(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor) (*types.NewAPIError, *dto.Usage) {
	if info == nil || info.ChannelMeta == nil {
		return types.NewError(errors.New("aws relay metadata is unavailable"), types.ErrorCodeChannelAwsClientError), nil
	}
	if a == nil || !bedrockRuntimeAPIAvailable(a.AwsClient) {
		return types.NewError(errors.New("aws runtime client is unavailable"), types.ErrorCodeChannelAwsClientError), nil
	}
	awsReq, ok := a.AwsReq.(*bedrockruntime.InvokeModelInput)
	if !ok || awsReq == nil {
		return types.NewError(errors.New("aws invoke request is invalid"), types.ErrorCodeBadRequestBody), nil
	}
	ctx, cancel, contextErr := newAwsInvokeContext(c, false)
	if contextErr != nil {
		return types.NewError(contextErr, types.ErrorCodeChannelAwsClientError), nil
	}
	defer cancel()

	awsResp, err := a.AwsClient.InvokeModel(ctx, awsReq)
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModel"), types.ErrorCodeAwsInvokeError, statusCode), nil
	}
	if awsResp == nil {
		service.MarkUpstreamAccepted(c)
		return channel.AcceptedResponseDeliveryError(), nil
	}

	claudeInfo := &claude.ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	// 复制上游 Content-Type 到客户端响应头
	if awsResp.ContentType != nil && *awsResp.ContentType != "" {
		c.Writer.Header().Set("Content-Type", *awsResp.ContentType)
	}

	handlerErr := claude.HandleClaudeResponseData(c, info, claudeInfo, nil, awsResp.Body)
	if handlerErr != nil {
		if !service.IsExplicitUpstreamRejection(handlerErr) {
			service.MarkUpstreamAccepted(c)
		}
		return handlerErr, nil
	}
	service.MarkUpstreamAccepted(c)
	return nil, claudeInfo.Usage
}

type awsResponseStream interface {
	Events() <-chan bedrockruntimeTypes.ResponseStream
	Err() error
}

func explicitAWSStreamRejection(err error) *types.NewAPIError {
	if err == nil {
		return nil
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return nil
	}
	status := 0
	switch apiErr.ErrorCode() {
	case "AccessDeniedException":
		status = http.StatusForbidden
	case "ConflictException":
		status = http.StatusConflict
	case "ModelNotReadyException", "ServiceUnavailableException":
		status = http.StatusServiceUnavailable
	case "ResourceNotFoundException":
		status = http.StatusNotFound
	case "ServiceQuotaExceededException", "ThrottlingException":
		status = http.StatusTooManyRequests
	case "ValidationException":
		status = http.StatusBadRequest
	default:
		return nil
	}
	common.SysError(fmt.Sprintf("aws event stream rejected request: error_type=%T code=%s", err, apiErr.ErrorCode()))
	return service.MarkExplicitUpstreamRejection(types.NewOpenAIError(
		errors.New("AWS Bedrock rejected the streaming request"),
		types.ErrorCodeAwsInvokeError,
		status,
	))
}

func acceptedAWSStreamFailure(c *gin.Context, err error) *types.NewAPIError {
	service.MarkUpstreamAccepted(c)
	common.SysError(fmt.Sprintf("aws event stream failed after dispatch: error_type=%T", err))
	return channel.AcceptedResponseDeliveryError()
}

func consumeAWSResponseStream(c *gin.Context, info *relaycommon.RelayInfo, stream awsResponseStream) (*types.NewAPIError, *dto.Usage) {
	claudeInfo := &claude.ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	for event := range stream.Events() {
		switch value := event.(type) {
		case *bedrockruntimeTypes.ResponseStreamMemberChunk:
			if value == nil {
				return acceptedAWSStreamFailure(c, errors.New("aws event stream returned a nil chunk")), nil
			}
			info.SetFirstResponseTime()
			responseErr := claude.HandleStreamResponseData(c, info, claudeInfo, string(value.Value.Bytes))
			if responseErr == nil {
				continue
			}
			if service.IsUpstreamAccepted(c) {
				return acceptedAWSStreamFailure(c, responseErr), nil
			}
			if service.IsExplicitUpstreamRejection(responseErr) {
				return responseErr, nil
			}
			return acceptedAWSStreamFailure(c, responseErr), nil
		case *bedrockruntimeTypes.UnknownUnionMember:
			return acceptedAWSStreamFailure(c, errors.New("aws event stream returned an unknown event")), nil
		default:
			return acceptedAWSStreamFailure(c, errors.New("aws event stream returned an invalid event")), nil
		}
	}

	if streamErr := stream.Err(); streamErr != nil {
		if service.IsUpstreamAccepted(c) {
			return acceptedAWSStreamFailure(c, streamErr), nil
		}
		if rejection := explicitAWSStreamRejection(streamErr); rejection != nil {
			return rejection, nil
		}
		return acceptedAWSStreamFailure(c, streamErr), nil
	}
	if !service.IsUpstreamAccepted(c) {
		return acceptedAWSStreamFailure(c, errors.New("aws event stream ended without a response")), nil
	}

	claude.HandleStreamFinalResponse(c, info, claudeInfo)
	return nil, claudeInfo.Usage
}

func awsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor) (*types.NewAPIError, *dto.Usage) {
	if info == nil || info.ChannelMeta == nil {
		return types.NewError(errors.New("aws relay metadata is unavailable"), types.ErrorCodeChannelAwsClientError), nil
	}
	if a == nil || !bedrockRuntimeAPIAvailable(a.AwsClient) {
		return types.NewError(errors.New("aws runtime client is unavailable"), types.ErrorCodeChannelAwsClientError), nil
	}
	awsReq, ok := a.AwsReq.(*bedrockruntime.InvokeModelWithResponseStreamInput)
	if !ok || awsReq == nil {
		return types.NewError(errors.New("aws streaming invoke request is invalid"), types.ErrorCodeBadRequestBody), nil
	}
	ctx, cancel, contextErr := newAwsInvokeContext(c, true)
	if contextErr != nil {
		return types.NewError(contextErr, types.ErrorCodeChannelAwsClientError), nil
	}
	defer cancel()

	awsResp, err := a.AwsClient.InvokeModelWithResponseStream(ctx, awsReq)
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModelWithResponseStream"), types.ErrorCodeAwsInvokeError, statusCode), nil
	}
	if awsResp == nil {
		return acceptedAWSStreamFailure(c, errors.New("aws streaming invoke returned no response")), nil
	}
	stream := awsResp.GetStream()
	if stream == nil {
		return acceptedAWSStreamFailure(c, errors.New("aws streaming invoke returned no event stream")), nil
	}
	defer stream.Close()
	responseErr, usage := consumeAWSResponseStream(c, info, stream)
	if responseErr != nil {
		return responseErr, usage
	}
	if closeErr := stream.Close(); closeErr != nil {
		return acceptedAWSStreamFailure(c, closeErr), nil
	}
	return nil, usage
}

// Nova模型处理函数
func handleNovaRequest(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor) (*types.NewAPIError, *dto.Usage) {
	if info == nil || info.ChannelMeta == nil {
		return types.NewError(errors.New("aws relay metadata is unavailable"), types.ErrorCodeChannelAwsClientError), nil
	}
	if a == nil || !bedrockRuntimeAPIAvailable(a.AwsClient) {
		return types.NewError(errors.New("aws runtime client is unavailable"), types.ErrorCodeChannelAwsClientError), nil
	}
	awsReq, ok := a.AwsReq.(*bedrockruntime.InvokeModelInput)
	if !ok || awsReq == nil {
		return types.NewError(errors.New("aws nova invoke request is invalid"), types.ErrorCodeBadRequestBody), nil
	}
	ctx, cancel, contextErr := newAwsInvokeContext(c, false)
	if contextErr != nil {
		return types.NewError(contextErr, types.ErrorCodeChannelAwsClientError), nil
	}
	defer cancel()

	awsResp, err := a.AwsClient.InvokeModel(ctx, awsReq)
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModel"), types.ErrorCodeAwsInvokeError, statusCode), nil
	}
	service.MarkUpstreamAccepted(c)
	if awsResp == nil {
		return channel.AcceptedResponseDeliveryError(), nil
	}

	// 解析Nova响应
	var novaResp struct {
		Output struct {
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
			TotalTokens  int `json:"totalTokens"`
		} `json:"usage"`
	}

	if err := common.Unmarshal(awsResp.Body, &novaResp); err != nil {
		return types.NewError(errors.Wrap(err, "unmarshal nova response"), types.ErrorCodeBadResponseBody), nil
	}
	if len(novaResp.Output.Message.Content) == 0 {
		return channel.AcceptedResponseDeliveryError(), nil
	}

	// 构造OpenAI格式响应
	response := dto.OpenAITextResponse{
		Id:      helper.GetResponseID(c),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Model:   info.UpstreamModelName,
		Choices: []dto.OpenAITextResponseChoice{{
			Index: 0,
			Message: dto.Message{
				Role:    "assistant",
				Content: novaResp.Output.Message.Content[0].Text,
			},
			FinishReason: "stop",
		}},
		Usage: dto.Usage{
			PromptTokens:     novaResp.Usage.InputTokens,
			CompletionTokens: novaResp.Usage.OutputTokens,
			TotalTokens:      novaResp.Usage.TotalTokens,
		},
	}

	c.JSON(http.StatusOK, response)
	return nil, &response.Usage
}
